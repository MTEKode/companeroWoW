//go:build !windows

package capture

import "image"

// FindWoWWindow is unsupported outside Windows.
func FindWoWWindow() (uintptr, image.Rectangle, error) {
	return 0, image.Rectangle{}, ErrUnsupported
}

// GrabScreen is unsupported outside Windows.
func GrabScreen(r image.Rectangle) (*image.RGBA, error) {
	return nil, ErrUnsupported
}
