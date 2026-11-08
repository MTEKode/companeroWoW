//go:build !windows

package ui

import "errors"

// Run needs Windows: the panel is a Win32 window.
func Run(Options) error {
	return errors.New("la ventana de Compañero WoW solo está disponible en Windows")
}

// Activate has nothing to bring forward outside Windows.
func Activate() bool { return false }
