package main

import (
	"context"
	"testing"
	"time"
)

// TestIdleTrackerReleasesOncePerSpell: the idle release runs once after the app goes inactive,
// and again only after it was active in between.
func TestIdleTrackerReleasesOncePerSpell(t *testing.T) {
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tr := newIdleTracker(func() time.Time { return clock })

	clock = clock.Add(30 * time.Second)
	if tr.newIdle(clock, idleAfter) {
		t.Fatal("idle before the idle time passed")
	}
	clock = clock.Add(idleAfter)
	if !tr.newIdle(clock, idleAfter) {
		t.Fatal("not idle after the idle time passed")
	}
	if tr.newIdle(clock.Add(time.Minute), idleAfter) {
		t.Fatal("the same idle spell released twice")
	}

	clock = clock.Add(time.Hour)
	tr.SetActive(true)
	clock = clock.Add(time.Hour)
	if tr.newIdle(clock, idleAfter) {
		t.Fatal("idle while the app is active")
	}
	tr.SetActive(false)
	if tr.newIdle(clock.Add(time.Second), idleAfter) {
		t.Fatal("idle right after the app went inactive")
	}
	if !tr.newIdle(clock.Add(idleAfter), idleAfter) {
		t.Fatal("no release in the next idle spell")
	}
}

// TestRunIdleReleaseCallsHooks: the idle hook and the releasers run from the check loop.
func TestRunIdleReleaseCallsHooks(t *testing.T) {
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tr := newIdleTracker(func() time.Time { return clock })
	clock = clock.Add(2 * idleAfter)

	hooks := 0
	releases := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runIdleRelease(ctx, tr, time.Millisecond, func() { hooks++ },
			func(time.Time) bool { releases++; cancel(); return false })
	}()
	<-done
	if hooks != 1 {
		t.Errorf("idle hook ran %d times, want 1", hooks)
	}
	if releases < 1 {
		t.Error("releaser did not run")
	}
}
