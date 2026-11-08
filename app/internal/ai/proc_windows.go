package ai

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// createNoWindow is the CREATE_NO_WINDOW process creation flag.
const createNoWindow = 0x08000000

// configureProcess hides the console window of the child process.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// killProcessTree ends p and all its children. taskkill /T walks the tree, which
// Process.Kill alone does not; Process.Kill is the fallback.
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	tk := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid))
	configureProcess(tk)
	if err := tk.Run(); err == nil {
		return nil
	}
	return p.Kill()
}
