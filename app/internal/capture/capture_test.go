package capture

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDownscaleLongestSide(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3200, 1800))
	for i := range src.Pix {
		src.Pix[i] = 0x80
	}
	dst := Downscale(src, 1600)
	b := dst.Bounds()
	if b.Dx() != 1600 || b.Dy() != 900 {
		t.Fatalf("downscaled to %dx%d, want 1600x900", b.Dx(), b.Dy())
	}
	if got := dst.RGBAAt(10, 10); got != (color.RGBA{0x80, 0x80, 0x80, 0x80}) {
		t.Fatalf("flat color not preserved: %v", got)
	}
}

func TestDownscaleKeepsSmallImages(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 800, 600))
	if Downscale(src, 1600) != src {
		t.Fatal("small image should be returned unchanged")
	}
}

func TestDownscaleAveragesPixels(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 1))
	for x := 0; x < 4; x++ {
		v := uint8(0)
		if x%2 == 1 {
			v = 200
		}
		src.SetRGBA(x, 0, color.RGBA{v, 0, 0, 255})
	}
	dst := Downscale(src, 2)
	if dst.Bounds().Dx() != 2 {
		t.Fatalf("width %d, want 2", dst.Bounds().Dx())
	}
	if got := dst.RGBAAt(0, 0).R; got != 100 {
		t.Fatalf("average = %d, want 100", got)
	}
}

func TestPruneScreenshotsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		p := filepath.Join(dir, fmt.Sprintf("screenshot-%d.png", 1000+i))
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(other, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pruneScreenshots(dir, 10); err != nil {
		t.Fatalf("prune: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 11 {
		t.Fatalf("after prune have %d files (%v), want 10 screenshots + notes.txt", len(names), names)
	}
	if _, err := os.Stat(filepath.Join(dir, "screenshot-1000.png")); !os.IsNotExist(err) {
		t.Fatalf("oldest screenshot should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "screenshot-1011.png")); err != nil {
		t.Fatalf("newest screenshot should remain: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("non-screenshot file must not be touched: %v", err)
	}
}
