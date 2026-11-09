//go:build !windows

package ui

// regString has no registry outside Windows.
func regString(string, string) (string, bool) { return "", false }
