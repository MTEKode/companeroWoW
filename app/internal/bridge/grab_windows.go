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

// grabWindowStrip returns the pixel strip (StripWidth x StripHeight, at the top-left of
// the client area rect) as WoW itself renders it. Unlike a screen copy it works while
// other windows cover WoW, because PrintWindow asks the window for its own content.
func grabWindowStrip(hwnd uintptr, rect image.Rectangle) (*image.RGBA, error) {
	w, h := rect.Dx(), rect.Dy()
	if hwnd == 0 || w < StripWidth || h < StripHeight {
		return nil, fmt.Errorf("bridge: window %#x client %dx%d too small for the strip", hwnd, w, h)
	}
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return nil, errors.New("bridge: GetDC failed")
	}
	defer procRelDC.Call(0, screen)

	mem, _, _ := procMemDC.Call(screen)
	if mem == 0 {
		return nil, errors.New("bridge: CreateCompatibleDC failed")
	}
	defer procDelDC.Call(mem)

	bmp, _, _ := procMemBmp.Call(screen, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil, errors.New("bridge: CreateCompatibleBitmap failed")
	}
	defer procDelObj.Call(bmp)

	old, _, _ := procSelObj.Call(mem, bmp)
	if old == 0 {
		return nil, errors.New("bridge: SelectObject failed")
	}
	ok, _, _ := procPrintWd.Call(hwnd, mem, pwRenderFullContent)
	procSelObj.Call(mem, old)
	if ok == 0 {
		return nil, errors.New("bridge: PrintWindow failed")
	}

	bi := bitmapInfoHeader{
		Size:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:    int32(w),
		Height:   -int32(h), // top-down rows
		Planes:   1,
		BitCount: 32,
	}
	buf := make([]byte, w*h*4)
	lines, _, _ := procGetBits.Call(mem, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0)
	if lines == 0 {
		return nil, errors.New("bridge: GetDIBits failed")
	}

	// Only the top-left strip is needed; convert that part from BGRX to RGBA.
	img := image.NewRGBA(image.Rect(0, 0, StripWidth, StripHeight))
	for y := 0; y < StripHeight; y++ {
		src := buf[y*w*4 : y*w*4+StripWidth*4]
		dst := img.Pix[y*img.Stride : y*img.Stride+StripWidth*4]
		for x := 0; x < StripWidth; x++ {
			dst[4*x+0] = src[4*x+2]
			dst[4*x+1] = src[4*x+1]
			dst[4*x+2] = src[4*x+0]
			dst[4*x+3] = 0xFF
		}
	}
	return img, nil
}
