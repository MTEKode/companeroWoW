package bridge

import (
	"math/rand"
	"testing"
)

// Byte 30 of the status payload is shareRemainingSec: the countdown of the addon's share
// window. The free bag slots moved to the character message.
func TestStatusByte30IsShareRemaining(t *testing.T) {
	s := LiveState{MapID: 1, EventSeq: 3, ShareRemainingSec: 17}
	p := encodePayload(s)
	if p[30] != 17 {
		t.Fatalf("payload byte 30 = %d, want shareRemainingSec 17", p[30])
	}
	got := parsePayload(p[:])
	if got.ShareRemainingSec != 17 {
		t.Fatalf("parsed shareRemainingSec = %d, want 17", got.ShareRemainingSec)
	}
	if got.MapID != 1 || got.EventSeq != 3 {
		t.Fatalf("other fields changed: %+v", got)
	}
}

func TestShareRemainingRoundTripsThroughBlocks(t *testing.T) {
	for _, n := range []uint8{0, 1, 20, 255} {
		s := LiveState{ShareRemainingSec: n}
		blocks := EncodeForTest(s, 5)
		img := renderStrip(blocks, 5.33, rand.New(rand.NewSource(int64(n)+1)), identity, 0)
		got, err := DecodeImage(img)
		if err != nil {
			t.Fatalf("share %d: decode: %v", n, err)
		}
		if got.ShareRemainingSec != n {
			t.Fatalf("share %d decoded as %d", n, got.ShareRemainingSec)
		}
	}
}
