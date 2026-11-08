package ai

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/mcpserver"
	"github.com/MTEKode/companeroWoW/app/internal/skill"
)

const (
	// codexWorkspaceDir is the folder under the data directory where codex runs. It holds only
	// AGENTS.md (the skill), the attachments of one question and the answer file. Nothing else
	// of the app lives there, and codex is told to read nothing outside it.
	codexWorkspaceDir = "codex"
	// codexTokenEnv is the environment variable that carries the MCP bearer token to codex. The
	// token is not put in the command line, where other processes could read it.
	codexTokenEnv = "COMPANERO_MCP_TOKEN"
	// codexFilesystemProfile is the filesystem permission of the codex sessions (config key
	// permissions.companero.filesystem): read access to the system files the sandbox needs and
	// to the working directory, nothing else. The write access of the -s read-only sandbox is
	// kept as well. Reads of the rest of the disk (documents, keys, the app's data folder) are
	// not granted; see README for what this does and does not guarantee.
	codexFilesystemProfile = `{":minimal"="read", ":project_roots"={"."="read"}}`
)

type codex struct {
	command   string
	workDir   string // the data directory
	workspace string // codex's working directory: workDir\codex
	mcpURL    string
	model     string // -m; empty leaves codex's default
	effort    string // model_reasoning_effort; empty leaves it out
	timeout   time.Duration
	run       runner
	lookPath  func(string) (string, error)
}

// codexEffortRE accepts reasoning effort names such as "low" (they go into a -c value).
var codexEffortRE = regexp.MustCompile(`^[a-z]{2,10}$`)

// codexShimScriptRE finds the JavaScript entry point in the npm shim (codex.cmd):
// "%dp0%\node_modules\@openai\codex\bin\codex.js".
var codexShimScriptRE = regexp.MustCompile(`%dp0%[\\/]+([^"\r\n]+?\.js)"`)

// NewCodex returns the Codex CLI provider. It runs `codex exec` in its own workspace under
// workDir, with the companion's MCP server passed as a -c override.
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
		command:   cfgString(cfg, func(c *config.Config) string { return c.CodexCommand }, "codex"),
		workDir:   workDir,
		workspace: filepath.Join(workDir, codexWorkspaceDir),
		mcpURL:    mcpURL(cfg),
		model:     model,
		effort:    effort,
		timeout:   aiTimeout(cfg),
		run:       r,
		lookPath:  lookPath,
	}
}

func (c *codex) Name() string { return "codex" }

func (c *codex) Available() error {
	if _, err := c.lookPath(c.command); err != nil {
		return fmt.Errorf("CLI codex no disponible: %w", err)
	}
	return nil
}

// launch returns the executable and the leading arguments that start codex. On Windows the
// npm shim (codex.cmd) is not run: cmd.exe would re-parse the arguments, so node.exe runs the
// script that the shim points at.
func (c *codex) launch() (string, []string, error) {
	p, err := c.lookPath(c.command)
	if err != nil {
		return "", nil, fmt.Errorf("CLI codex no disponible: %w", err)
	}
	return codexLaunch(p, c.lookPath)
}

// codexLaunch resolves a found codex path to a direct launch. A .cmd or .bat shim is read for
// its JavaScript entry point, which runs under node (node.exe next to the shim, else on PATH).
func codexLaunch(path string, lookPath func(string) (string, error)) (string, []string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
	default:
		return path, nil, nil
	}
	shim, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("leer el lanzador de codex: %w", err)
	}
	m := codexShimScriptRE.FindSubmatch(shim)
	if m == nil {
		return "", nil, fmt.Errorf("el lanzador %s no apunta a un script de codex", filepath.Base(path))
	}
	dir := filepath.Dir(path)
	script := filepath.Join(dir, filepath.FromSlash(strings.ReplaceAll(string(m[1]), `\`, "/")))
	if fi, err := os.Stat(script); err != nil || fi.IsDir() {
		return "", nil, fmt.Errorf("no encuentro el script de codex %s", script)
	}
	node := filepath.Join(dir, "node.exe")
	if _, err := os.Stat(node); err != nil {
		if node, err = lookPath("node"); err != nil {
			return "", nil, fmt.Errorf("no encuentro node.exe para ejecutar codex: %w", err)
		}
	}
	return node, []string{script}, nil
}

func (c *codex) Ask(ctx context.Context, req Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	exe, prefix, err := c.launch()
	if err != nil {
		return Response{}, err
	}
	if err := os.MkdirAll(c.workspace, 0o755); err != nil {
		return Response{}, fmt.Errorf("crear carpeta de trabajo: %w", err)
	}
	// Codex reads AGENTS.md from its working directory, so the skill is not repeated in
	// the prompt when the file is in place. Otherwise it travels inline.
	if _, err := skill.DeployCodex(c.workspace); err != nil {
		log.Printf("codex: skill inline (no pude escribir AGENTS.md): %v", err)
	} else {
		req.Skill = ""
	}

	// The final message is captured through --output-last-message into a file in the workspace.
	lastFile, err := os.CreateTemp(c.workspace, "last-*.txt")
	if err != nil {
		return Response{}, fmt.Errorf("crear archivo de respuesta: %w", err)
	}
	lastPath := lastFile.Name()
	_ = lastFile.Close()
	defer func() { _ = os.Remove(lastPath) }()

	args := append(append([]string{}, prefix...),
		"exec",
		"--skip-git-repo-check",
		"--ephemeral",
		"--strict-config",
		"--color", "never",
		"-s", "read-only",
		"-C", c.workspace,
		"-c", `approval_policy="never"`,
		"-c", `default_permissions="companero"`,
		"-c", "permissions.companero.filesystem="+codexFilesystemProfile,
		"-c", fmt.Sprintf(`mcp_servers.%s.url="%s"`, mcpServerName, c.mcpURL),
		"-c", fmt.Sprintf(`mcp_servers.%s.bearer_token_env_var="%s"`, mcpServerName, codexTokenEnv),
		"-c", fmt.Sprintf(`mcp_servers.%s.default_tools_approval_mode="approve"`, mcpServerName),
		"-o", lastPath,
	)
	if c.model != "" {
		args = append(args, "-m", c.model)
	}
	if c.effort != "" {
		args = append(args, "-c", fmt.Sprintf(`model_reasoning_effort="%s"`, c.effort))
	}

	var imageLine string
	if len(req.ImagePNG) > 0 {
		path, cleanup, err := writeAttachment(c.workspace, req.ImagePNG)
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
		Dir:   c.workspace,
		Stdin: buildPrompt(req, imageLine),
		Env:   []string{codexTokenEnv + "=" + mcpserver.Token()},
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
