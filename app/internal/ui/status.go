package ui

import (
	"fmt"
	"strings"
	"time"
)

// Share line limits. noStripHintAfter is how long a panel opened from the tray waits for the
// pixel strip before it says how to send the data; keyFreshFor is how old the key data may be
// and still count as up to date.
const (
	noStripHintAfter = 2 * time.Second
	keyFreshFor      = 5 * time.Minute
)

// formatStatus returns the one-line status bar text: the bridge state, the age of the
// SavedVariables data, the first hint, and any notices. While the assistant is busy the
// bar says so first and drops the hint.
func formatStatus(s Status, notice string, busy bool) string {
	var parts []string
	if busy {
		parts = append(parts, "Pensando…")
	}
	parts = append(parts, bridgeText(s), savedVarsText(s.SavedVarsAgeSec))
	if s.Model != "" {
		parts = append(parts, s.Model)
	}
	if s.Channel != "" {
		parts = append(parts, s.Channel)
	}
	if s.shareLine != "" {
		parts = append(parts, s.shareLine)
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

// shareNotice is the share line of the status bar: the countdown while the addon streams its
// share window; the hint when the panel was opened from the tray and no strip showed within
// noStripHintAfter; "Datos al día" with the age of the key data once it is fresh. hotkey is
// the configured panel key, spelled out in the hint. It returns "" when there is nothing to say.
func shareNotice(s Status, fromTray bool, sinceOpen time.Duration, hotkey string) string {
	switch {
	case s.ShareRemainingSec > 0:
		return fmt.Sprintf("Recibiendo datos del juego… %ds", s.ShareRemainingSec)
	case fromTray && s.Bridge != "connected" && sinceOpen >= noStripHintAfter:
		return "Pulsa " + spokenKey(hotkey) + " dentro del juego para enviar tus datos"
	case s.KeyKnown && time.Duration(s.KeyAgeSec)*time.Second <= keyFreshFor:
		return fmt.Sprintf("Datos al día (hace %ds)", s.KeyAgeSec)
	}
	return ""
}

// spokenKey writes a key combo for the status bar: "Ctrl+Shift+Space" reads "Ctrl+Shift+Espacio".
func spokenKey(hotkey string) string {
	return strings.ReplaceAll(hotkey, "Space", "Espacio")
}
