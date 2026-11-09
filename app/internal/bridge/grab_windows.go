//go:build windows

package bridge

import (
	"errors"
	"fmt"
	"image"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PrintWindow flag: render the window's full content, including composed/DirectX
// layers, even when another window covers it.
const pwRenderFullContent = 0x00000002

// srcCopy is the BitBlt raster operation that copies the source.
const srcCopy = 0x00CC0020

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modGDI32    = windows.NewLazySystemDLL("gdi32.dll")
	procPrintWd = modUser32.NewProc("PrintWindow")
	procGetDC   = modUser32.NewProc("GetDC")
	procRelDC   = modUser32.NewProc("ReleaseDC")
	procMemDC   = modGDI32.NewProc("CreateCompatibleDC")
	procMemBmp  = modGDI32.NewProc("CreateCompatibleBitmap")
	procSelObj  = modGDI32.NewProc("SelectObject")
	procDelObj  = modGDI32.NewProc("DeleteObject")
	procDelDC   = modGDI32.NewProc("DeleteDC")
	procGetBits = modGDI32.NewProc("GetDIBits")
	procBitBlt  = modGDI32.NewProc("BitBlt")
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// windowGrabber holds the GDI objects of the PrintWindow fallback between calls. The
// full-window bitmap is kept while the client size is unchanged, and the strip is
// cropped into a bitmap of the strip's size that is read back, so each call copies only
// the strip (the client width x StripHeightV3) to Go memory. The zero value is ready to use.
type windowGrabber struct {
	w, h    int     // size of the full-window bitmap
	memFull uintptr // DC holding the full-window bitmap
	bmpFull uintptr
	cw, ch  int     // size of the crop bitmap
	memCrop uintptr // DC holding the strip-sized bitmap
	bmpCrop uintptr
	oldCrop uintptr // the object memCrop held before bmpCrop was selected in
	buf     []byte
	img     *image.RGBA
}

// grab returns the pixel strip (the client width x StripHeightV3, at the top-left of the
// client area rect) as WoW itself renders it. Unlike a screen copy it works while other
// windows cover WoW, because PrintWindow asks the window for its own content. The
// returned image is reused by the next call.
func (g *windowGrabber) grab(hwnd uintptr, rect image.Rectangle) (*image.RGBA, error) {
	w, h := rect.Dx(), rect.Dy()
	cw, ch := w, StripHeightV3
	if hwnd == 0 || w < 8*BlockPx || h < ch {
		return nil, fmt.Errorf("bridge: window %#x client %dx%d too small for the strip", hwnd, w, h)
	}
	if err := g.ensure(w, h); err != nil {
		return nil, err
	}
	if err := g.ensureCrop(cw, ch); err != nil {
		return nil, err
	}
	ok, _, _ := procPrintWd.Call(hwnd, g.memFull, pwRenderFullContent)
	if ok == 0 {
		return nil, errors.New("bridge: PrintWindow failed")
	}

	// Crop: select the strip-sized bitmap, copy the top-left region into it, deselect it
	// (GetDIBits needs the bitmap unselected).
	if old, _, _ := procSelObj.Call(g.memCrop, g.bmpCrop); old == 0 {
		return nil, errors.New("bridge: SelectObject failed")
	}
	okBlt, _, _ := procBitBlt.Call(g.memCrop, 0, 0, uintptr(cw), uintptr(ch), g.memFull, 0, 0, srcCopy)
	procSelObj.Call(g.memCrop, g.oldCrop)
	if okBlt == 0 {
		return nil, errors.New("bridge: BitBlt failed")
	}

	bi := bitmapInfoHeader{
		Size:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:    int32(cw),
		Height:   -int32(ch), // top-down rows
		Planes:   1,
		BitCount: 32,
	}
	lines, _, _ := procGetBits.Call(g.memCrop, g.bmpCrop, 0, uintptr(ch), uintptr(unsafe.Pointer(&g.buf[0])), uintptr(unsafe.Pointer(&bi)), 0)
	if lines == 0 {
		return nil, errors.New("bridge: GetDIBits failed")
	}

	// BGRX to RGBA.
	img := g.img
	for y := 0; y < ch; y++ {
		src := g.buf[y*cw*4 : (y+1)*cw*4]
		dst := img.Pix[y*img.Stride : y*img.Stride+cw*4]
		for x := 0; x < cw; x++ {
			dst[4*x+0] = src[4*x+2]
			dst[4*x+1] = src[4*x+1]
			dst[4*x+2] = src[4*x+0]
			dst[4*x+3] = 0xFF
		}
	}
	return img, nil
}

// ensure makes sure the full-window bitmap is w x h.
func (g *windowGrabber) ensure(w, h int) error {
	if g.memFull != 0 && g.w == w && g.h == h {
		return nil
	}
	g.releaseFull()
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return errors.New("bridge: GetDC failed")
	}
	defer procRelDC.Call(0, screen)

	mem, _, _ := procMemDC.Call(screen)
	if mem == 0 {
		return errors.New("bridge: CreateCompatibleDC failed")
	}
	bmp, _, _ := procMemBmp.Call(screen, uintptr(w), uintptr(h))
	if bmp == 0 {
		procDelDC.Call(mem)
		return errors.New("bridge: CreateCompatibleBitmap failed")
	}
	if old, _, _ := procSelObj.Call(mem, bmp); old == 0 {
		procDelObj.Call(bmp)
		procDelDC.Call(mem)
		return errors.New("bridge: SelectObject failed")
	}
	g.memFull, g.bmpFull, g.w, g.h = mem, bmp, w, h
	return nil
}

// ensureCrop makes sure the strip-sized DC and bitmap are w x h. They are recreated when
// the size changes (the client width can change with the window).
func (g *windowGrabber) ensureCrop(w, h int) error {
	if g.memCrop != 0 && g.cw == w && g.ch == h {
		return nil
	}
	g.releaseCrop()
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return errors.New("bridge: GetDC failed")
	}
	defer procRelDC.Call(0, screen)
	mem, _, _ := procMemDC.Call(screen)
	if mem == 0 {
		return errors.New("bridge: CreateCompatibleDC failed")
	}
	bmp, _, _ := procMemBmp.Call(screen, uintptr(w), uintptr(h))
	if bmp == 0 {
		procDelDC.Call(mem)
		return errors.New("bridge: CreateCompatibleBitmap failed")
	}
	old, _, _ := procSelObj.Call(mem, bmp)
	if old == 0 {
		procDelObj.Call(bmp)
		procDelDC.Call(mem)
		return errors.New("bridge: SelectObject failed")
	}
	procSelObj.Call(mem, old) // leave it unselected; grab selects it when needed
	g.memCrop, g.bmpCrop, g.oldCrop = mem, bmp, old
	g.cw, g.ch = w, h
	g.buf = make([]byte, w*h*4)
	g.img = image.NewRGBA(image.Rect(0, 0, w, h))
	return nil
}

func (g *windowGrabber) releaseFull() {
	if g.memFull != 0 {
		procDelObj.Call(g.bmpFull)
		procDelDC.Call(g.memFull)
	}
	g.memFull, g.bmpFull, g.w, g.h = 0, 0, 0, 0
}

func (g *windowGrabber) releaseCrop() {
	if g.memCrop != 0 {
		procDelObj.Call(g.bmpCrop)
		procDelDC.Call(g.memCrop)
	}
	g.memCrop, g.bmpCrop, g.oldCrop = 0, 0, 0
	g.cw, g.ch = 0, 0
}

// close frees the GDI objects. The grabber can be used again afterwards.
func (g *windowGrabber) close() {
	g.releaseFull()
	g.releaseCrop()
}
