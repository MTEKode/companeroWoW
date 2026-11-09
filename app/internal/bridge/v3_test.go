package bridge

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"math/rand"
	"testing"
)

// renderV3 paints the blocks of a v3 frame into a width x StripHeightV3 image at block pitch
// p, the way a scaled client would (see renderStrip in protocol_test.go): a pixel straddling a
// block boundary takes the coverage-weighted blend, distort is applied to every channel
// value, then each channel gets uniform noise in [-noise, noise]. Background is random.
func renderV3(blocks [][3]byte, p float64, width int, rng *rand.Rand, distort func(byte) byte, noise int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, StripHeightV3))
	for y := 0; y < StripHeightV3; y++ {
		for x := 0; x < width; x++ {
			var bg [3]float64
			for c := 0; c < 3; c++ {
				bg[c] = float64(rng.Intn(256))
			}
			cy := math.Max(0, math.Min(1, math.Min(float64(y+1), p)-float64(y)))
			var acc [3]float64
			covered := 0.0
			// Only the blocks that touch column x can overlap it.
			first := int(math.Floor(float64(x) / p))
			for i := first; i <= first+2 && i < len(blocks); i++ {
				lo := math.Max(float64(x), float64(i)*p)
				hi := math.Min(float64(x+1), float64(i+1)*p)
				if hi <= lo {
					continue
				}
				cx := hi - lo
				covered += cx
				for c := 0; c < 3; c++ {
					acc[c] += cx * float64(blocks[i][c])
				}
			}
			var px [3]uint8
			for c := 0; c < 3; c++ {
				v := cy*(acc[c]+(1-covered)*bg[c]) + (1-cy)*bg[c]
				n := int(math.Round(v))
				if noise > 0 {
					n += rng.Intn(2*noise+1) - noise
				}
				px[c] = distort(uint8(clampInt(n, 0, 255)))
			}
			img.SetRGBA(x, y, color.RGBA{px[0], px[1], px[2], 0xFF})
		}
	}
	return img
}

// v3Status is the status the frames carry in these tests.
func v3Status() LiveState {
	return LiveState{MapID: 1429, X: 0.45, Y: 0.67, Level: 60, XPPct: 42, Money: 123456,
		HealthPct: 88, PowerPct: 100, InCombat: true, HasTarget: true, LastEventCode: 3, EventSeq: 9, EventArg: 77}
}

// v3Frame builds a frame with a chunk of the given data and n blocks, and returns its blocks.
func v3Frame(t testing.TB, seq uint8, n int, data []byte) ([]byte, [][3]byte) {
	t.Helper()
	frame, err := BuildFrameV3(v3Status(), seq, n, &ChunkV3{Type: MsgQuests, ID: 513, Index: 2, Count: 5, Data: data})
	if err != nil {
		t.Fatalf("BuildFrameV3: %v", err)
	}
	return frame, BlocksV3(frame, n)
}

func TestCRC16CheckValue(t *testing.T) {
	if got := CRC16([]byte("123456789")); got != 0x29B1 {
		t.Fatalf("CRC16(\"123456789\") = %#04x, want 0x29B1", got)
	}
	if got := CRC16(nil); got != 0xFFFF {
		t.Fatalf("CRC16(empty) = %#04x, want 0xFFFF", got)
	}
}

func TestCapacityV3(t *testing.T) {
	if got := CapacityV3(860); got != 1235 {
		t.Fatalf("CapacityV3(860) = %d, want 1235", got)
	}
	if got := CapacityV3(5); got != 0 {
		t.Fatalf("CapacityV3(5) = %d, want 0", got)
	}
}

