package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestSavedVariablesPathNoDataIsSentinel: the accounts exist, but none has the addon's
// file yet. The error must say so, so the status bar can ask for /compa sync.
func TestSavedVariablesPathNoDataIsSentinel(t *testing.T) {
	wow := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wow, "WTF", "Account", "ABC"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Config{WoWPath: wow}
	if _, err := c.SavedVariablesPath(); !errors.Is(err, ErrNoSavedVariables) {
		t.Fatalf("err = %v, want ErrNoSavedVariables", err)
	}
}

// TestSavedVariablesPathNoWoWIsNotSentinel: an empty wowPath is a configuration problem,
// not a missing sync.
func TestSavedVariablesPathNoWoWIsNotSentinel(t *testing.T) {
	c := &Config{}
	if _, err := c.SavedVariablesPath(); err == nil || errors.Is(err, ErrNoSavedVariables) {
		t.Fatalf("err = %v, want a wowPath error that is not ErrNoSavedVariables", err)
	}
}
