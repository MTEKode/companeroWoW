package bridge

import (
	"fmt"
	"math"
)

// Protocol v3.1 (version byte 4): the v3 frame with a colour layout that survives the client's
// colour gain. The frame bytes are the same as v3; only the strip differs:
//
//   - data levels are limited to the low and middle range: nibble n is the colour value
//     round(n * 110 / 15), so 0..110 instead of 0..255. A gain up to x2.2 keeps the top level
//     below 255 (242), so it never clips;
//   - the seven fixed blocks are the two magic blocks, four calibration blocks and one white
//     block. The calibration blocks show the levels 0, 37, 73 and 110 in a Latin square, so each
//     channel sees all four levels; the white block is the top reference (255) and a sentinel
//     for clipping;
//   - the data nibbles start at block 7 (0-based), so the capacity is floor(3*(N-7)/2) - 47.
//
// The decoder reads the calibration blocks as a colour model: an affine map from levels to
// observed colours, o = A e + b. It is fitted over the calibration blocks that do not clip, so a
// per-channel gain, an offset and a cross-channel change (digital vibrance) are all undone by the
// same inverse. The nibble of a channel is its nearest level.
const (
	ProtocolV31 = 4

	// calibrationBlocksV31 is the number of fixed blocks at the start of a v3.1 row.
	calibrationBlocksV31 = 7
	// nibbleTopV31 is the colour value of nibble 15 (the top data level).
	nibbleTopV31 = 110
	// whiteBlockV31 is the index of the white reference block.
	whiteBlockV31 = 6
	// calibrationRMSMax is the RMS error, in colour values, above which the fixed blocks of a row
	// are not the strip's own (see calibrateV31).
	calibrationRMSMax = 12
	// whiteTolerance is how far the white block may stray from the model before the
	// calibration is refused (only for channels that do not clip).
	whiteTolerance = 24
)

// calExpectedV31 are the known colours of the fixed blocks 0..6 of v3.1.
var calExpectedV31 = [calibrationBlocksV31][3]byte{
	magic0, magic1,
	{0, 37, 73}, {37, 73, 110}, {73, 110, 0}, {110, 0, 37},
	{255, 255, 255},
}

// NibbleValueV31 is the colour value of nibble n (0..15) in v3.1: round(n * 110 / 15).
func NibbleValueV31(n byte) byte {
	return byte(math.Round(float64(n) * nibbleTopV31 / 15))
}

// CapacityV31 is the number of chunk data bytes a v3.1 frame with n blocks carries.
func CapacityV31(n int) int {
	if n <= calibrationBlocksV31 {
		return 0
	}
	c := 3*(n-calibrationBlocksV31)/2 - v3Overhead
	if c < 0 {
		return 0
	}
	return c
}

// BuildFrameV31 composes the byte stream of one v3.1 frame: the v3 layout with version 4 and
// the v3.1 data capacity. A nil chunk means nothing pending.
func BuildFrameV31(s LiveState, seq uint8, n int, chunk *ChunkV3) ([]byte, error) {
	return buildFrame(ProtocolV31, calibrationBlocksV31, s, seq, n, chunk)
}

// BlocksV31 returns the colours of the n blocks of a v3.1 frame: blocks 0..6 are the fixed
// colours, blocks 7..n-1 the nibbles of frame (missing nibbles are 0).
func BlocksV31(frame []byte, n int) [][3]byte {
	return nibbleBlocks(frame, n, calExpectedV31[:], calibrationBlocksV31, NibbleValueV31)
}

// nibbleOfV31 rounds a level to its nibble, 0..15.
func nibbleOfV31(e float64) byte {
	return byte(math.Max(0, math.Min(15, math.Round(e*15/nibbleTopV31))))
}

// calibrateV31 checks the seven fixed colours of a v3.1 row and returns the colour model. A
// gain outside the decodable range, or clipping, is a ColourGainError. Calibration blocks
// with a channel at or above clipLevel are left out of the fit; white is then checked against
// the model.
func calibrateV31(seen [][3]byte) (colourModel, error) {
	if err := gainError(calExpectedV31[:], seen); err != nil {
		return colourModel{}, err
	}
	known := calExpectedV31[:]
	m, ok := fitColourModel(known, seen)
	if !ok {
		return colourModel{}, fmt.Errorf("%w: the calibration colours do not fix the colour model", ErrBadCalibration)
	}
	// A pitch that is not the strip's own shows colours the model cannot fit: the fixed blocks
	// of a true strip fit to a few units. Refusing here keeps the wrong pitches of the search
	// away from the full frame decode.
	if modelRMS(m, known, seen) > calibrationRMSMax {
		return colourModel{}, fmt.Errorf("%w: the fixed blocks do not fit a colour model", ErrNoMagic)
	}
	pred := m.forward([3]float64{255, 255, 255})
	for r := 0; r < 3; r++ {
		w := float64(seen[whiteBlockV31][r])
		if w < clipLevel && math.Abs(pred[r]-w) > whiteTolerance {
			return colourModel{}, fmt.Errorf("%w: white reads %d where the model says %.0f", ErrBadCalibration, seen[whiteBlockV31][r], pred[r])
		}
	}
	return m, nil
}

