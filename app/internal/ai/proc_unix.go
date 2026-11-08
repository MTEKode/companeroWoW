//go:build unix

package ai

import (
	"os"
	"os/exec"
	"syscall"
)

// configureProcess starts the child in its own process group so the whole tree can be killed.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree kills the child's process group, falling back to the process itself.
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return p.Kill()
}

// joinJob has no job object on unix: the process group is killed instead.
func joinJob(*os.Process) error { return nil }
