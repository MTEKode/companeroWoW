package bridge

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"
)

// Protocol v3: a live data channel on the top edge of the WoW client.
//
// The strip is one row of square blocks, nominally BlockPx physical pixels wide, that
// spans the client width: N = min(floor(clientWidth/4), 1024) blocks. Blocks 0..4 are the
// magic and calibration blocks of v2. Blocks 5..N-1 carry a nibble stream, 3 nibbles per
// block (R, G, B), high nibble first; unused trailing nibbles are 0.
//
// Byte stream (0-based here, big-endian except where noted):
//
//	[0] version = 3   [1] seq   [2..3] N (u16 little-endian)
//	[4..35] status payload (32 bytes, same layout and CRC-8 as v2)
//	[36] msgType  [37..38] msgId (LE)  [39..40] chunkIndex (LE)  [41..42] chunkCount (LE)
//	[43..44] dataLen (LE)
//	[45 .. 45+dataLen-1] chunk data
//	[45+dataLen .. 46+dataLen] CRC-16/CCITT-FALSE over bytes 0..44+dataLen, big-endian
//
// Without a pending chunk msgType = 0 and dataLen = 0. The data capacity of a frame is
// floor(3*(N-5)/2) - 47 bytes: 1235 bytes at N = 860 (3440 px wide).
const (
	ProtocolV3 = 3

	// StripHeightV3 is the number of rows captured for v3 (enough for pitches up to maxBlockPx).
	StripHeightV3 = maxBlockPx
	// MaxBlocksV3 caps the row: 1024 blocks x 4 px = 4096 px.
	MaxBlocksV3 = 1024
	// BlockPx is the nominal block size in physical pixels, the same as v2.
	BlockPx = nominalBlockPx

	calibrationBlocks = 5
	chunkHeaderLen    = 9
	// v3Overhead is the frame size without chunk data: header, status, chunk header, CRC-16.
	v3Overhead = 4 + PayloadLen + chunkHeaderLen + 2
	// v3FitBlocks is how many blocks the pitch fit looks at. It is enough to pin the pitch
	// and keeps the search cheap; the rest of the row is read only after the header.
	v3FitBlocks = 64
	// MaxChunkCount bounds a message: a count above it is taken as corrupt.
	MaxChunkCount = 256
)

// Message types of the v3 registry. The addon's Channel.lua uses the same numbers.
const (
	MsgCharacter       uint8 = 1  // money, xp, level, reputations, talents
	MsgQuests          uint8 = 2  // quest log with objectives
	MsgBags            uint8 = 3  // inventory
	MsgEquipment       uint8 = 4  // equipment, gear and proficiencies
	MsgProfessions     uint8 = 5  // professions, training and profession plan
	MsgCompletedQuests uint8 = 6  // completed quest ids: full list or {"add":[...]}
	MsgCombat          uint8 = 7  // one finished encounter
	MsgActionBars      uint8 = 8  // action bars
	MsgKnowledge       uint8 = 9  // knowledge delta: new or updated quests/npcs
	MsgHeavy           uint8 = 10 // questie, questWindow, gearCandidates, dungeons
	MsgMeta            uint8 = 11 // addon version, export timestamps, health
	MsgSheet           uint8 = 12 // character sheet: the same shape as characters[key].sheet (manual, every 60 s at most)
)

// MsgNames maps each message type to its name in the addon and in --diag.
var MsgNames = map[uint8]string{
	MsgCharacter: "character", MsgQuests: "quests", MsgBags: "bags", MsgEquipment: "equipment",
	MsgProfessions: "professions", MsgCompletedQuests: "completedQuests", MsgCombat: "combat",
	MsgActionBars: "actionBars", MsgKnowledge: "knowledge", MsgHeavy: "heavy", MsgMeta: "meta", MsgSheet: "sheet",
}

// ErrBadChunk reports a chunk header that cannot belong to a frame.
var ErrBadChunk = errors.New("bridge: bad chunk header")

