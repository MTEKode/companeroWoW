//go:build windows

package ui

import "golang.org/x/sys/windows/registry"

// regString reads a string value under HKEY_LOCAL_MACHINE. ok is false when the key or the
// value is missing.
func regString(keyPath, name string) (string, bool) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return "", false
	}
	return v, true
}
