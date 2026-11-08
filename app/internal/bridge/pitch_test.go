package bridge

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand"
	"os"
	"testing"
	"time"
)

// soften applies a [1 2 1]/4 filter along each row of the strip, so every colour edge
// spreads over about two pixels, as the scaled live client shows it.
func soften(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			l, r := x-1, x+1
			if l < b.Min.X {
				l = b.Min.X
			}
			if r >= b.Max.X {
				r = b.Max.X - 1
			}
			var px [3]uint8
			for c := 0; c < 3; c++ {
				v := 2*int(src.Pix[src.PixOffset(x, y)+c]) +
					int(src.Pix[src.PixOffset(l, y)+c]) + int(src.Pix[src.PixOffset(r, y)+c])
				px[c] = uint8((v + 2) / 4)
			}
			dst.SetRGBA(x, y, color.RGBA{px[0], px[1], px[2], 255})
		}
	}
	return dst
}

// TestDecodeLiveStrip7p5Fixture decodes the strip captured from the live client after
// the scale fix moved blocks to about 7.5 px (testdata/strip_v2_live_7p5.png, 464x16 at
// the top-left of the screen). It must decode with a valid CRC.
func TestDecodeLiveStrip7p5Fixture(t *testing.T) {
	f, err := os.Open("../../testdata/strip_v2_live_7p5.png")
	if err != nil {
		t.Fatalf("open 7.5 px fixture: %v", err)
	}
	defer f.Close()
	decoded, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode 7.5 px fixture png: %v", err)
	}
	img := image.NewRGBA(decoded.Bounds())
	draw.Draw(img, img.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	got, err := DecodeImage(img)
	if err != nil {
		t.Fatalf("DecodeImage(7.5 px fixture): %v", err)
	}
	t.Logf("decoded 7.5 px fixture: %+v", got)
	checkLive7p5Fixture(t, got)
}

// TestDecodeImageLargePitchesBlendedAndShifted renders the strip at pitches from 6 to
// 12 px with exact edge blending, the colour shift seen live, and optionally noise and a
// soft edge. Every state must decode exactly.
func TestDecodeImageLargePitchesBlendedAndShifted(t *testing.T) {
	pitches := []float64{6.0, 7.0, 7.5, 7.55, 8.0, 10.0, 12.0}
	for pi, p := range pitches {
		for si, s := range sampleStates() {
			for _, noise := range []int{0, 3} {
				for _, soft := range []bool{false, true} {
					seq := uint8(60 + si)
					rng := rand.New(rand.NewSource(int64(pi*1000 + si*100 + noise*10)))
					img := renderStrip(EncodeForTest(s, seq), p, rng, observedDistortion, noise)
					if soft {
						img = soften(img)
					}
					got, err := DecodeImage(img)
					if err != nil {
						t.Errorf("pitch %.2f, state %d, noise %d, soft %v: %v", p, si, noise, soft, err)
						continue
					}
					if got.Seq != seq {
						t.Errorf("pitch %.2f, state %d, noise %d, soft %v: seq %d, want %d", p, si, noise, soft, got.Seq, seq)
					}
					assertSameState(t, s, got)
				}
			}
		}
	}
}

// TestDecodeImageFitGateRejectsNeighbours checks that a damaged strip, whose nibble
// one-off decodes wrong at a neighbouring pitch, is never returned as a wrong state.
func TestDecodeImageFitGateRejectsNeighbours(t *testing.T) {
	s := sampleStates()[1]
	for _, p := range []float64{5.30, 7.5} {
		blocks := EncodeForTest(s, 9)
		blocks[12][1] ^= 0x70
		img := renderStrip(blocks, p, rand.New(rand.NewSource(4)), observedDistortion, 0)
		if got, err := DecodeImage(img); err == nil {
			assertSameState(t, s, got)
		}
	}
}

// TestDecodeImageSpeed bounds the cost of one decode, which runs every poll.
func TestDecodeImageSpeed(t *testing.T) {
	img := renderStrip(EncodeForTest(sampleStates()[1], 9), 7.5, rand.New(rand.NewSource(1)), observedDistortion, 3)
	noise := renderStrip(EncodeForTest(sampleStates()[1], 9), 7.5, rand.New(rand.NewSource(2)), identity, 0)
	noise2 := image.NewRGBA(image.Rect(0, 0, StripWidth, StripHeight))
	rng := rand.New(rand.NewSource(3))
	for i := range noise2.Pix {
		noise2.Pix[i] = byte(rng.Intn(256))
	}
	for name, im := range map[string]image.Image{"strip": img, "clean": noise, "random": noise2} {
		start := time.Now()
		_, _ = DecodeImage(im)
		if d := time.Since(start); d > 100*time.Millisecond {
			t.Errorf("%s: DecodeImage took %v, want under 100ms", name, d)
		} else {
			t.Logf("%s: DecodeImage took %v", name, d)
		}
	}
}
