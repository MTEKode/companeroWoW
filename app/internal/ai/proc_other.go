//go:build !windows && !unix

package ai

import (
	"os"
	"os/exec"
)

// configureProcess has no platform-specific settings on this OS.
func configureProcess(cmd *exec.Cmd) {}

// killProcessTree kills only the process itself on this OS.
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}

// joinJob has no job object on this OS.
func joinJob(*os.Process) error { return nil }
