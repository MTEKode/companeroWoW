//go:build !windows

package bridge

import (
	"image"

	"github.com/MTEKode/companeroWoW/app/internal/capture"
)

// grabWindowStrip is unsupported outside Windows.
func grabWindowStrip(hwnd uintptr, rect image.Rectangle) (*image.RGBA, error) {
	return nil, capture.ErrUnsupported
}
