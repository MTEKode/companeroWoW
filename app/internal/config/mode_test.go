package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A config from before the mode setting gets the on-demand mode and the wait, written once.
func TestLoadMigratesModeToOnDemandAndSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	old := `{"wowPath":"C:\\WoW","hotkey":"Ctrl+Shift+Space","tipsMode":"bandeja","hideInCombat":true,"verifyAnswers":true,"profile":{"tone":"brief"}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != ModeOnDemand {
		t.Errorf("Mode = %q, want %q", c.Mode, ModeOnDemand)
	}
	if c.OnDemandWaitSec != DefaultOnDemandWaitSec {
		t.Errorf("OnDemandWaitSec = %d, want %d", c.OnDemandWaitSec, DefaultOnDemandWaitSec)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"mode": "bajo_demanda"`) {
		t.Errorf("migrated file lacks the mode key:\n%s", data)
	}
}

// An explicit continuous mode is kept, and an unknown mode falls back to on demand.
func TestModeValuesAndClamp(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{ModeContinuous, ModeContinuous},
		{ModeOnDemand, ModeOnDemand},
		{"", ModeOnDemand},
		{"siempre", ModeOnDemand},
	}
	for _, tc := range cases {
		c := Defaults()
		c.Mode = tc.in
		c.clampMode()
		if c.Mode != tc.want {
			t.Errorf("clampMode(%q) = %q, want %q", tc.in, c.Mode, tc.want)
		}
	}
}

func TestOnDemandWaitRange(t *testing.T) {
	c := Defaults()
	c.OnDemandWaitSec = 0
	c.clampMode()
	if c.OnDemandWaitSec != DefaultOnDemandWaitSec {
		t.Errorf("zero wait = %d, want default %d", c.OnDemandWaitSec, DefaultOnDemandWaitSec)
	}
	c.OnDemandWaitSec = 90
	c.clampMode()
	if c.OnDemandWaitSec != MaxOnDemandWaitSec {
		t.Errorf("wait 90 = %d, want max %d", c.OnDemandWaitSec, MaxOnDemandWaitSec)
	}
	if got := c.OnDemandWait(); got != MaxOnDemandWaitSec*time.Second {
		t.Errorf("OnDemandWait = %v, want %v", got, MaxOnDemandWaitSec*time.Second)
	}
}

// In on-demand mode the tips are off whatever tipsMode says; continuous keeps the setting.
func TestEffectiveTipsModeFollowsMode(t *testing.T) {
	c := Defaults()
	c.TipsMode = TipsPanel
	c.Mode = ModeOnDemand
	if got := c.EffectiveTipsMode(); got != TipsOff {
		t.Errorf("on demand: EffectiveTipsMode = %q, want off", got)
	}
	c.Mode = ModeContinuous
	if got := c.EffectiveTipsMode(); got != TipsPanel {
		t.Errorf("continuous: EffectiveTipsMode = %q, want panel", got)
	}
}
