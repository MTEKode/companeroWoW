package bridge

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"math/rand"
	"os"
	"testing"
)

// colourTransform is what the client does to the colours of the strip, pixel by pixel.
type colourTransform func(rgb [3]float64) [3]float64

// gainTransform scales every channel by g (brightness).
func gainTransform(g float64) colourTransform {
	return func(c [3]float64) [3]float64 {
		for i := range c {
			c[i] = clampF(c[i] * g)
		}
		return c
	}
}

// gammaTransform applies out = 255 * (in/255)^gamma per channel.
func gammaTransform(gamma float64) colourTransform {
	return func(c [3]float64) [3]float64 {
		for i := range c {
			c[i] = clampF(255 * math.Pow(c[i]/255, gamma))
		}
		return c
	}
}

// vibranceTransform pushes every channel away from the luma (saturation boost).
func vibranceTransform(s float64) colourTransform {
	return func(c [3]float64) [3]float64 {
		y := 0.299*c[0] + 0.587*c[1] + 0.114*c[2]
		for i := range c {
			c[i] = clampF(y + (c[i]-y)*s)
		}
		return c
	}
}

// chain applies the transforms in order.
func chain(ts ...colourTransform) colourTransform {
	return func(c [3]float64) [3]float64 {
		for _, t := range ts {
			c = t(c)
		}
		return c
	}
}

func clampF(v float64) float64 { return math.Max(0, math.Min(255, v)) }

// renderV31 paints blocks at pitch p the way a scaled client would (coverage-weighted blend),
// applies the client transform to each pixel, then adds uniform noise in [-noise, noise].
func renderV31(blocks [][3]byte, p float64, width int, rng *rand.Rand, tr colourTransform, noise int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, StripHeightV3))
	for y := 0; y < StripHeightV3; y++ {
		for x := 0; x < width; x++ {
			var bg [3]float64
			for c := range bg {
				bg[c] = float64(rng.Intn(256))
			}
			cy := math.Max(0, math.Min(1, math.Min(float64(y+1), p)-float64(y)))
			var acc [3]float64
			covered := 0.0
			first := int(math.Floor(float64(x) / p))
			for i := first; i <= first+2 && i < len(blocks); i++ {
				lo := math.Max(float64(x), float64(i)*p)
				hi := math.Min(float64(x+1), float64(i+1)*p)
				if hi <= lo {
					continue
				}
				covered += hi - lo
				for c := 0; c < 3; c++ {
					acc[c] += (hi - lo) * float64(blocks[i][c])
				}
			}
			var v [3]float64
			for c := 0; c < 3; c++ {
				v[c] = cy*(acc[c]+(1-covered)*bg[c]) + (1-cy)*bg[c]
			}
			v = tr(v)
			var px [3]uint8
			for c := 0; c < 3; c++ {
				n := int(math.Round(v[c]))
				if noise > 0 {
					n += rng.Intn(2*noise+1) - noise
				}
				px[c] = uint8(clampInt(n, 0, 255))
			}
			img.SetRGBA(x, y, color.RGBA{px[0], px[1], px[2], 0xFF})
		}
	}
	return img
}

// v31Case is one synthetic client: a transform and the noise on top. A limit case is a client
// the protocol cannot always read: it may fail to decode, but it must never decode wrong data
// (see v31Cases).
type v31Case struct {
	name  string
	tr    colourTransform
	noise int
	limit bool
}

// v31Cases are the client settings the decoder must read exactly, and the limit cases. A
// vibrance boost clamps the dark channels at 0, and a clamped 0 is the same colour for level 0
// and for level 7 (B = 0 for (66,73,0) and (66,73,7) under x1.2 vibrance): no decoder can tell
// them apart. A gamma of 1.2 squeezes the lowest levels into 3 or 4 codes, and with +-3 noise
// the lowest nibble misreads about once in 450 nibbles, which the CRC rejects.
func v31Cases() []v31Case {
	return []v31Case{
		{"gain 1.0", gainTransform(1.0), 0, false},
		{"gain 0.6", gainTransform(0.6), 0, false},
		{"gain 1.0 noise 3", gainTransform(1.0), 3, false},
		{"gain 1.6 noise 3", gainTransform(1.6), 3, false},
		{"gain 2.2 noise 3", gainTransform(2.2), 3, false},
		{"gain 0.6 noise 3", gainTransform(0.6), 3, false},
		{"gamma 0.8 noise 3", gammaTransform(0.8), 3, false},
		{"gamma 1.2 noise 2", gammaTransform(1.2), 2, false},
		{"gamma 1.2 noise 3", gammaTransform(1.2), 3, true},
		{"vibrance 1.2 noise 3", vibranceTransform(1.2), 3, true},
		{"gain 1.6 vibrance 1.2 noise 3", chain(vibranceTransform(1.2), gainTransform(1.6)), 3, true},
	}
}

