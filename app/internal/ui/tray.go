package ui

import "strings"

// trayTipMax is the longest tooltip Shell_NotifyIcon accepts, in UTF-16 units, less the
// terminating zero. Runes are counted here, which is close enough for the Spanish text.
const trayTipMax = 127

// trayTip is the tray icon's tooltip for a status: the app name and the status line (the
// WoW connection, level and zone). It is cut to what the shell accepts.
func trayTip(s Status) string {
	tip := strings.Join([]string{"Compañero WoW", bridgeText(s)}, " · ")
	if u := []rune(tip); len(u) > trayTipMax {
		tip = string(u[:trayTipMax])
	}
	return tip
}

// opacityAlpha converts the opacity in percent to the 0-255 alpha of the layered window.
// Out-of-range values are clamped to the limits of the settings screen.
func opacityAlpha(pct int) uint8 {
	if pct < 60 {
		pct = 60
	}
	if pct > 100 {
		pct = 100
	}
	return uint8(pct * 255 / 100)
}
