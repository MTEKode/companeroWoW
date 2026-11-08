package bridge

import (
	"testing"
	"time"
)

// Every subscriber sees every valid read, and the event channel still gets the events.
func TestSubscribersSeeEveryReadAndEventsStillFlow(t *testing.T) {
	r := NewReader(time.Second)
	var seen []uint8
	r.Subscribe(func(st LiveState) { seen = append(seen, st.EventSeq) })
	r.Subscribe(nil) // ignored
	var second int
	r.Subscribe(func(LiveState) { second++ })

	r.publish(LiveState{EventSeq: 1, ReadAt: time.Now()})
	r.publish(LiveState{EventSeq: 1, ReadAt: time.Now()})
	r.publish(LiveState{EventSeq: 2, LastEventCode: 3, ReadAt: time.Now()})

	if len(seen) != 3 || second != 3 {
		t.Fatalf("subscribers saw %d and %d reads, want 3 each", len(seen), second)
	}
	select {
	case ev := <-r.Events():
		if ev.EventSeq != 2 || ev.LastEventCode != 3 {
			t.Errorf("event = %+v", ev)
		}
	default:
		t.Fatal("the event changed, so it must still be emitted")
	}
}
