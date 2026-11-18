package bridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A paused watcher checks the file only when asked: its timer is off, so a change made while
// paused is not loaded until Poll runs; resuming loads it at once.
func TestSVWatcherPausedDoesNotPoll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	if err := os.WriteFile(path, []byte("CompaneroWoWDB = {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewSVWatcher(path, 5*time.Millisecond)
	w.SetPaused(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	time.Sleep(40 * time.Millisecond)
	if w.Current() != nil {
		t.Fatal("a paused watcher loaded the file on its own")
	}
	if err := w.Poll(); err != nil {
		t.Fatalf("explicit poll: %v", err)
	}
	if w.Current() == nil {
		t.Fatal("explicit poll did not load the file")
	}

	// Resuming starts the timer; a change is then picked up without an explicit poll.
	if err := os.WriteFile(path, []byte("CompaneroWoWDB = { x = 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(path, later, later)
	w.SetPaused(false)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sv := w.Current(); sv != nil && sv.Raw["x"] == float64(1) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("resumed watcher did not pick up the change")
}
