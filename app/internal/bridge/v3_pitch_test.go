package bridge

import (
	"bytes"
	"math/rand"
	"testing"
)

// v3SyntheticN is the block count of a full-width frame on a 3440 px client (the addon's
// N = floor(3440/4) = 860).
const v3SyntheticN = 860

// TestDecodeV3SyntheticPitches renders a full 860-block frame at each pitch, with the
// colour shift seen live (0->1, 85->90, 170->178) and with +-3 noise, and decodes it from
// the rendered row alone. The 5.33 and 5.6 pitches are non-integer with alternating 5/6 px
// blocks; every edge is blended by one pixel.
func TestDecodeV3SyntheticPitches(t *testing.T) {
	for _, p := range []float64{4.0, 5.33, 5.6, 6.0, 7.5} {
		for _, noise := range []int{0, 3} {
			rng := rand.New(rand.NewSource(int64(p*1000) + int64(noise)))
			data := make([]byte, 800)
			rng.Read(data)
			const seq = 17
			_, blocks := v3Frame(t, seq, v3SyntheticN, data)
			width := int(float64(v3SyntheticN)*p) + 6
			img := renderV3(blocks, p, width, rng, observedDistortion, noise)
			f, gotP, err := DecodeFrameV3(img, 0)
			if err != nil {
				t.Errorf("pitch %.2f, noise %d: %v", p, noise, err)
				continue
			}
			if f.N != v3SyntheticN || f.Seq != seq || f.MsgType != MsgQuests || f.MsgID != 513 {
				t.Errorf("pitch %.2f, noise %d: frame N=%d seq=%d type=%d id=%d", p, noise, f.N, f.Seq, f.MsgType, f.MsgID)
			}
			if !bytes.Equal(f.Data, data) {
				t.Errorf("pitch %.2f, noise %d: chunk data differs", p, noise)
			}
			if d := gotP - p; d < -0.02 || d > 0.02 {
				t.Errorf("pitch %.2f, noise %d: decoded at pitch %.4f", p, noise, gotP)
			}
			if f.Status.Level != v3Status().Level || f.Status.MapID != v3Status().MapID {
				t.Errorf("pitch %.2f, noise %d: status differs", p, noise)
			}
		}
	}
}

// TestDecodeV3TruncatedRow renders the live case: 860 blocks at 5.625 px in a 3440 px row,
// so only 612 blocks are on screen. A chunk that ends inside the visible part must decode;
// a bigger one must not decode to wrong data.
func TestDecodeV3TruncatedRow(t *testing.T) {
	const p, width = 5.625, 3440
	rng := rand.New(rand.NewSource(5))

	short := make([]byte, 300)
	rng.Read(short)
	_, blocks := v3Frame(t, 5, v3SyntheticN, short)
	f, _, err := DecodeFrameV3(renderV3(blocks, p, width, rng, observedDistortion, 3), 0)
	if err != nil {
		t.Fatalf("short chunk in a truncated row: %v", err)
	}
	if !bytes.Equal(f.Data, short) || f.N != v3SyntheticN {
		t.Fatalf("short chunk: N=%d, data differs=%v", f.N, !bytes.Equal(f.Data, short))
	}

	long := make([]byte, 1000)
	rng.Read(long)
	_, blocks = v3Frame(t, 6, v3SyntheticN, long)
	f, _, err = DecodeFrameV3(renderV3(blocks, p, width, rng, observedDistortion, 3), 0)
	if err == nil && !bytes.Equal(f.Data, long) {
		t.Fatalf("long chunk in a truncated row decoded to wrong data")
	}
}
