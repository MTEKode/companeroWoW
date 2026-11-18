package ui

import (
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// The settings form carries the mode: the selector's value is saved, and an unknown value is
// refused with a message that names the field.
func TestSettingsFormAppliesMode(t *testing.T) {
	base := config.Defaults()
	base.Mode = config.ModeOnDemand

	f := formFromConfig(base)
	f.Mode = config.ModeContinuous
	got, err := f.apply(base)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got.Mode != config.ModeContinuous {
		t.Fatalf("Mode = %q, want continuous", got.Mode)
	}

	f.Mode = "siempre"
	if _, err := f.apply(base); err == nil || !strings.Contains(err.Error(), "Funcionamiento") {
		t.Fatalf("apply with unknown mode: err = %v, want a Funcionamiento message", err)
	}

	f.Mode = ""
	got, err = f.apply(base)
	if err != nil {
		t.Fatalf("apply without mode: %v", err)
	}
	if got.Mode != config.ModeOnDemand {
		t.Fatalf("empty mode must keep the base's mode, got %q", got.Mode)
	}
}

// The tips note is the one the Ajustes screen shows under the disabled tips selector.
func TestTipsOnDemandNoteText(t *testing.T) {
	if TipsOnDemandNote != "En modo bajo demanda la IA solo responde cuando preguntas" {
		t.Fatalf("note = %q", TipsOnDemandNote)
	}
}