func TestBuildParseFrameV3Roundtrip(t *testing.T) {
	data := []byte(`{"quests":{"1":{"title":"Hola"}}}`)
	frame, err := BuildFrameV3(v3Status(), 200, 860, &ChunkV3{Type: MsgQuests, ID: 0x1234, Index: 2, Count: 5, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) != 47+len(data) {
		t.Fatalf("frame length %d, want %d", len(frame), 47+len(data))
	}
	if frame[0] != ProtocolV3 || frame[1] != 200 || frame[2] != 860&0xFF || frame[3] != 860>>8 {
		t.Fatalf("header = % x", frame[:4])
	}
	f, err := ParseFrameV3(frame)
	if err != nil {
		t.Fatal(err)
	}
	if f.N != 860 || f.Seq != 200 || f.MsgType != MsgQuests || f.MsgID != 0x1234 || f.ChunkIndex != 2 || f.ChunkCount != 5 {
		t.Fatalf("parsed header %+v", f)
	}
	if !bytes.Equal(f.Data, data) {
		t.Fatalf("data %q, want %q", f.Data, data)
	}
	assertSameState(t, v3Status(), f.Status)
	frame[len(frame)-3] ^= 0x01
	if _, err := ParseFrameV3(frame); err == nil {
		t.Fatal("a corrupted data byte passed the CRC-16")
	}
}

func TestIdleFrameV3HasNoChunk(t *testing.T) {
	frame, err := BuildFrameV3(v3Status(), 1, 300, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFrameV3(frame)
	if err != nil {
		t.Fatal(err)
	}
	if f.MsgType != 0 || f.Data != nil || len(frame) != 47 {
		t.Fatalf("idle frame: type %d, data %v, length %d", f.MsgType, f.Data, len(frame))
	}
}

func TestBuildFrameV3RejectsOversizeChunk(t *testing.T) {
	big := make([]byte, CapacityV3(860)+1)
	if _, err := BuildFrameV3(v3Status(), 1, 860, &ChunkV3{Type: 2, Data: big}); err == nil {
		t.Fatal("chunk over the capacity was accepted")
	}
	if _, err := BuildFrameV3(v3Status(), 1, 1025, nil); err == nil {
		t.Fatal("more than 1024 blocks was accepted")
	}
}

// TestDecodeV3RoundtripAtPitches renders a frame at pitches 4, 5.33 and 7.5 with colour
// shift, noise and blending, and checks the decoded frame and status.
func TestDecodeV3RoundtripAtPitches(t *testing.T) {
	data := bytes.Repeat([]byte("roundtrip-v3 "), 40) // 520 bytes
	const n = 400 // capacity 545 bytes
	_, blocks := v3Frame(t, 42, n, data)
	for _, p := range []float64{4, 5.33, 7.5} {
		for seed := int64(1); seed <= 2; seed++ {
			width := int(math.Ceil(float64(n)*p)) + 6
			img := renderV3(blocks, p, width, rand.New(rand.NewSource(seed)), observedDistortion, 3)
			got, gp, err := DecodeFrameV3(img, 0)
			if err != nil {
				t.Fatalf("pitch %.2f seed %d: %v", p, seed, err)
			}
			if math.Abs(gp-p) > 0.02 {
				t.Errorf("pitch %.2f: decoded at %.3f", p, gp)
			}
			if got.N != n || got.Seq != 42 || got.MsgType != MsgQuests || got.MsgID != 513 || got.ChunkIndex != 2 {
				t.Errorf("pitch %.2f: header %+v", p, got)
			}
			if !bytes.Equal(got.Data, data) {
				t.Errorf("pitch %.2f: data mismatch", p)
			}
			assertSameState(t, v3Status(), got.Status)
		}
	}
}

func TestDecodeV3WithPitchHint(t *testing.T) {
	data := []byte("hint")
	const n = 120
	_, blocks := v3Frame(t, 3, n, data)
	img := renderV3(blocks, 5.33, int(math.Ceil(n*5.33))+6, rand.New(rand.NewSource(9)), identity, 0)
	_, p1, err := DecodeFrameV3(img, 0)
	if err != nil {
		t.Fatal(err)
	}
	f, p2, err := DecodeFrameV3(img, p1)
	if err != nil || math.Abs(p2-p1) > 0.001 || !bytes.Equal(f.Data, data) {
		t.Fatalf("hint decode: err %v pitch %.4f/%.4f", err, p1, p2)
	}
}

// TestDecodeV3NeverWrongOnDamagedFrame changes one data nibble: the CRC-16 must reject it.
func TestDecodeV3NeverWrongOnDamagedFrame(t *testing.T) {
	data := []byte("damage test payload that spans a few blocks")
	const n = 100
	_, blocks := v3Frame(t, 5, n, data)
	blocks[40][1] = blocks[40][1]/17*17 + 17 // one nibble off by one
	img := renderV3(blocks, 4, n*4+4, rand.New(rand.NewSource(3)), identity, 0)
	if f, _, err := DecodeFrameV3(img, 0); err == nil && bytes.Equal(f.Data, data) {
		t.Fatal("damaged frame decoded as the original")
	}
}

// TestDecodeAnyReadsV2AndV3 checks that the same reader decodes both protocols.
func TestDecodeAnyReadsV2AndV3(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	v2 := renderStrip(EncodeForTest(sampleStates()[0], 9), 5.33, rng, identity, 0)
	dec, err := DecodeAny(v2, 0)
	if err != nil || dec.Version != ProtocolVersion || dec.Frame != nil {
		t.Fatalf("v2 via DecodeAny: version %d, frame %v, err %v", dec.Version, dec.Frame, err)
	}
	assertSameState(t, sampleStates()[0], dec.Live)

	const n = 150
	_, blocks := v3Frame(t, 7, n, []byte("v3"))
	img := renderV3(blocks, 4, n*4+4, rng, identity, 0)
	dec, err = DecodeAny(img, 0)
	if err != nil || dec.Version != ProtocolV3 || dec.Frame == nil {
		t.Fatalf("v3 via DecodeAny: version %d, frame %v, err %v", dec.Version, dec.Frame, err)
	}
}

func TestDecodeV3GarbageDoesNotDecode(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	img := image.NewRGBA(image.Rect(0, 0, 1280, StripHeightV3))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Intn(256))
	}
	if _, _, err := DecodeFrameV3(img, 0); err == nil {
		t.Fatal("random noise decoded as a v3 frame")
	}
	if _, _, err := DecodeFrameV3(image.NewRGBA(image.Rect(0, 0, 4, 4)), 0); err == nil {
		t.Fatal("a tiny image decoded")
	}
}

// TestBlocksV3MatchesV2Calibration checks that the fixed blocks of v3 are the v2 ones.
func TestBlocksV3MatchesV2Calibration(t *testing.T) {
	blocks := BlocksV3(nil, 10)
	for i := 0; i < calibrationBlocks; i++ {
		if blocks[i] != calExpected[i] {
			t.Errorf("block %d = %v, want %v", i, blocks[i], calExpected[i])
		}
	}
}
