package bridge

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/capture"
)

const (
	staleAfter       = 5 * time.Second
	defaultPollEvery = 250 * time.Millisecond
	eventBuffer      = 16

	// retryEvery is the wait after a failed read. slowRetryEvery replaces it once
	// slowAfterFails reads in a row have failed, so a strip that never decodes (WoW
	// closed, loading screen, a cover window) costs little.
	retryEvery     = 2 * time.Second
	slowRetryEvery = 5 * time.Second
	slowAfterFails = 5
	// windowGrabEvery bounds the PrintWindow fallback, which renders the whole client.
	windowGrabEvery = 2 * time.Second
)

// FindWoWWindow returns the WoW window handle and its client-area rectangle in
// screen coordinates. It matches class "GxWindowClass" or title "World of Warcraft".
func FindWoWWindow() (hwnd uintptr, rect image.Rectangle, err error) {
	return capture.FindWoWWindow()
}

// Reader polls the top-left StripWidth x StripHeight region (NumBlocks blocks, up to 16 px
// pitch) of the WoW client area and decodes it.
// It keeps working across WoW being closed, reopened or minimized.
type Reader struct {
	pollEvery time.Duration
	events    chan LiveState

	startOnce sync.Once

	mu      sync.RWMutex
	latest  LiveState
	valid   bool
	haveSeq bool
	lastSeq uint8

	// Poll-loop state, used only by the run goroutine (step).
	failStreak     int
	pitch          float64   // pitch of the last successful decode; 0 when unknown
	lastWindowGrab time.Time // when the PrintWindow fallback last ran

	// Seams for tests; NewReader wires the real Windows calls.
	find       func() (uintptr, image.Rectangle, error)
	grabScreen func(image.Rectangle) (*image.RGBA, error)
	grabWindow func(uintptr, image.Rectangle) (*image.RGBA, error)
	now        func() time.Time
	win        windowGrabber
}

// NewReader creates a Reader that reads the strip every pollEvery (default 250ms).
func NewReader(pollEvery time.Duration) *Reader {
	if pollEvery <= 0 {
		pollEvery = defaultPollEvery
	}
	r := &Reader{
		pollEvery:  pollEvery,
		events:     make(chan LiveState, eventBuffer),
		find:       FindWoWWindow,
		grabScreen: capture.GrabScreen,
		now:        time.Now,
	}
	r.grabWindow = r.win.grab
	return r
}

// Start launches the poll loop in its own goroutine and returns immediately.
// Calling Start more than once has no further effect; the loop stops when ctx ends.
func (r *Reader) Start(ctx context.Context) {
	r.startOnce.Do(func() {
		go r.run(ctx)
	})
}

// Latest returns the last valid state. ok is false if there is none, or if it is
// older than 5 seconds.
func (r *Reader) Latest() (LiveState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.valid || time.Since(r.latest.ReadAt) > staleAfter {
		return LiveState{}, false
	}
	return r.latest, true
}

// Events emits a state each time EventSeq changes. The channel is buffered (16);
// when full the oldest queued event is dropped. The first valid read does not emit.
func (r *Reader) Events() <-chan LiveState {
	return r.events
}

func (r *Reader) run(ctx context.Context) {
	defer r.win.close()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		timer.Reset(r.step())
	}
}

// step does one poll and returns how long to wait before the next one: the configured
// interval while reads succeed, retryEvery after a failure, and slowRetryEvery once
// slowAfterFails reads in a row have failed.
func (r *Reader) step() time.Duration {
	err := r.pollOnce()
	if err == nil {
		r.failStreak = 0
		return r.pollEvery
	}
	if errors.Is(err, capture.ErrWoWNotFound) {
		r.lost()
	}
	r.failStreak++
	if r.failStreak >= slowAfterFails {
		return slowRetryEvery
	}
	return retryEvery
}

func (r *Reader) pollOnce() error {
	hwnd, rect, err := r.find()
	if err != nil {
		return fmt.Errorf("locate WoW: %w", err)
	}
	st, err := r.readStrip(hwnd, rect)
	if err != nil {
		return err
	}
	st.ReadAt = r.now()
	r.publish(st)
	return nil
}

// readStrip decodes the strip at the top-left of WoW's client area rect. It first
// copies the strip from the screen, which is cheap. If that does not decode, a
// window on top of WoW (the companion app, a browser) is probably covering the
// strip, so it asks WoW to render its own content instead. That fallback runs at
// most once per windowGrabEvery.
func (r *Reader) readStrip(hwnd uintptr, rect image.Rectangle) (LiveState, error) {
	strip := image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+StripWidth, rect.Min.Y+StripHeight)
	img, err := r.grabScreen(strip)
	if err != nil {
		return LiveState{}, fmt.Errorf("grab pixel strip: %w", err)
	}
	st, pitch, err := decodeStrip(img, r.pitch)
	if err == nil {
		r.pitch = pitch
		return st, nil
	}
	now := r.now()
	if !r.lastWindowGrab.IsZero() && now.Sub(r.lastWindowGrab) < windowGrabEvery {
		return LiveState{}, err
	}
	r.lastWindowGrab = now
	own, gerr := r.grabWindow(hwnd, rect)
	if gerr != nil {
		return LiveState{}, err
	}
	if st2, p2, err2 := decodeStrip(own, r.pitch); err2 == nil {
		r.pitch = p2
		return st2, nil
	}
	return LiveState{}, err
}

// lost is called when WoW is not visible. The next valid read starts a new
// event baseline, so reopening the game does not replay old events.
func (r *Reader) lost() {
	r.mu.Lock()
	r.haveSeq = false
	r.mu.Unlock()
}

func (r *Reader) publish(st LiveState) {
	r.mu.Lock()
	r.latest = st
	r.valid = true
	emit := r.haveSeq && st.EventSeq != r.lastSeq
	r.haveSeq = true
	r.lastSeq = st.EventSeq
	r.mu.Unlock()

	if emit {
		r.send(st)
	}
}

// send queues st without blocking. When the buffer is full the oldest event is dropped.
func (r *Reader) send(st LiveState) {
	for {
		select {
		case r.events <- st:
			return
		default:
		}
		select {
		case <-r.events:
		default:
		}
	}
}
