package bridge

import (
	"errors"
	"image"
	"math/rand"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/capture"
)

// The addon draws a new frame every 0.2 s and sends each chunk once, in the frame that shows it.
// A watching reader must sample every frame: its poll is shorter than the frame period.
func TestWatchPollIsShorterThanTheAddonFrame(t *testing.T) {
	const frameEvery = 200 * time.Millisecond
	if watchPollEvery >= frameEvery {
		t.Fatalf("watch poll %v is not shorter than the %v frame period: frames would be skipped", watchPollEvery, frameEvery)
	}
}

// While it watches, a failed read is retried at the watch rate: the strip can appear at any time
// during a share, and backing off would miss its frames.
func TestWatchRetriesFailedReadsAtTheWatchRate(t *testing.T) {
	f := newFakeReader(t)
	f.r.SetWatch(true)
	for i := 0; i < 3*slowAfterFails; i++ {
		if got := f.r.step(); got != watchPollEvery {
			t.Fatalf("failure %d while watching: wait %v, want %v", i, got, watchPollEvery)
		}
	}
}

// WoW not found still backs off while watching: nothing is being shared then.
func TestWatchBacksOffWhenWoWIsNotFound(t *testing.T) {
	f := newFakeReader(t)
	f.r.SetWatch(true)
	f.findErr = errors.New("locate: no window")
	if got := f.r.step(); got != watchPollEvery {
		t.Fatalf("generic locate error while watching: wait %v, want %v", got, watchPollEvery)
	}
	f.findErr = capture.ErrWoWNotFound
	if got := f.r.step(); got != retryEvery {
		t.Fatalf("WoW not found while watching: wait %v, want %v", got, retryEvery)
	}
}

// Switching the watch off returns the reader to the normal backoff.
func TestWatchOffRestoresBackoff(t *testing.T) {
	f := newFakeReader(t)
	f.r.SetWatch(true)
	f.r.step()
	f.r.SetWatch(false)
	if f.r.Watching() {
		t.Fatal("SetWatch(false) did not stick")
	}
	for i := 1; i < slowAfterFails; i++ {
		f.r.step()
	}
	if got := f.r.step(); got != slowRetryEvery {
		t.Fatalf("after the watch: wait %v after %d failures, want %v", got, slowAfterFails, slowRetryEvery)
	}
}

// A good read while watching polls at the watch rate, and never slower than the configured interval.
func TestWatchPollIntervalAfterGoodRead(t *testing.T) {
	f := newFakeReader(t)
	f.screen = validStrip(7)
	f.r.SetWatch(true)
	if got := f.r.step(); got != watchPollEvery {
		t.Fatalf("good read while watching: wait %v, want %v", got, watchPollEvery)
	}
	slow := newFakeReader(t)
	slow.r.pollEvery = time.Second
	slow.screen = validStrip(8)
	slow.r.SetWatch(true)
	if got := slow.r.step(); got != watchPollEvery {
		t.Fatalf("slow configured poll while watching: wait %v, want %v", got, watchPollEvery)
	}
}

// Every read is counted: the captures, the decoded strips and the failed reads, with the last error
// and the share countdown of the last strip (what runtime.json reports).
func TestReaderStatsCountReads(t *testing.T) {
	f := newFakeReader(t)
	f.windowErr = errors.New("PrintWindow failed")
	f.r.step() // screen not decoded (blank strip), window failed: a failed read
	st := f.r.Stats()
	if st.FramesErr != 1 || st.FramesOK != 0 {
		t.Fatalf("after one failed read: ok %d, err %d", st.FramesOK, st.FramesErr)
	}
	if st.LastError == "" {
		t.Fatal("the failed read left no error text")
	}
	if st.Captures < 2 {
		t.Fatalf("captures = %d, want the screen and the window attempt (2)", st.Captures)
	}

	ok := validStrip(9)
	f.screen = ok
	f.clock.advance(time.Second)
	f.r.step()
	st = f.r.Stats()
	if st.FramesOK != 1 {
		t.Fatalf("FramesOK = %d after a decoded strip, want 1", st.FramesOK)
	}
	if st.Source != SourceScreen {
		t.Fatalf("source %q, want %q", st.Source, SourceScreen)
	}
	if st.LastOKAt.IsZero() {
		t.Fatal("LastOKAt not set after a decoded strip")
	}
}

// The strip of the addon's frames reaches the channel whole when the reader watches: eight chunks
// drawn one per 0.2 s frame are all sampled, so the message completes. (At the 250 ms interval of
// the normal mode, a frame is missed now and then; a missed chunk is never sent again.)
func TestWatchingReaderCompletesAMultiChunkMessage(t *testing.T) {
	const (
		n       = 300
		pitch   = 5.33
		chunks  = 8
		frameIv = 200 * time.Millisecond
	)
	f := newFakeReader(t)
	rng := rand.New(rand.NewSource(11))
	var got []byte
	f.r.Channel().OnMessage(func(mt uint8, body []byte, _ time.Time) {
		if mt == MsgQuests {
			got = append([]byte(nil), body...)
		}
	})
	body := make([]byte, 0, chunks*150)
	frames := make([]*image.RGBA, 0, chunks+5)
	for k := 0; k < chunks; k++ {
		part := make([]byte, 150)
		for i := range part {
			part[i] = byte(k*31 + i)
		}
		body = append(body, part...)
		fr, err := BuildFrameV3(v3Status(), uint8(k+1), n, &ChunkV3{Type: MsgQuests, ID: 77, Index: uint16(k), Count: chunks, Data: part})
		if err != nil {
			t.Fatalf("BuildFrameV3 %d: %v", k, err)
		}
		frames = append(frames, renderV3(BlocksV3(fr, n), pitch, 1920, rng, identity, 0))
	}
	// Idle frames after the chunks keep the status alive.
	idle, err := BuildFrameV3(v3Status(), 99, n, nil)
	if err != nil {
		t.Fatalf("BuildFrameV3 idle: %v", err)
	}
	idleImg := renderV3(BlocksV3(idle, n), pitch, 1920, rng, identity, 0)
	for len(frames) < chunks+5 {
		frames = append(frames, idleImg)
	}

	start := f.clock.t
	end := start.Add(time.Duration(len(frames)) * frameIv)
	f.r.SetWatch(true)
	for f.clock.t.Before(end) {
		// The frame on screen at the poll time: frame k is shown from start+k*frameIv.
		k := int(f.clock.t.Sub(start) / frameIv)
		if k >= len(frames) {
			k = len(frames) - 1
		}
		f.screen = frames[k]
		f.clock.advance(f.r.step()) // the reader's own interval while watching
	}
	if got == nil {
		t.Fatal("the watching reader did not complete the message")
	}
	if string(got) != string(body) {
		t.Fatalf("message body differs: %d bytes, want %d", len(got), len(body))
	}
}
