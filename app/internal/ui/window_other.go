//go:build !windows

package ui

import "errors"

// Run needs Windows and WebView2; elsewhere it reports that the overlay is unavailable.
func Run(Options) error {
	return errors.New("la ventana de Compañero WoW solo está disponible en Windows")
}

// Activate has nothing to bring forward outside Windows.
func Activate() bool { return false }

// RuntimeInstalled is always false outside Windows.
func RuntimeInstalled() bool { return false }
