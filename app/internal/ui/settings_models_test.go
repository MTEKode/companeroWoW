package ui

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

func TestSettingsFormModelsAndCooldownRoundTrip(t *testing.T) {
	base := config.Defaults()
	base.AntigravityModels = []string{"gemini-3.8-flash-medium", "gemini-4-hydrogen-high"}
	base.AntigravityQuotaCooldownMin = 45
	f := formFromConfig(base)
	if f.QuotaCooldownMin != "45" || !slices.Equal(f.AntigravityModels, base.AntigravityModels) {
		t.Fatalf("form = %+v", f)
	}
	got, err := f.apply(base)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !reflect.DeepEqual(got, base) {
		t.Errorf("round trip changed the config:\n got %+v\nwant %+v", got, base)
	}
}

func TestSettingsFormModelsValidation(t *testing.T) {
	base := config.Defaults()
	cases := []struct {
		name string
		edit func(*settingsForm)
		want string
	}{
		{"no models", func(f *settingsForm) { f.AntigravityModels = nil }, "al menos un modelo"},
		{"bad slug", func(f *settingsForm) { f.AntigravityModels = []string{"gemini 4"} }, "no es un nombre de modelo válido"},
		{"cooldown text", func(f *settingsForm) { f.QuotaCooldownMin = "treinta" }, "Enfriamiento de la cuota"},
		{"cooldown too long", func(f *settingsForm) { f.QuotaCooldownMin = "1000" }, "Enfriamiento de la cuota"},
		{"cooldown zero", func(f *settingsForm) { f.QuotaCooldownMin = "0" }, "Enfriamiento de la cuota"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := formFromConfig(base)
			c.edit(&f)
			_, err := f.apply(base)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestSettingsFormDedupesModels(t *testing.T) {
	base := config.Defaults()
	f := formFromConfig(base)
	f.AntigravityModels = []string{" gemini-4-argon-low ", "gemini-4-argon-low", "", "gemini-3.8-flash-low"}
	got, err := f.apply(base)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if want := []string{"gemini-4-argon-low", "gemini-3.8-flash-low"}; !slices.Equal(got.AntigravityModels, want) {
		t.Errorf("models = %v, want %v", got.AntigravityModels, want)
	}
}

func TestSettingsModelChangesApplyOnRestart(t *testing.T) {
	old := config.Defaults()
	next := old
	next.AntigravityModels = []string{"gemini-3.8-flash-medium"}
	next.AntigravityQuotaCooldownMin = 60
	live, restart := settingsChanges(old, next)
	if len(live) != 0 {
		t.Errorf("live = %v, the models need a restart", live)
	}
	if !slices.Contains(restart, "modelos de Antigravity") || !slices.Contains(restart, "enfriamiento de la cuota") {
		t.Errorf("restart = %v", restart)
	}
}

// TestSettingsSaveWritesModelsToConfig: what the settings screen saves is what the next start reads.
func TestSettingsSaveWritesModelsToConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	base := config.Defaults()
	f := formFromConfig(base)
	f.AntigravityModels = []string{"gemini-3.8-flash-medium", "gemini-4-hydrogen-high"}
	f.QuotaCooldownMin = "20"
	next, err := f.apply(base)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := next.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(got.AntigravityModels, f.AntigravityModels) || got.AntigravityQuotaCooldownMin != 20 {
		t.Errorf("saved models %v cooldown %d", got.AntigravityModels, got.AntigravityQuotaCooldownMin)
	}
}

func TestFormatStatusShowsModelLine(t *testing.T) {
	s := Status{Bridge: "no_wow", SavedVarsAgeSec: -1, Model: "Modelo: Gemini 3.8 Flash (cuota agotada hasta 15:42)"}
	if got := formatStatus(s, "", false); !strings.Contains(got, "· Modelo: Gemini 3.8 Flash (cuota agotada hasta 15:42)") {
		t.Errorf("status = %q", got)
	}
}
