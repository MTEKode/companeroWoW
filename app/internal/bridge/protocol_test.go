package bridge

import (
	"bufio"
	"errors"
	"image"
	"image/color"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestCRC8Vector(t *testing.T) {
	if got := CRC8([]byte("123456789")); got != 0xF4 {
		t.Fatalf("CRC8(\"123456789\") = %#02x, want 0xF4", got)
	}
	if got := CRC8(nil); got != 0 {
		t.Fatalf("CRC8(empty) = %#02x, want 0", got)
	}
}

func sampleStates() []LiveState {
	return []LiveState{
		{},
		{
			MapID: 85, X: 0.5123, Y: 0.3871, Level: 38, XPPct: 42,
			Money: 1234567, TrackedQuestID: 90001, TargetNpcID: 3296, TargetLevel: 40,
			HealthPct: 87, PowerPct: 60, HasTarget: true, TargetHostile: true,
			LastEventCode: 2, EventSeq: 7, EventArg: 90001, FreeBagSlots: 9,
		},
		{
			// Every flag set, and the 255 sentinels for unavailable values.
			MapID: 65535, X: 1, Y: 1, Level: 255, XPPct: 100,
			Money: math.MaxUint32, TrackedQuestID: math.MaxUint32, TargetNpcID: math.MaxUint32,
			TargetLevel: 255, HealthPct: 255, PowerPct: 255,
			InCombat: true, Dead: true, Resting: true, Mounted: true, InGroup: true,
			TargetHostile: true, HasTarget: true, InInstance: true,
			LastEventCode: 7, EventSeq: 255, EventArg: math.MaxUint32, FreeBagSlots: 255,
		},
		{
			MapID: 1, X: 0.25, Y: 0.75, Level: 1, Money: 1,
			Mounted: true, InInstance: true, EventSeq: 1, EventArg: 1,
		},
	}
}

func assertSameState(t *testing.T, want, got LiveState) {
	t.Helper()
	const coordTol = 1.0 / 65535
	if math.Abs(want.X-got.X) > coordTol || math.Abs(want.Y-got.Y) > coordTol {
		t.Errorf("coords: got (%v,%v) want (%v,%v)", got.X, got.Y, want.X, want.Y)
	}
	want.X, want.Y = got.X, got.Y
	want.Seq, want.ReadAt = got.Seq, got.ReadAt
	if want != got {
		t.Errorf("state mismatch:\n got  %+v\n want %+v", got, want)
	}
}

// renderStrip paints blocks into a StripWidth x StripHeight image at block pitch p,
// the way a scaled client would: pixels fully inside a block take its colour, and a
// pixel straddling a boundary takes the coverage-weighted blend. Pixels outside the
// strip are random background. distort is applied to every channel value, then
// each channel gets uniform noise in [-noise, noise].
func renderStrip(blocks [NumBlocks][3]byte, p float64, rng *rand.Rand, distort func(byte) byte, noise int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, StripWidth, StripHeight))
	for y := 0; y < StripHeight; y++ {
		for x := 0; x < StripWidth; x++ {
			var bg [3]float64
			for c := 0; c < 3; c++ {
				bg[c] = float64(rng.Intn(256))
			}
			// Vertical coverage of [y, y+1) by the strip rows [0, p).
			cy := math.Min(float64(y+1), p) - float64(y)
			cy = math.Max(0, math.Min(1, cy))
			var acc [3]float64
			covered := 0.0
			for i := 0; i < NumBlocks; i++ {
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
			img.SetRGBA(x, y, color.RGBA{px[0], px[1], px[2], 255})
		}
	}
	return img
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func identity(v uint8) uint8 { return v }

func offset(d int) func(uint8) uint8 {
	return func(v uint8) uint8 { return uint8(clampInt(int(v)+d, 0, 255)) }
}

func gamma(g float64) func(uint8) uint8 {
	return func(v uint8) uint8 {
		return uint8(math.Round(255 * math.Pow(float64(v)/255, g)))
	}
}

func TestEncodeDecodeRoundtrip(t *testing.T) {
	for i, s := range sampleStates() {
		seq := uint8(i*37 + 5)
		blocks := EncodeForTest(s, seq)
		got, err := DecodeBlocks(blocks)
		if err != nil {
			t.Fatalf("state %d: DecodeBlocks: %v", i, err)
		}
		if got.Seq != seq {
			t.Errorf("state %d: seq = %d, want %d", i, got.Seq, seq)
		}
		assertSameState(t, s, got)
	}
}

func TestEncodeFixedBlocks(t *testing.T) {
	blocks := EncodeForTest(sampleStates()[1], 7)
	want := [5][3]byte{{255, 0, 255}, {0, 255, 255}, {85, 170, 0}, {170, 85, 85}, {0, 255, 170}}
	for i := range want {
		if blocks[i] != want[i] {
			t.Errorf("block %d = %v, want %v", i, blocks[i], want[i])
		}
	}
	// Header nibbles of version 2, seq 7, length 32: bytes 2,7,32 -> block 5 = (0,2,0), block 6 = (7,2,0).
	if blocks[5] != [3]byte{0, 34, 0} || blocks[6] != [3]byte{119, 34, 0} {
		t.Errorf("header blocks = %v %v", blocks[5], blocks[6])
	}
	for i, b := range blocks {
		for c := 0; c < 3; c++ {
			if b[c]%nibbleStep != 0 {
				t.Errorf("block %d channel %d = %d, not a multiple of 17", i, c, b[c])
			}
		}
	}
}

func TestEncodeWrapsSeqAt255(t *testing.T) {
	blocks := EncodeForTest(LiveState{}, 255)
	got, err := DecodeBlocks(blocks)
	if err != nil || got.Seq != 255 {
		t.Fatalf("seq 255: got %d err %v", got.Seq, err)
	}
}

func TestDecodeBlocksCorruptCRC(t *testing.T) {
	blocks := EncodeForTest(sampleStates()[1], 3)
	blocks[10][1] ^= 0x10 // a data block; the calibration blocks stay intact
	_, err := DecodeBlocks(blocks)
	if !errors.Is(err, ErrBadCRC) {
		t.Fatalf("err = %v, want ErrBadCRC", err)
	}
}

func TestDecodeBlocksBadVersionAndLength(t *testing.T) {
	blocks := EncodeForTest(sampleStates()[1], 3)
	blocks[5][1] = 1 * nibbleStep // version byte 0x02 -> 0x01
	if _, err := DecodeBlocks(blocks); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("version: err = %v, want ErrBadVersion", err)
	}
	blocks = EncodeForTest(sampleStates()[1], 3)
	blocks[6][1] = 0 // length byte high nibble 2 -> 0 : length 0x00
	if _, err := DecodeBlocks(blocks); !errors.Is(err, ErrBadLength) {
		t.Fatalf("length: err = %v, want ErrBadLength", err)
	}
}

