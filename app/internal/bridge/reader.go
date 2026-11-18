package bridge

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sync"
	"sync/atomic"
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
	// screenRetryEvery is how often the screen copy is tried again while the strip is read
	// from WoW's own render (a window covers WoW). The reader switches back as soon as the
	// screen copy decodes.
	screenRetryEvery = 5 * time.Second
)

// FindWoWWindow returns the WoW window handle and its client-area rectangle in
// screen coordinates. It matches class "GxWindowClass" or title "World of Warcraft".
func FindWoWWindow() (hwnd uintptr, rect image.Rectangle, err error) {
	return capture.FindWoWWindow()
}

// Reader polls the top-left StripWidth x StripHeight region (NumBlocks blocks, up to 16 px
// pitch) of the WoW client area and decodes it.
// It keeps working across WoW being closed, reopened or minimized.
//
// The reader can be switched off (SetActive). While it is off the poll goroutine waits on a
// channel: no window is located, no pixel is copied and no capture is made. A new reader is
// active, so the continuous mode and the tools that run a reader of their own are unchanged.
type Reader struct {
	pollEvery time.Duration
	events    chan LiveState

	startOnce sync.Once

	// active gates the poll loop (see SetActive); wake ends the wait of an inactive loop.
	activeMu sync.Mutex
	active   bool
	wake     chan struct{}
	// captures counts the screen and window copies made so far (see Captures).
	captures atomic.Int64

	mu      sync.RWMutex
	latest  LiveState
	valid   bool
	haveSeq bool
	lastSeq uint8
	subs    []func(LiveState) // see Subscribe; guarded by mu

	// Poll-loop state, used only by the run goroutine (step).
	failStreak    int
	pitch         float64   // pitch of the last successful decode; 0 when unknown
	onWindow      bool      // the last good read came from WoW's own render (PrintWindow)
	lastScreenTry time.Time // when the screen copy was last tried

	// channel receives the v3 frames: messages reassembled into LiveData. Set once in NewReader.
	channel *Channel

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
		channel:    NewChannel(),
		active:     true,
		wake:       make(chan struct{}, 1),
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

// SetActive switches the poll loop on or off. While it is off no capture is made and the
// loop only waits; switching it on polls at once. The call never blocks.
func (r *Reader) SetActive(on bool) {
	r.activeMu.Lock()
	changed := r.active != on
	r.active = on
	r.activeMu.Unlock()
	if changed && on {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}

// Active reports whether the poll loop is on (see SetActive).
func (r *Reader) Active() bool {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	return r.active
}

// Captures is the number of screen or window copies the reader has made since it was
// created. It does not move while the reader is inactive; --diag reports it.
func (r *Reader) Captures() int64 {
	return r.captures.Load()
}

// Channel returns the live data channel (v3 messages). Its store is safe to read from any goroutine.
func (r *Reader) Channel() *Channel {
	return r.channel
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
		if !r.Active() {
			// Idle: no step, so no window lookup and no capture, until SetActive(true).
			select {
			case <-ctx.Done():
				return
			case <-r.wake:
			}
			timer.Reset(0)
			continue
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
	dec, err := r.readStrip(hwnd, rect)
	if err != nil {
		return err
	}
	now := r.now()
	dec.Live.ReadAt = now
	if dec.Frame != nil {
		r.channel.Feed(*dec.Frame, now)
	}
	r.publish(dec.Live)
	return nil
}

// readStrip decodes the strip at the top-left of WoW's client area rect: the top
// StripHeightV3 rows at the client's full width (v3 spans the width; v2 uses its
// left 464 px).
//
// It copies the strip from the screen first, which is cheap. When that does not decode,
// a window on top of WoW (the companion app, a browser) is probably covering the strip,
// so the strip is read from WoW's own render (PrintWindow) instead, at the poll rate, for
// as long as that decodes. While it does, the screen copy is retried every
// screenRetryEvery, and the reader switches back to the screen as soon as it decodes.
func (r *Reader) readStrip(hwnd uintptr, rect image.Rectangle) (Decoded, error) {
	now := r.now()
	var screenErr error
	if !r.onWindow || now.Sub(r.lastScreenTry) >= screenRetryEvery {
		r.lastScreenTry = now
		dec, err := r.decodeScreen(rect)
		if err == nil {
			r.onWindow = false
			r.channel.SetSource(SourceScreen)
			return dec, nil
		}
		screenErr = err
	}
	r.captures.Add(1)
	own, err := r.grabWindow(hwnd, rect)
	if err != nil {
		if screenErr != nil {
			return Decoded{}, screenErr
		}
		return Decoded{}, err
	}
	dec, err := DecodeAny(own, r.pitch)
	if err != nil {
		if screenErr != nil {
			return Decoded{}, screenErr
		}
		return Decoded{}, err
	}
	r.pitch = dec.Pitch
	r.onWindow = true
	r.channel.SetSource(SourceWindow)
	return dec, nil
}

// decodeScreen copies the strip from the screen and decodes it.
func (r *Reader) decodeScreen(rect image.Rectangle) (Decoded, error) {
	strip := image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+rect.Dx(), rect.Min.Y+StripHeightV3)
	r.captures.Add(1)
	img, err := r.grabScreen(strip)
	if err != nil {
		return Decoded{}, fmt.Errorf("grab pixel strip: %w", err)
	}
	dec, err := DecodeAny(img, r.pitch)
	if err != nil {
		return Decoded{}, err
	}
	r.pitch = dec.Pitch
	return dec, nil
}

// lost is called when WoW is not visible. The next valid read starts a new
// event baseline, so reopening the game does not replay old events.
func (r *Reader) lost() {
	r.mu.Lock()
	r.haveSeq = false
	r.mu.Unlock()
}

// Subscribe registers f to be called with every valid state, in the poll goroutine, after
// the state is stored. Unlike Events, every subscriber sees every read, so several
// components can follow the game without splitting the events between them. f must be
// quick and must not block.
func (r *Reader) Subscribe(f func(LiveState)) {
	if f == nil {
		return
	}
	r.mu.Lock()
	r.subs = append(r.subs, f)
	r.mu.Unlock()
}

func (r *Reader) publish(st LiveState) {
	r.mu.Lock()
	r.latest = st
	r.valid = true
	emit := r.haveSeq && st.EventSeq != r.lastSeq
	r.haveSeq = true
	r.lastSeq = st.EventSeq
	subs := r.subs
	r.mu.Unlock()

	for _, f := range subs {
		f(st)
	}
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
