//go:build windows

package capture

import (
	"errors"
	"fmt"
	"image"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wowClass = "GxWindowClass"
	wowTitle = "World of Warcraft"

	srcCopy       = 0x00CC0020
	biRGB         = 0
	dibRGBColors  = 0
	dpiAwareV2Ctx = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procFindWindowW                   = user32.NewProc("FindWindowW")
	procIsWindowVisible               = user32.NewProc("IsWindowVisible")
	procIsIconic                      = user32.NewProc("IsIconic")
	procGetClientRect                 = user32.NewProc("GetClientRect")
	procClientToScreen                = user32.NewProc("ClientToScreen")
	procGetDC                         = user32.NewProc("GetDC")
	procReleaseDC                     = user32.NewProc("ReleaseDC")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")

	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
)

type winRect struct{ Left, Top, Right, Bottom int32 }

type winPoint struct{ X, Y int32 }

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

var dpiOnce sync.Once

// ensureDPIAware makes coordinates physical pixels; without it Windows would
// virtualize window rects and the pixel strip would be read from the wrong place.
func ensureDPIAware() {
	dpiOnce.Do(func() {
		_, _, _ = procSetProcessDpiAwarenessContext.Call(dpiAwareV2Ctx)
	})
}

func findWindow(class, title string) (uintptr, error) {
	var cp, tp *uint16
	var err error
	if class != "" {
		if cp, err = windows.UTF16PtrFromString(class); err != nil {
			return 0, err
		}
	}
	if title != "" {
		if tp, err = windows.UTF16PtrFromString(title); err != nil {
			return 0, err
		}
	}
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cp)), uintptr(unsafe.Pointer(tp)))
	return hwnd, nil
}

// FindWoWWindow returns the WoW window handle and its client-area rectangle in
// screen (physical pixel) coordinates. Minimized or hidden windows do not count.
func FindWoWWindow() (uintptr, image.Rectangle, error) {
	ensureDPIAware()
	hwnd, err := findWindow(wowClass, "")
	if err != nil {
		return 0, image.Rectangle{}, fmt.Errorf("find WoW window: %w", err)
	}
	if hwnd == 0 {
		hwnd, err = findWindow("", wowTitle)
		if err != nil {
			return 0, image.Rectangle{}, fmt.Errorf("find WoW window: %w", err)
		}
	}
	if hwnd == 0 {
		return 0, image.Rectangle{}, ErrWoWNotFound
	}
	if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
		return 0, image.Rectangle{}, ErrWoWNotFound
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		return 0, image.Rectangle{}, ErrWoWNotFound
	}
	var cr winRect
	if ok, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr))); ok == 0 {
		return 0, image.Rectangle{}, fmt.Errorf("get WoW client rect: %w", ErrWoWNotFound)
	}
	w := int(cr.Right - cr.Left)
	h := int(cr.Bottom - cr.Top)
	if w <= 0 || h <= 0 {
		return 0, image.Rectangle{}, ErrWoWNotFound
	}
	var origin winPoint
	if ok, _, _ := procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&origin))); ok == 0 {
		return 0, image.Rectangle{}, errors.New("capture: ClientToScreen failed")
	}
	x, y := int(origin.X), int(origin.Y)
	return hwnd, image.Rect(x, y, x+w, y+h), nil
}

// GrabScreen copies the given screen rectangle (physical pixels) into an RGBA image
// whose origin is (0,0).
func GrabScreen(r image.Rectangle) (*image.RGBA, error) {
	w, h := r.Dx(), r.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("capture: empty rectangle %v", r)
	}
	ensureDPIAware()

	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return nil, errors.New("capture: GetDC failed")
	}
	defer procReleaseDC.Call(0, screen)

	mem, _, _ := procCreateCompatibleDC.Call(screen)
	if mem == 0 {
		return nil, errors.New("capture: CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(mem)

	bmp, _, _ := procCreateCompatibleBitmap.Call(screen, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil, errors.New("capture: CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(bmp)

	old, _, _ := procSelectObject.Call(mem, bmp)
	if old == 0 {
		return nil, errors.New("capture: SelectObject failed")
	}
	blt, _, _ := procBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), screen, uintptr(r.Min.X), uintptr(r.Min.Y), srcCopy)
	// The bitmap must be deselected before GetDIBits reads it.
	procSelectObject.Call(mem, old)
	if blt == 0 {
		return nil, fmt.Errorf("capture: BitBlt failed for %v (screen locked or unavailable?)", r)
	}

	bi := bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(w),
		Height:      -int32(h), // negative: top-down rows
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}
	buf := make([]byte, w*h*4)
	lines, _, _ := procGetDIBits.Call(mem, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors)
	if lines == 0 {
		return nil, errors.New("capture: GetDIBits failed")
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(buf); i += 4 {
		img.Pix[i] = buf[i+2]
		img.Pix[i+1] = buf[i+1]
		img.Pix[i+2] = buf[i]
		img.Pix[i+3] = 0xFF
	}
	return img, nil
}
