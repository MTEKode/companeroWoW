// Package bridge reads the pixel strip drawn by the CompaneroWoW addon (protocol v2)
// and the addon's SavedVariables file.
package bridge

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"
	"time"
)

// Protocol v2 layout constants.
//
// The strip is a horizontal row of NumBlocks solid squares, each nominally
// nominalBlockPx physical pixels wide. Each colour channel carries one nibble n
// (0..15) as the value n*17. Blocks 0..4 are fixed: magic, magic and three
// calibration blocks that together give every channel the levels 0, 85, 170 and 255.
// Blocks 5..28 carry 72 nibbles: the 35-byte stream
// [version, seq, payloadLen] ++ payload[32] (70 nibbles, high nibble first) plus
// 2 zero padding nibbles.
const (
	ProtocolVersion = 2
	PayloadLen      = 32
	NumBlocks       = 29
	nominalBlockPx  = 4  // block size on screen for a correctly scaled addon
	maxBlockPx      = 16 // largest block pitch the decoder accepts

	// StripWidth and StripHeight are the captured region: enough for a pitch of
	// maxBlockPx, so that every legitimate pitch is inside it.
	StripWidth  = NumBlocks * maxBlockPx // 464
	StripHeight = maxBlockPx             // 16

	minBlockPx     = 1.5
	nibbleStep     = 17
	magicTolerance = 40 // per channel, used to locate the magic runs
	pitchSearch    = 1.0
	pitchStep      = 0.004

	// Pitch fit (see fitCost): noiseSlack is the spread the +-3 noise can add to one
	// block channel and is not counted as misalignment. maxFitCost bounds the total
	// misalignment cost of a usable strip; costTolerance is how far above the lowest
	// cost a pitch may be and still count as a good fit; maxCandidates caps the
	// pitches tried per image.
	noiseSlack    = 6
	maxFitCost    = NumBlocks * 3 * 12
	costTolerance = NumBlocks
	maxCandidates = 64

	streamLen     = 3 + PayloadLen // version, seq, length, payload
	crcPayloadLen = 31             // bytes 0..30 are covered by the CRC at byte 31
)

// Decoding errors. Use errors.Is to test them.
var (
	ErrNoMagic        = errors.New("bridge: pixel magic not found")
	ErrBadCalibration = errors.New("bridge: pixel calibration is not monotonic")
	ErrBadVersion     = errors.New("bridge: unsupported pixel protocol version")
	ErrBadLength      = errors.New("bridge: unexpected payload length")
	ErrBadCRC         = errors.New("bridge: payload CRC mismatch")
	ErrBadImage       = errors.New("bridge: pixel strip is missing, too small or not uniform")
)

var (
	magic0 = [3]byte{255, 0, 255}
	magic1 = [3]byte{0, 255, 255}
	// calExpected lists the known colours of blocks 0..4, the calibration reference.
	// Per channel the levels are R{255,0,85,170,0}, G{0,255,170,85,255}, B{255,255,0,85,170}.
	calExpected = [5][3]byte{magic0, magic1, {85, 170, 0}, {170, 85, 85}, {0, 255, 170}}
)

// LiveState is one decoded snapshot of the game state.
type LiveState struct {
	MapID                    uint16
	X, Y                     float64 // 0..1
	Level, XPPct             uint8
	Money                    uint32
	TrackedQuestID           uint32
	TargetNpcID              uint32
	TargetLevel              uint8 // 255 = UnitLevel -1 (boss/skull)
	HealthPct, PowerPct      uint8 // 255 = unavailable/secret
	InCombat, Dead, Resting  bool
	Mounted, InGroup         bool
	TargetHostile, HasTarget bool
	InInstance               bool
	LastEventCode            uint8
	EventSeq                 uint8
	EventArg                 uint32
	FreeBagSlots             uint8
	Seq                      uint8
	ReadAt                   time.Time
}

