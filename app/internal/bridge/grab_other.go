//go:build !windows

package bridge

import (
	"image"

	"github.com/MTEKode/companeroWoW/app/internal/capture"
)

// windowGrabber is the PrintWindow fallback; it is unsupported outside Windows.
type windowGrabber struct{}

func (g *windowGrabber) grab(hwnd uintptr, rect image.Rectangle) (*image.RGBA, error) {
	return nil, capture.ErrUnsupported
}

func (g *windowGrabber) close() {}
