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
	magicTolerance = 40 // per channel, used to recognise the magic blocks
	// pitchStep is the spacing of the pitches tried. The sample of the last block
	// (centre 28.5 pitches) moves by at most 28.5*pitchStep/2 from the true pitch,
	// about 0.06 px, well inside a block.
	pitchStep = 0.004

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

// level returns the calibrated level of an observed channel value.
func (cal channelCal) level(v byte) float64 {
	return cal.expected(float64(v))
}

// nibble converts an observed channel value to its nibble, 0..15.
func (cal channelCal) nibble(v byte) byte {
	return nibbleOf(cal.expected(float64(v)))
}

// DecodeBlocks decodes the NumBlocks sampled blocks (3 bytes each) of the strip. The
// values are the observed colours; the piecewise calibration from blocks 0..4 is applied.
func DecodeBlocks(blocks [NumBlocks][3]byte) (LiveState, error) {
	var cal [3]levelMap
	for c := 0; c < 3; c++ {
		pc, err := newChannelCal(blocks, c)
		if err != nil {
			return LiveState{}, err
		}
		cal[c] = pc
	}
	return decodeWithCal(blocks, cal)
}

// levelMap converts an observed channel value to its level and nibble.
type levelMap interface {
	level(v byte) float64
	nibble(v byte) byte
}

// decodeWithCal decodes the data blocks with the given channel calibrations.
func decodeWithCal(blocks [NumBlocks][3]byte, cal [3]levelMap) (LiveState, error) {
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

// linearCal is a straight-line calibration, level = (observed - b) / a, for a client
// that scales every level by one gain and clips at 255. Observed 255 is the top level,
// 15. A clipping client shows level 14 (238) as 249, which the piecewise calibration
// maps to 255 and so reads as 15; this line maps it to 14.
type linearCal struct{ a, b float64 }

func (l linearCal) level(v byte) float64 {
	if v == 255 {
		return 255
	}
	return math.Max(0, math.Min(255, (float64(v)-l.b)/l.a))
}

func (l linearCal) nibble(v byte) byte {
	return nibbleOf(l.level(v))
}

// nibbleOf rounds a level to its nibble, 0..15.
func nibbleOf(e float64) byte {
	return byte(math.Max(0, math.Min(15, math.Round(e/nibbleStep))))
}

// fitLinearCal fits a straight line through blocks 0..4 for channel c, using the levels
// 0, 85 and 170. Level 255 is left out: a clipping client shows it as 255 whatever the
// gain. ok is false when the fit is not a rising line.
func fitLinearCal(blocks [NumBlocks][3]byte, c int) (linearCal, bool) {
	var n, sx, sy, sxx, sxy float64
	for k := range calExpected {
		e := float64(calExpected[k][c])
		if e == 255 {
			continue
		}
		o := float64(blocks[k][c])
		n++
		sx += e
		sy += o
		sxx += e * e
		sxy += e * o
	}
	den := n*sxx - sx*sx
	if n < 2 || den == 0 {
		return linearCal{}, false
	}
	a := (n*sxy - sx*sy) / den
	b := (sy - a*sx) / n
	if a <= 0 {
		return linearCal{}, false
	}
	return linearCal{a: a, b: b}, true
}

// decodeAuto decodes the blocks with whichever calibration puts the data channels
// closer to the nibble grid. The piecewise calibration is exact for a client that
// keeps 255 at 255 and scales the levels between the knots. The linear one is exact
// for a clipping client. Choosing by the grid fit, not by trying both, keeps the CRC
// at one chance in 256 per decode.
func decodeAuto(blocks [NumBlocks][3]byte) (LiveState, error) {
	var pw [3]levelMap
	for c := 0; c < 3; c++ {
		p, err := newChannelCal(blocks, c)
		if err != nil {
			return LiveState{}, err
		}
		pw[c] = p
	}
	var lin [3]levelMap
	linOK := true
	for c := 0; c < 3; c++ {
		l, ok := fitLinearCal(blocks, c)
		if !ok {
			linOK = false
			break
		}
		lin[c] = l
	}
	cal := pw
	if linOK && gridError(blocks, lin) < gridError(blocks, pw) {
		cal = lin
	}
	return decodeWithCal(blocks, cal)
}

// gridError sums, over the data channels, the squared distance of the calibrated level
// from its nearest nibble level.
func gridError(blocks [NumBlocks][3]byte, cal [3]levelMap) float64 {
	var sum float64
	for b := 5; b < NumBlocks; b++ {
		for c := 0; c < 3; c++ {
			e := cal[c].level(blocks[b][c])
			d := e - float64(nibbleOf(e))*nibbleStep
			sum += d * d
		}
	}
	return sum
}

// EncodeForTest builds the v2 strip for s, in the same 29-block layout as the
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
