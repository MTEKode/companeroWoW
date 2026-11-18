package bridge

import (
	"image"
	"image/draw"
	"image/png"
	"math"
	"math/rand"
	"os"
	"testing"
)

// observedDistortion maps the encoded levels to the colours the live WoW client
// showed on screen (3440x1440 monitor, WoW at 0,0): 0->1, 85->90, 170->178, 255->255.
// Values between the knots are interpolated linearly.
func observedDistortion(v uint8) uint8 {
	knots := [][2]float64{{0, 1}, {85, 90}, {170, 178}, {255, 255}}
	x := float64(v)
	for i := 0; i < len(knots)-1; i++ {
		a, b := knots[i], knots[i+1]
		if x <= b[0] {
			return uint8(math.Round(a[1] + (x-a[0])*(b[1]-a[1])/(b[0]-a[0])))
		}
	}
	return 255
}

// TestDecodeLiveStripFixture decodes a strip captured from the user's screen while
// the game was running (testdata/strip_v2_live.png, 464x16 at the top-left of the
// client area). It must decode with a valid CRC to the state the game was showing.
func TestDecodeLiveStripFixture(t *testing.T) {
	f, err := os.Open("../../testdata/strip_v2_live.png")
	if err != nil {
		t.Fatalf("open live fixture: %v", err)
	}
	defer f.Close()
	decoded, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode live fixture png: %v", err)
	}
	img := image.NewRGBA(decoded.Bounds())
	draw.Draw(img, img.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	got, err := DecodeImage(img)
	if err != nil {
		t.Fatalf("DecodeImage(live fixture): %v", err)
	}
	checkLiveFixture(t, got)
}

func checkLiveFixture(t *testing.T, got LiveState) {
	t.Helper()
	want := LiveState{
		MapID: 1433, X: 0.1085, Y: 0.6239, Level: 17, XPPct: 73,
		Money: 14514, TrackedQuestID: 95999, Seq: 129, EventSeq: 2,
		InCombat: true, InGroup: true, HasTarget: true,
	}
	if got.MapID != want.MapID || got.Level != want.Level || got.XPPct != want.XPPct ||
		got.Money != want.Money || got.TrackedQuestID != want.TrackedQuestID ||
		got.Seq != want.Seq || got.EventSeq != want.EventSeq ||
		got.InCombat != want.InCombat || got.InGroup != want.InGroup || got.HasTarget != want.HasTarget {
		t.Fatalf("live fixture decoded to %+v, want %+v", got, want)
	}
	if math.Abs(got.X-want.X) > 1e-3 || math.Abs(got.Y-want.Y) > 1e-3 {
		t.Fatalf("live fixture position (%.4f, %.4f), want (%.4f, %.4f)", got.X, got.Y, want.X, want.Y)
	}
}

// TestDecodeImageAtObservedPitchAndColours renders the strip at the pitch the live
// addon produces on the user's screen (about 5.3 px per block, edges blended by the
// scaling) with the colour shift seen live, with and without noise. Every block must
// decode, including the edge pixels that a pitch fit must not count.
func TestDecodeImageAtObservedPitchAndColours(t *testing.T) {
	pitches := []float64{5.25, 5.30, 5.333}
	for pi, p := range pitches {
		for si, s := range sampleStates() {
			for _, noise := range []int{0, 3} {
				seq := uint8(100 + si)
				rng := rand.New(rand.NewSource(int64(pi*31 + si*7 + noise)))
				img := renderStrip(EncodeForTest(s, seq), p, rng, observedDistortion, noise)
				got, err := DecodeImage(img)
				if err != nil {
					t.Errorf("pitch %.3f, state %d, noise %d: %v", p, si, noise, err)
					continue
				}
				if got.Seq != seq {
					t.Errorf("pitch %.3f, state %d, noise %d: seq %d, want %d", p, si, noise, got.Seq, seq)
				}
				assertSameState(t, s, got)
			}
		}
	}
}

// TestDecodeImageNeverWrongOnDamagedStrip checks that a strip with one corrupted data
// nibble never decodes to a wrong state: the CRC must reject it.
func TestDecodeImageNeverWrongOnDamagedStrip(t *testing.T) {
	s := sampleStates()[1]
	blocks := EncodeForTest(s, 9)
	// Flip one data nibble: the CRC must reject it, never return altered fields.
	blocks[12][1] = blocks[12][1] ^ 0x70
	img := renderStrip(blocks, 5.30, rand.New(rand.NewSource(4)), observedDistortion, 0)
	got, err := DecodeImage(img)
	if err == nil {
		assertSameState(t, s, got)
	}
}

// checkLive7p5Fixture pins the state of the 7.5 px live capture (map 1433, the player at
// level 18 with 6000 copper), as decoded when the capture was taken.
func checkLive7p5Fixture(t *testing.T, got LiveState) {
	t.Helper()
	if got.Seq != 76 || got.MapID != 1433 || got.Level != 18 || got.XPPct != 37 || got.Money != 5976 || got.ShareRemainingSec != 14 {
		t.Fatalf("7.5 px fixture decoded to %+v, want seq 76, map 1433, level 18, xp 37%%, money 5976, shareRemainingSec 14", got)
	}
}
