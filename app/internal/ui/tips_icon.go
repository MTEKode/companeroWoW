package ui

import (
	"encoding/binary"
	"math"
)

// badgeColours of the tray icon in the badge state: the panel's gold ring and dark centre,
// and a red dot with a white rim in the corner.
var (
	badgeGold  = [3]float64{0xC8, 0xA2, 0x4A}
	badgeDark  = [3]float64{0x2A, 0x1F, 0x14}
	badgeRed   = [3]float64{0xE0, 0x44, 0x3A}
	badgeWhite = [3]float64{0xFF, 0xFF, 0xFF}
)

// badgeSample is the supersampling per axis, for smooth edges.
const badgeSample = 4

// badgeIconImage returns one icon image of size×size pixels in the form the icon resource
// format uses (CreateIconFromResourceEx takes it without the file header): a 40-byte
// BITMAPINFOHEADER with twice the height (colour and mask), the 32-bit BGRA pixels from the
// bottom row up, and an all-opaque 1-bit AND mask, which the alpha channel overrides.
func badgeIconImage(size int) []byte {
	const hdr = 40
	maskRow := ((size + 31) / 32) * 4
	out := make([]byte, hdr+size*size*4+maskRow*size)
	binary.LittleEndian.PutUint32(out[0:], hdr)
	binary.LittleEndian.PutUint32(out[4:], uint32(size))
	binary.LittleEndian.PutUint32(out[8:], uint32(2*size))
	binary.LittleEndian.PutUint16(out[12:], 1)  // planes
	binary.LittleEndian.PutUint16(out[14:], 32) // bits per pixel
	binary.LittleEndian.PutUint32(out[20:], uint32(size*size*4))

	px := out[hdr:]
	for y := 0; y < size; y++ {
		row := size - 1 - y // the file stores the bottom row first
		for x := 0; x < size; x++ {
			r, g, b, a := badgePixel(x, y, size)
			o := (row*size + x) * 4
			px[o+0] = b
			px[o+1] = g
			px[o+2] = r
			px[o+3] = a
		}
	}
	return out
}

// badgePixel returns the colour and alpha of the pixel (x, y) of an icon of the given size,
// averaged over the supersampled points so that the edges are smooth.
func badgePixel(x, y, size int) (r, g, b, a uint8) {
	s := float64(size)
	radius := s/2 - 0.5
	dotR := s * 0.27
	dotX := s - dotR - 0.5
	dotY := dotR + 0.5
	rimW := s / 12

	var sumR, sumG, sumB, sumA float64
	n := badgeSample * badgeSample
	for sy := 0; sy < badgeSample; sy++ {
		for sx := 0; sx < badgeSample; sx++ {
			px := float64(x) + (float64(sx)+0.5)/float64(badgeSample)
			py := float64(y) + (float64(sy)+0.5)/float64(badgeSample)
			c, alpha := badgeShape(px, py, s/2, s/2, radius, dotX, dotY, dotR, rimW)
			sumR += c[0] * alpha
			sumG += c[1] * alpha
			sumB += c[2] * alpha
			sumA += alpha
		}
	}
	if sumA == 0 {
		return 0, 0, 0, 0
	}
	cov := sumA / float64(n)
	return uint8(sumR/sumA + 0.5), uint8(sumG/sumA + 0.5), uint8(sumB/sumA + 0.5), uint8(cov*255 + 0.5)
}

// badgeShape is the colour of the shape at one point, and its coverage: 1 inside a shape,
// 0 outside.
func badgeShape(px, py, cx, cy, radius, dotX, dotY, dotR, rimW float64) ([3]float64, float64) {
	if math.Hypot(px-dotX, py-dotY) <= dotR {
		return badgeRed, 1
	}
	if math.Hypot(px-dotX, py-dotY) <= dotR+rimW {
		return badgeWhite, 1
	}
	d := math.Hypot(px-cx, py-cy)
	switch {
	case d <= radius-rimW:
		return badgeDark, 1
	case d <= radius:
		return badgeGold, 1
	}
	return [3]float64{}, 0
}
