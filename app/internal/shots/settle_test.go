package shots

import (
	"testing"
	"time"
)

// A file being written: its size grows, then holds still. It is read only once the size has
// held for the quiet time and the file opens.
func TestSettleWaitsForQuietAndOpen(t *testing.T) {
	mfs := newMemFS()
	p := `C:\WoW\Screenshots\WoWScrnShot_1.png`
	f := mfs.put(p, []byte("part"), t0)
	s := newSettler(time.Second, 30*time.Second)
	s.note(p, t0)

	now := t0
	if got := s.ready(now, mfs); len(got) != 0 {
		t.Fatalf("ready at first sight: %v", got)
	}
	// The writer keeps going: the size changes each second, so nothing is ready.
	for i := 1; i <= 3; i++ {
		now = now.Add(time.Second)
		f.data = append(f.data, 'x')
		if got := s.ready(now, mfs); len(got) != 0 {
			t.Fatalf("ready while the file grows (step %d): %v", i, got)
		}
	}
	// It holds still for less than the quiet time: not yet.
	now = now.Add(900 * time.Millisecond)
	if got := s.ready(now, mfs); len(got) != 0 {
		t.Fatalf("ready before the quiet time: %v", got)
	}
	// The writer still holds the file open: not ready, however quiet.
	f.locked = true
	now = now.Add(2 * time.Second)
	if got := s.ready(now, mfs); len(got) != 0 {
		t.Fatalf("ready while the file is open for writing: %v", got)
	}
	// Released and quiet: ready, once, and then forgotten.
	f.locked = false
	if got := s.ready(now, mfs); len(got) != 1 || got[0] != p {
		t.Fatalf("ready after release = %v, want %s", got, p)
	}
	if got := s.ready(now.Add(time.Hour), mfs); len(got) != 0 {
		t.Fatalf("a settled file came back: %v", got)
	}
}

func TestSettleDropsGoneAndStuckFiles(t *testing.T) {
	mfs := newMemFS()
	gone := `C:\WoW\Screenshots\WoWScrnShot_gone.png`
	stuck := `C:\WoW\Screenshots\WoWScrnShot_stuck.png`
	mfs.put(stuck, []byte("x"), t0).locked = true
	s := newSettler(time.Millisecond, time.Minute)
	s.note(gone, t0) // never existed: the stat fails
	s.note(stuck, t0)
	if got := s.ready(t0, mfs); len(got) != 0 {
		t.Fatalf("ready: %v", got)
	}
	if s.pending() != 1 {
		t.Fatalf("pending = %d, want only the stuck file (gone must be forgotten)", s.pending())
	}
	// After maxWait the stuck file is dropped even though it is still locked.
	s.ready(t0.Add(2*time.Minute), mfs)
	if s.pending() != 0 {
		t.Fatalf("pending = %d after maxWait", s.pending())
	}
}

func TestSettleSameVersionReadyOnceNoted(t *testing.T) {
	mfs := newMemFS()
	p := `C:\WoW\Screenshots\WoWScrnShot_2.tga`
	mfs.put(p, []byte("done"), t0.Add(-time.Hour))
	s := newSettler(quiet, time.Minute)
	s.note(p, t0)
	s.note(p, t0.Add(time.Second)) // a second event for the same path keeps the first time
	if got := s.ready(t0, mfs); len(got) != 0 {
		t.Fatal("ready before the quiet time")
	}
	if got := s.ready(t0.Add(quiet+time.Millisecond), mfs); len(got) != 1 {
		t.Fatalf("ready = %v", got)
	}
}
