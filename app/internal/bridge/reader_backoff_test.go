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

func TestReaderWindowFallbackRunsAtMostEvery2s(t *testing.T) {
	f := newFakeReader(t)
	for i := 0; i < 8; i++ { // 8 polls, all within 2 s of each other
		f.r.step()
	}
	if f.windowCalls != 1 {
		t.Fatalf("window fallback ran %d times in one burst, want 1", f.windowCalls)
	}
	f.clock.advance(windowGrabEvery - time.Millisecond)
	f.r.step()
	if f.windowCalls != 1 {
		t.Fatalf("window fallback ran %d times before 2 s, want 1", f.windowCalls)
	}
	f.clock.advance(2 * time.Millisecond)
	f.r.step()
	if f.windowCalls != 2 {
		t.Fatalf("window fallback ran %d times after 2 s, want 2", f.windowCalls)
	}
	if f.screenCalls != 10 {
		t.Fatalf("screen strip read %d times, want 10 (every poll)", f.screenCalls)
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