func TestDecodeBlocksNonMonotonicCalibration(t *testing.T) {
	blocks := EncodeForTest(sampleStates()[1], 3)
	// Green: block 3 (expected 85) observed brighter than block 2 (expected 170).
	blocks[3][1] = 200
	blocks[2][1] = 10
	if _, err := DecodeBlocks(blocks); !errors.Is(err, ErrBadCalibration) {
		t.Fatalf("err = %v, want ErrBadCalibration", err)
	}
}

func TestDecodeBlocksAppliesCalibration(t *testing.T) {
	s := sampleStates()[1]
	blocks := EncodeForTest(s, 11)
	// Shift every block by +2 (calibration blocks included): the decoder must undo it.
	for i := range blocks {
		for c := 0; c < 3; c++ {
			blocks[i][c] = byte(clampInt(int(blocks[i][c])+2, 0, 255))
		}
	}
	got, err := DecodeBlocks(blocks)
	if err != nil {
		t.Fatalf("DecodeBlocks with +2 shift: %v", err)
	}
	assertSameState(t, s, got)
}

// v1Blocks lays out the old 14-block protocol (version 1) at the start of the strip.
func v1Blocks(seq uint8) [NumBlocks][3]byte {
	var blocks [NumBlocks][3]byte
	blocks[0] = magic0
	blocks[1] = magic1
	blocks[2] = [3]byte{1, seq, 32}
	payload := encodePayload(sampleStates()[1])
	var raw [33]byte
	copy(raw[:], payload[:])
	k := 0
	for b := 3; b < 14; b++ {
		for c := 0; c < 3; c++ {
			blocks[b][c] = raw[k]
			k++
		}
	}
	return blocks
}

func TestV1StripNeverDecodes(t *testing.T) {
	for _, seq := range []uint8{0, 3, 200} {
		img := renderStrip(v1Blocks(seq), 4, rand.New(rand.NewSource(int64(seq))), identity, 0)
		st, err := DecodeImage(img)
		if err == nil {
			t.Fatalf("seq %d: v1 strip decoded as %+v, want an error", seq, st)
		}
		if !errors.Is(err, ErrNoMagic) && !errors.Is(err, ErrBadVersion) && !errors.Is(err, ErrBadCalibration) &&
			!errors.Is(err, ErrBadCRC) && !errors.Is(err, ErrBadLength) {
			t.Fatalf("seq %d: unexpected error kind: %v", seq, err)
		}
		t.Logf("seq %d: v1 strip rejected with: %v", seq, err)
	}
}

func TestDecodeImageSyntheticAtPitches(t *testing.T) {
	pitches := []float64{2.0, 2.64, 3.0, 4.0, 5.33}
	distortions := []struct {
		name  string
		fn    func(uint8) uint8
		noise int
	}{
		{"exact", identity, 0},
		{"offset +1", offset(1), 0},
		{"offset +2", offset(2), 0},
		{"gamma 0.9", gamma(0.9), 0},
		{"gamma 1.1", gamma(1.1), 0},
		{"noise +-3", identity, 3},
	}
	for pi, p := range pitches {
		for di, d := range distortions {
			for si, s := range sampleStates() {
				seq := uint8(40 + si)
				rng := rand.New(rand.NewSource(int64(pi*100 + di*10 + si)))
				img := renderStrip(EncodeForTest(s, seq), p, rng, d.fn, d.noise)
				got, err := DecodeImage(img)
				if err != nil {
					t.Errorf("pitch %.2f, %s, state %d: %v", p, d.name, si, err)
					continue
				}
				if got.Seq != seq {
					t.Errorf("pitch %.2f, %s, state %d: seq %d, want %d", p, d.name, si, got.Seq, seq)
				}
				assertSameState(t, s, got)
			}
		}
	}
}