// streamFromModel turns data blocks into the first nbytes bytes of the nibble stream, through
// the colour model.
func streamFromModel(blocks [][3]byte, m colourModel, nbytes int) []byte {
	nib := make([]byte, 0, len(blocks)*3)
	for _, blk := range blocks {
		n := m.nibbles(blk)
		nib = append(nib, n[0], n[1], n[2])
	}
	out := make([]byte, 0, nbytes)
	for j := 0; j < nbytes && 2*j+1 < len(nib); j++ {
		out = append(out, nib[2*j]<<4|nib[2*j+1])
	}
	return out
}

// magicShapeV31 recognises the magic blocks by their pattern, not by their colour: magic 0 is
// magenta (high R and B, low G) and magic 1 cyan (high G and B, low R), whatever the gain. An
// absolute match would fail at a gain of 0.6, where (255,0,255) reads (153,0,153).
func magicShapeV31(m0, m1 [3]byte) bool {
	hi := func(v byte) bool { return v >= 40 }
	lo := func(v byte) bool { return v <= 60 }
	near := func(a, b byte) bool { return math.Abs(float64(a)-float64(b)) <= 40 }
	return hi(m0[0]) && hi(m0[2]) && lo(m0[1]) && near(m0[0], m0[2]) &&
		hi(m1[1]) && hi(m1[2]) && lo(m1[0]) && near(m1[1], m1[2])
}

// blockMean is the mean colour of block i at pitch p over the pixels the block fully covers:
// the rows 0..floor(p)-1 and the columns ceil(i*p)..floor((i+1)*p)-1 (a row or column only partly
// inside the block is left out, so blended edges do not count). The frame starts on a whole
// pixel, so these pixels are exact. Averaging 8 to 30 of them keeps the nibbles inside the noise
// of a scaled client. A pitch too small for that falls back to the centre sample (see sample).
func (s *v3Image) blockMean(i int, p float64) [3]byte {
	x0 := int(math.Ceil(float64(i) * p))
	x1 := int(math.Floor(float64(i+1)*p)) - 1
	if x1 >= s.w {
		x1 = s.w - 1
	}
	y0, y1 := 0, int(math.Floor(p))-1
	var sum [3]float64
	n := 0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			px := s.pix[y*s.w+x]
			for c := 0; c < 3; c++ {
				sum[c] += float64(px[c])
			}
			n++
		}
	}
	if n == 0 {
		return s.sample(i, p, rowFor(p))
	}
	var out [3]byte
	for c := 0; c < 3; c++ {
		out[c] = byte(math.Round(sum[c] / float64(n)))
	}
	return out
}

// calibrateAtV31 checks the magic blocks at pitch p and builds the colour model from the fixed
// blocks.
func (s *v3Image) calibrateAtV31(p float64) (colourModel, error) {
	var fixed [calibrationBlocksV31][3]byte
	for i := range fixed {
		fixed[i] = s.blockMean(i, p)
	}
	if !magicShapeV31(fixed[0], fixed[1]) {
		return colourModel{}, ErrNoMagic
	}
	if k := s.blocksFit(p) - calibrationBlocksV31; k < 3 {
		return colourModel{}, fmt.Errorf("%w: row of %d px is too short at pitch %.2f", ErrBadImage, s.w, p)
	}
	return calibrateV31(fixed[:])
}

// headerV31 reads the frame's block count from the first data blocks at pitch p.
func (s *v3Image) headerV31(p float64) (int, error) {
	m, err := s.calibrateAtV31(p)
	if err != nil {
		return 0, err
	}
	hdrBlocks := make([][3]byte, 3)
	for i := range hdrBlocks {
		hdrBlocks[i] = s.blockMean(calibrationBlocksV31+i, p)
	}
	hdr := streamFromModel(hdrBlocks, m, 4)
	if hdr[0] != ProtocolV31 {
		return 0, fmt.Errorf("%w: got %d, want %d", ErrBadVersion, hdr[0], ProtocolV31)
	}
	n := int(hdr[2]) | int(hdr[3])<<8
	if n <= calibrationBlocksV31 || n > MaxBlocksV3 || n > s.w/BlockPx+1 {
		return 0, fmt.Errorf("%w: header names %d blocks, the %d px row holds at most %d", ErrBadLength, n, s.w, s.w/BlockPx+1)
	}
	return n, nil
}

