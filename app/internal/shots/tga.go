package shots

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
)

// ErrTGA reports a TGA file this decoder cannot read.
var ErrTGA = errors.New("shots: TGA no soportado")

// maxTGASide bounds a TGA side, so a corrupt header cannot ask for gigabytes.
const maxTGASide = 16384

// DecodeTGA reads an uncompressed or run-length encoded TGA: true colour (16, 24 or 32 bits per
// pixel) or greyscale (8 or 16 bits), without a colour map. The origin (top or bottom, left or
// right) is honoured. Alpha is ignored: the screenshot panel is opaque.
func DecodeTGA(data []byte) (*image.RGBA, error) {
	if len(data) < 18 {
		return nil, fmt.Errorf("%w: %d bytes is shorter than the header", ErrTGA, len(data))
	}
	idLen := int(data[0])
	cmType := data[1]
	imgType := data[2]
	w := int(binary.LittleEndian.Uint16(data[12:]))
	h := int(binary.LittleEndian.Uint16(data[14:]))
	bpp := int(data[16])
	desc := data[17]
	if cmType != 0 {
		return nil, fmt.Errorf("%w: tabla de colores (tipo %d)", ErrTGA, cmType)
	}
	rle := false
	grey := false
	switch imgType {
	case 2:
	case 10:
		rle = true
	case 3:
		grey = true
	case 11:
		rle, grey = true, true
	default:
		return nil, fmt.Errorf("%w: tipo de imagen %d", ErrTGA, imgType)
	}
	bytesPP := bpp / 8
	switch {
	case grey && (bpp == 8 || bpp == 16):
	case !grey && (bpp == 16 || bpp == 24 || bpp == 32):
	default:
		return nil, fmt.Errorf("%w: %d bits por pixel", ErrTGA, bpp)
	}
	if bpp%8 != 0 {
		return nil, fmt.Errorf("%w: %d bits por pixel", ErrTGA, bpp)
	}
	if w <= 0 || h <= 0 || w > maxTGASide || h > maxTGASide {
		return nil, fmt.Errorf("%w: tamaño %dx%d", ErrTGA, w, h)
	}
	off := 18 + idLen
	if off > len(data) {
		return nil, fmt.Errorf("%w: el identificador pasa del archivo", ErrTGA)
	}
	total := w * h
	out := image.NewRGBA(image.Rect(0, 0, w, h))

	// put stores pixel i (in file order) at its place in the image.
	put := func(i int, px []byte) {
		x, y := i%w, i/w
		if desc&0x20 == 0 { // bottom-left origin: the file starts at the bottom row
			y = h - 1 - y
		}
		if desc&0x10 != 0 { // right origin
			x = w - 1 - x
		}
		var r, g, b byte
		switch {
		case grey && bytesPP == 1:
			r, g, b = px[0], px[0], px[0]
		case grey:
			v := px[0]
			r, g, b = v, v, v
		case bytesPP == 2:
			v := binary.LittleEndian.Uint16(px)
			r = expand5(v >> 10)
			g = expand5(v >> 5)
			b = expand5(v)
		default: // BGR or BGRA
			b, g, r = px[0], px[1], px[2]
		}
		o := out.PixOffset(x, y)
		out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = r, g, b, 255
	}

	pos := off
	n := 0
	if !rle {
		need := total * bytesPP
		if pos+need > len(data) {
			return nil, fmt.Errorf("%w: %d pixeles pasan del archivo", ErrTGA, total)
		}
		for ; n < total; n++ {
			put(n, data[pos+n*bytesPP:pos+(n+1)*bytesPP])
		}
		return out, nil
	}
	for n < total {
		if pos >= len(data) {
			return nil, fmt.Errorf("%w: la compresión acaba antes de la imagen", ErrTGA)
		}
		hdr := data[pos]
		pos++
		count := int(hdr&0x7f) + 1
		if n+count > total {
			return nil, fmt.Errorf("%w: un paquete pasa de la imagen", ErrTGA)
		}
		if hdr&0x80 != 0 { // run: one pixel repeated
			if pos+bytesPP > len(data) {
				return nil, fmt.Errorf("%w: paquete de repetición truncado", ErrTGA)
			}
			px := data[pos : pos+bytesPP]
			pos += bytesPP
			for k := 0; k < count; k++ {
				put(n, px)
				n++
			}
			continue
		}
		if pos+count*bytesPP > len(data) { // raw: count pixels follow
			return nil, fmt.Errorf("%w: paquete literal truncado", ErrTGA)
		}
		for k := 0; k < count; k++ {
			put(n, data[pos+k*bytesPP:pos+(k+1)*bytesPP])
			n++
		}
		pos += count * bytesPP
	}
	return out, nil
}

// expand5 scales a 5-bit channel to 8 bits.
func expand5(v uint16) byte {
	x := byte(v & 0x1f)
	return x<<3 | x>>2
}
