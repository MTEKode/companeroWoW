package bridge

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"math"
)

// The data panel of a screenshot (the SCREENSHOT CHANNEL CONTRACT). The addon draws the panel
// full-opacity at the top-left of the screen, with square blocks of 2 physical px (4 px for the
// JPEG fallback). Each row of blocks is one screenshot frame (see frame_v5.go), the rows are
// stacked downwards, and the last row is the end frame. The screenshot is lossless, so the
// decoder reads the blocks at their pitch with no colour gain to undo beyond the v3.1 model.
const (
	// panelMinPitch and panelMaxPitch bound the block size searched, in physical px.
	panelMinPitch = 1.5
	panelMaxPitch = 8.0
	// panelPitchStep is the pitch resolution of the search. The candidates that decode are
	// averaged (their median is taken), so a grid aligned to the image is found exactly.
	panelPitchStep = 0.002
	// panelMaxRows bounds the rows read: 512 rows of 2 px blocks is 1024 px, past the panel's
	// largest height.
	panelMaxRows = 512
)

// ErrNoPanel reports that the top-left of an image holds no data panel.
var ErrNoPanel = errors.New("bridge: no hay panel de datos en la esquina superior izquierda")

// PanelResult is what DecodePanel read from a screenshot.
type PanelResult struct {
	// Frames are the rows read, in order. The end frame is the last one when Complete.
	Frames []FrameV3
	// ShotID is the shot id shared by the rows.
	ShotID uint16
	// Pitch is the block size found, in physical px.
	Pitch float64
	// Complete is true when the end frame was read and its row count matches the rows above it.
	Complete bool
	// Stop is why the rows ended before a complete panel (nil when Complete): a row that did not
	// decode, a row of another shot, a count that does not match, or the image edge.
	Stop error
}

// DecodePanel reads the data panel at the top-left of img. It returns ErrNoPanel (wrapped) when
// no pitch gives a valid first row with a screenshot version byte; otherwise the rows read.
//
// The first row tells which pitches decode. A pitch a few thousandths off still decodes the
// first rows, and drifts off the grid further down, so each candidate reads the whole panel and
// the one that reads the most rows (a complete panel first) wins. Ties take their middle.
func DecodePanel(img image.Image) (PanelResult, error) {
	if img == nil {
		return PanelResult{}, fmt.Errorf("%w: nil image", ErrBadImage)
	}
	rgba := toRGBA(img)
	b := rgba.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 16 || h < 2 {
		return PanelResult{}, fmt.Errorf("%w: %dx%d is too small for a data panel", ErrBadImage, w, h)
	}
	view := &v3Image{w: w, pix: make([][3]byte, w*StripHeightV3)}
	view.fillFrom(rgba, 0, StripHeightV3)

	var cands []float64
	var lastErr error
	steps := int(math.Round((panelMaxPitch - panelMinPitch) / panelPitchStep))
	for k := 0; k <= steps; k++ {
		p := panelMinPitch + float64(k)*panelPitchStep
		if _, err := view.decodePanelRow(p); err != nil {
			lastErr = err
			continue
		}
		cands = append(cands, p)
	}
	if len(cands) == 0 {
		return PanelResult{}, fmt.Errorf("%w: %v", ErrNoPanel, lastErr)
	}
	// A wide spread of candidates is thinned evenly: the decodable band of a noisy panel is
	// narrow, and the reads are the cost.
	const maxCands = 48
	if len(cands) > maxCands {
		thin := make([]float64, 0, maxCands)
		for i := 0; i < maxCands; i++ {
			thin = append(thin, cands[i*len(cands)/maxCands])
		}
		cands = thin
	}
	bestScore := -1
	var ties []int
	results := make([]PanelResult, len(cands))
	for i, p := range cands {
		results[i] = readPanelAt(rgba, view, p, h)
		score := len(results[i].Frames)
		if results[i].Complete {
			score += 1 << 20
		}
		switch {
		case score > bestScore:
			bestScore, ties = score, []int{i}
		case score == bestScore:
			ties = append(ties, i)
		}
	}
	return results[ties[len(ties)/2]], nil
}

