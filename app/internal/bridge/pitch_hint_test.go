package bridge

import (
	"math/rand"
	"testing"
)

// TestDecodeStripLastPitchFirst checks the pitch hint: a hint that still decodes is
// used as it is (the search would return a grid pitch, not the hint), and a wrong hint
// falls back to the full search and still decodes.
func TestDecodeStripLastPitchFirst(t *testing.T) {
	s := sampleStates()[0]
	const pitch = 5.33 // not on the search grid, so a returned 5.33 proves the hint was used
	img := renderStrip(EncodeForTest(s, 3), pitch, rand.New(rand.NewSource(7)), identity, 0)

	st, p, err := decodeStrip(img, 0)
	if err != nil {
		t.Fatalf("decode without hint: %v", err)
	}
	assertSameState(t, s, st)

	st, p2, err := decodeStrip(img, pitch)
	if err != nil {
		t.Fatalf("decode with the right hint: %v", err)
	}
	if p2 != pitch {
		t.Errorf("right hint: returned pitch %.4f, want the hint %.2f back", p2, pitch)
	}
	assertSameState(t, s, st)

	st, p3, err := decodeStrip(img, 9.9)
	if err != nil {
		t.Fatalf("decode with a wrong hint: %v", err)
	}
	if p3 == 9.9 {
		t.Errorf("wrong hint was returned as the decoded pitch")
	}
	assertSameState(t, s, st)
	if p == 0 {
		t.Error("decode without hint returned pitch 0")
	}
}

// TestDecodeStripHintNotUsedOnFailure checks that a hint never turns a bad strip into a
// decoded one.
func TestDecodeStripHintNotUsedOnFailure(t *testing.T) {
	img := renderStrip(EncodeForTest(sampleStates()[0], 3), 5.33, rand.New(rand.NewSource(8)), identity, 0)
	for y := 0; y < StripHeight; y++ {
		for x := 0; x < StripWidth; x++ {
			img.Pix[img.PixOffset(x, y)] ^= 0x80
		}
	}
	if _, p, err := decodeStrip(img, 5.33); err == nil {
		t.Fatalf("damaged strip decoded at pitch %.2f with a hint", p)
	}
}
