package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadMissingWritesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(*c, Defaults()) {
		t.Fatalf("Load on missing file returned %+v, want defaults", *c)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected default file to be written: %v", err)
	}
	if !strings.Contains(string(data), "\n  \"") {
		t.Fatalf("expected 2-space indented JSON, got:\n%s", data)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if !reflect.DeepEqual(*again, Defaults()) {
		t.Fatalf("round trip of defaults differs: %+v", *again)
	}
}

func TestLoadPartialKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"mcpPort": 9999, "proactive": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.MCPPort != 9999 {
		t.Errorf("MCPPort = %d, want 9999", c.MCPPort)
	}
	if c.Proactive {
		t.Errorf("Proactive = true, want false (explicit value)")
	}
	d := Defaults()
	if c.Hotkey != d.Hotkey || c.Language != d.Language || c.PixelPollMs != d.PixelPollMs || c.AITimeoutSec != d.AITimeoutSec {
		t.Errorf("missing fields did not keep defaults: %+v", *c)
	}
	if !reflect.DeepEqual(c.ProviderOrder, d.ProviderOrder) || !reflect.DeepEqual(c.WebSources, d.WebSources) {
		t.Errorf("missing slices did not keep defaults: %+v", *c)
	}
}

func TestLoadInvalidJSONMentionsPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"mcpPort": `), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error %q does not mention path %q", err, path)
	}
}

func TestSavedVariablesPathExplicitAccount(t *testing.T) {
	c := Defaults()
	c.WoWPath = filepath.Join("C:", "WoW", "_forever_")
	c.Account = "TURIET"
	got, err := c.SavedVariablesPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(c.WoWPath, "WTF", "Account", "TURIET", "SavedVariables", "CompaneroWoW.lua")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSavedVariablesPathAutoDetectsNewest(t *testing.T) {
	wow := t.TempDir()
	accounts := filepath.Join(wow, "WTF", "Account")
	for _, name := range []string{"OLD", "NEW", "NOFILE"} {
		if name == "NOFILE" {
			if err := os.MkdirAll(filepath.Join(accounts, name), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		dir := filepath.Join(accounts, name, "SavedVariables")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "CompaneroWoW.lua"), []byte("CompaneroWoWDB = {}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldFile := filepath.Join(accounts, "OLD", "SavedVariables", "CompaneroWoW.lua")
	newFile := filepath.Join(accounts, "NEW", "SavedVariables", "CompaneroWoW.lua")
	if err := os.Chtimes(oldFile, mustTime(t, "2026-01-01"), mustTime(t, "2026-01-01")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newFile, mustTime(t, "2026-06-01"), mustTime(t, "2026-06-01")); err != nil {
		t.Fatal(err)
	}
	c := Defaults()
	c.WoWPath = wow
	got, err := c.SavedVariablesPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != newFile {
		t.Fatalf("auto-detect got %q want %q", got, newFile)
	}
}

func mustTime(t *testing.T, day string) time.Time {
	t.Helper()
	tm, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestSavedVariablesPathNoAccount(t *testing.T) {
	c := Defaults()
	c.WoWPath = t.TempDir()
	if _, err := c.SavedVariablesPath(); err == nil {
		t.Fatal("expected error when no account has SavedVariables")
	}
}