// FrameV3 is one decoded v3 frame.
type FrameV3 struct {
	Seq        uint8
	N          int       // blocks in the row
	Status     LiveState // the status payload (Seq set)
	MsgType    uint8     // 0 = no chunk pending
	MsgID      uint16    // message id, the same on every chunk of a message
	ChunkIndex uint16    // 0-based
	ChunkCount uint16
	Data       []byte // chunk data (nil when MsgType is 0)
}

// ChunkV3 describes the chunk to put in a frame.
type ChunkV3 struct {
	Type  uint8
	ID    uint16
	Index uint16 // 0-based
	Count uint16
	Data  []byte
}

// CapacityV3 is the number of chunk data bytes a frame with n blocks carries.
func CapacityV3(n int) int {
	if n <= calibrationBlocks {
		return 0
	}
	c := 3*(n-calibrationBlocks)/2 - v3Overhead
	if c < 0 {
		return 0
	}
	return c
}

// CRC16 is CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF, no reflection, no xorout).
// The check value of "123456789" is 0x29B1.
func CRC16(b []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, v := range b {
		crc ^= uint16(v) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// BuildFrameV3 composes the byte stream of one frame. A nil chunk means nothing pending.
func BuildFrameV3(s LiveState, seq uint8, n int, chunk *ChunkV3) ([]byte, error) {
	dlen := 0
	var msgType uint8
	var id, index, count uint16
	var data []byte
	if chunk != nil {
		msgType, id, index, count = chunk.Type, chunk.ID, chunk.Index, chunk.Count
		data = chunk.Data
		dlen = len(data)
	}
	if n <= calibrationBlocks || n > MaxBlocksV3 {
		return nil, fmt.Errorf("%w: %d blocks", ErrBadLength, n)
	}
	if dlen > CapacityV3(n) {
		return nil, fmt.Errorf("bridge: chunk of %d bytes exceeds the %d-byte capacity of %d blocks", dlen, CapacityV3(n), n)
	}
	f := make([]byte, 0, v3Overhead+dlen)
	f = append(f, ProtocolV3, seq, byte(n), byte(n>>8))
	status := encodePayload(s)
	f = append(f, status[:]...)
	f = append(f, msgType, byte(id), byte(id>>8), byte(index), byte(index>>8),
		byte(count), byte(count>>8), byte(dlen), byte(dlen>>8))
	f = append(f, data...)
	crc := CRC16(f)
	f = append(f, byte(crc>>8), byte(crc))
	return f, nil
}

// ParseFrameV3 reads a frame from its bytes, checking the version and the CRC-16.
func ParseFrameV3(b []byte) (FrameV3, error) {
	if len(b) < v3Overhead {
		return FrameV3{}, fmt.Errorf("%w: %d bytes", ErrBadLength, len(b))
	}
	if b[0] != ProtocolV3 {
		return FrameV3{}, fmt.Errorf("%w: got %d, want %d", ErrBadVersion, b[0], ProtocolV3)
	}
	dlen := int(b[43]) | int(b[44])<<8
	crcAt := 45 + dlen
	if len(b) < crcAt+2 {
		return FrameV3{}, fmt.Errorf("%w: frame is %d bytes, need %d", ErrBadLength, len(b), crcAt+2)
	}
	if crc := CRC16(b[:crcAt]); crc != uint16(b[crcAt])<<8|uint16(b[crcAt+1]) {
		return FrameV3{}, fmt.Errorf("%w: computed %#04x, stored %#04x", ErrBadCRC, crc, uint16(b[crcAt])<<8|uint16(b[crcAt+1]))
	}
	payload := b[4 : 4+PayloadLen]
	if CRC8(payload[:crcPayloadLen]) != payload[crcPayloadLen] {
		return FrameV3{}, fmt.Errorf("%w: status CRC-8", ErrBadCRC)
	}
	st := parsePayload(payload)
	st.Seq = b[1]
	f := FrameV3{
		Seq:        b[1],
		N:          int(b[2]) | int(b[3])<<8,
		Status:     st,
		MsgType:    b[36],
		MsgID:      uint16(b[37]) | uint16(b[38])<<8,
		ChunkIndex: uint16(b[39]) | uint16(b[40])<<8,
		ChunkCount: uint16(b[41]) | uint16(b[42])<<8,
	}
	if dlen > 0 {
		f.Data = append([]byte(nil), b[45:crcAt]...)
	}
	return f, nil
}

// BlocksV3 returns the colours of the n blocks of a frame: blocks 0..4 are the fixed magic
// and calibration colours, blocks 5..n-1 the nibbles of frame (missing nibbles are 0).
func BlocksV3(frame []byte, n int) [][3]byte {
	out := make([][3]byte, n)
	for i := 0; i < calibrationBlocks && i < n; i++ {
		out[i] = calExpected[i]
	}
	nibble := func(k int) byte {
		j := k / 2
		if j >= len(frame) {
			return 0
		}
		if k%2 == 0 {
			return frame[j] >> 4
		}
		return frame[j] & 0x0f
	}
	for b := calibrationBlocks; b < n; b++ {
		for c := 0; c < 3; c++ {
			out[b][c] = nibble((b-calibrationBlocks)*3+c) * nibbleStep
		}
	}
	return out
}

// v3Image is the top StripHeightV3 rows of the client, read at full width.
type v3Image struct {
	w   int
	pix [][3]byte // row-major: pix[y*w+x]
}

func loadV3(img image.Image, w int) *v3Image {
	s := &v3Image{w: w, pix: make([][3]byte, w*StripHeightV3)}
	b := img.Bounds()
	if rgba, ok := img.(*image.RGBA); ok {
		for y := 0; y < StripHeightV3; y++ {
			row := rgba.Pix[(b.Min.Y+y-rgba.Rect.Min.Y)*rgba.Stride:]
			for x := 0; x < w; x++ {
				o := 4 * (x + b.Min.X - rgba.Rect.Min.X)
				s.pix[y*w+x] = [3]byte{row[o], row[o+1], row[o+2]}
			}
		}
		return s
	}
	for y := 0; y < StripHeightV3; y++ {
		for x := 0; x < w; x++ {
			s.pix[y*w+x] = pixelRGB(img, b.Min.X+x, b.Min.Y+y)
		}
	}
	return s
}

// blocksFit is how many blocks at pitch p fit in the row (the width over the pitch, rounded
// to the nearest block so that a pitch a few thousandths off still gives the true count).
func (s *v3Image) blocksFit(p float64) int {
	return int(math.Floor(float64(s.w)/p + 0.5))
}

// sample returns the average colour of block i at pitch p on row y (see sampleBlock).
func (s *v3Image) sample(i int, p float64, y int) [3]byte {
	start := float64(i) * p
	end := float64(i+1) * p
	c := int(math.Floor((float64(i) + 0.5) * p))
	if c >= s.w {
		c = s.w - 1
	}
	var sum [3]float64
	n := 0
	add := func(x int) {
		px := s.pix[y*s.w+x]
		for ch := 0; ch < 3; ch++ {
			sum[ch] += float64(px[ch])
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

// calibrationV3 builds the per-channel calibration from blocks 0..4 and picks, by the
// grid fit over the sampled data blocks, the piecewise or the linear one (as decodeAuto).
func calibrationV3(cal5 [5][3]byte, data [][3]byte) ([3]levelMap, error) {
	var pad [NumBlocks][3]byte
	copy(pad[:calibrationBlocks], cal5[:])
	var pw, lin [3]levelMap
	linOK := true
	for c := 0; c < 3; c++ {
		p, err := newChannelCal(pad, c)
		if err != nil {
			return pw, err
		}
		pw[c] = p
	}
	for c := 0; c < 3; c++ {
		l, ok := fitLinearCal(pad, c)
		if !ok {
			linOK = false
			break
		}
		lin[c] = l
	}
	if linOK && gridErrorV3(data, lin) < gridErrorV3(data, pw) {
		return lin, nil
	}
	return pw, nil
}

// gridErrorV3 is gridError over a list of data blocks.
func gridErrorV3(data [][3]byte, cal [3]levelMap) float64 {
	var sum float64
	for _, blk := range data {
		for c := 0; c < 3; c++ {
			e := cal[c].level(blk[c])
			d := e - float64(nibbleOf(e))*nibbleStep
			sum += d * d
		}
	}
	return sum
}

// streamFromBlocks turns data blocks (3 nibbles each, R G B) into the first nbytes bytes
// of the nibble stream. The calibration maps observed channel values to nibbles.
func streamFromBlocks(blocks [][3]byte, cal [3]levelMap, nbytes int) []byte {
	nib := make([]byte, 0, len(blocks)*3)
	for _, blk := range blocks {
		for c := 0; c < 3; c++ {
			nib = append(nib, cal[c].nibble(blk[c]))
		}
	}
	out := make([]byte, 0, nbytes)
	for j := 0; j < nbytes && 2*j+1 < len(nib); j++ {
		out = append(out, nib[2*j]<<4|nib[2*j+1])
	}
	return out
}

// v3Pitches returns the pitch search result for s: the candidate pitches, lowest cost first.
func (s *v3Image) v3Candidates() []struct {
	p    float64
	cost int
} {
	steps := int(math.Round((maxBlockPx - minBlockPx) / pitchStep))
	type cand = struct {
		p    float64
		cost int
	}
	var out []cand
	for k := 0; k <= steps; k++ {
		p := minBlockPx + float64(k)*pitchStep
		if c := s.fitCostV3(p); c <= maxFitCost {
			out = append(out, cand{p, c})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].cost < out[j].cost })
	return out
}

// DecodeFrameV3 reads a v3 frame from img, whose top-left corner holds the strip at full
// width (at least 8 blocks of pitch 4 px and StripHeightV3 rows). hint is the pitch that
// decoded last, or 0. It returns the frame and the pitch that decoded.
func DecodeFrameV3(img image.Image, hint float64) (FrameV3, float64, error) {
	if img == nil {
		return FrameV3{}, 0, fmt.Errorf("%w: nil image", ErrBadImage)
	}
	b := img.Bounds()
	if b.Dy() < StripHeightV3 || b.Dx() < 8*BlockPx {
		return FrameV3{}, 0, fmt.Errorf("%w: %dx%d is too small for a v3 strip", ErrBadImage, b.Dx(), b.Dy())
	}
	s := loadV3(img, b.Dx())
	if hint > 0 {
		if s.fitCostV3(hint) <= maxFitCost {
			if f, p, err := s.decodeV3AtPitch(hint); err == nil {
				return f, p, nil
			}
		}
	}
	cands := s.v3Candidates()
	if len(cands) == 0 {
		return FrameV3{}, 0, fmt.Errorf("%w: no pitch in [%.1f, %d] gives uniform blocks", ErrNoMagic, minBlockPx, maxBlockPx)
	}
	var firstErr error
	for _, c := range cands {
		f, p, err := s.decodeV3AtPitch(c.p)
		if err == nil {
			return f, p, nil
		}
		if firstErr == nil && !errors.Is(err, ErrNoMagic) {
			firstErr = err
		}
	}
	if firstErr != nil {
		return FrameV3{}, 0, firstErr
	}
	return FrameV3{}, 0, fmt.Errorf("%w: blocks 0-1 are not the magic colours", ErrNoMagic)
}

// Decoded is the result of DecodeAny: the status, and the v3 frame when the strip is v3.
type Decoded struct {
	Live    LiveState
	Frame   *FrameV3 // nil for a v2 strip
	Pitch   float64
	Version int // 2 or 3
}

// DecodeAny reads the strip of either protocol from img: v3 first, then v2 for addons that
// still draw the 29-block strip. hint is the pitch of the last decode, or 0.
func DecodeAny(img image.Image, hint float64) (Decoded, error) {
	f, p, err := DecodeFrameV3(img, hint)
	if err == nil {
		return Decoded{Live: f.Status, Frame: &f, Pitch: p, Version: ProtocolV3}, nil
	}
	st, p2, err2 := decodeStrip(img, hint)
	if err2 == nil {
		return Decoded{Live: st, Pitch: p2, Version: ProtocolVersion}, nil
	}
	return Decoded{}, err2
}
