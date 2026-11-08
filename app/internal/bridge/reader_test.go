package bridge

import (
	"testing"
	"time"
)

func drain(ch <-chan LiveState) []LiveState {
	var out []LiveState
	for {
		select {
		case st := <-ch:
			out = append(out, st)
		default:
			return out
		}
	}
}

func TestReaderEventsOnSeqChange(t *testing.T) {
	r := NewReader(time.Second)
	now := time.Now()
	r.publish(LiveState{EventSeq: 4, ReadAt: now})
	if evs := drain(r.Events()); len(evs) != 0 {
		t.Fatalf("first valid read must not emit, got %d events", len(evs))
	}
	r.publish(LiveState{EventSeq: 4, ReadAt: now})
	if evs := drain(r.Events()); len(evs) != 0 {
		t.Fatalf("same EventSeq must not emit, got %d", len(evs))
	}
	r.publish(LiveState{EventSeq: 5, LastEventCode: 1, EventArg: 12, ReadAt: now})
	evs := drain(r.Events())
	if len(evs) != 1 || evs[0].EventSeq != 5 || evs[0].LastEventCode != 1 || evs[0].EventArg != 12 {
		t.Fatalf("expected one event for seq 5, got %+v", evs)
	}
}

func TestReaderEventBufferDropsOldest(t *testing.T) {
	r := NewReader(time.Second)
	now := time.Now()
	r.publish(LiveState{EventSeq: 0, ReadAt: now})
	for seq := 1; seq <= 20; seq++ {
		r.publish(LiveState{EventSeq: uint8(seq), ReadAt: now})
	}
	evs := drain(r.Events())
	if len(evs) != eventBuffer {
		t.Fatalf("buffered %d events, want %d", len(evs), eventBuffer)
	}
	if evs[0].EventSeq != 5 || evs[len(evs)-1].EventSeq != 20 {
		t.Fatalf("expected seqs 5..20, got %d..%d", evs[0].EventSeq, evs[len(evs)-1].EventSeq)
	}
}

func TestReaderLostResetsBaseline(t *testing.T) {
	r := NewReader(time.Second)
	now := time.Now()
	r.publish(LiveState{EventSeq: 3, ReadAt: now})
	r.lost()
	r.publish(LiveState{EventSeq: 9, ReadAt: now})
	if evs := drain(r.Events()); len(evs) != 0 {
		t.Fatalf("reopening WoW must not replay events, got %d", len(evs))
	}
	r.publish(LiveState{EventSeq: 10, ReadAt: now})
	if evs := drain(r.Events()); len(evs) != 1 {
		t.Fatalf("expected one event after baseline, got %d", len(evs))
	}
}

func TestReaderLatestFreshness(t *testing.T) {
	r := NewReader(time.Second)
	if _, ok := r.Latest(); ok {
		t.Fatal("Latest must be !ok before any valid read")
	}
	r.publish(LiveState{Level: 12, ReadAt: time.Now()})
	st, ok := r.Latest()
	if !ok || st.Level != 12 {
		t.Fatalf("Latest = %+v ok=%v", st, ok)
	}
	r.publish(LiveState{Level: 13, ReadAt: time.Now().Add(-10 * time.Second)})
	if _, ok := r.Latest(); ok {
		t.Fatal("state older than 5s must not be reported as current")
	}
}

func TestNewReaderDefaultsPollInterval(t *testing.T) {
	if r := NewReader(0); r.pollEvery != defaultPollEvery {
		t.Fatalf("pollEvery = %v, want %v", r.pollEvery, defaultPollEvery)
	}
}
