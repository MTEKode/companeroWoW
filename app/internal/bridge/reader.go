package bridge

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/capture"
	"github.com/MTEKode/companeroWoW/app/internal/diag"
)

const (
	staleAfter       = 5 * time.Second
	defaultPollEvery = 250 * time.Millisecond
	eventBuffer      = 16

	// watchPollEvery is the poll interval while the reader watches for the addon's share (the
	// on-demand hold). The addon draws a new frame every REFRESH_SEC (0.2 s) and sends each chunk
	// once, in the frame that shows it. A poll slower than the frame rate skips frames, and the
	// chunks in them are never sent again, so the messages of a share would not complete. 100 ms
	// samples every frame at least once.
	watchPollEvery = 100 * time.Millisecond

	// retryEvery is the wait after a failed read. slowRetryEvery replaces it once
	// slowAfterFails reads in a row have failed, so a strip that never decodes (WoW
	// closed, loading screen, a cover window) costs little. Neither applies while the reader
	// watches (see SetWatch): there the strip is expected and a failed read waits watchPollEvery.
	retryEvery     = 2 * time.Second
	slowRetryEvery = 5 * time.Second
	slowAfterFails = 5
	// screenRetryEvery is how often the screen copy is tried again while the strip is read
	// from WoW's own render (a window covers WoW). The reader switches back as soon as the
	// screen copy decodes.
	screenRetryEvery = 5 * time.Second

	// summaryEvery is the span of the one-line summary of the reads while the reader watches.
	summaryEvery = time.Second
)

// FindWoWWindow returns the WoW window handle and its client-area rectangle in
// screen coordinates. It matches class "GxWindowClass" or title "World of Warcraft".
func FindWoWWindow() (hwnd uintptr, rect image.Rectangle, err error) {
	return capture.FindWoWWindow()
}

// ReaderStats are the counters of the reader, for the runtime state and the log (see Stats).
type ReaderStats struct {
	Captures          int64     // screen and window copies made (each attempt counts)
	FramesOK          int64     // reads that decoded a strip (v3 or v2)
	FramesErr         int64     // reads that failed: no WoW, a failed copy or a strip that did not decode
	LastError         string    // the error of the last failed read ("" when none yet)
	LastErrorAt       time.Time // when LastError happened
	LastOKAt          time.Time // when the last read decoded
	Source            string    // where the strip was last read from (SourceScreen or SourceWindow)
	Pitch             float64   // pitch of the last decode
	ShareRemainingSec int       // the addon's share countdown in the last decoded status
	Gain              float64   // the colour gain of the last read, when it failed for that reason (0 otherwise)
}

// Reader polls the top-left StripWidth x StripHeight region (NumBlocks blocks, up to 16 px
// pitch) of the WoW client area and decodes it.
// It keeps working across WoW being closed, reopened or minimized.
//
// The reader can be switched off (SetActive). While it is off the poll goroutine waits on a
// channel: no window is located, no pixel is copied and no capture is made. A new reader is
// active, so the continuous mode and the tools that run a reader of their own are unchanged.
// While it watches (SetWatch) it polls at watchPollEvery and does not back off on failures.
type Reader struct {
	pollEvery time.Duration
	events    chan LiveState

	startOnce sync.Once

	// active gates the poll loop (see SetActive); watch selects the watch polling (see SetWatch);
	// wake ends the wait of an inactive loop.
	activeMu sync.Mutex
	active   bool
	watch    bool
	wake     chan struct{}
	// captures counts the screen and window copies made so far (see Captures).
	captures atomic.Int64

	// stats are the counters read by Stats; the poll goroutine writes them under statsMu.
	statsMu sync.Mutex
	stats   ReaderStats

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
	lastPath      string    // where the last read came from: SourceScreen, SourceWindow or ""
	lastShare     int       // shareRemainingSec of the last decoded status
	lastN         int       // blocks in the row of the last decoded v3 frame (0 for v2)
	gainSince     time.Time // when the current run of colour-gain reads began (statsMu)
	gainTraced    bool      // the trace has said that this run passed ColourGainAfter (poll goroutine)
	sum           summary   // the reads since the last summary line (see flushSummary)

	// channel receives the v3 frames: messages reassembled into LiveData. Set once in NewReader.
	channel *Channel

	// Seams for tests; NewReader wires the real Windows calls.
	find       func() (uintptr, image.Rectangle, error)
	grabScreen func(image.Rectangle) (*image.RGBA, error)
	grabWindow func(uintptr, image.Rectangle) (*image.RGBA, error)
	now        func() time.Time
	win        windowGrabber
}

