package ui

import (
	"strings"
	"testing"
)

// The on-demand mode, at rest, says so in the status bar instead of the bridge state.
func TestStatusIdleSaysItIsAtRest(t *testing.T) {
	got := formatStatus(Status{Idle: true, Bridge: "connected", Level: 19}, "", false)
	if !strings.HasPrefix(got, IdleText) {
		t.Fatalf("status = %q, want it to start with the idle text", got)
	}
	if strings.Contains(got, "WoW conectado") {
		t.Fatalf("an idle status must not show the bridge state: %q", got)
	}
}

// While a question waits for the game, the bar shows the reading progress instead of "Pensando".
func TestStatusShowsReadingProgressWhileBusy(t *testing.T) {
	ReportReading("Leyendo el juego… (3/5)")
	defer ReportReading("")
	got := formatStatus(Status{Bridge: "connected"}, "", true)
	if !strings.Contains(got, "Leyendo el juego… (3/5)") || strings.Contains(got, "Pensando") {
		t.Fatalf("status = %q, want the reading progress and no Pensando", got)
	}
	ReportReading("")
	if got := formatStatus(Status{Bridge: "connected"}, "", true); !strings.Contains(got, "Pensando") {
		t.Fatalf("status = %q, want Pensando once the reading is done", got)
	}
}

// A normal status (active, not busy) is unchanged by the on-demand text.
func TestStatusActiveIsUnchanged(t *testing.T) {
	got := formatStatus(Status{Bridge: "connected", Level: 19, Zone: "Orgrimmar", SavedVarsAgeSec: 30}, "", false)
	if strings.Contains(got, IdleText) || !strings.Contains(got, "WoW conectado") {
		t.Fatalf("status = %q", got)
	}
}
