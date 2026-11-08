//go:build !windows

package ui

import "errors"

// CopyText is a stub outside Windows: the overlay only runs there.
func CopyText(text string) error {
	return errors.New("copiar al portapapeles solo está disponible en Windows")
}
