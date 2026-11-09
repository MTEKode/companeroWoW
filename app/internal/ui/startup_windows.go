//go:build windows

package ui

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

// runKeyPath is the per-user list of programs started at logon.
const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// runValueName names the app's entry in that list.
const runValueName = "CompaneroWoW"

// SetStartWithWoW adds or removes the app's entry in the HKCU Run key. Adding writes the
// path of the running executable, so a moved install is fixed by enabling it again.
func SetStartWithWoW(enabled bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("abrir la clave Run: %w", err)
	}
	defer k.Close()
	if !enabled {
		if err := k.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("quitar el inicio automático: %w", err)
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("localizar el programa: %w", err)
	}
	if err := k.SetStringValue(runValueName, runEntry(exe)); err != nil {
		return fmt.Errorf("escribir el inicio automático: %w", err)
	}
	return nil
}

// StartWithWoWRegistered reports whether the Run entry exists.
func StartWithWoWRegistered() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValueName)
	return err == nil
}
