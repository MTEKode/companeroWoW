//go:build !windows

package ui

import "errors"

// keyHook is the keyboard-hook panel key; it exists only on Windows.
type keyHook struct{}

// startKeyHook is unavailable outside Windows.
func startKeyHook(Hotkey, func()) (*keyHook, error) {
	return nil, errors.New("el atajo global solo está disponible en Windows")
}

// Change is unavailable outside Windows.
func (*keyHook) Change(Hotkey) error {
	return errors.New("el atajo global solo está disponible en Windows")
}

// Close has nothing to stop outside Windows.
func (*keyHook) Close() {}
