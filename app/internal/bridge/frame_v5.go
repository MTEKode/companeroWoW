package bridge

import (
	"fmt"
)

// Screenshot frames (protocol version byte 5, the SCREENSHOT CHANNEL CONTRACT).
//
// A screenshot frame is a v3.1 frame with a shot id after the block count. Its byte stream:
//
//	[0] version = 5   [1] seq   [2..3] N (u16 LE)   [4..5] shotId (u16 LE)
//	[6..37] status payload (32 bytes, same layout and CRC-8 as v2/v3)
//	[38] msgType  [39..40] msgId (LE)  [41..42] chunkIndex (LE)  [43..44] chunkCount (LE)
//	[45..46] dataLen (LE)
//	[47 .. 47+dataLen-1] chunk data
//	[47+dataLen .. 48+dataLen] CRC-16/CCITT-FALSE over bytes 0..46+dataLen, big-endian
//
// Each row of a data panel is one such frame, drawn with the v3.1 pixel layout (see v31.go):
// the seven fixed blocks, then nibbles. All rows of one screenshot share shotId. The last row
// is the end frame: msgType 0, dataLen 0, and chunkCount holds the number of data rows above
// it (a data row without a chunk always has chunkCount 0).
const (
	ProtocolV5 = 5

	// shotHeaderLenV5 is the header before the status payload: version, seq, N and shotId.
	shotHeaderLenV5 = 6
	// v5Overhead is the frame size without chunk data.
	v5Overhead = shotHeaderLenV5 + PayloadLen + chunkHeaderLen + 2
)

// CapacityV5 is the number of chunk data bytes a v5 frame with n blocks carries.
func CapacityV5(n int) int {
	if n <= calibrationBlocksV31 {
		return 0
	}
	c := 3*(n-calibrationBlocksV31)/2 - v5Overhead
	if c < 0 {
		return 0
	}
	return c
}

// BuildFrameV5 composes the byte stream of one screenshot frame of n blocks. A nil chunk means
// nothing pending. The end frame is built with an empty chunk whose Count is the number of data
// rows (see BuildEndFrameV5).
func BuildFrameV5(s LiveState, seq uint8, n int, shotID uint16, chunk *ChunkV3) ([]byte, error) {
	dlen := 0
	var msgType uint8
	var id, index, count uint16
	var data []byte
	if chunk != nil {
		msgType, id, index, count = chunk.Type, chunk.ID, chunk.Index, chunk.Count
		data = chunk.Data
		dlen = len(data)
	}
	if n <= calibrationBlocksV31 || n > MaxBlocksV3 {
		return nil, fmt.Errorf("%w: %d blocks", ErrBadLength, n)
	}
	if capacity := CapacityV5(n); dlen > capacity {
		return nil, fmt.Errorf("bridge: chunk of %d bytes exceeds the %d-byte capacity of %d blocks", dlen, capacity, n)
	}
	f := make([]byte, 0, v5Overhead+dlen)
	f = append(f, ProtocolV5, seq, byte(n), byte(n>>8), byte(shotID), byte(shotID>>8))
	status := encodePayload(s)
	f = append(f, status[:]...)
	f = append(f, msgType, byte(id), byte(id>>8), byte(index), byte(index>>8),
		byte(count), byte(count>>8), byte(dlen), byte(dlen>>8))
	f = append(f, data...)
	crc := CRC16(f)
	f = append(f, byte(crc>>8), byte(crc))
	return f, nil
}

// BuildEndFrameV5 composes the end frame of a screenshot after rows data rows.
func BuildEndFrameV5(s LiveState, seq uint8, n int, shotID uint16, rows int) ([]byte, error) {
	return BuildFrameV5(s, seq, n, shotID, &ChunkV3{Count: uint16(rows)})
}

// ParseFrameV5 reads a screenshot frame from its bytes, checking the version and the CRC-16.
func ParseFrameV5(b []byte) (FrameV3, error) {
	if len(b) < v5Overhead {
		return FrameV3{}, fmt.Errorf("%w: %d bytes", ErrBadLength, len(b))
	}
	if b[0] != ProtocolV5 {
		return FrameV3{}, fmt.Errorf("%w: got %d, want %d", ErrBadVersion, b[0], ProtocolV5)
	}
	dlen := int(b[45]) | int(b[46])<<8
	crcAt := 47 + dlen
	if len(b) < crcAt+2 {
		return FrameV3{}, fmt.Errorf("%w: frame is %d bytes, need %d", ErrBadLength, len(b), crcAt+2)
	}
	if crc := CRC16(b[:crcAt]); crc != be16(b[crcAt:]) {
		return FrameV3{}, fmt.Errorf("%w: computed %#04x, stored %#04x", ErrBadCRC, crc, be16(b[crcAt:]))
	}
	payload := b[6 : 6+PayloadLen]
	if CRC8(payload[:crcPayloadLen]) != payload[crcPayloadLen] {
		return FrameV3{}, fmt.Errorf("%w: status CRC-8", ErrBadCRC)
	}
	st := parsePayload(payload)
	st.Seq = b[1]
	f := FrameV3{
		Version:    b[0],
		Seq:        b[1],
		N:          int(b[2]) | int(b[3])<<8,
		ShotID:     uint16(b[4]) | uint16(b[5])<<8,
		Status:     st,
		MsgType:    b[38],
		MsgID:      uint16(b[39]) | uint16(b[40])<<8,
		ChunkIndex: uint16(b[41]) | uint16(b[42])<<8,
		ChunkCount: uint16(b[43]) | uint16(b[44])<<8,
	}
	if dlen > 0 {
		f.Data = append([]byte(nil), b[47:crcAt]...)
	}
	return f, nil
}

// IsEndFrame reports whether f is the end frame of a screenshot: no chunk pending and a row
// count in ChunkCount.
func IsEndFrame(f FrameV3) bool {
	return f.MsgType == 0 && f.ChunkCount > 0
}