// readPanelAt reads the rows of the panel at pitch p, from row 0 down. The first row must decode
// (the caller checked it); the rows stop at the end frame, at a row that does not decode, or at the
// image edge.
func readPanelAt(rgba *image.RGBA, view *v3Image, p float64, h int) PanelResult {
	view.fillFrom(rgba, 0, panelRowsFor(p))
	first, err := view.decodePanelRow(p)
	if err != nil {
		return PanelResult{Pitch: p, Stop: err}
	}
	res := PanelResult{Pitch: p, ShotID: first.ShotID, Frames: []FrameV3{first}}
	if IsEndFrame(first) {
		res.Stop = fmt.Errorf("el panel dice %d filas y la primera ya es el final", first.ChunkCount)
		return res
	}
	for r := 1; r < panelMaxRows; r++ {
		oy := int(math.Round(float64(r) * p))
		if oy >= h {
			res.Stop = fmt.Errorf("la imagen acaba antes de la fila final (fila %d)", r)
			return res
		}
		view.fillFrom(rgba, oy, panelRowsFor(p))
		f, err := view.decodePanelRow(p)
		if err != nil {
			res.Stop = fmt.Errorf("fila %d: %w", r, err)
			return res
		}
		if f.ShotID != res.ShotID || f.N != first.N {
			res.Stop = fmt.Errorf("fila %d: de otra captura (id %d, %d bloques)", r, f.ShotID, f.N)
			return res
		}
		res.Frames = append(res.Frames, f)
		if IsEndFrame(f) {
			res.Complete = int(f.ChunkCount) == len(res.Frames)-1
			if !res.Complete {
				res.Stop = fmt.Errorf("la fila final dice %d filas y se leyeron %d", f.ChunkCount, len(res.Frames)-1)
			}
			return res
		}
	}
	res.Stop = fmt.Errorf("no hay fila final en %d filas", panelMaxRows)
	return res
}

