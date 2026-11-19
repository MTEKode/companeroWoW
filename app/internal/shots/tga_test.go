package shots

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"testing"
)

// patternImage is a w x h image whose pixels differ, with a run of equal pixels in each row so the
// RLE packets get both kinds.
func patternImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{uint8(x * 7), uint8(y * 13), uint8(x + y), 255}
			if x >= 2 && x < 9 {
				c = color.RGBA{200, 10, 30, 255} // a run
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func assertSame(t *testing.T, got, want *image.RGBA) {
	t.Helper()
	if got.Rect != want.Rect {
		t.Fatalf("size %v, want %v", got.Rect, want.Rect)
	}
	for y := 0; y < want.Rect.Dy(); y++ {
		for x := 0; x < want.Rect.Dx(); x++ {
			g := got.RGBAAt(x, y)
			w := want.RGBAAt(x, y)
			if g.R != w.R || g.G != w.G || g.B != w.B {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, g, w)
			}
		}
	}
}

func TestTGAUncompressedBothOrigins(t *testing.T) {
	want := patternImage(13, 7)
	for _, bottomLeft := range []bool{false, true} {
		got, err := DecodeTGA(tgaBytes(want, false, bottomLeft))
		if err != nil {
			t.Fatalf("bottomLeft %v: %v", bottomLeft, err)
		}
		assertSame(t, got, want)
	}
}

func TestTGARunLength(t *testing.T) {
	want := patternImage(40, 9) // rows longer than one packet, so runs cross packets
	for _, bottomLeft := range []bool{false, true} {
		got, err := DecodeTGA(tgaBytes(want, true, bottomLeft))
		if err != nil {
			t.Fatalf("RLE bottomLeft %v: %v", bottomLeft, err)
		}
		assertSame(t, got, want)
	}
}

func TestTGARunLengthFixture(t *testing.T) {
	// A hand-made 5x1 image, top-left origin: a run of 3 red pixels, then a raw packet of green and blue.
	b := []byte{0, 0, 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 5, 0, 1, 0, 24, 0x20}
	b = append(b, 0x82, 0, 0, 255)            // run of 3: BGR (0,0,255) = red
	b = append(b, 0x01, 0, 255, 0, 255, 0, 0) // raw of 2: green, then blue
	got, err := DecodeTGA(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []color.RGBA{{255, 0, 0, 255}, {255, 0, 0, 255}, {255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}}
	for x, c := range want {
		if g := got.RGBAAt(x, 0); g != c {
			t.Fatalf("pixel %d = %v, want %v", x, g, c)
		}
	}
}

func TestTGAGreyscaleAnd16Bit(t *testing.T) {
	grey := []byte{0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 1, 0, 8, 0x20, 0, 128}
	got, err := DecodeTGA(grey)
	if err != nil {
		t.Fatal(err)
	}
	if g := got.RGBAAt(1, 0); g.R != 128 || g.G != 128 || g.B != 128 {
		t.Fatalf("grey pixel = %v", g)
	}
	// 16 bit: 5-5-5 with the top bit unused; value 0x7FFF is white.
	h16 := []byte{0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 16, 0x20, 0xFF, 0x7F}
	got, err = DecodeTGA(h16)
	if err != nil {
		t.Fatal(err)
	}
	if g := got.RGBAAt(0, 0); g.R != 255 || g.G != 255 || g.B != 255 {
		t.Fatalf("16-bit white = %v", g)
	}
}

func TestTGARejectsBadFiles(t *testing.T) {
	good := tgaBytes(patternImage(8, 4), false, false)
	cases := map[string][]byte{
		"short header":   good[:10],
		"truncated data": good[:len(good)-5],
		"colour map":     append([]byte{0, 1}, good[2:]...),
		"bad bpp":        withByte(good, 16, 12),
		"zero size":      withByte(withByte(good, 12, 0), 13, 0),
		"unknown type":   withByte(good, 2, 9),
	}
	for name, b := range cases {
		if _, err := DecodeTGA(b); !errors.Is(err, ErrTGA) {
			t.Errorf("%s: err = %v, want ErrTGA", name, err)
		}
	}
	rle := tgaBytes(patternImage(8, 4), true, false)
	bad := append([]byte(nil), rle[:18]...)
	bad = append(bad, 0x7F, 1, 2, 3) // a raw packet of 128 pixels in a 32-pixel image
	if _, err := DecodeTGA(bad); !errors.Is(err, ErrTGA) {
		t.Errorf("RLE overrun: err = %v, want ErrTGA", err)
	}
}

func withByte(b []byte, at int, v byte) []byte {
	out := append([]byte(nil), b...)
	out[at] = v
	return out
}

func TestLoadImageSniffsFormats(t *testing.T) {
	want := patternImage(6, 3)
	for name, data := range map[string][]byte{
		"a.png": pngBytes(t, want),
		"a.jpg": jpegBytes(t, want, 100),
	} {
		if _, err := LoadImage(name, data); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	img, err := LoadImage("shot.TGA", tgaBytes(want, false, true))
	if err != nil {
		t.Fatal(err)
	}
	assertSame(t, toRGBAForTest(img), want)
	if _, err := LoadImage("notes.txt", []byte("hola")); !errors.Is(err, errNotPanel) {
		t.Errorf("unknown bytes: err = %v", err)
	}
	if !bytes.HasPrefix(pngBytes(t, want), pngSig) {
		t.Fatal("png signature missing")
	}
}

func toRGBAForTest(img image.Image) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bb, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			out.SetRGBA(x, y, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bb >> 8), 255})
		}
	}
	return out
}
