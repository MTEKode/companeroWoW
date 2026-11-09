package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUXDefaults(t *testing.T) {
	d := Defaults()
	if !d.HideInCombat {
		t.Error("HideInCombat default = false, want true")
	}
	if d.StartWithWoW {
		t.Error("StartWithWoW default = true, want false")
	}
	if d.Opacity != 90 {
		t.Errorf("Opacity default = %d, want 90", d.Opacity)
	}
	if d.FontSize != 10 {
		t.Errorf("FontSize default = %v, want 10", d.FontSize)
	}
}

func TestLoadInfoReportsCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_, created, err := LoadInfo(path)
	if err != nil {
		t.Fatalf("LoadInfo: %v", err)
	}
	if !created {
		t.Error("first LoadInfo: created = false, want true")
	}
	_, created, err = LoadInfo(path)
	if err != nil {
		t.Fatalf("second LoadInfo: %v", err)
	}
	if created {
		t.Error("second LoadInfo: created = true, want false")
	}
}

// An older config.json has none of the UX fields. They must take their defaults and the
// existing values must survive.
func TestOldConfigGetsUXDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	old := `{"wowPath": "F:\\Games\\World of Warcraft\\_forever_", "hotkey": "Alt+F1", "proactive": false}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	c, created, err := LoadInfo(path)
	if err != nil {
		t.Fatalf("LoadInfo: %v", err)
	}
	if created {
		t.Error("an existing file must not count as created")
	}
	if c.WoWPath != `F:\Games\World of Warcraft\_forever_` || c.Hotkey != "Alt+F1" || c.Proactive {
		t.Errorf("existing values lost: %+v", *c)
	}
	if !c.HideInCombat || c.StartWithWoW || c.Opacity != 90 || c.FontSize != 10 {
		t.Errorf("UX fields did not take defaults: hide=%v start=%v opacity=%d font=%v",
			c.HideInCombat, c.StartWithWoW, c.Opacity, c.FontSize)
	}
	// The migrated file now carries the new keys, so the user can see and edit them.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"hideInCombat"`, `"startWithWoW"`, `"opacity"`, `"fontSize"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("migrated config lacks %s", key)
		}
	}
}

func TestAppearanceIsClamped(t *testing.T) {
	cases := []struct {
		name        string
		json        string
		wantOpacity int
		wantFont    float64
	}{
		{"too low", `{"opacity": 10, "fontSize": 6}`, MinOpacity, MinFontSize},
		{"too high", `{"opacity": 150, "fontSize": 30}`, MaxOpacity, MaxFontSize},
		{"in range", `{"opacity": 75, "fontSize": 12.5}`, 75, 12.5},
		{"explicit zero gets default", `{"opacity": 0, "fontSize": 0}`, 90, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.json), 0o644); err != nil {
				t.Fatal(err)
			}
			c, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if c.Opacity != tc.wantOpacity || c.FontSize != tc.wantFont {
				t.Errorf("opacity=%d font=%v, want %d and %v", c.Opacity, c.FontSize, tc.wantOpacity, tc.wantFont)
			}
		})
	}
}

func TestClampAppearanceForSettings(t *testing.T) {
	c := Defaults()
	c.Opacity = 20
	c.FontSize = 40
	c.ClampAppearance()
	if c.Opacity != MinOpacity || c.FontSize != MaxFontSize {
		t.Errorf("after ClampAppearance: opacity=%d font=%v", c.Opacity, c.FontSize)
	}
}
