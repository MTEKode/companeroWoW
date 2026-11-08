package ai

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

type codex struct {
	command  string
	workDir  string
	mcpURL   string
	timeout  time.Duration
	run      runner
	lookPath func(string) (string, error)
}

// NewCodex returns the Codex CLI provider. It runs `codex exec` read-only with
// cwd = workDir and the companion's MCP server passed as a -c override.
func NewCodex(cfg *config.Config, workDir string) Provider {
	return newCodex(cfg, workDir, execRunner{}, resolveCommand)
}

func newCodex(cfg *config.Config, workDir string, r runner, lookPath func(string) (string, error)) *codex {
	return &codex{
		command:  cfgString(cfg, func(c *config.Config) string { return c.CodexCommand }, "codex"),
		workDir:  workDir,
		mcpURL:   mcpURL(cfg),
		timeout:  aiTimeout(cfg),
		run:      r,
		lookPath: lookPath,
	}
}

func (c *codex) Name() string { return "codex" }

func (c *codex) Available() error {
	if _, err := c.lookPath(c.command); err != nil {
		return fmt.Errorf("CLI codex no disponible: %w", err)
	}
	return nil
}

func (c *codex) Ask(ctx context.Context, req Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	exe, err := c.lookPath(c.command)
	if err != nil {
		return Response{}, fmt.Errorf("CLI codex no disponible: %w", err)
	}
	if err := os.MkdirAll(c.workDir, 0o755); err != nil {
		return Response{}, fmt.Errorf("crear carpeta de trabajo: %w", err)
	}

	// The final message is captured through --output-last-message into a file in workDir.
	lastFile, err := os.CreateTemp(c.workDir, "last-*.txt")
	if err != nil {
		return Response{}, fmt.Errorf("crear archivo de respuesta: %w", err)
	}
	lastPath := lastFile.Name()
	_ = lastFile.Close()
	defer func() { _ = os.Remove(lastPath) }()

	args := []string{
		"exec",
		"--skip-git-repo-check",
		"--ephemeral",
		"--color", "never",
		"-s", "read-only",
		"-C", c.workDir,
		"-c", `approval_policy="never"`,
		"-c", fmt.Sprintf(`mcp_servers.%s.url="%s"`, mcpServerName, c.mcpURL),
		"-c", fmt.Sprintf(`mcp_servers.%s.default_tools_approval_mode="approve"`, mcpServerName),
		"-o", lastPath,
	}

	var imageLine string
	if len(req.ImagePNG) > 0 {
		path, cleanup, err := writeAttachment(c.workDir, req.ImagePNG)
		if err != nil {
			return Response{}, err
		}
		defer cleanup()
		// -i is variadic, so it goes last; with no positional prompt codex reads stdin.
		args = append(args, "-i", path)
		imageLine = "Captura de pantalla adjunta: la imagen que acompaña esta petición."
	}

	res, err := c.run.run(ctx, cmdSpec{
		Path:  exe,
		Args:  args,
		Dir:   c.workDir,
		Stdin: buildPrompt(req, imageLine),
	})
	if err != nil {
		return Response{}, fmt.Errorf("codex: %w", err)
	}
	if res.ExitCode != 0 {
		return Response{}, cliFailure("codex", res)
	}
	data, err := os.ReadFile(lastPath)
	if err != nil {
		return Response{}, fmt.Errorf("codex no escribió la respuesta: %w", err)
	}
	return Response{Text: strings.TrimSpace(string(data)), Provider: c.Name()}, nil
}
