package shots

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// watchNothing is a Watch that reports no events until ctx ends: the rescan does the work.
func watchNothing(ctx context.Context, _ string, _ chan<- string) error {
	<-ctx.Done()
	return nil
}

// logSink collects the watcher's log lines.
type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, format)
}

func (l *logSink) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// waitFor polls cond until it holds or the test's deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func fastOptions(dir string, since time.Time, sink Sink, mfs *memFS) Options {
	return Options{
		Dir: dir, Since: since, Sink: sink, FS: mfs,
		Quiet: quiet, Tick: 5 * time.Millisecond, Rescan: 20 * time.Millisecond,
		RetryWatch: 10 * time.Millisecond,
	}
}

func TestWatcherReadsNewPanelsAndIgnoresOldFiles(t *testing.T) {
	dir := `C:\WoW\Screenshots`
	since := time.Now().Add(-time.Minute)
	mfs := newMemFS()
	old := dir + `\WoWScrnShot_old.png`
	shot := dir + `\WoWScrnShot_new.png`
	user := dir + `\WoWScrnShot_user.png`
	mfs.put(old, pngBytes(t, panelImage(t, testBody(200), 1, 2)), since.Add(-time.Hour))
	mfs.put(shot, pngBytes(t, panelImage(t, testBody(400), 2, 2)), time.Now())
	mfs.put(user, pngBytes(t, gameImage(128, 96)), time.Now())

	sink := &recSink{}
	logs := &logSink{}
	opt := fastOptions(dir, since, sink, mfs)
	opt.Logf = logs.logf
	opt.Watch = watchNothing
	w := New(opt)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	waitFor(t, "the new panel to be read", func() bool { return !mfs.has(shot) })
	waitFor(t, "the user screenshot to be left alone", func() bool { return w.Status().Ignored == 1 })

	if !mfs.has(old) {
		t.Fatal("an old file was read and deleted")
	}
	if !mfs.has(user) {
		t.Fatal("a user screenshot was deleted")
	}
	st := w.Status()
	if st.Shots != 1 || st.Last.ShotID != 2 || !st.Last.Deleted || !st.Last.Complete {
		t.Fatalf("status = %+v", st)
	}
	if sink.count() == 0 {
		t.Fatal("no frames reached the sink")
	}
	if !strings.Contains(logs.text(), "captura:") {
		t.Fatalf("no log lines: %q", logs.text())
	}
}

// A file that is still open for writing is not read; once the writer releases it (and it is quiet),
// the watcher reads it from an event.
func TestWatcherWaitsForWriterToRelease(t *testing.T) {
	dir := `C:\WoW\Screenshots`
	since := time.Now().Add(-time.Minute)
	mfs := newMemFS()
	shot := dir + `\WoWScrnShot_event.png`
	f := mfs.put(shot, []byte("half"), time.Now())
	f.locked = true // WoW still writes it

	sink := &recSink{}
	events := make(chan string, 1)
	opt := fastOptions(dir, since, sink, mfs)
	opt.Rescan = time.Hour
	opt.Watch = func(ctx context.Context, _ string, out chan<- string) error {
		select {
		case p := <-events:
			out <- p
		case <-ctx.Done():
		}
		<-ctx.Done()
		return nil
	}
	w := New(opt)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	// The panel is written in full and the event comes after it.
	full := pngBytes(t, panelImage(t, testBody(300), 3, 2))
	f.data = full
	f.modTime = time.Now()
	events <- shot
	time.Sleep(3 * quiet) // the file is still locked: not read yet
	if !mfs.has(shot) {
		t.Fatal("a locked file was read")
	}
	f.locked = false
	waitFor(t, "the event's panel to be read", func() bool { return !mfs.has(shot) })
	if sink.count() == 0 {
		t.Fatal("the frames were not fed")
	}
}

func TestWatcherReportsWatchFailuresAndRetries(t *testing.T) {
	dir := `C:\WoW\Screenshots`
	mfs := newMemFS()
	calls := make(chan struct{}, 8)
	opt := fastOptions(dir, time.Now(), &recSink{}, mfs)
	opt.Watch = func(ctx context.Context, _ string, _ chan<- string) error {
		calls <- struct{}{}
		if len(calls) < 2 {
			return errors.New("access denied")
		}
		<-ctx.Done()
		return nil
	}
	w := New(opt)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitFor(t, "the second watch attempt", func() bool { return len(calls) >= 2 })
	waitFor(t, "the watch to be on again", func() bool { return w.Status().Watching })
	if !strings.Contains(w.Status().LastError, "access denied") {
		t.Fatalf("LastError = %q", w.Status().LastError)
	}
}

// On disk, in a temporary folder: the shot is deleted and the other files stay.
func TestWatcherOnDiskDeletesOnlyTheShot(t *testing.T) {
	dir := t.TempDir()
	shot := filepath.Join(dir, "WoWScrnShot_010101_120000.png")
	user := filepath.Join(dir, "WoWScrnShot_010101_120001.tga")
	notes := filepath.Join(dir, "leeme.txt")
	mustWrite(t, shot, pngBytes(t, panelImage(t, testBody(256), 4, 2)))
	mustWrite(t, user, tgaBytes(gameImage(64, 48), true, true))
	mustWrite(t, notes, []byte("no es una captura"))
	sink := &recSink{}
	opt := fastOptions(dir, time.Now().Add(-time.Minute), sink, nil)
	opt.FS = OSFS{}
	opt.Watch = watchNothing
	w := New(opt)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	waitFor(t, "the shot to be deleted", func() bool {
		_, err := os.Stat(shot)
		return errors.Is(err, os.ErrNotExist)
	})
	waitFor(t, "the user screenshot to be left alone", func() bool { return w.Status().Ignored == 1 })
	for _, p := range []string{user, notes} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Written now: after the watcher's Since, which is a minute ago.
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
}
