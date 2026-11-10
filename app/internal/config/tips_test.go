package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadJSON(t *testing.T, body string) (*Config, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return c, string(saved)
}

func TestTipsDefaults(t *testing.T) {
	d := Defaults()
	if d.TipsMode != TipsBandeja {
		t.Errorf("TipsMode default = %q, want %q", d.TipsMode, TipsBandeja)
	}
	if d.TipsSound {
		t.Error("TipsSound default = true, want false")
	}
	if !d.Proactive {
		t.Error("Proactive default = false, want true (mirrors bandeja)")
	}
}

// An existing config has proactive but no tipsMode: it migrates to bandeja, and the file is
// rewritten with the new key.
func TestTipsMigrateExistingProactiveToBandeja(t *testing.T) {
	c, saved := loadJSON(t, `{"mcpPort": 47321, "proactive": true}`)
	if c.TipsMode != TipsBandeja || !c.Proactive {
		t.Fatalf("TipsMode = %q, Proactive = %v; want bandeja, true", c.TipsMode, c.Proactive)
	}
	if !strings.Contains(saved, `"tipsMode": "bandeja"`) {
		t.Errorf("migrated file lacks tipsMode:\n%s", saved)
	}
}

// proactive false in an older config meant "no tips": it becomes off.
func TestTipsMigrateProactiveFalseToOff(t *testing.T) {
	c, saved := loadJSON(t, `{"proactive": false}`)
	if c.TipsMode != TipsOff || c.Proactive {
		t.Fatalf("TipsMode = %q, Proactive = %v; want off, false", c.TipsMode, c.Proactive)
	}
	if !strings.Contains(saved, `"tipsMode": "off"`) {
		t.Errorf("migrated file lacks tipsMode off:\n%s", saved)
	}
}

// A config without any tips key (the very old file) gets bandeja, the default.
func TestTipsMigrateNoKeysToBandeja(t *testing.T) {
	c, _ := loadJSON(t, `{"mcpPort": 47321}`)
	if c.TipsMode != TipsBandeja || !c.Proactive {
		t.Fatalf("TipsMode = %q, Proactive = %v; want bandeja, true", c.TipsMode, c.Proactive)
	}
}

// tipsMode wins over proactive, and proactive follows it.
func TestTipsModeWinsOverProactive(t *testing.T) {
	c, _ := loadJSON(t, `{"tipsMode": "panel", "proactive": false}`)
	if c.TipsMode != TipsPanel || !c.Proactive {
		t.Fatalf("TipsMode = %q, Proactive = %v; want panel, true", c.TipsMode, c.Proactive)
	}
	c, _ = loadJSON(t, `{"tipsMode": "off", "proactive": true}`)
	if c.TipsMode != TipsOff || c.Proactive {
		t.Fatalf("TipsMode = %q, Proactive = %v; want off, false", c.TipsMode, c.Proactive)
	}
}

func TestTipsUnknownModeBecomesBandeja(t *testing.T) {
	c, _ := loadJSON(t, `{"tipsMode": "campana", "proactive": true}`)
	if c.TipsMode != TipsBandeja || !c.Proactive {
		t.Fatalf("TipsMode = %q, Proactive = %v; want bandeja, true", c.TipsMode, c.Proactive)
	}
}

func TestTipsSoundRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Defaults()
	c.TipsSound = true
	c.TipsMode = TipsPanel
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !again.TipsSound || again.TipsMode != TipsPanel {
		t.Fatalf("round trip = sound %v, mode %q", again.TipsSound, again.TipsMode)
	}
}

// Save keeps proactive in step with the mode, even when the caller set only the mode.
func TestSaveSyncsProactiveWithMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Defaults()
	c.TipsMode = TipsOff
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), `"proactive": false`) {
		t.Errorf("proactive not synced to off:\n%s", saved)
	}
	// Save works on a copy: the caller's value is not changed by it.
	if !c.Proactive {
		t.Errorf("Save changed the caller's Proactive")
	}
}

func TestSyncTipsReportsChange(t *testing.T) {
	c := Defaults()
	if c.syncTips() {
		t.Error("syncTips changed a valid default")
	}
	// A set mode wins: proactive is pulled back in line, and that counts as a change.
	c.Proactive = false
	if !c.syncTips() {
		t.Error("syncTips did not report the proactive correction")
	}
	if c.TipsMode != TipsBandeja || !c.Proactive {
		t.Errorf("after sync: mode %q, proactive %v", c.TipsMode, c.Proactive)
	}
	c.TipsMode = ""
	c.Proactive = false
	if !c.syncTips() || c.TipsMode != TipsOff {
		t.Errorf("empty mode with proactive false: mode %q", c.TipsMode)
	}
}