// toRGBA returns img as an RGBA image whose bounds start at (0,0).
func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok && r.Rect.Min == (image.Point{}) {
		return r
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

// panelRowsFor is how many pixel rows a block row at pitch p reads: a block is p rows high, and
// the rows past it are never read. Copying only these keeps the reads of a long panel cheap.
func panelRowsFor(p float64) int {
	return min(StripHeightV3, int(math.Ceil(p))+1)
}

// fillFrom loads the first rows pixel rows of img from y0 into the view. Rows outside the image
// are black; the rows past rows keep their old values, which the decoder does not read.
func (s *v3Image) fillFrom(img *image.RGBA, y0, rows int) {
	h := img.Rect.Dy()
	for y := 0; y < rows; y++ {
		sy := y0 + y
		row := s.pix[y*s.w : (y+1)*s.w]
		if sy < 0 || sy >= h {
			clear(row)
			continue
		}
		src := img.Pix[sy*img.Stride:]
		for x := range row {
			o := 4 * x
			row[x] = [3]byte{src[o], src[o+1], src[o+2]}
		}
	}
}

// panelBlockMean is the mean colour of block i at pitch p, leaving out a rim of the block for
// pitches of 4 px and more: a JPEG's ringing sits on the block edges, so the middle is read.
// Below 4 px the whole block is used (the same pixels as blockMean).
func (s *v3Image) panelBlockMean(i int, p float64) [3]byte {
	margin := int(p / 4)
	x0 := int(math.Ceil(float64(i)*p)) + margin
	x1 := int(math.Floor(float64(i+1)*p)) - 1 - margin
	y0, y1 := margin, int(math.Floor(p))-1-margin
	if x1 >= s.w {
		x1 = s.w - 1
	}
	if x1 < x0 || y1 < y0 {
		return s.blockMean(i, p)
	}
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
	var out [3]byte
	for c := 0; c < 3; c++ {
		out[c] = byte(math.Round(sum[c] / float64(n)))
	}
	return out
}

// decodePanelRow reads the screenshot frame of the row whose blocks are at pitch p, from the
// top of the view. It checks the magic and calibration blocks, the version byte and the CRC.
func (s *v3Image) decodePanelRow(p float64) (FrameV3, error) {
	var fixed [calibrationBlocksV31][3]byte
	for i := range fixed {
		fixed[i] = s.panelBlockMean(i, p)
	}
	if !magicShapeV31(fixed[0], fixed[1]) {
		return FrameV3{}, ErrNoMagic
	}
	if k := s.blocksFit(p) - calibrationBlocksV31; k < 4 {
		return FrameV3{}, fmt.Errorf("%w: row of %d px is too short at pitch %.3f", ErrBadImage, s.w, p)
	}
	cal, err := fitPanelCal(fixed)
	if err != nil {
		return FrameV3{}, err
	}
	hdrBlocks := make([][3]byte, 4)
	for i := range hdrBlocks {
		hdrBlocks[i] = s.panelBlockMean(calibrationBlocksV31+i, p)
	}
	hdr := cal.stream(hdrBlocks, 6)
	if hdr[0] != ProtocolV5 {
		return FrameV3{}, fmt.Errorf("%w: got %d, want %d", ErrBadVersion, hdr[0], ProtocolV5)
	}
	n := int(hdr[2]) | int(hdr[3])<<8
	if n <= calibrationBlocksV31 || n > MaxBlocksV3 || n > s.blocksFit(p)+1 {
		return FrameV3{}, fmt.Errorf("%w: header names %d blocks, the %d px row holds at most %d", ErrBadLength, n, s.w, s.blocksFit(p))
	}
	last := n
	if vis := s.visibleBlocks(p); vis < last {
		last = vis
	}
	if last <= calibrationBlocksV31 {
		return FrameV3{}, fmt.Errorf("%w: only %d blocks of the row are on screen", ErrBadLength, last)
	}
	all := make([][3]byte, last-calibrationBlocksV31)
	for i := range all {
		all[i] = s.panelBlockMean(calibrationBlocksV31+i, p)
	}
	f, err := ParseFrameV5(cal.stream(all, len(all)*3/2))
	if err != nil {
		return FrameV3{}, err
	}
	f.N = n
	return f, nil
}

// panelCal is the colour calibration of one panel row: per channel, the observed value is
// o = a*level + b, fitted by least squares over the seven fixed blocks. A panel is lossless, so
// no gain, offset or cross-channel change has to be undone; the per-channel fit is far less
// sensitive to the noise of the calibration blocks than the full colour model the screen
// strip uses (see colour.go), whose cross-channel terms can flip a nibble at +-2 noise.
type panelCal struct {
	a, b [3]float64
}

// panelCalRMSMax is the RMS residual, in colour values, above which the fixed blocks of a row
// are not a panel's (a true panel fits to about one unit per channel).
const panelCalRMSMax = 6

// fitPanelCal fits the per-channel calibration of a row from its fixed blocks.
func fitPanelCal(fixed [calibrationBlocksV31][3]byte) (panelCal, error) {
	var cal panelCal
	var rss float64
	const k = float64(calibrationBlocksV31)
	for c := 0; c < 3; c++ {
		var sx, sy, sxx, sxy float64
		for i := range fixed {
			x := float64(calExpectedV31[i][c])
			y := float64(fixed[i][c])
			sx += x
			sy += y
			sxx += x * x
			sxy += x * y
		}
		den := k*sxx - sx*sx
		if den < 1e-9 {
			return panelCal{}, fmt.Errorf("%w: channel %d has one level", ErrBadCalibration, c)
		}
		a := (k*sxy - sx*sy) / den
		b := (sy - a*sx) / k
		if a < 0.5 {
			return panelCal{}, fmt.Errorf("%w: channel %d runs the wrong way (slope %.2f)", ErrBadCalibration, c, a)
		}
		cal.a[c], cal.b[c] = a, b
		for i := range fixed {
			r := float64(fixed[i][c]) - (a*float64(calExpectedV31[i][c]) + b)
			rss += r * r
		}
	}
	if rms := math.Sqrt(rss / (3 * k)); rms > panelCalRMSMax {
		return panelCal{}, fmt.Errorf("%w: the fixed blocks fit a row with %.1f of error", ErrNoMagic, rms)
	}
	return cal, nil
}

// nibble returns the nibble of channel c for an observed value: the level is (o-b)/a, and the
// nibble is the level's place on the scale 0..110 in 15 steps.
func (cal panelCal) nibble(c int, o byte) byte {
	level := (float64(o) - cal.b[c]) / cal.a[c]
	return byte(math.Max(0, math.Min(15, math.Round(level*15/nibbleTopV31))))
}

// stream turns n data blocks (3 nibbles each, R G B, high nibble first) into the first nbytes
// bytes of the frame stream.
func (cal panelCal) stream(blocks [][3]byte, nbytes int) []byte {
	nib := make([]byte, 0, 3*len(blocks))
	for _, blk := range blocks {
		for c := 0; c < 3; c++ {
			nib = append(nib, cal.nibble(c, blk[c]))
		}
	}
	out := make([]byte, 0, nbytes)
	for j := 0; j < nbytes && 2*j+1 < len(nib); j++ {
		out = append(out, nib[2*j]<<4|nib[2*j+1])
	}
	return out
}
