// Package bridge reads the pixel strip drawn by the CompaneroWoW addon (protocol v1)
// and the addon's SavedVariables file.
package bridge

import (
	"errors"
	"fmt"
	"image"
	"math"
	"time"
)

// Protocol v1 layout constants.
const (
	ProtocolVersion = 1
	PayloadLen      = 32
	NumBlocks       = 14
	StripWidth      = NumBlocks * blockPx // 28
	StripHeight     = blockPx             // 2

	blockPx        = 2
	magicTolerance = 2
	crcPayloadLen  = 31 // bytes 0..30 are covered by the CRC at byte 31
)

// Decoding errors. Use errors.Is to test them.
var (
	ErrNoMagic    = errors.New("bridge: pixel magic not found")
	ErrBadVersion = errors.New("bridge: unsupported pixel protocol version")
	ErrBadLength  = errors.New("bridge: unexpected payload length")
	ErrBadCRC     = errors.New("bridge: payload CRC mismatch")
	ErrBadImage   = errors.New("bridge: pixel strip is missing, too small or not uniform")
)

var (
	magic0 = [3]byte{255, 0, 255}
	magic1 = [3]byte{0, 255, 255}
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

// DecodeBlocks decodes the 14 raw blocks (3 bytes each) of the strip.
func DecodeBlocks(blocks [NumBlocks][3]byte) (LiveState, error) {
	if blocks[0] != magic0 || blocks[1] != magic1 {
		return LiveState{}, fmt.Errorf("%w: blocks 0-1 are %v %v", ErrNoMagic, blocks[0], blocks[1])
	}
	if blocks[2][0] != ProtocolVersion {
		return LiveState{}, fmt.Errorf("%w: got %d, want %d", ErrBadVersion, blocks[2][0], ProtocolVersion)
	}
	if blocks[2][2] != PayloadLen {
		return LiveState{}, fmt.Errorf("%w: got %d, want %d", ErrBadLength, blocks[2][2], PayloadLen)
	}
	seq := blocks[2][1]

	// Blocks 3..13 carry 11*3 = 33 bytes: the 32-byte payload plus one padding byte.
	var raw [PayloadLen + 1]byte
	k := 0
	for b := 3; b < NumBlocks; b++ {
		for c := 0; c < 3; c++ {
			raw[k] = blocks[b][c]
			k++
		}
	}
	payload := raw[:PayloadLen]
	if crc := CRC8(payload[:crcPayloadLen]); crc != payload[crcPayloadLen] {
		return LiveState{}, fmt.Errorf("%w: computed %#02x, stored %#02x", ErrBadCRC, crc, payload[crcPayloadLen])
	}
	st := parsePayload(payload)
	st.Seq = seq
	return st, nil
}

// DecodeImage reads the strip from the top-left of img. Block i is the 2x2 square
// at x=2i..2i+1, y=0..1. Its data is read exactly from pixel (2i, 0); pixel (2i+1, 1)
// must match it within tolerance. Magic blocks are accepted within ±2 per channel.
// Pixels outside the strip are ignored.
func DecodeImage(img image.Image) (LiveState, error) {
	if img == nil {
		return LiveState{}, fmt.Errorf("%w: nil image", ErrBadImage)
	}
	b := img.Bounds()
	if b.Dx() < StripWidth || b.Dy() < StripHeight {
		return LiveState{}, fmt.Errorf("%w: %dx%d is smaller than %dx%d", ErrBadImage, b.Dx(), b.Dy(), StripWidth, StripHeight)
	}

	var blocks [NumBlocks][3]byte
	for i := 0; i < NumBlocks; i++ {
		x := b.Min.X + blockPx*i
		y := b.Min.Y
		c := pixelRGB(img, x, y)
		if i < 2 {
			want := magic0
			if i == 1 {
				want = magic1
			}
			if !withinTolerance(c, want, magicTolerance) {
				return LiveState{}, fmt.Errorf("%w: block %d is %v, want about %v", ErrNoMagic, i, c, want)
			}
			c = want
		}
		if other := pixelRGB(img, x+1, y+1); !withinTolerance(c, other, magicTolerance) {
			return LiveState{}, fmt.Errorf("%w: block %d differs across its 2x2 area (%v vs %v)", ErrBadImage, i, c, other)
		}
		blocks[i] = c
	}
	return DecodeBlocks(blocks)
}

// EncodeForTest builds the strip for s. It is the inverse of DecodeBlocks and is
// used by tests and by the UI's diagnostic. s.Seq is ignored; seq is written.
func EncodeForTest(s LiveState, seq uint8) [NumBlocks][3]byte {
	payload := encodePayload(s)
	var raw [PayloadLen + 1]byte
	copy(raw[:], payload[:])

	var blocks [NumBlocks][3]byte
	blocks[0] = magic0
	blocks[1] = magic1
	blocks[2] = [3]byte{ProtocolVersion, seq, PayloadLen}
	k := 0
	for b := 3; b < NumBlocks; b++ {
		for c := 0; c < 3; c++ {
			blocks[b][c] = raw[k]
			k++
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
