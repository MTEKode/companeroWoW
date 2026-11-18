package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// The hotkey only activates the reading in the game mode: it never shows the panel.
func TestHotkeyDoesNotShowThePanelInTheGameMode(t *testing.T) {
	if got := hotkeyFor(config.UIModeGame); got != hotkeyActivate {
		t.Fatalf("game mode hotkey = %v, want activate (the panel must stay closed)", got)
	}
	if got := hotkeyFor(""); got != hotkeyActivate {
		t.Fatalf("a config without uiMode must behave as the game mode, got %v", got)
	}
	if got := hotkeyFor(config.UIModeWindow); got != hotkeyToggle {
		t.Fatalf("window mode hotkey = %v, want toggle", got)
	}
}

func TestGameModeIsTheDefault(t *testing.T) {
	if !gameMode(config.Defaults().UIMode) {
		t.Fatal("the default interface must be the game mode")
	}
	if gameMode(config.UIModeWindow) {
		t.Fatal("the window mode is not the game mode")
	}
}

// Tips in the game mode stay in the conversation (and the tray badge counts them): nothing else.
func TestGameTipRouteNeverPopsUp(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		visible bool
		combat  bool
	}{
		{"bandeja closed", config.TipsBandeja, false, false},
		{"bandeja in combat", config.TipsBandeja, false, true},
		{"panel closed", config.TipsPanel, false, false},
		{"panel visible", config.TipsPanel, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gameTipRoute(routeTip(tc.mode, tc.visible, tc.combat))
			if r.ShowPanel || r.Balloon || r.Defer {
				t.Fatalf("game mode tip route shows something: %+v", r)
			}
			if !r.Conversation {
				t.Fatalf("the tip must still reach the conversation: %+v", r)
			}
		})
	}
	off := gameTipRoute(routeTip(config.TipsOff, false, false))
	if off.Conversation || off.Badge {
		t.Fatalf("tips off must stay off: %+v", off)
	}
}

func TestSettingsFormCarriesTheInterfaceAndAnswerSound(t *testing.T) {
	base := config.Defaults()
	f := formFromConfig(base)
	if f.UIMode != config.UIModeGame || f.AnswerSound != config.AnswerVoice || f.AnswerVolume != "80" {
		t.Fatalf("form defaults: %+v", f)
	}
	f.UIMode = config.UIModeWindow
	f.AnswerSound = config.AnswerBeep
	f.AnswerVolume = " 40 "
	next, err := f.apply(base)
	if err != nil {
		t.Fatal(err)
	}
	if next.UIMode != config.UIModeWindow || next.AnswerSound != config.AnswerBeep || next.AnswerVolume != 40 {
		t.Fatalf("apply lost the values: %+v", next)
	}
}

func TestSettingsFormRejectsBadAnswerSettings(t *testing.T) {
	base := config.Defaults()
	cases := []struct {
		name   string
		change func(*settingsForm)
		want   string
	}{
		{"unknown interface", func(f *settingsForm) { f.UIMode = "flotante" }, "Interfaz"},
		{"unknown sound", func(f *settingsForm) { f.AnswerSound = "campana" }, "Aviso"},
		{"volume too high", func(f *settingsForm) { f.AnswerVolume = "150" }, "Volumen"},
		{"volume not a number", func(f *settingsForm) { f.AnswerVolume = "mucho" }, "Volumen"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := formFromConfig(base)
			tc.change(&f)
			_, err := f.apply(base)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("apply error = %v, want one naming %q", err, tc.want)
			}
		})
	}
}

func TestSettingsChangesReportTheNewSettingsAsLive(t *testing.T) {
	old := config.Defaults()
	next := old
	next.UIMode = config.UIModeWindow
	next.AnswerVolume = 30
	live, restart := settingsChanges(old, next)
	if len(restart) != 0 {
		t.Fatalf("interface and sound take effect at once, restart = %v", restart)
	}
	joined := strings.Join(live, ", ")
	if !strings.Contains(joined, "interfaz") || !strings.Contains(joined, "aviso de respuesta") {
		t.Fatalf("live = %v", live)
	}
}

func TestFriendlyErrorIsSpanish(t *testing.T) {
	cases := map[string]error{
		"no respondió ningún proveedor de IA": errors.New("ningún proveedor de IA respondió"),
		"tiempo agotado":                      context.DeadlineExceeded,
		"cancelada":                           context.Canceled,
		"sin proveedores":                     errors.New("no hay proveedores de IA configurados"),
		"otro error":                          errors.New("boom"),
	}
	for name, err := range cases {
		if got := FriendlyError(err); got == "" || strings.Contains(got, "boom") {
			t.Fatalf("%s: %q", name, got)
		}
	}
	if FriendlyError(nil) != "" {
		t.Fatal("no error, no text")
	}
}
