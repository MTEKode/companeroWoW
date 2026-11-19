//go:build windows

package shots

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The folder's change notifications report a file created in a temporary folder (not the game's).
func TestWatchDirReportsNewFiles(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan string, 16)
	errc := make(chan error, 1)
	go func() { errc <- watchDir(ctx, dir, out) }()

	time.Sleep(200 * time.Millisecond) // the watch is armed before the file appears
	want := filepath.Join(dir, "WoWScrnShot_test.tga")
	if err := os.WriteFile(want, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case got := <-out:
			if got == want {
				cancel()
				if err := <-errc; err != nil {
					t.Fatalf("watchDir: %v", err)
				}
				return
			}
		case err := <-errc:
			t.Fatalf("watchDir ended early: %v", err)
		case <-deadline:
			t.Fatal("no notification for the new file")
		}
	}
}

// A folder that does not exist cannot be watched: the error is returned, so the watcher retries.
func TestWatchDirMissingFolderFails(t *testing.T) {
	err := watchDir(context.Background(), filepath.Join(t.TempDir(), "no-existe"), make(chan string, 1))
	if err == nil {
		t.Fatal("a missing folder was watched")
	}
}
