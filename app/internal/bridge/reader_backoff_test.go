package bridge

import (
	"errors"
	"image"
	"math/rand"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/capture"
)

// stepClock is the fake clock of the reader tests.
type stepClock struct{ t time.Time }

func (c *stepClock) now() time.Time          { return c.t }
func (c *stepClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// fakeReader is a Reader whose screen, window and WoW lookup are scripted.
type fakeReader struct {
	r           *Reader
	clock       *stepClock
	screenCalls int
	windowCalls int
	findErr     error
	screen      *image.RGBA
	window      *image.RGBA
	windowErr   error
}

func newFakeReader(t *testing.T) *fakeReader {
	t.Helper()
	f := &fakeReader{clock: &stepClock{t: time.Now()}}
	f.screen = image.NewRGBA(image.Rect(0, 0, StripWidth, StripHeight))
	f.window = image.NewRGBA(image.Rect(0, 0, StripWidth, StripHeight))
	r := NewReader(250 * time.Millisecond)
	r.now = f.clock.now
	r.find = func() (uintptr, image.Rectangle, error) {
		if f.findErr != nil {
			return 0, image.Rectangle{}, f.findErr
		}
		return 0x1234, image.Rect(0, 0, 1920, 1080), nil
	}
	r.grabScreen = func(image.Rectangle) (*image.RGBA, error) {
		f.screenCalls++
		return f.screen, nil
	}
	r.grabWindow = func(uintptr, image.Rectangle) (*image.RGBA, error) {
		f.windowCalls++
		return f.window, f.windowErr
	}
	f.r = r
	return f
}

// validStrip renders a strip that decodes to a known state.
func validStrip(seed int64) *image.RGBA {
	s := sampleStates()[0]
	return renderStrip(EncodeForTest(s, 9), 5.33, rand.New(rand.NewSource(seed)), identity, 0)
}

func TestReaderBacksOffWhileDecodeFails(t *testing.T) {
	f := newFakeReader(t)
	for i := 1; i < slowAfterFails; i++ {
		if got := f.r.step(); got != retryEvery {
			t.Fatalf("failure %d: wait %v, want %v", i, got, retryEvery)
		}
	}
	for i := 0; i < 3; i++ {
		if got := f.r.step(); got != slowRetryEvery {
			t.Fatalf("after %d+ failures: wait %v, want %v", slowAfterFails, got, slowRetryEvery)
		}
	}
}

func TestReaderBacksOffOnAnyError(t *testing.T) {
	f := newFakeReader(t)
	f.findErr = errors.New("FindWindow: access denied")
	if got := f.r.step(); got != retryEvery {
		t.Fatalf("generic error: wait %v, want %v", got, retryEvery)
	}
	f.findErr = capture.ErrWoWNotFound
	if got := f.r.step(); got != retryEvery {
		t.Fatalf("WoW not found: wait %v, want %v", got, retryEvery)
	}
}

func TestReaderResetsToPollIntervalOnSuccess(t *testing.T) {
	f := newFakeReader(t)
	for i := 0; i < slowAfterFails+1; i++ {
		f.r.step()
	}
	f.screen = validStrip(1)
	if got := f.r.step(); got != f.r.pollEvery {
		t.Fatalf("after a good read: wait %v, want %v", got, f.r.pollEvery)
	}
	if f.r.failStreak != 0 {
		t.Fatalf("failStreak = %d after a good read, want 0", f.r.failStreak)
	}
	if _, ok := f.r.Latest(); !ok {
		t.Fatal("good read did not publish a state")
	}
}

func TestReaderWindowFallbackRunsAtPollRate(t *testing.T) {
	f := newFakeReader(t)
	f.window = validStrip(3) // the screen copy is covered; WoW's own render decodes
	for i := 0; i < 8; i++ {
		if got := f.r.step(); got != f.r.pollEvery {
			t.Fatalf("poll %d while covered: wait %v, want %v", i, got, f.r.pollEvery)
		}
		f.clock.advance(f.r.pollEvery)
	}
	if f.windowCalls != 8 {
		t.Fatalf("window fallback ran %d times over 8 covered polls, want 8", f.windowCalls)
	}
	if f.screenCalls != 1 {
		t.Fatalf("screen copy tried %d times in 2 s while on the window path, want 1 (the first poll)", f.screenCalls)
	}
	if got := f.r.channel.Source(); got != SourceWindow {
		t.Fatalf("source %q, want %q", got, SourceWindow)
	}
}

func TestReaderReturnsToScreenAfterRetry(t *testing.T) {
	f := newFakeReader(t)
	f.window = validStrip(4)
	f.r.step() // covered: the screen copy fails, WoW's render decodes
	f.clock.advance(screenRetryEvery - time.Millisecond)
	f.r.step()
	if f.screenCalls != 1 {
		t.Fatalf("screen copy retried before %v: %d calls", screenRetryEvery, f.screenCalls)
	}
	f.clock.advance(time.Millisecond)
	f.screen = validStrip(5) // the cover is gone
	f.r.step()
	if f.screenCalls != 2 {
		t.Fatalf("screen copy calls = %d after %v, want 2", f.screenCalls, screenRetryEvery)
	}
	if got := f.r.channel.Source(); got != SourceScreen {
		t.Fatalf("source %q after the screen decoded, want %q", got, SourceScreen)
	}
	if f.windowCalls != 2 {
		t.Fatalf("window calls = %d, want 2 (none after the switch back)", f.windowCalls)
	}
}

func TestReaderWindowFallbackDecodesCoveredStrip(t *testing.T) {
	f := newFakeReader(t)
	f.window = validStrip(2) // the screen copy is covered; WoW's own render decodes
	if got := f.r.step(); got != f.r.pollEvery {
		t.Fatalf("wait %v after a fallback read, want %v", got, f.r.pollEvery)
	}
	if f.windowCalls != 1 {
		t.Fatalf("window calls = %d, want 1", f.windowCalls)
	}
	if _, ok := f.r.Latest(); !ok {
		t.Fatal("fallback read did not publish a state")
	}
}
