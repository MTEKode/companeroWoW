package bridge

import (
	"image"
	"image/color"
	"math/rand"
)

// PanelOptions shape a rendered data panel (see RenderPanel).
type PanelOptions struct {
	// BlockPx is the side of a block in physical px (2 for the panel, 4 for the JPEG fallback).
	BlockPx int
	// Noise is the largest offset added to each channel of each pixel of the panel (0 for none).
	Noise int
	// Seed seeds the noise.
	Seed int64
	// Width and Height are the canvas size. A canvas smaller than the panel grows to fit it.
	Width, Height int
}

// RenderPanel draws the rows as a data panel, the way the addon does: row r is the frame
// rows[r] (BuildFrameV5 or BuildEndFrameV5 output) of n blocks, drawn at the top-left. The
// canvas beyond the panel is a flat game-like background, so the decoder has to find the panel
// edge itself. The result is what a screenshot of the panel looks like, before any encoding.
func RenderPanel(rows [][]byte, n int, o PanelOptions) *image.RGBA {
	bp := o.BlockPx
	if bp <= 0 {
		bp = BlockPx
	}
	w, h := n*bp, len(rows)*bp
	if o.Width > w {
		w = o.Width
	}
	if o.Height > h {
		h = o.Height
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// The background: a dark blue gradient, the colours of a scene.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(20 + x%64), uint8(40 + y%48), uint8(90 + (x+y)%40), 255})
		}
	}
	rng := rand.New(rand.NewSource(o.Seed))
	noise := func(v byte) byte {
		if o.Noise == 0 {
			return v
		}
		d := rng.Intn(2*o.Noise+1) - o.Noise
		x := int(v) + d
		if x < 0 {
			x = 0
		}
		if x > 255 {
			x = 255
		}
		return byte(x)
	}
	for r, frame := range rows {
		blocks := BlocksV31(frame, n)
		for c, blk := range blocks {
			for y := 0; y < bp; y++ {
				for x := 0; x < bp; x++ {
					img.SetRGBA(c*bp+x, r*bp+y, color.RGBA{noise(blk[0]), noise(blk[1]), noise(blk[2]), 255})
				}
			}
		}
	}
	return img
}
