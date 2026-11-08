package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupLeftoversRemovesOnlyOldFiles(t *testing.T) {
	dir := t.TempDir()
	mk := func(rel string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("webview2/EBWebView/Default/cache.bin")
	mk("webview2.stale-20260101/data")
	mk("webview2.stale-20260201/data")
	mk("antigravity-settings.json")
	mk("agy/antigravity-settings.json")
	mk("companero.db")
	mk("config.json")
	mk("screens/shot.png")

	removed, err := CleanupLeftovers(dir)
	if err != nil {
		t.Fatalf("CleanupLeftovers: %v", err)
	}
	if len(removed) != 4 {
		t.Fatalf("removed %v, want 4 items", removed)
	}
	for _, gone := range []string{"webview2", "webview2.stale-20260101", "webview2.stale-20260201", "antigravity-settings.json"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s still exists (err %v)", gone, err)
		}
	}
	for _, kept := range []string{"agy/antigravity-settings.json", "companero.db", "config.json", "screens/shot.png"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s was removed: %v", kept, err)
		}
	}
}

func TestCleanupLeftoversMissingDirAndSecondRun(t *testing.T) {
	if removed, err := CleanupLeftovers(filepath.Join(t.TempDir(), "absent")); err != nil || len(removed) != 0 {
		t.Fatalf("missing dir: removed %v, err %v", removed, err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "antigravity-settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed, _ := CleanupLeftovers(dir); len(removed) != 1 {
		t.Fatalf("first run removed %v", removed)
	}
	if removed, err := CleanupLeftovers(dir); err != nil || len(removed) != 0 {
		t.Fatalf("second run: removed %v, err %v", removed, err)
	}
}
