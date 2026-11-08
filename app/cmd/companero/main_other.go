//go:build !windows

package main

// acquireSingleInstance always succeeds outside Windows.
func acquireSingleInstance() (release func(), first bool) {
	return func() {}, true
}

// attachConsole is a no-op outside Windows: stdout is already the terminal.
func attachConsole() bool {
	return true
}
