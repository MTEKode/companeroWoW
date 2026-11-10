package assistant

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// receive waits for one event on ch, or reports false after a short wait.
func receive(ch <-chan bridge.LiveState) (bridge.LiveState, bool) {
	select {
	case ev, ok := <-ch:
		return ev, ok
	case <-time.After(time.Second):
		return bridge.LiveState{}, false
	}
}

func TestGateForwardsOnlyWhileEnabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := make(chan bridge.LiveState, 4)
	var on atomic.Bool
	on.Store(true)
	out := Gate(ctx, in, on.Load)

	in <- bridge.LiveState{LastEventCode: 1}
	if ev, ok := receive(out); !ok || ev.LastEventCode != 1 {
		t.Fatalf("enabled: got %v, %v; want event 1", ev.LastEventCode, ok)
	}

	on.Store(false)
	in <- bridge.LiveState{LastEventCode: 2}
	if ev, ok := receive(out); ok {
		t.Fatalf("disabled: event %d was forwarded", ev.LastEventCode)
	}

	on.Store(true)
	in <- bridge.LiveState{LastEventCode: 3}
	if ev, ok := receive(out); !ok || ev.LastEventCode != 3 {
		t.Fatalf("re-enabled: got %v, %v; want event 3", ev.LastEventCode, ok)
	}
}

func TestGateClosesWhenInputCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := make(chan bridge.LiveState)
	out := Gate(ctx, in, nil)
	close(in)
	if _, ok := receive(out); ok {
		t.Fatal("output still open after input closed")
	}
}

func TestGateStopsOnContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan bridge.LiveState)
	out := Gate(ctx, in, nil)
	cancel()
	if _, ok := receive(out); ok {
		t.Fatal("output still open after context end")
	}
}
