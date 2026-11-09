package bridge

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"testing"
)

// TestDecodeV3LiveFixture decodes a v3 strip captured from the running WoW client
// (testdata/strip_v3_live.png, 3440x16: WoW's own render by PrintWindow, taken while another
// window covered the screen). The CRC-16 and CRC-8 must pass and the state must be plausible
// for the character it was taken from (level 19, on a map).
//
// The header names N=645 blocks, not the 860 that a 3440 px row holds: the addon sized the
// row from a 2580 px wide window (3440/1.333), and the blocks are 5.6 px, so only 612 of the
// 645 are on screen. The frame is an idle one, so its CRC covers only the visible part.
func TestDecodeV3LiveFixture(t *testing.T) {
	f, err := os.Open("../../testdata/strip_v3_live.png")
	if err != nil {
		t.Fatalf("open live v3 fixture: %v", err)
	}
	defer f.Close()
	decoded, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode live v3 fixture png: %v", err)
	}
	img := image.NewRGBA(decoded.Bounds())
	draw.Draw(img, img.Bounds(), decoded, decoded.Bounds().Min, draw.Src)

	dec, err := DecodeAny(img, 0)
	if err != nil {
		t.Fatalf("DecodeAny(live v3 fixture): %v", err)
	}
	if dec.Version != ProtocolV3 || dec.Frame == nil {
		t.Fatalf("live fixture decoded as protocol v%d, want v3", dec.Version)
	}
	if dec.Frame.N != 645 {
		t.Errorf("N = %d, want 645 (the addon's count for the 2580 px row it saw)", dec.Frame.N)
	}
	if lv := dec.Live.Level; lv < 19 || lv > 20 {
		t.Errorf("level %d, want 19 or 20", lv)
	}
	if dec.Live.MapID == 0 {
		t.Errorf("map id 0: the status does not look like a map")
	}
	if dec.Live.Seq != dec.Frame.Seq {
		t.Errorf("status seq %d differs from frame seq %d", dec.Live.Seq, dec.Frame.Seq)
	}
	if dec.Pitch < 5.5 || dec.Pitch > 5.7 {
		t.Errorf("pitch %.4f, want about 5.6 (the live addon draws 5.6 px blocks)", dec.Pitch)
	}
}