// CRC8 computes CRC-8 with polynomial 0x07, init 0x00, no reflection, no xorout.
func CRC8(b []byte) byte {
	var crc byte
	for _, v := range b {
		crc ^= v
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// calPoint maps an observed channel value to the expected one.
type calPoint struct{ obs, exp float64 }

// channelCal is the piecewise-linear correction of one colour channel.
// Points are sorted by expected value and strictly increasing in both coordinates.
type channelCal []calPoint

// newChannelCal builds the correction of channel c from blocks 0..4. Observations
// that share an expected level are averaged. The result must be strictly
// monotonic and have at least two distinct levels.
func newChannelCal(blocks [NumBlocks][3]byte, c int) (channelCal, error) {
	var pts []calPoint
	for k := range calExpected {
		e := float64(calExpected[k][c])
		o := float64(blocks[k][c])
		merged := false
		for i := range pts {
			if pts[i].exp == e {
				// Keep a running mean of the observations for this level.
				pts[i].obs = (pts[i].obs + o) / 2
				merged = true
				break
			}
		}
		if !merged {
			pts = append(pts, calPoint{obs: o, exp: e})
		}
	}
	if len(pts) < 2 {
		return nil, fmt.Errorf("%w: channel %d has %d level(s)", ErrBadCalibration, c, len(pts))
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].exp < pts[j].exp })
	for i := 1; i < len(pts); i++ {
		if !(pts[i].obs > pts[i-1].obs) {
			return nil, fmt.Errorf("%w: channel %d observes %v at level %v but %v at level %v",
				ErrBadCalibration, c, pts[i-1].obs, pts[i-1].exp, pts[i].obs, pts[i].exp)
		}
	}
	return channelCal(pts), nil
}

// expected maps an observed value to the expected one. Values outside the
// calibrated range are extrapolated along the nearest segment, then clamped to 0..255.
func (cal channelCal) expected(v float64) float64 {
	n := len(cal)
	seg := 0
	switch {
	case v <= cal[0].obs:
		seg = 0
	case v >= cal[n-1].obs:
		seg = n - 2
	default:
		for i := 0; i < n-1; i++ {
			if v >= cal[i].obs && v <= cal[i+1].obs {
				seg = i
				break
			}
		}
	}
	a, b := cal[seg], cal[seg+1]
	e := a.exp + (v-a.obs)*(b.exp-a.exp)/(b.obs-a.obs)
	return math.Max(0, math.Min(255, e))
}

// nibble converts an observed channel value to its nibble, 0..15.
func (cal channelCal) nibble(v byte) byte {
	n := math.Round(cal.expected(float64(v)) / nibbleStep)
	return byte(math.Max(0, math.Min(15, n)))
}

// DecodeBlocks decodes the NumBlocks sampled blocks (3 bytes each) of the strip. The
// values are the observed colours; the calibration from blocks 0..4 is applied.
func DecodeBlocks(blocks [NumBlocks][3]byte) (LiveState, error) {
	var cal [3]channelCal
	for c := 0; c < 3; c++ {
		var err error
		if cal[c], err = newChannelCal(blocks, c); err != nil {
			return LiveState{}, err
		}
	}

	// Blocks 5..28: 24 blocks x 3 channels = 72 nibbles, in R,G,B order.
	var nib [72]byte
	for b := 5; b < NumBlocks; b++ {
		for c := 0; c < 3; c++ {
			nib[(b-5)*3+c] = cal[c].nibble(blocks[b][c])
		}
	}
	var raw [streamLen]byte // version, seq, length, payload; the padding nibbles are dropped
	for k := 0; k < streamLen; k++ {
		raw[k] = nib[2*k]<<4 | nib[2*k+1]
	}

	if raw[0] != ProtocolVersion {
		return LiveState{}, fmt.Errorf("%w: got %d, want %d", ErrBadVersion, raw[0], ProtocolVersion)
	}
	if raw[2] != PayloadLen {
		return LiveState{}, fmt.Errorf("%w: got %d, want %d", ErrBadLength, raw[2], PayloadLen)
	}
	seq := raw[1]
	payload := raw[3:streamLen]
	if crc := CRC8(payload[:crcPayloadLen]); crc != payload[crcPayloadLen] {
		return LiveState{}, fmt.Errorf("%w: computed %#02x, stored %#02x", ErrBadCRC, crc, payload[crcPayloadLen])
	}
	st := parsePayload(payload)
	st.Seq = seq
	return st, nil
}

