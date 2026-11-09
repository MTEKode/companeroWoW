//go:build !windows

package ui

import "errors"

// SetStartWithWoW is Windows-only.
func SetStartWithWoW(bool) error {
	return errors.New("iniciar con WoW solo está disponible en Windows")
}

// StartWithWoWRegistered is always false outside Windows.
func StartWithWoWRegistered() bool { return false }
