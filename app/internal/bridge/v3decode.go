package bridge

import (
	"fmt"
	"math"
)

// fitCostBlocks is fitCost over the first nb blocks of the row at pitch p. Blocks whose
// interior reaches past the row are skipped.
func (s *v3Image) fitCostBlocks(p float64, nb int) int {
	y := rowFor(p)
	cost := 0
	for i := 0; i < nb; i++ {
		lo := int(math.Ceil(float64(i)*p)) + 1
		hi := int(math.Floor(float64(i+1)*p)) - 2
		if hi < lo {
			continue
		}
		if hi >= s.w {
			break
		}
		var mn, mx [3]int
		for x := lo; x <= hi; x++ {
			for ch := 0; ch < 3; ch++ {
				v := int(s.pix[y*s.w+x][ch])
				if x == lo || v < mn[ch] {
					mn[ch] = v
				}
				if x == lo || v > mx[ch] {
					mx[ch] = v
				}
			}
		}
		for ch := 0; ch < 3; ch++ {
			if e := mx[ch] - mn[ch] - noiseSlack; e > 0 {
				cost += e
			}
		}
	}
	return cost
}

// fitCostV3 is fitCost for the first v3FitBlocks blocks of the row: the coarse pitch search.
func (s *v3Image) fitCostV3(p float64) int {
	nb := s.blocksFit(p)
	if nb > v3FitBlocks {
		nb = v3FitBlocks
	}
	return s.fitCostBlocks(p, nb)
}

// refinePitchN narrows a coarse pitch p0 to the one whose fit over the n blocks of the frame
// is lowest. The coarse search reads only 64 blocks, so its pitch can be off by 0.002 px,
// which moves the last of 860 samples by almost 2 px. The scan covers +-0.004 px in 0.0001
// px steps. A flat minimum (every pitch in it fits equally well) gives its middle.
func (s *v3Image) refinePitchN(p0 float64, n int) float64 {
	const span, step = 40, 0.0001
	bestCost := math.MaxInt32
	var best []float64
	for k := -span; k <= span; k++ {
		q := p0 + float64(k)*step
		if q < minBlockPx || q > maxBlockPx {
			continue
		}
		c := s.fitCostBlocks(q, n)
		switch {
		case c < bestCost:
			bestCost = c
			best = append(best[:0], q)
		case c == bestCost:
			best = append(best, q)
		}
	}
	if len(best) == 0 {
		return p0
	}
	return best[len(best)/2]
}

// calibrateAt checks the magic blocks 0..1 at pitch p and builds the calibration from blocks
// 0..4 and the first data blocks. It returns the calibration and the colours of blocks 0..4.
func (s *v3Image) calibrateAt(p float64) ([3]levelMap, [5][3]byte, error) {
	y := rowFor(p)
	var cal5 [5][3]byte
	for i := 0; i < calibrationBlocks; i++ {
		cal5[i] = s.sample(i, p, y)
	}
	if !withinTolerance(cal5[0], magic0, magicTolerance) || !withinTolerance(cal5[1], magic1, magicTolerance) {
		return [3]levelMap{}, cal5, ErrNoMagic
	}
	if err := gainError(calExpected[:], cal5[:]); err != nil {
		return [3]levelMap{}, cal5, err
	}
	k := s.blocksFit(p) - calibrationBlocks
	if k > v3FitBlocks {
		k = v3FitBlocks
	}
	if k < 3 {
		return [3]levelMap{}, cal5, fmt.Errorf("%w: row of %d px is too short at pitch %.2f", ErrBadImage, s.w, p)
	}
	data := make([][3]byte, k)
	for i := range data {
		data[i] = s.sample(calibrationBlocks+i, p, y)
	}
	cal, err := calibrationV3(cal5, data)
	return cal, cal5, err
}

// headerAt reads the frame's block count from the first blocks of the stream at pitch p.
// The first samples are accurate at any pitch the coarse search accepts, so this is the
// step that tells how far the row goes.
func (s *v3Image) headerAt(p float64) (int, error) {
	cal, _, err := s.calibrateAt(p)
	if err != nil {
		return 0, err
	}
	y := rowFor(p)
	hdrBlocks := make([][3]byte, 3)
	for i := range hdrBlocks {
		hdrBlocks[i] = s.sample(calibrationBlocks+i, p, y)
	}
	hdr := streamFromBlocks(hdrBlocks, cal, 4)
	if hdr[0] != ProtocolV3 {
		return 0, fmt.Errorf("%w: got %d, want %d", ErrBadVersion, hdr[0], ProtocolV3)
	}
	n := int(hdr[2]) | int(hdr[3])<<8
	// The addon sizes the row as floor(clientWidth/4) blocks, so a header naming more than
	// that is not a frame of this window. A header naming more blocks than are on screen is
	// fine: the window's right edge may cut the row (see readAt).
	if n <= calibrationBlocks || n > MaxBlocksV3 || n > s.w/BlockPx+1 {
		return 0, fmt.Errorf("%w: header names %d blocks, the %d px row holds at most %d", ErrBadLength, n, s.w, s.w/BlockPx+1)
	}
	return n, nil
}

// visibleBlocks is how many blocks of the row at pitch p have their centre inside the row.
// A window whose right part is covered, or a row wider than the screen, shows only these.
func (s *v3Image) visibleBlocks(p float64) int {
	n := int(math.Ceil(float64(s.w)/p - 0.5))
	if n < 0 {
		return 0
	}
	return n
}

// readAt decodes the frame of n blocks at pitch p from the blocks that are on screen. When
// the row is cut short (n*p beyond the row), the frame's CRC-16 covers bytes that are not
// there: only a chunk that ends inside the visible part decodes, and bigger ones fail with
// ErrBadLength. The CRCs decide whether the frame is right.
func (s *v3Image) readAt(p float64, n int) (FrameV3, error) {
	cal, _, err := s.calibrateAt(p)
	if err != nil {
		return FrameV3{}, err
	}
	y := rowFor(p)
	last := n
	if vis := s.visibleBlocks(p); vis < last {
		last = vis
	}
	if last <= calibrationBlocks {
		return FrameV3{}, fmt.Errorf("%w: only %d blocks of the frame are on screen", ErrBadLength, last)
	}
	all := make([][3]byte, last-calibrationBlocks)
	for i := range all {
		all[i] = s.sample(calibrationBlocks+i, p, y)
	}
	stream := streamFromBlocks(all, cal, len(all)*3/2)
	f, err := ParseFrameV3(stream)
	if err != nil {
		return FrameV3{}, err
	}
	f.N = n
	return f, nil
}

// decodeV3AtPitch decodes the frame for a coarse pitch p0: header, then the fine pitch over
// the frame's blocks, then the whole frame. It returns the frame and the refined pitch.
func (s *v3Image) decodeV3AtPitch(p0 float64) (FrameV3, float64, error) {
	n, err := s.headerAt(p0)
	if err != nil {
		return FrameV3{}, 0, err
	}
	p := s.refinePitchN(p0, n)
	f, err := s.readAt(p, n)
	if err != nil {
		return FrameV3{}, 0, err
	}
	return f, p, nil
}
