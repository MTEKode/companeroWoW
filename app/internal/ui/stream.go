package ui

import (
	"context"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
)

// streamInterval is the shortest time between two repaints of a streamed answer: at most ten
// per second, so that a fast provider does not flood the window with repaints.
const streamInterval = 100 * time.Millisecond

// deltaThrottle collects the text of a streamed answer on its way from the ask goroutine to the
// UI thread. Each piece is added as it arrives; add reports when the UI should be asked to repaint
// (at most once per streamInterval), and when a repaint is due later it is scheduled through
// due. The UI thread takes the text collected so far with take.
type deltaThrottle struct {
	mu       sync.Mutex
	interval time.Duration
	pending  string    // text not yet taken by the UI
	reset    bool      // the text taken so far is void: the UI starts the answer again
	last     time.Time // when the last repaint was requested
	armed    bool      // a repaint is scheduled for later
}

func newDeltaThrottle(interval time.Duration) *deltaThrottle {
	return &deltaThrottle{interval: interval}
}

// add appends text. post is true when the UI should be asked to repaint now. Otherwise, when
// after is positive, a repaint is due after that long: the caller schedules it and then calls due.
func (t *deltaThrottle) add(text string, now time.Time) (post bool, after time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending += text
	return t.requestLocked(now)
}

// restart drops the pending text and says that the text taken so far is void. It is a request
// for repaint like add.
func (t *deltaThrottle) restart(now time.Time) (post bool, after time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending = ""
	t.reset = true
	return t.requestLocked(now)
}

// requestLocked decides the repaint. Repaints are at least interval apart; while one is scheduled
// no other is.
func (t *deltaThrottle) requestLocked(now time.Time) (bool, time.Duration) {
	if t.armed {
		return false, 0
	}
	elapsed := now.Sub(t.last)
	if t.last.IsZero() || elapsed >= t.interval {
		t.last = now
		return true, 0
	}
	t.armed = true
	return false, t.interval - elapsed
}

// due is called when a scheduled repaint fires. It reports whether the UI should be asked to repaint.
func (t *deltaThrottle) due(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.armed = false
	t.last = now
	return true
}

// take returns the text collected since the last take. reset is true when the answer was started
// again since then: the UI replaces what it shows with text.
func (t *deltaThrottle) take() (text string, reset bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	text, reset = t.pending, t.reset
	t.pending = ""
	t.reset = false
	return text, reset
}

// streamBackend is implemented by a Backend that writes the answer as it comes (the assistant).
// A Backend without it is asked whole, and its answer appears at once.
type streamBackend interface {
	AskStream(ctx context.Context, question string, withScreenshot bool, s ai.Stream) (ai.Response, error)
}