func TestDecodeImageMagicTolerance(t *testing.T) {
	s := sampleStates()[1]
	blocks := EncodeForTest(s, 9)
	img := renderStrip(blocks, 4, rand.New(rand.NewSource(1)), identity, 0)
	if _, err := DecodeImage(img); err != nil {
		t.Fatalf("clean strip: %v", err)
	}
	// Destroy the first magic block: the image no longer shows block 0 at the left edge.
	blocks[0] = [3]byte{250, 200, 250}
	img = renderStrip(blocks, 4, rand.New(rand.NewSource(2)), identity, 0)
	if _, err := DecodeImage(img); !errors.Is(err, ErrNoMagic) {
		t.Fatalf("damaged magic: err = %v, want ErrNoMagic", err)
	}
}

func TestDecodeImageNoStrip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, StripWidth, StripHeight))
	rng := rand.New(rand.NewSource(7))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Intn(256))
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	if _, err := DecodeImage(img); !errors.Is(err, ErrNoMagic) {
		t.Fatalf("pure noise: err = %v, want ErrNoMagic", err)
	}
	if _, err := DecodeImage(image.NewRGBA(image.Rect(0, 0, 10, 1))); !errors.Is(err, ErrBadImage) {
		t.Fatalf("too small image: err = %v, want ErrBadImage", err)
	}
	if _, err := DecodeImage(nil); !errors.Is(err, ErrBadImage) {
		t.Fatalf("nil image: err = %v, want ErrBadImage", err)
	}
}

// fixtureState is the state encoded in testdata/strip_v2_blocks.txt. The file is
// generated by the Lua ns.EncodeBlocks(ns.BuildPayload(state), 42) with state
// mapID=85 x=0.5 y=0.25 level=38 xpPct=42 money=1234567 trackedQuestID=90001
// targetNpcID=3296 targetLevel=40 healthPct=87 powerPct=60 hasTarget,targetHostile
// lastEventCode=2 eventSeq=7 eventArg=90001 freeBagSlots=9.
func fixtureState() LiveState {
	return LiveState{
		MapID: 85, X: 0.5, Y: 0.25, Level: 38, XPPct: 42,
		Money: 1234567, TrackedQuestID: 90001, TargetNpcID: 3296, TargetLevel: 40,
		HealthPct: 87, PowerPct: 60, HasTarget: true, TargetHostile: true,
		LastEventCode: 2, EventSeq: 7, EventArg: 90001, FreeBagSlots: 9,
	}
}

func readFixture(t *testing.T) [NumBlocks][3]byte {
	t.Helper()
	f, err := os.Open("../../testdata/strip_v2_blocks.txt")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	var blocks [NumBlocks][3]byte
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || n >= NumBlocks {
			t.Fatalf("fixture line %d: %q", n+1, line)
		}
		for c, f := range fields {
			v, err := strconv.Atoi(f)
			if err != nil || v < 0 || v > 255 {
				t.Fatalf("fixture block %d channel %d: %q", n, c, f)
			}
			blocks[n][c] = byte(v)
		}
		n++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n != NumBlocks {
		t.Fatalf("fixture has %d blocks, want %d", n, NumBlocks)
	}
	return blocks
}

func TestLuaFixtureDecodes(t *testing.T) {
	blocks := readFixture(t)
	got, err := DecodeBlocks(blocks)
	if err != nil {
		t.Fatalf("DecodeBlocks(fixture): %v", err)
	}
	if got.Seq != 42 {
		t.Errorf("seq = %d, want 42", got.Seq)
	}
	assertSameState(t, fixtureState(), got)

	// The same blocks rendered as a screen at pitch 4 must decode too.
	img := renderStrip(blocks, 4, rand.New(rand.NewSource(5)), identity, 0)
	fromImg, err := DecodeImage(img)
	if err != nil {
		t.Fatalf("DecodeImage(fixture): %v", err)
	}
	assertSameState(t, fixtureState(), fromImg)
}

func TestDecodersNeverPanicOnGarbage(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		var blocks [NumBlocks][3]byte
		for b := range blocks {
			for c := 0; c < 3; c++ {
				blocks[b][c] = byte(rng.Intn(256))
			}
		}
		if i%2 == 0 {
			blocks[0], blocks[1] = magic0, magic1
		}
		_, _ = DecodeBlocks(blocks)
		img := renderStrip(blocks, 4, rng, identity, 0)
		_, _ = DecodeImage(img)
	}
}
