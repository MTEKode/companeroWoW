package store

import "testing"

// TestCacheSizeIsSmall: the page cache is capped at 512 KiB (a negative cache_size is KiB).
func TestCacheSizeIsSmall(t *testing.T) {
	s := openTemp(t)
	var size int
	if err := s.db.QueryRow(`PRAGMA cache_size`).Scan(&size); err != nil {
		t.Fatal(err)
	}
	if size != -512 {
		t.Errorf("cache_size = %d, want -512", size)
	}
}

// TestShrinkMemoryAfterUse: shrinking after the store was used works, and it is safe on a nil
// store.
func TestShrinkMemoryAfterUse(t *testing.T) {
	s := openTemp(t)
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS scratch (v TEXT)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if _, err := s.db.Exec(`INSERT INTO scratch (v) VALUES (?)`, "x"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ShrinkMemory(); err != nil {
		t.Fatalf("ShrinkMemory: %v", err)
	}
	var nilStore *Store
	if err := nilStore.ShrinkMemory(); err != nil {
		t.Errorf("ShrinkMemory on nil store: %v", err)
	}
}
