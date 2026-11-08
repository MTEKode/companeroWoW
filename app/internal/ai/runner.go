package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// cmdSpec describes one CLI invocation.
type cmdSpec struct {
	Path  string   // resolved executable
	Args  []string // arguments, passed without a shell
	Dir   string   // working directory
	Stdin string   // full prompt text
	Env   []string // extra KEY=value pairs for the child, on top of the app's environment
}

// cmdResult is what a finished invocation produced. A non-zero exit is reported in
// ExitCode, not as an error; the error is reserved for failing to start or for the
// context ending (timeout or cancel).
type cmdResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// runner executes CLI invocations. Tests substitute a fake.
type runner interface {
	run(ctx context.Context, spec cmdSpec) (cmdResult, error)
}

// maxCapturedBytes bounds how much output of one CLI call is kept in memory.
const maxCapturedBytes = 8 << 20

// execRunner runs real processes. It kills the whole process tree when the context ends.
type execRunner struct{}

func (execRunner) run(ctx context.Context, spec cmdSpec) (cmdResult, error) {
	if err := refuseBatchFile(spec.Path); err != nil {
		return cmdResult{}, err
	}
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	cmd.Stdin = strings.NewReader(spec.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{buf: &stdout, max: maxCapturedBytes}
	cmd.Stderr = &limitWriter{buf: &stderr, max: maxCapturedBytes}
	configureProcess(cmd)
	cmd.Cancel = func() error {
		_ = killProcessTree(cmd.Process)
		return nil
	}
	cmd.WaitDelay = 5 * time.Second

	err := cmd.Start()
	if err == nil {
		// The child joins the app's job object, so it dies with the app even if the app crashes.
		if jerr := joinJob(cmd.Process); jerr != nil {
			log.Printf("ai: el proceso %s no entra en el job object (se usa taskkill): %v", filepath.Base(spec.Path), jerr)
		}
		err = cmd.Wait()
	}
	res := cmdResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return res, ctxErr
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return res, nil
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
		if res.ExitCode == 0 {
			res.ExitCode = -1
		}
		return res, nil
	default:
		return res, fmt.Errorf("start %s: %w", filepath.Base(spec.Path), err)
	}
}

// limitWriter keeps the first max bytes and silently discards the rest.
type limitWriter struct {
	buf *bytes.Buffer
	max int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if room := w.max - w.buf.Len(); room > 0 {
		if len(p) > room {
			w.buf.Write(p[:room])
		} else {
			w.buf.Write(p)
		}
	}
	return len(p), nil
}

// resolveCommand finds an executable on PATH. On Windows the npm shims are plain
// .cmd files, so <name>.cmd is tried when the bare name is not found.
func resolveCommand(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("comando vacío")
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		for _, ext := range []string{".cmd", ".exe", ".bat"} {
			if p, err := exec.LookPath(name + ext); err == nil {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("no se encuentra %q en PATH", name)
}

// writeAttachment stores a screenshot in dir as attach-<unix>-*.png and returns its path
// and a cleanup func that deletes it.
func writeAttachment(dir string, png []byte) (string, func(), error) {
	f, err := os.CreateTemp(dir, fmt.Sprintf("attach-%d-*.png", time.Now().Unix()))
	if err != nil {
		return "", nil, fmt.Errorf("crear adjunto: %w", err)
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := f.Write(png); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("escribir adjunto: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("cerrar adjunto: %w", err)
	}
	return path, cleanup, nil
}

// classifyFailure turns CLI output into a Spanish hint for the user.
func classifyFailure(output string) string {
	lower := strings.ToLower(output)
	switch {
	case containsAny(lower, "login", "auth", "sign in", "api key", "not logged in"):
		return "sesión no iniciada o autenticación fallida; ejecuta el CLI y haz login"
	case containsAny(lower, "quota", "429", "resource_exhausted", "rate limit", "too many requests"):
		return "cuota o límite de uso agotado"
	}
	return ""
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// cliFailure builds the error for a CLI that exited with a non-zero code.
func cliFailure(name string, res cmdResult) error {
	combined := string(res.Stderr) + "\n" + string(res.Stdout)
	msg := fmt.Sprintf("%s terminó con código %d", name, res.ExitCode)
	if hint := classifyFailure(combined); hint != "" {
		msg += ": " + hint
	}
	if tail := tailText(res.Stderr, 300); tail != "" {
		msg += " (" + tail + ")"
	}
	return errors.New(msg)
}

// tailText returns the last n runes of the trimmed text, on a single line.
func tailText(b []byte, n int) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	r := []rune(s)
	if len(r) > n {
		return "…" + string(r[len(r)-n:])
	}
	return s
}

// refuseBatchFile rejects .cmd and .bat files. cmd.exe re-parses their arguments, so a prompt
// in argv could inject commands. The providers run the real executable instead: agy.exe, or
// node.exe with the codex script.
func refuseBatchFile(path string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
		return fmt.Errorf("no se ejecuta %s: los ficheros por lotes pasan los argumentos por cmd.exe", filepath.Base(path))
	}
	return nil
}
