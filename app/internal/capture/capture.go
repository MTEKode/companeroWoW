// Package capture grabs pixels from the screen and the World of Warcraft window.
// Platform specifics live in capture_windows.go and capture_other.go.
package capture

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

var (
	// ErrWoWNotFound means no visible, non-minimized WoW window exists.
	ErrWoWNotFound = errors.New("capture: World of Warcraft window not found")
	// ErrUnsupported is returned on platforms without screen capture support.
	ErrUnsupported = errors.New("capture: screen capture is only supported on Windows")
)

const (
	maxSide         = 1600
	keepScreenshots = 10
	screenshotGlob  = "screenshot-"
)

// CaptureWoW returns a PNG of the WoW client area, downscaled so that the longest
// side is at most 1600 px.
func CaptureWoW() ([]byte, error) {
	_, rect, err := FindWoWWindow()
	if err != nil {
		return nil, err
	}
	img, err := GrabScreen(rect)
	if err != nil {
		return nil, fmt.Errorf("capture WoW client area: %w", err)
	}
	img = Downscale(img, maxSide)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode screenshot png: %w", err)
	}
	return buf.Bytes(), nil
}

// CaptureWoWToFile takes a screenshot of the WoW window and saves it with SavePNG.
func CaptureWoWToFile(dir string) (string, error) {
	data, err := CaptureWoW()
	if err != nil {
		return "", err
	}
	return SavePNG(dir, data)
}

// screenshotSeq numbers the files written by this process, so that two captures in the
// same millisecond do not share a name.
var screenshotSeq atomic.Uint32

// SavePNG writes data into dir as a new screenshot-<time>-<n>.png file, keeps only the
// newest keepScreenshots files there and returns the new path. If pruning fails the
// path is still returned together with a non-nil error.
func SavePNG(dir string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create screenshot dir: %w", err)
	}
	name := fmt.Sprintf("%s%s-%04d.png", screenshotGlob, time.Now().Format("20060102-150405.000"), screenshotSeq.Add(1)%10000)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write screenshot %s: %w", path, err)
	}
	if err := pruneScreenshots(dir, keepScreenshots); err != nil {
		return path, fmt.Errorf("screenshot saved to %s but pruning failed: %w", path, err)
	}
	return path, nil
}

// pruneScreenshots removes all but the `keep` newest screenshot-*.png files in dir.
func pruneScreenshots(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("list %s: %w", dir, err)
	}
	type file struct {
		name string
		mod  time.Time
	}
	var files []file
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, screenshotGlob) || !strings.HasSuffix(name, ".png") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, file{name: name, mod: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool {
		if !files[i].mod.Equal(files[j].mod) {
			return files[i].mod.After(files[j].mod)
		}
		return files[i].name > files[j].name
	})
	var firstErr error
	for i := keep; i < len(files); i++ {
		if err := os.Remove(filepath.Join(dir, files[i].name)); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("remove %s: %w", files[i].name, err)
		}
	}
	return firstErr
}

// Downscale box-averages src so that its longest side is at most maxSide.
// Images already small enough are returned unchanged. src must start at (0,0).
func Downscale(src *image.RGBA, maxSide int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	long := w
	if h > long {
		long = h
	}
	if maxSide <= 0 || long <= maxSide || w == 0 || h == 0 {
		return src
	}
	nw := w * maxSide / long
	nh := h * maxSide / long
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for dy := 0; dy < nh; dy++ {
		sy0, sy1 := dy*h/nh, (dy+1)*h/nh
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for dx := 0; dx < nw; dx++ {
			sx0, sx1 := dx*w/nw, (dx+1)*w/nw
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var r, g, bl, a, n uint64
			for sy := sy0; sy < sy1; sy++ {
				row := sy * src.Stride
				for sx := sx0; sx < sx1; sx++ {
					i := row + sx*4
					r += uint64(src.Pix[i])
					g += uint64(src.Pix[i+1])
					bl += uint64(src.Pix[i+2])
					a += uint64(src.Pix[i+3])
					n++
				}
			}
			o := (dy*nw + dx) * 4
			dst.Pix[o] = uint8(r / n)
			dst.Pix[o+1] = uint8(g / n)
			dst.Pix[o+2] = uint8(bl / n)
			dst.Pix[o+3] = uint8(a / n)
		}
	}
	return dst
}