// readAtV31 decodes the v3.1 frame of n blocks at pitch p from the blocks on screen (see readAt).
func (s *v3Image) readAtV31(p float64, n int) (FrameV3, error) {
	m, err := s.calibrateAtV31(p)
	if err != nil {
		return FrameV3{}, err
	}
	last := n
	if vis := s.visibleBlocks(p); vis < last {
		last = vis
	}
	if last <= calibrationBlocksV31 {
		return FrameV3{}, fmt.Errorf("%w: only %d blocks of the frame are on screen", ErrBadLength, last)
	}
	all := make([][3]byte, last-calibrationBlocksV31)
	for i := range all {
		all[i] = s.blockMean(calibrationBlocksV31+i, p)
	}
	f, err := parseFrame(streamFromModel(all, m, len(all)*3/2), ProtocolV31)
	if err != nil {
		return FrameV3{}, err
	}
	f.N = n
	return f, nil
}

// decodeV31AtPitch decodes the v3.1 frame for a coarse pitch p0: header, fine pitch, frame.
func (s *v3Image) decodeV31AtPitch(p0 float64) (FrameV3, float64, error) {
	n, err := s.headerV31(p0)
	if err != nil {
		return FrameV3{}, 0, err
	}
	p := s.refinePitchN(p0, n)
	f, err := s.readAtV31(p, n)
	if err != nil {
		return FrameV3{}, 0, err
	}
	return f, p, nil
}

// solve4 solves the 4x4 system a x = b by Gaussian elimination with partial pivoting. ok is
// false when the system is singular.
func solve4(a [4][4]float64, b [4]float64) ([4]float64, bool) {
	for col := 0; col < 4; col++ {
		piv := col
		for r := col + 1; r < 4; r++ {
			if math.Abs(a[r][col]) > math.Abs(a[piv][col]) {
				piv = r
			}
		}
		if math.Abs(a[piv][col]) < 1e-9 {
			return [4]float64{}, false
		}
		a[col], a[piv] = a[piv], a[col]
		b[col], b[piv] = b[piv], b[col]
		for r := col + 1; r < 4; r++ {
			f := a[r][col] / a[col][col]
			for c := col; c < 4; c++ {
				a[r][c] -= f * a[col][c]
			}
			b[r] -= f * b[col]
		}
	}
	var x [4]float64
	for r := 3; r >= 0; r-- {
		s := b[r]
		for c := r + 1; c < 4; c++ {
			s -= a[r][c] * x[c]
		}
		x[r] = s / a[r][r]
	}
	return x, true
}

// invert3 returns the inverse of a 3x3 matrix; ok is false when it is singular.
func invert3(a [3][3]float64) ([3][3]float64, bool) {
	det := a[0][0]*(a[1][1]*a[2][2]-a[1][2]*a[2][1]) -
		a[0][1]*(a[1][0]*a[2][2]-a[1][2]*a[2][0]) +
		a[0][2]*(a[1][0]*a[2][1]-a[1][1]*a[2][0])
	if math.Abs(det) < 1e-9 {
		return [3][3]float64{}, false
	}
	var inv [3][3]float64
	inv[0][0] = (a[1][1]*a[2][2] - a[1][2]*a[2][1]) / det
	inv[0][1] = (a[0][2]*a[2][1] - a[0][1]*a[2][2]) / det
	inv[0][2] = (a[0][1]*a[1][2] - a[0][2]*a[1][1]) / det
	inv[1][0] = (a[1][2]*a[2][0] - a[1][0]*a[2][2]) / det
	inv[1][1] = (a[0][0]*a[2][2] - a[0][2]*a[2][0]) / det
	inv[1][2] = (a[0][2]*a[1][0] - a[0][0]*a[1][2]) / det
	inv[2][0] = (a[1][0]*a[2][1] - a[1][1]*a[2][0]) / det
	inv[2][1] = (a[0][1]*a[2][0] - a[0][0]*a[2][1]) / det
	inv[2][2] = (a[0][0]*a[1][1] - a[0][1]*a[1][0]) / det
	return inv, true
}