// TestBlocksV31Layout checks the fixed blocks and the level cover of the calibration.
func TestBlocksV31Layout(t *testing.T) {
	if calExpectedV31[0] != magic0 || calExpectedV31[1] != magic1 || calExpectedV31[6] != [3]byte{255, 255, 255} {
		t.Fatalf("magic or white block differ: %v", calExpectedV31)
	}
	for c := 0; c < 3; c++ {
		seen := map[byte]bool{}
		for b := 2; b <= 5; b++ {
			seen[calExpectedV31[b][c]] = true
		}
		for _, lv := range []byte{0, 37, 73, 110} {
			if !seen[lv] {
				t.Errorf("channel %d never shows level %d in the calibration blocks", c, lv)
			}
		}
	}
	if got := NibbleValueV31(15); got != 110 {
		t.Errorf("nibble 15 = %d, want 110", got)
	}
	for n := byte(0); n < 16; n++ {
		if v := NibbleValueV31(n); v > 110 {
			t.Errorf("nibble %d = %d, above 110", n, v)
		}
	}
	frame, err := BuildFrameV31(v3Status(), 7, 120, nil)
	if err != nil {
		t.Fatal(err)
	}
	if frame[0] != ProtocolV31 {
		t.Errorf("version byte %d, want %d", frame[0], ProtocolV31)
	}
	blocks := BlocksV31(frame, 120)
	for i := 0; i < calibrationBlocksV31; i++ {
		if blocks[i] != calExpectedV31[i] {
			t.Errorf("fixed block %d = %v, want %v", i, blocks[i], calExpectedV31[i])
		}
	}
	if got := CapacityV31(860); got != 1232 {
		t.Errorf("CapacityV31(860) = %d, want 1232", got)
	}
}

// TestDecodeV31Synthetic decodes v3.1 frames through each client setting, at two pitches.
func TestDecodeV31Synthetic(t *testing.T) {
	const n = 160
	for _, tc := range v31Cases() {
		for _, p := range []float64{4, 5.6} {
			t.Run(fmt.Sprintf("%s pitch %.1f", tc.name, p), func(t *testing.T) {
				rng := rand.New(rand.NewSource(int64(p*100) + int64(tc.noise)))
				data := make([]byte, CapacityV31(n))
				rng.Read(data)
				frame, err := BuildFrameV31(v3Status(), 9, n, &ChunkV3{Type: MsgQuests, ID: 42, Index: 0, Count: 1, Data: data})
				if err != nil {
					t.Fatal(err)
				}
				img := renderV31(BlocksV31(frame, n), p, int(float64(n)*p)+8, rng, tc.tr, tc.noise)
				dec, err := DecodeAny(img, 0)
				if err != nil {
					if tc.limit {
						t.Logf("limit case, not decoded: %v", err)
						return
					}
					t.Fatalf("DecodeAny: %v", err)
				}
				if dec.Version != ProtocolV31 || dec.Frame == nil {
					t.Fatalf("decoded as version %d, want %d", dec.Version, ProtocolV31)
				}
				if !bytes.Equal(dec.Frame.Data, data) || dec.Frame.N != n {
					t.Fatalf("frame differs: N=%d data equal=%v", dec.Frame.N, bytes.Equal(dec.Frame.Data, data))
				}
				if dec.Live.Level != v3Status().Level || dec.Live.Money != v3Status().Money {
					t.Errorf("status differs: %+v", dec.Live)
				}
			})
		}
	}
}

// TestDecodeV31ColourGain checks that a gain the decoder cannot read is reported as such.
func TestDecodeV31ColourGain(t *testing.T) {
	const n = 160
	cases := []struct {
		name string
		tr   colourTransform
		want float64 // approximate gain in the error
	}{
		{"gain 0.4", gainTransform(0.4), 0.4},
		{"gain 2.5 clips", gainTransform(2.5), 2.5},
		{"gain 3.0 clips", gainTransform(3.0), 3.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(3))
			frame, err := BuildFrameV31(v3Status(), 1, n, nil)
			if err != nil {
				t.Fatal(err)
			}
			img := renderV31(BlocksV31(frame, n), 4, n*4+8, rng, tc.tr, 0)
			_, err = DecodeAny(img, 0)
			if !errors.Is(err, ErrColourGain) {
				t.Fatalf("err = %v, want ErrColourGain", err)
			}
			g, ok := ColourGainOf(err)
			if !ok || math.Abs(g-tc.want) > 0.3 {
				t.Errorf("gain %.2f (ok=%v), want about %.1f", g, ok, tc.want)
			}
		})
	}
}

// liveGainFixture loads testdata/strip_v3_gain_<i>.png as the RGBA image DecodeAny reads.
func liveGainFixture(t *testing.T, i int) *image.RGBA {
	t.Helper()
	f, err := os.Open(fmt.Sprintf("../../testdata/strip_v3_gain_%d.png", i))
	if err != nil {
		t.Fatalf("open fixture %d: %v", i, err)
	}
	defer f.Close()
	decoded, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode fixture %d: %v", i, err)
	}
	img := image.NewRGBA(decoded.Bounds())
	draw.Draw(img, img.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	return img
}

// TestLiveGainFixturesReportGain checks the six strips captured on the user's PC with the
// display altered (about x1.6 with clipping): each is a gain error, not a decode.
func TestLiveGainFixturesReportGain(t *testing.T) {
	for i := 0; i < 6; i++ {
		_, err := DecodeAny(liveGainFixture(t, i), 0)
		if !errors.Is(err, ErrColourGain) {
			t.Errorf("fixture %d: err = %v, want ErrColourGain", i, err)
			continue
		}
		if g, ok := ColourGainOf(err); !ok || g < 1.5 || g > 1.8 {
			t.Errorf("fixture %d: gain %.2f (ok=%v), want about 1.6", i, g, ok)
		}
	}
}

// TestColourGainText checks the notice names the gain.
func TestColourGainText(t *testing.T) {
	s := ColourGainText(1.6)
	for _, want := range []string{"alterados", "×1.6", "HDR"} {
		if !bytes.Contains([]byte(s), []byte(want)) {
			t.Errorf("notice %q lacks %q", s, want)
		}
	}
}
