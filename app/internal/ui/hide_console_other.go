//go:build !windows

package ui

import "os/exec"

// hideConsole does nothing outside Windows, where children have no console window.
func hideConsole(*exec.Cmd) {}