// DecodeImage reads the strip from the top-left corner of img (region StripWidth x StripHeight).
//
//  1. The magic runs in the middle of row 0 give a pitch estimate p0.
//  2. The pitches near p0 are scored by how uniform the blocks' interior pixels are
//     (pitchCandidates). The best-scoring pitches are tried in turn.
//  3. Each candidate samples the blocks (decodeAtPitch) and decodes them with
//     DecodeBlocks. The first candidate whose magic, version, length and CRC all
//     check out gives the result; the CRC makes a false match very unlikely.
//
// Pixels outside the strip are ignored.
func DecodeImage(img image.Image) (LiveState, error) {
	if img == nil {
		return LiveState{}, fmt.Errorf("%w: nil image", ErrBadImage)
	}
	b := img.Bounds()
	if b.Dx() < StripWidth || b.Dy() < StripHeight {
		return LiveState{}, fmt.Errorf("%w: %dx%d is smaller than %dx%d", ErrBadImage, b.Dx(), b.Dy(), StripWidth, StripHeight)
	}

	p0, err := estimatePitch(img)
	if err != nil {
		return LiveState{}, err
	}
	cands, err := pitchCandidates(img, p0)
	if err != nil {
		return LiveState{}, err
	}
	var firstErr error
	for _, p := range cands {
		st, err := decodeAtPitch(img, p)
		if err == nil {
			return st, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return LiveState{}, firstErr
}

// estimatePitch measures the pitch from the magic runs in row 0. Block 0 starts at
// x = s0 and block 1 at s1; their ends give a second estimate. Both are averaged.
// Boundary pixels blended between the two colours are excluded by the tolerance.
func estimatePitch(img image.Image) (float64, error) {
	b := img.Bounds()
	scanEnd := 4 * maxBlockPx
	if scanEnd > b.Dx() {
		scanEnd = b.Dx()
	}
	y := b.Min.Y
	match := func(x int, want [3]byte) bool {
		return withinTolerance(pixelRGB(img, b.Min.X+x, y), want, magicTolerance)
	}

	// Run of magic0 from the left edge.
	s0 := -1
	for x := 0; x < scanEnd; x++ {
		if match(x, magic0) {
			s0 = x
			break
		}
	}
	if s0 < 0 || s0 > maxBlockPx {
		return 0, fmt.Errorf("%w: no block 0 at the left edge", ErrNoMagic)
	}
	e0 := s0
	for e0 < scanEnd && match(e0, magic0) {
		e0++
	}

	// First run of magic1 after block 0.
	s1 := -1
	for x := e0; x < scanEnd; x++ {
		if match(x, magic1) {
			s1 = x
			break
		}
	}
	if s1 < 0 {
		return 0, fmt.Errorf("%w: no block 1 after block 0", ErrNoMagic)
	}
	e1 := s1
	for e1 < scanEnd && match(e1, magic1) {
		e1++
	}

	pa := float64(s1 - s0)
	pb := float64(e1-s0) / 2
	p := (pa + pb) / 2
	if p < minBlockPx || p > maxBlockPx {
		return 0, fmt.Errorf("%w: block pitch %.2f outside [%.1f, %d]", ErrNoMagic, p, minBlockPx, maxBlockPx)
	}
	return p, nil
}

// fitCost measures how well block pitch p fits the strip on the middle row. For
// every block it takes the pixels that lie fully inside the block and sums, per
// channel, how far their spread (max-min) exceeds noiseSlack, the spread the +-3
// noise of a scaled client can produce. A pixel straddling an edge mixes two blocks
// and adds a large spread, so a wrong pitch, whose interiors reach across an edge,
// costs a lot; the correct pitch excludes the soft edge pixels and costs almost
// nothing. It also returns the number of interior pixels, which peaks at the true pitch.
func fitCost(img image.Image, p float64) (cost, pixels int) {
	b := img.Bounds()
	y := b.Min.Y + int(math.Floor(p/2))
	for i := 0; i < NumBlocks; i++ {
		lo, hi := interiorRange(i, p)
		if hi < lo {
			continue
		}
		var mn, mx [3]int
		for x := lo; x <= hi; x++ {
			c := pixelRGB(img, b.Min.X+x, y)
			for ch := 0; ch < 3; ch++ {
				v := int(c[ch])
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
		pixels += hi - lo + 1
	}
	return cost, pixels
}

// pitchCandidates returns the block pitches to try, best first. It searches
// [p0-pitchSearch, p0+pitchSearch] in pitchStep steps and keeps the pitches whose
// fitCost is within costTolerance of the lowest. Those are ordered by the number of
// interior pixels (a wrong pitch always loses pixels from some blocks), then by cost.
// If the lowest cost is above maxFitCost the blocks are not there and ErrNoMagic is
// returned.
func pitchCandidates(img image.Image, p0 float64) ([]float64, error) {
	lo := math.Max(minBlockPx, p0-pitchSearch)
	hi := math.Min(maxBlockPx, p0+pitchSearch)
	n := int(math.Round((hi - lo) / pitchStep))

	type candidate struct {
		p            float64
		cost, pixels int
	}
	all := make([]candidate, 0, n+1)
	bestCost := math.MaxInt
	for k := 0; k <= n; k++ {
		c := candidate{p: lo + float64(k)*pitchStep}
		c.cost, c.pixels = fitCost(img, c.p)
		all = append(all, c)
		if c.cost < bestCost {
			bestCost = c.cost
		}
	}
	if bestCost > maxFitCost {
		return nil, fmt.Errorf("%w: no uniform blocks at pitch near %.2f (fit cost %d)", ErrNoMagic, p0, bestCost)
	}
	var near []candidate
	for _, c := range all {
		if c.cost <= bestCost+costTolerance {
			near = append(near, c)
		}
	}
	sort.SliceStable(near, func(i, j int) bool {
		if near[i].pixels != near[j].pixels {
			return near[i].pixels > near[j].pixels
		}
		return near[i].cost < near[j].cost
	})
	if len(near) > maxCandidates {
		near = near[:maxCandidates]
	}
	out := make([]float64, len(near))
	for i, c := range near {
		out[i] = c.p
	}
	return out, nil
}

// decodeAtPitch samples the blocks at pitch p and decodes them. The magic blocks must
// show the expected colours, and the CRC must match.
func decodeAtPitch(img image.Image, p float64) (LiveState, error) {
	y0 := int(math.Floor(p / 2))
	var blocks [NumBlocks][3]byte
	for i := 0; i < NumBlocks; i++ {
		blocks[i] = sampleBlock(img, i, p, y0)
	}
	if !withinTolerance(blocks[0], magic0, magicTolerance) || !withinTolerance(blocks[1], magic1, magicTolerance) {
		return LiveState{}, fmt.Errorf("%w: blocks 0-1 are %v %v", ErrNoMagic, blocks[0], blocks[1])
	}
	return DecodeBlocks(blocks)
}

// interiorRange returns the first and last pixel column that lie fully inside
// block i at pitch p (hi < lo when there is none).
func interiorRange(i int, p float64) (lo, hi int) {
	lo = int(math.Ceil(float64(i) * p))
	hi = int(math.Floor(float64(i+1)*p)) - 1
	return lo, hi
}

// sampleBlock returns the average colour of block i at pitch p, read on row y.
// It uses the centre pixel, plus the left and right neighbours when p >= 3 and
// they lie strictly inside the block.
func sampleBlock(img image.Image, i int, p float64, y int) [3]byte {
	b := img.Bounds()
	start := float64(i) * p
	end := float64(i+1) * p
	c := int(math.Floor((float64(i) + 0.5) * p))
	xs := []int{c}
	if p >= 3 {
		for _, x := range []int{c - 1, c + 1} {
			if float64(x) >= start && float64(x)+1 <= end {
				xs = append(xs, x)
			}
		}
	}
	var sum [3]float64
	for _, x := range xs {
		px := pixelRGB(img, b.Min.X+x, b.Min.Y+y)
		for ch := 0; ch < 3; ch++ {
			sum[ch] += float64(px[ch])
		}
	}
	var out [3]byte
	for ch := 0; ch < 3; ch++ {
		out[ch] = byte(math.Round(sum[ch] / float64(len(xs))))
	}
	return out
}

// EncodeForTest builds the v2 strip for s, in the same 28-block layout as the
// addon's ns.EncodeBlocks. It is the inverse of DecodeBlocks, used by tests and
// by the UI's diagnostic. s.Seq is ignored; seq is written.
func EncodeForTest(s LiveState, seq uint8) [NumBlocks][3]byte {
	payload := encodePayload(s)
	stream := make([]byte, 0, streamLen+2)
	stream = append(stream, ProtocolVersion, seq, PayloadLen)
	stream = append(stream, payload[:]...)

	var nib [72]byte
	for k, v := range stream {
		nib[2*k] = v >> 4
		nib[2*k+1] = v & 0x0f
	}

	var blocks [NumBlocks][3]byte
	copy(blocks[:], calExpected[:])
	for bi := 5; bi < NumBlocks; bi++ {
		for c := 0; c < 3; c++ {
			blocks[bi][c] = nib[(bi-5)*3+c] * nibbleStep
		}
	}
	return blocks
}

func parsePayload(p []byte) LiveState {
	f := p[23]
	return LiveState{
		MapID:          be16(p[0:2]),
		X:              float64(be16(p[2:4])) / 65535,
		Y:              float64(be16(p[4:6])) / 65535,
		Level:          p[6],
		XPPct:          p[7],
		Money:          be32(p[8:12]),
		TrackedQuestID: be32(p[12:16]),
		TargetNpcID:    be32(p[16:20]),
		TargetLevel:    p[20],
		HealthPct:      p[21],
		PowerPct:       p[22],
		InCombat:       f&(1<<0) != 0,
		Dead:           f&(1<<1) != 0,
		Resting:        f&(1<<2) != 0,
		Mounted:        f&(1<<3) != 0,
		InGroup:        f&(1<<4) != 0,
		TargetHostile:  f&(1<<5) != 0,
		HasTarget:      f&(1<<6) != 0,
		InInstance:     f&(1<<7) != 0,
		LastEventCode:  p[24],
		EventSeq:       p[25],
		EventArg:       be32(p[26:30]),
		FreeBagSlots:   p[30],
	}
}

func encodePayload(s LiveState) [PayloadLen]byte {
	var p [PayloadLen]byte
	put16(p[0:2], s.MapID)
	put16(p[2:4], encodeCoord(s.X))
	put16(p[4:6], encodeCoord(s.Y))
	p[6] = s.Level
	p[7] = s.XPPct
	put32(p[8:12], s.Money)
	put32(p[12:16], s.TrackedQuestID)
	put32(p[16:20], s.TargetNpcID)
	p[20] = s.TargetLevel
	p[21] = s.HealthPct
	p[22] = s.PowerPct
	var f byte
	for i, on := range []bool{s.InCombat, s.Dead, s.Resting, s.Mounted, s.InGroup, s.TargetHostile, s.HasTarget, s.InInstance} {
		if on {
			f |= 1 << i
		}
	}
	p[23] = f
	p[24] = s.LastEventCode
	p[25] = s.EventSeq
	put32(p[26:30], s.EventArg)
	p[30] = s.FreeBagSlots
	p[31] = CRC8(p[:crcPayloadLen])
	return p
}

func encodeCoord(v float64) uint16 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		v = 1
	}
	return uint16(math.Round(v * 65535))
}

func be16(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) }

func be32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func put16(b []byte, v uint16) {
	b[0] = byte(v >> 8)
	b[1] = byte(v)
}

func put32(b []byte, v uint32) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}

func pixelRGB(img image.Image, x, y int) [3]byte {
	r, g, b, _ := img.At(x, y).RGBA()
	return [3]byte{byte(r >> 8), byte(g >> 8), byte(b >> 8)}
}

func withinTolerance(a, b [3]byte, tol int) bool {
	for i := 0; i < 3; i++ {
		d := int(a[i]) - int(b[i])
		if d < -tol || d > tol {
			return false
		}
	}
	return true
}
