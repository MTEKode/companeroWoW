//go:build !windows

package ui

import "errors"

// startHotkey is unavailable outside Windows.
func startHotkey(Hotkey, func()) error {
	return errors.New("el atajo global solo está disponible en Windows")
}
