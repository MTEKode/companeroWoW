package ui

import (
	"context"
	"errors"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// The game mode (uiMode "juego", the default) keeps the app invisible: the external panel never
// opens by itself. The hotkey only activates the reading of the game channel, tips do not pop up,
// and the answers arrive on the clipboard with a sound (see internal/gamemode). The panel opens
// when the player asks for it from the tray menu, and the first-run wizard still opens it.

// gameMode reports whether the interface is the game mode. Anything but the window mode is the
// game mode, so a config that lost its uiMode behaves as the default.
func gameMode(uiMode string) bool {
	return uiMode != config.UIModeWindow
}

// hotkeyAction is what the global shortcut does.
type hotkeyAction int

const (
	// hotkeyToggle shows the panel, or hides it when shown (window mode).
	hotkeyToggle hotkeyAction = iota
	// hotkeyActivate only activates the reading of the game channel; the panel stays closed (game mode).
	hotkeyActivate
)

// hotkeyFor returns what the shortcut does in the given interface mode.
func hotkeyFor(uiMode string) hotkeyAction {
	if gameMode(uiMode) {
		return hotkeyActivate
	}
	return hotkeyToggle
}

// gameTipRoute turns the route of a proactive tip into the game mode's: the tip is kept in the
// conversation and the badge of the tray icon counts it, but nothing pops up outside the game.
func gameTipRoute(r TipRoute) TipRoute {
	r.ShowPanel = false
	r.Balloon = false
	r.Defer = false
	return r
}

// FriendlyError is the one-line Spanish text for an answer that could not be given. It is what
// the game-mode clipboard block says for an error.
func FriendlyError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "La respuesta tardó demasiado. Inténtalo de nuevo."
	case errors.Is(err, context.Canceled):
		return "Pregunta cancelada."
	case strings.Contains(err.Error(), "ningún proveedor de IA respondió"):
		return "No respondió ningún proveedor de IA. Revisa la sesión de agy/codex o la clave de API."
	case strings.Contains(err.Error(), "no hay proveedores de IA configurados"):
		return "No hay proveedores de IA configurados. Revisa config.json."
	}
	return "No pude responder. Revisa companero.log."
}
