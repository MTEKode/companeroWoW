package ui

import (
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// The panel key is seen two ways. The default is a low-level keyboard hook that only
// observes: the key also reaches WoW, so the addon's share binding (Ctrl+Shift+Space) fires
// in the game. RegisterHotKey ("exclusivo") takes the key away from every application.
// The matching below is platform-neutral so that it can be tested everywhere.

// debounceWindow is the least time between two accepted presses of the panel key.
const debounceWindow = 300 * time.Millisecond

// keyEvent is one key transition seen by the hook: a Windows virtual-key code and
// whether the key went down (true) or up.
type keyEvent struct {
	VK   uint32
	Down bool
}

// Virtual-key codes of the modifiers. The low-level hook reports the left and right keys
// separately; the generic codes are accepted too.
const (
	vkModShift    = 0x10
	vkModControl  = 0x11
	vkModMenu     = 0x12 // Alt
	vkModLWin     = 0x5B
	vkModRWin     = 0x5C
	vkModLShift   = 0xA0
	vkModRShift   = 0xA1
	vkModLControl = 0xA2
	vkModRControl = 0xA3
	vkModLMenu    = 0xA4
	vkModRMenu    = 0xA5
)

// modifierBit returns the Mods bit of a modifier key, or 0 for any other key.
func modifierBit(vk uint32) uint32 {
	switch vk {
	case vkModControl, vkModLControl, vkModRControl:
		return ModCtrl
	case vkModShift, vkModLShift, vkModRShift:
		return ModShift
	case vkModMenu, vkModLMenu, vkModRMenu:
		return ModAlt
	case vkModLWin, vkModRWin:
		return ModWin
	}
	return 0
}

// comboMatcher follows the modifier keys and reports when the combo is pressed: the
// trigger key goes down while exactly the combo's modifiers are held. Auto-repeat of the
// trigger does not fire again, and the trigger must be released before it can fire again.
// It is used from the hook thread only.
type comboMatcher struct {
	down        [256]bool // modifier keys currently held, by virtual-key code
	triggerDown bool
}

// feed takes one key event and reports whether it completes the combo.
func (m *comboMatcher) feed(ev keyEvent, combo Hotkey) bool {
	if ev.VK >= uint32(len(m.down)) {
		return false
	}
	if modifierBit(ev.VK) != 0 {
		m.down[ev.VK] = ev.Down
		return false
	}
	if ev.VK != combo.VK {
		return false
	}
	if !ev.Down {
		m.triggerDown = false
		return false
	}
	if m.triggerDown {
		return false // auto-repeat
	}
	m.triggerDown = true
	return m.mods() == combo.Mods
}

// mods returns the modifier bits currently held.
func (m *comboMatcher) mods() uint32 {
	var bits uint32
	for vk, on := range m.down {
		if on {
			bits |= modifierBit(uint32(vk))
		}
	}
	return bits
}

// debouncer accepts a press only when debounceWindow has passed since the last accepted one.
type debouncer struct {
	window time.Duration
	last   time.Time
	fired  bool
}

// allow reports whether a press at now is accepted. A refused press does not restart the wait.
func (d *debouncer) allow(now time.Time) bool {
	if d.fired && now.Sub(d.last) < d.window {
		return false
	}
	d.fired = true
	d.last = now
	return true
}

// hotkeyMethodFor returns the method a config value selects. Anything but the exclusive
// method listens with the hook, so an unknown or empty value keeps the default.
func hotkeyMethodFor(s string) string {
	if s == config.HotkeyExclusive {
		return config.HotkeyExclusive
	}
	return config.HotkeyListen
}

// hotkeyListener is the panel key: the keyboard hook or RegisterHotKey. Change moves it to
// another combo; Close stops it.
type hotkeyListener interface {
	Change(hk Hotkey) error
	Close()
}
