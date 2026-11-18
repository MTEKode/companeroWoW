package bridge

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSVWatcherReleaseAndReload: an idle watcher drops its decoded tree, and the next
// Current reads the file again without the caller noticing.
func TestSVWatcherReleaseAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	if err := os.WriteFile(path, []byte(`CompaneroWoWDB = {maps = {["1"] = "Elwynn"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewSVWatcher(path, time.Second)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if w.Current() == nil {
		t.Fatal("no data after the first load")
	}

	// Recently used: nothing is released.
	if w.ReleaseIdle(time.Now(), time.Minute) {
		t.Fatal("ReleaseIdle dropped data that was used a moment ago")
	}

	// Unused for longer than the idle time: dropped, and reported as released.
	later := time.Now().Add(10 * time.Minute)
	if !w.ReleaseIdle(later, 5*time.Minute) {
		t.Fatal("ReleaseIdle kept data unused for 10 minutes")
	}
	if !w.Released() {
		t.Fatal("Released = false after ReleaseIdle")
	}
	if w.ReleaseIdle(later, 5*time.Minute) {
		t.Fatal("ReleaseIdle reported a second release of nothing")
	}

	// A poll of the unchanged file does not load it again...
	if err := w.Poll(); err != nil {
		t.Fatalf("poll after release: %v", err)
	}
	if w.current != nil {
		t.Fatal("poll of an unchanged file loaded the data again")
	}
	// ...but the next Current does, with the same content.
	sv := w.Current()
	if sv == nil || sv.Raw["maps"] == nil {
		t.Fatalf("Current after release = %+v, want the reloaded tree", sv)
	}
	if w.Released() {
		t.Error("Released still true after the reload")
	}
}

// TestSVWatcherReloadSeesNewFile: a file changed while the data is released is read with its
// new content on the next Current, and the change is still published to the importer.
func TestSVWatcherReloadSeesNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	if err := os.WriteFile(path, []byte(`CompaneroWoWDB = {v = 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewSVWatcher(path, time.Second)
	if err := w.Poll(); err != nil {
		t.Fatal(err)
	}
	<-w.Changes() // the first load was published; only the change below is checked
	w.ReleaseIdle(time.Now().Add(time.Hour), time.Minute)

	if err := os.WriteFile(path, []byte(`CompaneroWoWDB = {v = 2, extra = true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make sure the new file is seen as changed even on a coarse clock.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if err := w.Poll(); err != nil {
		t.Fatalf("poll of the changed file: %v", err)
	}
	sv := w.Current()
	if sv == nil || sv.Raw["extra"] != true {
		t.Fatalf("Current after a change = %+v, want the new content", sv)
	}
	select {
	case got := <-w.Changes():
		if got.Raw["extra"] != true {
			t.Errorf("published change has the old content")
		}
	default:
		t.Error("the change was not published to the importer")
	}
}

// TestSVWatcherNoReloadWithoutFile: a released watcher whose file is gone returns nil and
// keeps the error, instead of inventing data.
func TestSVWatcherNoReloadWithoutFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	if err := os.WriteFile(path, []byte(`CompaneroWoWDB = {}`), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewSVWatcher(path, time.Second)
	if err := w.Poll(); err != nil {
		t.Fatal(err)
	}
	w.ReleaseIdle(time.Now().Add(time.Hour), time.Minute)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if w.Current() != nil {
		t.Fatal("Current returned data for a file that no longer exists")
	}
	if w.LastError() == nil {
		t.Error("LastError is nil after a failed reload")
	}
}
