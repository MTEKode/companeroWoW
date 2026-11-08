package ui

import (
	"fmt"
	"strings"
)

// Modifier bits as used by RegisterHotKey.
const (
	ModAlt   uint32 = 0x0001
	ModCtrl  uint32 = 0x0002
	ModShift uint32 = 0x0004
	ModWin   uint32 = 0x0008
)

// Hotkey is a parsed global shortcut: modifier bits and a Windows virtual-key code.
type Hotkey struct {
	Mods uint32
	VK   uint32
}

// ParseHotkey parses strings such as "Ctrl+Shift+Space" or "Alt+F1". Modifiers are
// Ctrl, Alt, Shift and Win (case-insensitive, with a few aliases). The key is A-Z,
// 0-9, F1-F24, Space, Enter, Tab or Esc. Exactly one key is required.
func ParseHotkey(s string) (Hotkey, error) {
	var hk Hotkey
	raw := strings.TrimSpace(s)
	if raw == "" {
		return hk, fmt.Errorf("atajo vacío")
	}
	parts := strings.Split(raw, "+")
	keys := 0
	for _, p := range parts {
		tok := strings.ToUpper(strings.TrimSpace(p))
		if tok == "" {
			return Hotkey{}, fmt.Errorf("atajo %q: hay un «+» sin tecla", s)
		}
		switch tok {
		case "CTRL", "CONTROL":
			hk.Mods |= ModCtrl
			continue
		case "ALT":
			hk.Mods |= ModAlt
			continue
		case "SHIFT":
			hk.Mods |= ModShift
			continue
		case "WIN", "WINDOWS":
			hk.Mods |= ModWin
			continue
		}
		vk, ok := keyCode(tok)
		if !ok {
			return Hotkey{}, fmt.Errorf("atajo %q: tecla desconocida %q", s, strings.TrimSpace(p))
		}
		hk.VK = vk
		keys++
	}
	if keys != 1 {
		return Hotkey{}, fmt.Errorf("atajo %q: debe tener exactamente una tecla además de los modificadores", s)
	}
	return hk, nil
}

// String returns the canonical form, for example "Ctrl+Shift+Space".
func (h Hotkey) String() string {
	var parts []string
	if h.Mods&ModCtrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if h.Mods&ModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if h.Mods&ModShift != 0 {
		parts = append(parts, "Shift")
	}
	if h.Mods&ModWin != 0 {
		parts = append(parts, "Win")
	}
	parts = append(parts, keyName(h.VK))
	return strings.Join(parts, "+")
}

func keyCode(tok string) (uint32, bool) {
	switch {
	case len(tok) == 1 && tok[0] >= 'A' && tok[0] <= 'Z':
		return uint32(tok[0]), true
	case len(tok) == 1 && tok[0] >= '0' && tok[0] <= '9':
		return uint32(tok[0]), true
	case strings.HasPrefix(tok, "F"):
		n, ok := smallInt(tok[1:])
		if ok && n >= 1 && n <= 24 {
			return uint32(0x70 + n - 1), true // VK_F1 = 0x70
		}
		return 0, false
	}
	switch tok {
	case "SPACE":
		return 0x20, true
	case "ENTER", "RETURN":
		return 0x0D, true
	case "TAB":
		return 0x09, true
	case "ESC", "ESCAPE":
		return 0x1B, true
	}
	return 0, false
}

func keyName(vk uint32) string {
	switch {
	case vk >= 'A' && vk <= 'Z', vk >= '0' && vk <= '9':
		return string(rune(vk))
	case vk >= 0x70 && vk <= 0x87:
		return fmt.Sprintf("F%d", vk-0x70+1)
	}
	switch vk {
	case 0x20:
		return "Space"
	case 0x0D:
		return "Enter"
	case 0x09:
		return "Tab"
	case 0x1B:
		return "Esc"
	}
	return fmt.Sprintf("VK%02X", vk)
}

func smallInt(s string) (int, bool) {
	if s == "" || len(s) > 2 {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}
