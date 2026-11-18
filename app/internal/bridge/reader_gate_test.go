package bridge

import (
	"context"
	"errors"
	"image"
	"sync/atomic"
	"testing"
	"time"
)

// gateRig is a reader whose window is found and whose copies fail, so that every poll is a
// counted capture attempt and nothing is decoded.
func gateRig(t *testing.T) (*Reader, *atomic.Int64) {
	t.Helper()
	r := NewReader(time.Millisecond)
	var screens atomic.Int64
	r.find = func() (uintptr, image.Rectangle, error) {
		return 1, image.Rect(0, 0, 800, 600), nil
	}
	r.grabScreen = func(image.Rectangle) (*image.RGBA, error) {
		screens.Add(1)
		return nil, errors.New("fake capture")
	}
	r.grabWindow = func(uintptr, image.Rectangle) (*image.RGBA, error) {
		return nil, errors.New("fake capture")
	}
	return r, &screens
}

// waitFor polls cond for up to two seconds.
func waitFor(cond func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

// An inactive reader makes no capture and no window lookup, however long it runs; once switched
// on it captures at once, and switching off stops the captures again.
func TestReaderInactiveMakesNoCaptures(t *testing.T) {
	r, screens := gateRig(t)
	r.SetActive(false)
	if r.Active() {
		t.Fatal("SetActive(false) did not stick")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	time.Sleep(60 * time.Millisecond)
	if n := screens.Load(); n != 0 {
		t.Fatalf("inactive reader copied the screen %d times", n)
	}
	if n := r.Captures(); n != 0 {
		t.Fatalf("inactive reader counted %d captures", n)
	}

	r.SetActive(true)
	if !waitFor(func() bool { return r.Captures() > 0 }) {
		t.Fatal("active reader made no capture")
	}
	r.SetActive(false)
	// Let the step in flight finish and the loop reach its idle wait.
	time.Sleep(20 * time.Millisecond)
	before := r.Captures()
	time.Sleep(120 * time.Millisecond)
	if after := r.Captures(); after != before {
		t.Fatalf("captures moved from %d to %d after the reader was switched off", before, after)
	}
}

// A new reader is active, so the continuous mode and the diagnosis read it as before.
func TestNewReaderIsActive(t *testing.T) {
	if !NewReader(time.Second).Active() {
		t.Fatal("NewReader must start active")
	}
}
