package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

func TestSettingsFormRoundTrip(t *testing.T) {
	base := config.Defaults()
	f := formFromConfig(base)
	got, err := f.apply(base)
	if err != nil {
		t.Fatalf("apply of unchanged defaults: %v", err)
	}
	if !reflect.DeepEqual(got, base) {
		t.Errorf("round trip changed the config:\n got %+v\nwant %+v", got, base)
	}
}

func TestSettingsFormKeepsUneditedFields(t *testing.T) {
	base := config.Defaults()
	base.MCPPort = 9999
	base.AntigravityModel = "otro"
	f := formFromConfig(base)
	f.Opacity = "75"
	got, err := f.apply(base)
	if err != nil {
		t.Fatal(err)
	}
	if got.MCPPort != 9999 || got.AntigravityModel != "otro" || got.Opacity != 75 {
		t.Errorf("apply lost fields or lost the edit: %+v", got)
	}
}

func TestSettingsFormValidation(t *testing.T) {
	base := config.Defaults()
	cases := []struct {
		name   string
		mutate func(*settingsForm)
		want   string
	}{
		{"bad hotkey", func(f *settingsForm) { f.Hotkey = "Ctrl+" }, "Atajo"},
		{"opacity low", func(f *settingsForm) { f.Opacity = "59" }, "Opacidad"},
		{"opacity high", func(f *settingsForm) { f.Opacity = "101" }, "Opacidad"},
		{"opacity text", func(f *settingsForm) { f.Opacity = "mucha" }, "Opacidad"},
		{"font low", func(f *settingsForm) { f.FontSize = "8.5" }, "Tamaño"},
		{"font high", func(f *settingsForm) { f.FontSize = "15" }, "Tamaño"},
		{"no providers", func(f *settingsForm) { f.ProviderOrder = nil }, "al menos un proveedor"},
		{"unknown provider", func(f *settingsForm) { f.ProviderOrder = []string{"gemini"} }, "gemini"},
		{"empty wow path", func(f *settingsForm) { f.WoWPath = "  " }, "Carpeta de WoW"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := formFromConfig(base)
			tc.mutate(&f)
			_, err := f.apply(base)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

func TestSettingsFormAcceptsCommaDecimal(t *testing.T) {
	f := formFromConfig(config.Defaults())
	f.FontSize = "10,5"
	got, err := f.apply(config.Defaults())
	if err != nil || got.FontSize != 10.5 {
		t.Errorf("font 10,5 -> %v, %v", got.FontSize, err)
	}
}

func TestProviderOrderDedupesAndNormalizes(t *testing.T) {
	got, err := checkProviderOrder([]string{" Codex ", "api", "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"codex", "api"}) {
		t.Errorf("order = %v", got)
	}
}

func TestSettingsChangesLiveAndRestart(t *testing.T) {
	old := config.Defaults()
	next := old
	next.Opacity = 70
	next.HideInCombat = false
	next.Proactive = false
	next.ProviderOrder = []string{"codex", "antigravity", "api"}
	next.Hotkey = "Alt+F1"
	live, restart := settingsChanges(old, next)
	if !reflect.DeepEqual(live, []string{"atajo de teclado", "opacidad", "ocultar en combate"}) {
		t.Errorf("live = %v", live)
	}
	if !reflect.DeepEqual(restart, []string{"orden de proveedores", "consejos proactivos"}) {
		t.Errorf("restart = %v", restart)
	}
	if msg := saveMessage(live, restart); !strings.Contains(msg, "Se aplicará al reiniciar: orden de proveedores") {
		t.Errorf("save message = %q", msg)
	}
	if msg := saveMessage(nil, nil); msg != "Sin cambios" {
		t.Errorf("no changes message = %q", msg)
	}
}

func TestMoveItem(t *testing.T) {
	list := []string{"antigravity", "codex", "api"}
	got, idx := moveItem(list, 1, -1)
	if !reflect.DeepEqual(got, []string{"codex", "antigravity", "api"}) || idx != 0 {
		t.Errorf("up = %v (%d)", got, idx)
	}
	if list[0] != "antigravity" {
		t.Error("moveItem changed its input")
	}
	got, idx = moveItem(list, 0, -1)
	if !reflect.DeepEqual(got, list) || idx != 0 {
		t.Errorf("up at the top moved: %v (%d)", got, idx)
	}
	got, idx = moveItem(list, 2, 1)
	if !reflect.DeepEqual(got, list) || idx != 2 {
		t.Errorf("down at the bottom moved: %v (%d)", got, idx)
	}
}

func TestKeyStatusNeverShowsValue(t *testing.T) {
	if got := keyStatusText(true); got != "Clave de NVIDIA: configurada" {
		t.Errorf("configured = %q", got)
	}
	if got := keyStatusText(false); got != "Clave de NVIDIA: no configurada" {
		t.Errorf("not configured = %q", got)
	}
}