// summary counts the reads of one summary span.
type summary struct {
	start    time.Time
	reads    int
	okScreen int
	okWindow int
	failed   int
	lastErr  string
	spent    time.Duration // time spent in the reads
	share    int           // the last shareRemainingSec decoded
	pitch    float64
	blocks   int // blocks in the row of the last decoded v3 frame
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

// SetWatch sets the watch of the on-demand hold: while it is on the reader polls at
// watchPollEvery and a failed read is retried at that rate instead of backing off, because the
// addon shows its strip only while it shares (a question is then sent in one frame). The call
// never blocks.
func (r *Reader) SetWatch(on bool) {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	r.watch = on
}

// Watching reports whether the watch is on (see SetWatch).
func (r *Reader) Watching() bool {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	return r.watch
}

// Captures is the number of screen or window copies the reader has made since it was
// created. It does not move while the reader is inactive; --diag reports it.
func (r *Reader) Captures() int64 {
	return r.captures.Load()
}

// ColourGainAlert reports the colour gain of the strip when it has been out of the decodable range
// for ColourGainAfter or longer while the reader is on. The player is then told to reset the
// display settings (see ColourGainText).
func (r *Reader) ColourGainAlert(now time.Time) (float64, bool) {
	if !r.Active() {
		return 0, false
	}
	r.statsMu.Lock()
	defer r.statsMu.Unlock()
	if r.gainSince.IsZero() || now.Sub(r.gainSince) < ColourGainAfter {
		return 0, false
	}
	return r.stats.Gain, true
}

// Stats returns the reader's counters (see ReaderStats).
func (r *Reader) Stats() ReaderStats {
	r.statsMu.Lock()
	defer r.statsMu.Unlock()
	st := r.stats
	st.Captures = r.captures.Load()
	return st
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
			r.flushSummary()
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
// slowAfterFails reads in a row have failed. While the reader watches, the interval is
// watchPollEvery and a failed read waits watchPollEvery too (WoW not found still backs off).
func (r *Reader) step() time.Duration {
	watch := r.Watching()
	started := time.Now()
	err := r.pollOnce()
	r.note(time.Since(started), err, watch)
	if err == nil {
		r.failStreak = 0
		return r.interval(watch)
	}
	if errors.Is(err, capture.ErrWoWNotFound) {
		r.lost()
	}
	r.failStreak++
	if watch && !errors.Is(err, capture.ErrWoWNotFound) {
		return watchPollEvery
	}
	if r.failStreak >= slowAfterFails {
		return slowRetryEvery
	}
	return retryEvery
}

// interval is the wait after a successful read.
func (r *Reader) interval(watch bool) time.Duration {
	if watch && r.pollEvery > watchPollEvery {
		return watchPollEvery
	}
	return r.pollEvery
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

// note records the outcome of one poll: the counters, the summary span and, with --trace, the
// read itself. It runs on the poll goroutine.
func (r *Reader) note(spent time.Duration, err error, watch bool) {
	now := r.now()
	path := r.lastPath
	r.lastPath = ""
	r.statsMu.Lock()
	st := &r.stats
	if err == nil {
		st.FramesOK++
		st.LastOKAt = now
		st.Source = path
		st.Pitch = r.pitch
		st.ShareRemainingSec = r.lastShare
	} else {
		st.FramesErr++
		st.LastError = err.Error()
		st.LastErrorAt = now
	}
	gain, isGain := ColourGainOf(err)
	if isGain {
		if r.gainSince.IsZero() {
			r.gainSince = now
		}
		st.Gain = gain
	} else {
		r.gainSince = time.Time{}
		r.gainTraced = false
		st.Gain = 0
	}
	since := r.gainSince
	r.statsMu.Unlock()

	if isGain && !r.gainTraced && now.Sub(since) >= ColourGainAfter {
		r.gainTraced = true
		diag.Tracef("colores alterados: ganancia x%.2f desde hace %.1f s; el juego no se lee (brillo/gamma/intensidad digital/HDR)", gain, now.Sub(since).Seconds())
	}

	if diag.Tracing() {
		if err == nil {
			diag.Tracef("lectura: %s ok, pitch %.3f, compartiendo %d s, %.1f ms", path, r.pitch, r.lastShare, float64(spent.Microseconds())/1000)
		} else {
			diag.Tracef("lectura: fallo (%.1f ms): %v", float64(spent.Microseconds())/1000, err)
		}
	}
	if !watch {
		// Not watching (continuous mode): no summary lines, so nothing is kept for one.
		r.sum = summary{}
		return
	}
	if r.sum.start.IsZero() {
		r.sum.start = now
	}
	s := &r.sum
	s.reads++
	s.spent += spent
	if err == nil {
		switch path {
		case SourceWindow:
			s.okWindow++
		default:
			s.okScreen++
		}
		s.pitch = r.pitch
		s.share = r.lastShare
		s.blocks = r.lastN
	} else {
		s.failed++
		s.lastErr = err.Error()
	}
	if now.Sub(s.start) >= summaryEvery {
		r.flushSummary()
	}
}

// flushSummary writes the one-line summary of the reads since the last one, when there were any.
func (r *Reader) flushSummary() {
	s := &r.sum
	if s.reads == 0 {
		return
	}
	avg := float64(s.spent.Microseconds()) / 1000 / float64(s.reads)
	span := r.now().Sub(s.start).Seconds()
	line := fmt.Sprintf("lectura del juego: %d lecturas en %.1f s: pantalla ok %d, ventana ok %d, fallidas %d; %.1f ms por lectura; pitch %.3f; N %d; compartiendo %d s",
		s.reads, span, s.okScreen, s.okWindow, s.failed, avg, s.pitch, s.blocks, s.share)
	if s.failed > 0 && s.lastErr != "" {
		line += "; último error: " + s.lastErr
	}
	log.Print(line)
	*s = summary{}
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
// r.lastPath is set to the source of a decoded strip.
func (r *Reader) readStrip(hwnd uintptr, rect image.Rectangle) (Decoded, error) {
	r.lastPath = ""
	now := r.now()
	var screenErr error
	if !r.onWindow || now.Sub(r.lastScreenTry) >= screenRetryEvery {
		r.lastScreenTry = now
		dec, err := r.decodeScreen(rect)
		if err == nil {
			r.onWindow = false
			r.channel.SetSource(SourceScreen)
			r.lastPath = SourceScreen
			r.lastShare = int(dec.Live.ShareRemainingSec)
			r.lastN = frameBlocks(dec)
			return dec, nil
		}
		screenErr = fmt.Errorf("pantalla: %w", err)
	}
	r.captures.Add(1)
	own, err := r.grabWindow(hwnd, rect)
	if err != nil {
		err = fmt.Errorf("ventana: %w", err)
		if screenErr != nil {
			return Decoded{}, fmt.Errorf("%w; %v", screenErr, err)
		}
		return Decoded{}, err
	}
	dec, err := DecodeAny(own, r.pitch)
	if err != nil {
		err = fmt.Errorf("ventana: %w", err)
		if screenErr != nil {
			return Decoded{}, fmt.Errorf("%w; %v", screenErr, err)
		}
		return Decoded{}, err
	}
	r.pitch = dec.Pitch
	r.onWindow = true
	r.channel.SetSource(SourceWindow)
	r.lastPath = SourceWindow
	r.lastShare = int(dec.Live.ShareRemainingSec)
	r.lastN = frameBlocks(dec)
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

// frameBlocks is the block count of a decoded v3 frame (0 for a v2 strip).
func frameBlocks(dec Decoded) int {
	if dec.Frame == nil {
		return 0
	}
	return dec.Frame.N
}
