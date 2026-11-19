package shots

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"
)

var (
	pngSig  = []byte("\x89PNG\r\n\x1a\n")
	jpegSig = []byte{0xFF, 0xD8, 0xFF}
)

// LoadImage decodes a screenshot. PNG and JPEG are recognised by their signature; a TGA has none,
// so it is read as TGA when its name says so.
func LoadImage(name string, data []byte) (image.Image, error) {
	switch {
	case bytes.HasPrefix(data, pngSig):
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("png: %w", err)
		}
		return img, nil
	case bytes.HasPrefix(data, jpegSig):
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("jpeg: %w", err)
		}
		return img, nil
	case strings.EqualFold(filepath.Ext(name), ".tga"):
		img, err := DecodeTGA(data)
		if err != nil {
			return nil, err
		}
		return img, nil
	}
	return nil, fmt.Errorf("%w: formato desconocido de %s", errNotPanel, name)
}
