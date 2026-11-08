package bridge

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"
)

// Pitch search tuning.
const (
	// noiseSlack is the spread the +-3 noise of a scaled client can add to one block
	// channel. Spread above it inside a block counts as misalignment.
	noiseSlack = 6
	// maxFitCost bounds the misalignment a pitch may show and still be decoded. The
	// true pitch scores 0 on a clean strip. A pitch 0.05 px off already scores in the
	// hundreds, and a pitch twice the true one in the thousands.
	maxFitCost = 60
)

// strip is the StripWidth x StripHeight region at the top-left of a screen image,
// copied once so that the pitch search reads plain bytes.
type strip [StripHeight][StripWidth][3]byte

func loadStrip(img image.Image) *strip {
	var s strip
	b := img.Bounds()
	for y := 0; y < StripHeight; y++ {
		for x := 0; x < StripWidth; x++ {
			s[y][x] = pixelRGB(img, b.Min.X+x, b.Min.Y+y)
		}
	}
	return &s
}

// DecodeImage reads the strip from the top-left corner of img (region StripWidth x StripHeight).
//
// The block pitch is not read off the colour edges: the scaling blends them, and a pitch
// off by a few hundredths of a pixel drifts by a pixel within a few blocks. Instead:
//
//  1. Each pitch from minBlockPx to maxBlockPx in pitchStep steps is scored by fitCost:
//     the colour spread inside the blocks it puts on the strip (from the left edge, block
//     i spanning [i*p, (i+1)*p)). The true pitch scores 0 on a clean strip.
//  2. Pitches with a fitCost of at most maxFitCost are decoded, lowest cost first. Block i
//     is sampled at its centre floor((i+0.5)*p). Blocks 0 and 1 must show the magic
//     colours, and the blocks must decode with a valid CRC.
//  3. The first pitch that passes gives the result.
//
// The fit is what keeps the CRC from deciding alone. The version and length bytes lie in
// the first blocks, which hardly move for a slightly wrong pitch, so a wrong pitch that
// samples the same stream passes them and the CRC with probability 1/256. Such a pitch
// has a large fitCost, so it is never decoded.
//
// Pixels outside the strip are ignored.
func DecodeImage(img image.Image) (LiveState, error) {
	st, _, err := decodeStrip(img, 0)
	return st, err
}

// decodeStrip is DecodeImage with a pitch hint. When hint is a pitch that decoded last
// time, it is tried first (if it still fits and decodes); only on failure does the full
// search run. It returns the pitch that decoded, so the caller can pass it back next time.
func decodeStrip(img image.Image, hint float64) (LiveState, float64, error) {
	if img == nil {
		return LiveState{}, 0, fmt.Errorf("%w: nil image", ErrBadImage)
	}
	b := img.Bounds()
	if b.Dx() < StripWidth || b.Dy() < StripHeight {
		return LiveState{}, 0, fmt.Errorf("%w: %dx%d is smaller than %dx%d", ErrBadImage, b.Dx(), b.Dy(), StripWidth, StripHeight)
	}

	s := loadStrip(img)
	if hint > 0 && s.fitCost(hint) <= maxFitCost {
		if st, err := s.decodeAtPitch(hint); err == nil {
			return st, hint, nil
		}
	}
	steps := int(math.Round((maxBlockPx - minBlockPx) / pitchStep))
	type candidate struct {
		k    int
		cost int
	}
	var cands []candidate
	for k := 0; k <= steps; k++ {
		if c := s.fitCost(minBlockPx + float64(k)*pitchStep); c <= maxFitCost {
			cands = append(cands, candidate{k, c})
		}
	}
	if len(cands) == 0 {
		return LiveState{}, 0, fmt.Errorf("%w: no pitch in [%.1f, %d] gives uniform blocks", ErrNoMagic, minBlockPx, maxBlockPx)
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].cost < cands[j].cost })

	var firstErr error
	for _, c := range cands {
		p := minBlockPx + float64(c.k)*pitchStep
		st, err := s.decodeAtPitch(p)
		if err == nil {
			return st, p, nil
		}
		if firstErr == nil && !errors.Is(err, ErrNoMagic) {
			firstErr = err
		}
	}
	if firstErr != nil {
		return LiveState{}, 0, firstErr
	}
	return LiveState{}, 0, fmt.Errorf("%w: blocks 0-1 are not the magic colours at any pitch in [%.1f, %d]",
		ErrNoMagic, minBlockPx, maxBlockPx)
}

// rowFor is the row a pitch p is sampled on: the middle of the first block row.
func rowFor(p float64) int {
	return int(math.Floor(p / 2))
}

// fitCost sums, over the blocks that pitch p puts on the strip, how far the colour
// spread of each block's interior exceeds noiseSlack. The interior of a block leaves out
// one pixel at each edge, which holds the blended edge pixels. A block whose interior
// reaches across a real edge has a large spread. Blocks too narrow for an interior are
// skipped.
func (s *strip) fitCost(p float64) int {
	y := rowFor(p)
	cost := 0
	for i := 0; i < NumBlocks; i++ {
		lo := int(math.Ceil(float64(i)*p)) + 1
		hi := int(math.Floor(float64(i+1)*p)) - 2
		if hi < lo {
			continue
		}
		var mn, mx [3]int
		for x := lo; x <= hi; x++ {
			for ch := 0; ch < 3; ch++ {
				v := int(s[y][x][ch])
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

// decodeAtPitch samples the blocks at pitch p and decodes them. The magic blocks must
// show the expected colours, and the CRC must match.
func (s *strip) decodeAtPitch(p float64) (LiveState, error) {
	y := rowFor(p)
	var blocks [NumBlocks][3]byte
	blocks[0] = s.sampleBlock(0, p, y)
	blocks[1] = s.sampleBlock(1, p, y)
	if !withinTolerance(blocks[0], magic0, magicTolerance) || !withinTolerance(blocks[1], magic1, magicTolerance) {
		return LiveState{}, ErrNoMagic
	}
	for i := 2; i < NumBlocks; i++ {
		blocks[i] = s.sampleBlock(i, p, y)
	}
	return decodeAuto(blocks)
}

// sampleBlock returns the average colour of block i at pitch p, read on row y. It uses
// the centre pixel, plus the left and right neighbours when p >= 3 and they lie
// strictly inside the block, so the soft edges do not reach the sample.
func (s *strip) sampleBlock(i int, p float64, y int) [3]byte {
	start := float64(i) * p
	end := float64(i+1) * p
	c := int(math.Floor((float64(i) + 0.5) * p))
	var sum [3]float64
	n := 0
	add := func(x int) {
		for ch := 0; ch < 3; ch++ {
			sum[ch] += float64(s[y][x][ch])
		}
		n++
	}
	add(c)
	if p >= 3 {
		for _, x := range [2]int{c - 1, c + 1} {
			if float64(x) >= start && float64(x)+1 <= end {
				add(x)
			}
		}
	}
	var out [3]byte
	for ch := 0; ch < 3; ch++ {
		out[ch] = byte(math.Round(sum[ch] / float64(n)))
	}
	return out
}
