package ui

import (
	"fmt"
	"strings"
	"sync"
)

// IdleText is the status line of the on-demand mode while the game is not read.
const IdleText = "En reposo — abre el panel o pregunta para leer el juego"

// reading is the progress of a question that waits for fresh game data ("Leyendo el juego…
// (3/5)"); empty when no question is waiting. See ReportReading.
var (
	readingMu sync.Mutex
	reading   string
)

// ReportReading sets the progress line shown while a question waits for the game data, and
// refreshes the status bar. An empty text clears it.
func ReportReading(text string) {
	readingMu.Lock()
	reading = text
	readingMu.Unlock()
	wakeStatus()
}

// readingText returns the progress line set by ReportReading.
func readingText() string {
	readingMu.Lock()
	defer readingMu.Unlock()
	return reading
}

// formatStatus returns the one-line status bar text: the bridge state, the age of the
// SavedVariables data, the first hint, and any notices. While the assistant is busy the
// bar says so first and drops the hint. In the on-demand mode, while the game is not read,
// the bar says so instead of the bridge state.
func formatStatus(s Status, notice string, busy bool) string {
	var parts []string
	if s.Idle && !busy {
		parts = append(parts, IdleText)
		for _, n := range []string{s.Notice, notice} {
			if n != "" {
				parts = append(parts, n)
			}
		}
		return strings.Join(parts, " · ")
	}
	if busy {
		if r := readingText(); r != "" {
			parts = append(parts, r)
		} else {
			parts = append(parts, "Pensando…")
		}
	}
	parts = append(parts, bridgeText(s), savedVarsText(s.SavedVarsAgeSec))
	if s.Model != "" {
		parts = append(parts, s.Model)
	}
	if s.Channel != "" {
		parts = append(parts, s.Channel)
	}
	if !busy && len(s.Hints) > 0 {
		parts = append(parts, s.Hints[0])
	}
	for _, n := range []string{s.Notice, notice} {
		if n != "" {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, " · ")
}

// bridgeText describes the WoW connection.
func bridgeText(s Status) string {
	switch s.Bridge {
	case "connected":
		t := "● WoW conectado"
		if s.Level > 0 {
			t += fmt.Sprintf(" · nivel %d", s.Level)
		}
		if s.Zone != "" {
			t += " · " + s.Zone
		}
		return t
	case "no_strip":
		return "○ Sin franja de píxeles"
	case "no_wow":
		return "○ WoW no detectado"
	case "":
		return "○ Comprobando…"
	}
	return "○ WoW no detectado"
}

// savedVarsText describes how old the SavedVariables file is; age is in seconds, or
// negative when unknown.
func savedVarsText(age int64) string {
	if age < 0 {
		return "SavedVariables sin datos"
	}
	return "SavedVariables " + humanAge(age)
}

// humanAge formats an age in seconds in Spanish, for example "hace 5 min".
func humanAge(sec int64) string {
	switch {
	case sec < 60:
		return "hace menos de 1 min"
	case sec < 3600:
		return fmt.Sprintf("hace %d min", sec/60)
	case sec < 86400:
		return fmt.Sprintf("hace %d h", sec/3600)
	default:
		return fmt.Sprintf("hace %d d", sec/86400)
	}
}
