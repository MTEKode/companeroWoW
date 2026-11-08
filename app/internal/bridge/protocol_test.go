package bridge

import (
	"errors"
	"image"
	"image/color"
	"math"
	"math/rand"
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

func TestEncodeWrapsSeqAt255(t *testing.T) {
	blocks := EncodeForTest(LiveState{}, 255)
	got, err := DecodeBlocks(blocks)
	if err != nil || got.Seq != 255 {
		t.Fatalf("seq 255: got %d err %v", got.Seq, err)
	}
}

func TestDecodeBlocksCorruptCRC(t *testing.T) {
	blocks := EncodeForTest(sampleStates()[1], 3)
	blocks[5][1] ^= 0x10
	_, err := DecodeBlocks(blocks)
	if !errors.Is(err, ErrBadCRC) {
		t.Fatalf("err = %v, want ErrBadCRC", err)
	}
}

func TestDecodeBlocksNoMagic(t *testing.T) {
	blocks := EncodeForTest(sampleStates()[1], 3)
	blocks[0] = [3]byte{0, 0, 0}
	if _, err := DecodeBlocks(blocks); !errors.Is(err, ErrNoMagic) {
		t.Fatalf("err = %v, want ErrNoMagic", err)
	}
}

func TestDecodeBlocksBadVersionAndLength(t *testing.T) {
	blocks := EncodeForTest(sampleStates()[1], 3)
	blocks[2][0] = 2
	if _, err := DecodeBlocks(blocks); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("version: err = %v, want ErrBadVersion", err)
	}
	blocks = EncodeForTest(sampleStates()[1], 3)
	blocks[2][2] = 31
	if _, err := DecodeBlocks(blocks); !errors.Is(err, ErrBadLength) {
		t.Fatalf("length: err = %v, want ErrBadLength", err)
	}
}

// paintStrip draws blocks as 2x2 squares at the top-left of img.
func paintStrip(img *image.RGBA, blocks [NumBlocks][3]byte) {
	for i, c := range blocks {
		col := color.RGBA{c[0], c[1], c[2], 255}
		for dy := 0; dy < 2; dy++ {
			for dx := 0; dx < 2; dx++ {
				img.SetRGBA(2*i+dx, dy, col)
			}
		}
	}
}

func noisyImage(w, h int, seed int64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(seed))
	rng.Read(img.Pix)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	return img
}

func TestDecodeImageSyntheticStrip(t *testing.T) {
	for i, s := range sampleStates() {
		img := noisyImage(120, 80, int64(i+1))
		// Noise everywhere except the strip itself, including the rows right below it.
		blocks := EncodeForTest(s, uint8(40+i))
		paintStrip(img, blocks)
		got, err := DecodeImage(img)
		if err != nil {
			t.Fatalf("state %d: DecodeImage: %v", i, err)
		}
		if got.Seq != uint8(40+i) {
			t.Errorf("state %d: seq %d", i, got.Seq)
		}
		assertSameState(t, s, got)
	}
}

func TestDecodeImageMagicTolerance(t *testing.T) {
	s := sampleStates()[1]
	blocks := EncodeForTest(s, 9)
	blocks[0] = [3]byte{253, 2, 253} // magic0 nudged by 2 per channel
	img := noisyImage(64, 16, 99)
	paintStrip(img, blocks)
	if _, err := DecodeImage(img); err != nil {
		t.Fatalf("magic within ±2 should be accepted: %v", err)
	}
	blocks[0] = [3]byte{250, 0, 255}
	paintStrip(img, blocks)
	if _, err := DecodeImage(img); !errors.Is(err, ErrNoMagic) {
		t.Fatalf("magic off by more than 2: err = %v, want ErrNoMagic", err)
	}
}

func TestDecodeImageNoStrip(t *testing.T) {
	img := noisyImage(100, 50, 7)
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

func TestDecodeImageNotUniformBlock(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, StripWidth, StripHeight))
	paintStrip(img, EncodeForTest(sampleStates()[1], 1))
	// Partially cover block 6 with a different colour.
	img.SetRGBA(13, 1, color.RGBA{1, 2, 3, 255})
	if _, err := DecodeImage(img); !errors.Is(err, ErrBadImage) {
		t.Fatalf("err = %v, want ErrBadImage", err)
	}
}

func TestDecodersNeverPanicOnGarbage(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
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
		img := image.NewRGBA(image.Rect(0, 0, StripWidth, StripHeight))
		paintStrip(img, blocks)
		_, _ = DecodeImage(img)
	}
}
