package main

import (
	"context"
	"log"
	"runtime/debug"
	"sync"
	"time"
)

const (
	// gcPercent is the GC target: the heap may grow 50 % past the live data before a
	// collection (the default is 100). The live heap is small, so the extra collections are
	// cheap and the idle app keeps less garbage around.
	gcPercent = 50
	// idleAfter is how long the panel must go without asking for its status before the app
	// counts as idle. The panel asks only while it is visible (every two seconds).
	idleAfter = 60 * time.Second
	// svIdleAfter is how long the SavedVariables tree may go unused before it is dropped.
	svIdleAfter = 5 * time.Minute
	// idleCheckEvery is how often the idle checks run.
	idleCheckEvery = 15 * time.Second
)

// idleTracker follows whether the app is active: the on-demand controller reports it, and the
// reader is on while the panel is open, a question runs or the continuous mode is set. Idle
// means inactive for at least a given time.
type idleTracker struct {
	now func() time.Time

	mu       sync.Mutex
	active   bool
	since    time.Time // when the app last changed between active and idle
	released bool      // the idle release already ran for this idle spell
}

func newIdleTracker(now func() time.Time) *idleTracker {
	return &idleTracker{now: now, since: now()}
}

// SetActive records whether the app is active now. Each change starts a new spell, so the
// release runs again after the next idle period.
func (t *idleTracker) SetActive(on bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active == on {
		return
	}
	t.active = on
	t.since = t.now()
	t.released = false
}

// newIdle reports whether the app has just become idle: it has been inactive for at least after
// and no release ran for this spell yet. It marks the spell as released.
func (t *idleTracker) newIdle(now time.Time, after time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active || t.released || now.Sub(t.since) < after {
		return false
	}
	t.released = true
	return true
}

// idleReleaser is one piece of memory the app can give back when idle. It reports whether it
// freed anything.
type idleReleaser func(now time.Time) bool

// runIdleRelease runs the idle checks until ctx ends. When the app first goes idle it shrinks
// the store cache and returns the freed heap to the OS. A releaser that drops data (the
// SavedVariables tree after svIdleAfter) is followed by the same return to the OS. Nothing
// runs while the app is in use, so the steady CPU stays near zero.
func runIdleRelease(ctx context.Context, t *idleTracker, every time.Duration, idleHook func(), releasers ...idleReleaser) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		now := t.now()
		freed := false
		if t.newIdle(now, idleAfter) {
			idleHook()
			freed = true
		}
		for _, r := range releasers {
			if r(now) {
				freed = true
			}
		}
		if freed {
			debug.FreeOSMemory()
		}
	}
}

// releaseStoreCache is the idle hook for the store: it asks SQLite to shrink its cache.
func releaseStoreCache(shrink func() error) func() {
	return func() {
		if err := shrink(); err != nil {
			log.Printf("idle: %v", err)
		}
	}
}
