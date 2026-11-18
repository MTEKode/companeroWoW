package bridge

import (
	"errors"
	"math/rand"
	"testing"
	"time"
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

// The reader keeps the normal poll rate for the share window, even while the strip is
// missing, and backs off again after it.
func TestReaderActivateKeepsNormalRateWhileStripMissing(t *testing.T) {
	f := newFakeReader(t)
	f.findErr = errors.New("WoW closed")
	f.r.Activate(f.clock.now())
	if got := f.r.step(); got != f.r.pollEvery {
		t.Fatalf("during the share watch: wait %v, want %v", got, f.r.pollEvery)
	}
	f.clock.advance(shareWatch + time.Second)
	if got := f.r.step(); got != retryEvery {
		t.Fatalf("after the share watch: wait %v, want %v", got, retryEvery)
	}
}

func TestReaderActivateWakesThePollLoop(t *testing.T) {
	r := NewReader(time.Second)
	r.Activate(time.Now())
	r.Activate(time.Now()) // a second press while one wake-up is pending must not block
	if n := len(r.wake); n != 1 {
		t.Fatalf("pending wake-ups = %d, want 1", n)
	}
}
