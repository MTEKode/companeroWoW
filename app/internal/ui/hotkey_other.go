//go:build !windows

package ui

import "errors"

// hotkeyLoop is the global shortcut; it exists only on Windows.
type hotkeyLoop struct{}

// startHotkey is unavailable outside Windows.
func startHotkey(Hotkey, func()) (*hotkeyLoop, error) {
	return nil, errors.New("el atajo global solo está disponible en Windows")
}

// Change is unavailable outside Windows.
func (*hotkeyLoop) Change(Hotkey) error {
	return errors.New("el atajo global solo está disponible en Windows")
}

// Close has nothing to stop outside Windows.
func (*hotkeyLoop) Close() {}
