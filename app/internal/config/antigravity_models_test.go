package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAntigravityModelsDefaultOrderAndCooldown(t *testing.T) {
	d := Defaults()
	if !slices.Equal(d.AntigravityModels, []string{"gemini-4-hydrogen-high", "gemini-3.8-flash-medium"}) {
		t.Errorf("default models = %v", d.AntigravityModels)
	}
	if d.AntigravityQuotaCooldownMin != 30 {
		t.Errorf("default cooldown = %d, want 30", d.AntigravityQuotaCooldownMin)
	}
	if d.AntigravityModel != "" {
		t.Errorf("the legacy field must not be set by default: %q", d.AntigravityModel)
	}
}

func TestAntigravityModelMigratesIntoTheList(t *testing.T) {
	path := writeConfigFile(t, `{"antigravityModel":"gemini-4-argon-high","antigravityEffort":"low"}`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"gemini-4-argon-high", "gemini-4-hydrogen-high", "gemini-3.8-flash-medium"}
	if !slices.Equal(c.AntigravityModels, want) {
		t.Errorf("models = %v, want %v", c.AntigravityModels, want)
	}
	if c.AntigravityModel != "" {
		t.Errorf("legacy field kept in memory: %q", c.AntigravityModel)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"antigravityModels"`) || strings.Contains(string(data), `"antigravityModel":`) {
		t.Errorf("file not rewritten with the list and without the legacy key:\n%s", data)
	}
}

func TestAntigravityOldDefaultModelGetsTheNewOrder(t *testing.T) {
	path := writeConfigFile(t, `{"antigravityModel":"gemini-3.8-flash-low","antigravityEffort":"low"}`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"gemini-4-hydrogen-high", "gemini-3.8-flash-medium"}; !slices.Equal(c.AntigravityModels, want) {
		t.Errorf("models = %v, want %v", c.AntigravityModels, want)
	}
}

func TestAntigravityModelsListIsCleaned(t *testing.T) {
	path := writeConfigFile(t, `{"antigravityModels":[" gemini-4-argon-low ","gemini-4-argon-low","bad model;rm","gemini-3.8-flash-low"]}`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"gemini-4-argon-low", "gemini-3.8-flash-low"}; !slices.Equal(c.AntigravityModels, want) {
		t.Errorf("models = %v, want %v", c.AntigravityModels, want)
	}
}

func TestAntigravityEmptyListGetsDefault(t *testing.T) {
	path := writeConfigFile(t, `{"antigravityModels":[],"antigravityQuotaCooldownMin":0}`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(c.AntigravityModels, DefaultAntigravityModels) {
		t.Errorf("models = %v, want the default", c.AntigravityModels)
	}
	if c.AntigravityQuotaCooldownMin != DefaultQuotaCooldownMin {
		t.Errorf("cooldown = %d, want %d", c.AntigravityQuotaCooldownMin, DefaultQuotaCooldownMin)
	}
}

func TestAntigravityModelsSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Defaults()
	c.AntigravityModels = []string{"gemini-3.8-flash-medium", "gemini-4-hydrogen-xhigh"}
	c.AntigravityQuotaCooldownMin = 45
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(got.AntigravityModels, c.AntigravityModels) || got.AntigravityQuotaCooldownMin != 45 {
		t.Errorf("round trip: models %v, cooldown %d", got.AntigravityModels, got.AntigravityQuotaCooldownMin)
	}
}

func TestValidAntigravityModel(t *testing.T) {
	for s, want := range map[string]bool{
		"gemini-4-hydrogen-high": true,
		"gpt-oss-120b-medium":    true,
		"bad model":              false,
		"x;rm -rf":               false,
		"":                       false,
	} {
		if got := ValidAntigravityModel(s); got != want {
			t.Errorf("ValidAntigravityModel(%q) = %v, want %v", s, got, want)
		}
	}
}
