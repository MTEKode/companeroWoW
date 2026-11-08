package ai

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/skill"
)

type codex struct {
	command  string
	workDir  string
	mcpURL   string
	model    string // -m; empty leaves codex's default
	effort   string // model_reasoning_effort; empty leaves it out
	timeout  time.Duration
	run      runner
	lookPath func(string) (string, error)
}

// codexEffortRE accepts reasoning effort names such as "low" (they go into a -c value).
var codexEffortRE = regexp.MustCompile(`^[a-z]{2,10}$`)

// NewCodex returns the Codex CLI provider. It runs `codex exec` read-only with
// cwd = workDir and the companion's MCP server passed as a -c override.
func NewCodex(cfg *config.Config, workDir string) Provider {
	return newCodex(cfg, workDir, execRunner{}, resolveCommand)
}

func newCodex(cfg *config.Config, workDir string, r runner, lookPath func(string) (string, error)) *codex {
	model := cfgString(cfg, func(c *config.Config) string { return c.CodexModel }, "")
	if !agyNameRE.MatchString(model) {
		if model != "" {
			log.Printf("codex: modelo %q no válido, se usa el de codex", model)
		}
		model = ""
	}
	// An empty effort is a choice: it leaves the override out. The default comes from config.Defaults.
	effort := effortOf(cfg, func(c *config.Config) string { return c.CodexEffort })
	if !codexEffortRE.MatchString(effort) {
		log.Printf("codex: esfuerzo %q no válido, se omite", effort)
		effort = ""
	}
	return &codex{
		command:  cfgString(cfg, func(c *config.Config) string { return c.CodexCommand }, "codex"),
		workDir:  workDir,
		mcpURL:   mcpURL(cfg),
		model:    model,
		effort:   effort,
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
	// Codex reads AGENTS.md from its working directory, so the skill is not repeated in
	// the prompt when the file is in place. Otherwise it travels inline.
	if _, err := skill.DeployCodex(c.workDir); err != nil {
		log.Printf("codex: skill inline (no pude escribir AGENTS.md): %v", err)
	} else {
		req.Skill = ""
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
	if c.model != "" {
		args = append(args, "-m", c.model)
	}
	if c.effort != "" {
		args = append(args, "-c", fmt.Sprintf(`model_reasoning_effort="%s"`, c.effort))
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
