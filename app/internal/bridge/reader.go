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
	retryWhenNoWoW   = 2 * time.Second
	defaultPollEvery = 250 * time.Millisecond
	eventBuffer      = 16
)

// FindWoWWindow returns the WoW window handle and its client-area rectangle in
// screen coordinates. It matches class "GxWindowClass" or title "World of Warcraft".
func FindWoWWindow() (hwnd uintptr, rect image.Rectangle, err error) {
	return capture.FindWoWWindow()
}

// Reader polls the top-left StripWidth x StripHeight region (28 blocks, up to 16 px
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
}

// NewReader creates a Reader that reads the strip every pollEvery (default 250ms).
func NewReader(pollEvery time.Duration) *Reader {
	if pollEvery <= 0 {
		pollEvery = defaultPollEvery
	}
	return &Reader{
		pollEvery: pollEvery,
		events:    make(chan LiveState, eventBuffer),
	}
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
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		next := r.pollEvery
		if err := r.pollOnce(); err != nil {
			if errors.Is(err, capture.ErrWoWNotFound) {
				r.lost()
			}
			if errors.Is(err, capture.ErrWoWNotFound) || errors.Is(err, capture.ErrUnsupported) {
				next = retryWhenNoWoW
			}
		}
		timer.Reset(next)
	}
}

func (r *Reader) pollOnce() error {
	_, rect, err := FindWoWWindow()
	if err != nil {
		return fmt.Errorf("locate WoW: %w", err)
	}
	strip := image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+StripWidth, rect.Min.Y+StripHeight)
	img, err := capture.GrabScreen(strip)
	if err != nil {
		return fmt.Errorf("grab pixel strip: %w", err)
	}
	st, err := DecodeImage(img)
	if err != nil {
		return err
	}
	st.ReadAt = time.Now()
	r.publish(st)
	return nil
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
