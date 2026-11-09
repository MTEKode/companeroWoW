package ui

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"
)

// osEnv is the real wizardEnv: the disk, the environment and the CLIs. The registry
// lookup is platform-specific (see wizard_reg_*.go).
type osEnv struct{}

// commandTimeout bounds each status command of the checks.
const commandTimeout = 10 * time.Second

func (osEnv) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (osEnv) IsDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func (osEnv) Getenv(key string) string { return os.Getenv(key) }

func (osEnv) RegString(keyPath, name string) (string, bool) { return regString(keyPath, name) }

// Run executes the command without a console window and with no input, and reports
// whether it exited with 0. Output is discarded: the checks never print secrets.
func (osEnv) Run(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	hideConsole(cmd)
	return cmd.Run()
}
