package store

import (
	"path/filepath"
	"testing"
)

// A snapshot taken while the database is open for writing holds the committed writes that are still in the
// write-ahead log, and leaves the source as it was.
func TestSnapshotCopiesTheCommittedWritesWhileTheSourceIsOpen(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "companero.db")
	st, err := Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.CachePut("teoria.prueba", "linea nueva"); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "snapshot.db")
	if err := Snapshot(src, dst); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	copy, err := Open(dst)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer func() { _ = copy.Close() }()
	if v, ok, err := copy.CacheGet("teoria.prueba", 0); err != nil || !ok || v != "linea nueva" {
		t.Fatalf("snapshot lacks the write: v=%q ok=%v err=%v", v, ok, err)
	}
	// The source is still usable by its own handle after the snapshot.
	if err := st.CachePut("teoria.otra", "x"); err != nil {
		t.Fatalf("source after snapshot: %v", err)
	}
}

func TestSnapshotRefusesEmptyPaths(t *testing.T) {
	if err := Snapshot("", filepath.Join(t.TempDir(), "x.db")); err == nil {
		t.Fatal("an empty source must fail")
	}
}
