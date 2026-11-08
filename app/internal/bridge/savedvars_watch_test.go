package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSVWatcherPicksUpFileLater: the path is unknown at start (WoW not set up, or no
// /compa sync yet). The watcher keeps looking and loads the file once it appears.
func TestSVWatcherPicksUpFileLater(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	calls := 0
	w := NewSVWatcherFunc(func() (string, error) {
		calls++
		if calls < 3 {
			return "", errors.New("aún no hay datos")
		}
		return path, nil
	}, time.Second)

	if err := w.Poll(); err == nil {
		t.Fatal("first poll: want an error while the path is unknown")
	}
	if w.PathError() == nil {
		t.Fatal("PathError is nil while the path is unknown")
	}
	_ = w.Poll() // second lookup still fails

	if err := os.WriteFile(path, []byte("CompaneroWoWDB = {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Poll(); err != nil {
		t.Fatalf("third poll (path found, file present): %v", err)
	}
	if w.PathError() != nil {
		t.Errorf("PathError = %v after the path was found", w.PathError())
	}
	if w.Current() == nil {
		t.Error("no SavedVariables loaded after the path was found")
	}
}

// TestSVWatcherReportsMissingFile: the path is known but the file does not exist yet.
func TestSVWatcherReportsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.lua")
	w := NewSVWatcher(path, time.Second)
	if err := w.Poll(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("poll of a missing file: %v, want os.ErrNotExist", err)
	}
	if w.PathError() != nil {
		t.Errorf("PathError = %v for a known path", w.PathError())
	}
}
