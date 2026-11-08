package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MTEKode/companeroWoW/app/internal/capture"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/skill"
)

const (
	// antigravityPromptLimit caps the prompt passed as the -p argument (in runes); a
	// Windows command line holds about 32k UTF-16 units.
	antigravityPromptLimit = 24000
	// antigravityMCPTimeoutSec is the per-call timeout agy applies to our MCP tools.
	antigravityMCPTimeoutSec = 120
	// antigravityWorkspaceDir is the folder under the data directory where agy runs. It holds
	// only the skill, the MCP registration and the session settings: no keys, no database.
	antigravityWorkspaceDir = "agy"
	// antigravitySettingsFile is the per-session settings file passed with --settings.
	antigravitySettingsFile = "antigravity-settings.json"
	// antigravitySkillLine asks for the workspace skill by name; agy also activates skills
	// by their description, the explicit mention makes it deterministic.
	antigravitySkillLine = "Aplica la skill \"" + skill.Name + "\" de este workspace a la respuesta."
)

// antigravityEfforts are the values of agy's --effort flag (see agy --help).
var antigravityEfforts = []string{"low", "medium", "high", "xhigh", "max"}

// agyNameRE accepts model names such as "gemini-3.8-flash-low": a flag value never holds
// spaces or quotes.
var agyNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,80}$`)

// antigravityAllow lets the model use only the companion's MCP server; writeAntigravitySettings
// adds read access to the screens folder.
var antigravityAllow = []string{"mcp(" + mcpServerName + "/*)"}

// antigravityDeny overrides any allow rule (deny > ask > allow), including the user's global
// settings: shell and any command, unsandboxed execution, file writes and URL fetches.
// Reads are not denied as a whole: read_file(*) would also deny the screens folder, and a deny
// cannot be carved out by an allow. Instead, the workspace holds no secrets, reads outside it
// need approval (refused in a headless run) and the secret files are denied by path.
var antigravityDeny = []string{"command(*)", "unsandboxed(*)", "write_file(*)", "read_url(*)", "execute_url(*)"}

// antigravity is the Antigravity CLI (agy) in headless mode: one `agy -p` process per question,
// with cwd = the workspace under the data directory, the companion's MCP server registered in
// the workspace and a session settings file that restricts tools. The user's global agy
// settings are not modified.
type antigravity struct {
	command   string
	workDir   string // the data directory: the screens folder and the secret files live here
	workspace string // agy's working directory: workDir\agy
	mcpURL    string
	model     string // --model; empty leaves agy's default
	effort    string // --effort; empty leaves it out
	timeout   time.Duration
	run       runner
	lookPath  func(string) (string, error)
}

// NewAntigravity returns the Antigravity CLI provider. Its command is cfg.AntigravityCommand
// (default "agy") on PATH, falling back to %LOCALAPPDATA%\agy\bin\agy.exe.
func NewAntigravity(cfg *config.Config, workDir string) Provider {
	return newAntigravity(cfg, workDir, execRunner{}, resolveAntigravity)
}

func newAntigravity(cfg *config.Config, workDir string, r runner, lookPath func(string) (string, error)) *antigravity {
	model := cfgString(cfg, func(c *config.Config) string { return c.AntigravityModel }, "")
	if !agyNameRE.MatchString(model) {
		if model != "" {
			log.Printf("antigravity: modelo %q no válido, se usa el de agy", model)
		}
		model = ""
	}
	// An empty effort is a choice: it leaves --effort out. The default comes from config.Defaults.
	effort := effortOf(cfg, func(c *config.Config) string { return c.AntigravityEffort })
	if !slices.Contains(antigravityEfforts, effort) {
		log.Printf("antigravity: esfuerzo %q no válido, se omite --effort", effort)
		effort = ""
	}
	return &antigravity{
		command:   cfgString(cfg, func(c *config.Config) string { return c.AntigravityCommand }, "agy"),
		workDir:   workDir,
		workspace: filepath.Join(workDir, antigravityWorkspaceDir),
		mcpURL:    mcpURL(cfg),
		model:     model,
		effort:    effort,
		timeout:   aiTimeout(cfg),
		run:       r,
		lookPath:  lookPath,
	}
}

func (a *antigravity) Name() string { return "antigravity" }

func (a *antigravity) Available() error {
	if _, err := a.lookPath(a.command); err != nil {
		return fmt.Errorf("CLI antigravity (agy) no disponible: %w", err)
	}
	return nil
}

// screensDir is the folder where screenshots are saved for agy to read.
func (a *antigravity) screensDir() string {
	return config.ScreensDir(a.workDir)
}

func (a *antigravity) Ask(ctx context.Context, req Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	if strings.TrimSpace(req.Prompt) == "" {
		return Response{}, errors.New("antigravity: la pregunta está vacía")
	}
	exe, err := a.lookPath(a.command)
	if err != nil {
		return Response{}, fmt.Errorf("CLI antigravity (agy) no disponible: %w", err)
	}
	if err := os.MkdirAll(a.workspace, 0o755); err != nil {
		return Response{}, fmt.Errorf("crear carpeta de trabajo: %w", err)
	}
	if err := writeAntigravityMCP(filepath.Join(a.workspace, ".agents", "mcp_config.json"), a.mcpURL); err != nil {
		return Response{}, err
	}
	// The skill lives in the workspace (.agents/skills/companero/SKILL.md, plus AGENTS.md as
	// always-on rules). When it cannot be written, the instructions travel inline instead.
	if _, err := skill.DeployAntigravity(a.workspace); err != nil {
		log.Printf("antigravity: skill inline (no pude escribir el workspace): %v", err)
	} else {
		req.Skill = ""
		req.System = antigravitySkillLine + "\n\n" + req.System
	}
	settingsPath := filepath.Join(a.workspace, antigravitySettingsFile)
	if err := writeAntigravitySettings(settingsPath, a.screensDir(), a.protectedFiles()); err != nil {
		return Response{}, err
	}

	// A screenshot from the overlay is saved as a file: headless agy takes no image, but it can
	// read the file that the prompt names.
	var imageLine string
	if len(req.ImagePNG) > 0 {
		path, err := capture.SavePNG(a.screensDir(), req.ImagePNG)
		if err != nil {
			log.Printf("antigravity: captura sin archivo: %v", err)
		} else {
			imageLine = "El usuario ha adjuntado una captura: está en " + path + ". Léela con read_file antes de responder."
		}
	}
	prompt := buildPromptCapped(req, imageLine, antigravityPromptLimit)

	args := []string{
		"--settings", settingsPath,
		"--output-format", "json",
	}
	if a.model != "" {
		args = append(args, "--model", a.model)
	}
	if a.effort != "" {
		args = append(args, "--effort", a.effort)
	}
	args = append(args, "-p", prompt)
	res, err := a.run.run(ctx, cmdSpec{
		Path: exe,
		Args: args,
		Dir:  a.workspace,
	})
	if err != nil {
		return Response{}, fmt.Errorf("antigravity: %w", err)
	}
	if res.ExitCode != 0 {
		return Response{}, cliFailure("antigravity", res)
	}
	text, err := parseAntigravityOutput(res.Stdout, res.Stderr)
	if err != nil {
		return Response{}, err
	}
	return Response{Text: text, Provider: a.Name()}, nil
}

// protectedFiles lists the files agy must never read: the API keys in every .env path, and the
// companion's database and configuration. Files are denied by exact path, never a folder that
// holds the screens folder.
func (a *antigravity) protectedFiles() []string {
	files := append([]string{}, config.DotenvPaths()...)
	files = append(files,
		filepath.Join(a.workDir, "companero.db"),
		filepath.Join(a.workDir, "config.json"),
	)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		files = append(files,
			filepath.Join(home, ".env"),
			filepath.Join(home, ".ssh"),
			filepath.Join(home, ".aws"),
		)
	}
	return files
}

// agyResult is the payload of a finished run: the json envelope itself, or the "result"
// event of stream-json output.
type agyResult struct {
	Status   string          `json:"status"`
	Response string          `json:"response"`
	Error    json.RawMessage `json:"error"`
}

// agyDoc is one JSON document of the output: a json envelope (the result fields at the top
// level) or one NDJSON event (the result under "result" when event is "result").
type agyDoc struct {
	Event  string     `json:"event"`
	Result *agyResult `json:"result"`
	agyResult
}

// parseAntigravityOutput reads the final answer. The output is either one JSON envelope
// (--output-format json) or NDJSON events of which the "result" event holds the answer.
// A status other than SUCCESS is an error, with a hint when the text names a login or quota
// problem so the chain can fall back.
func parseAntigravityOutput(stdout, stderr []byte) (string, error) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return "", nil
	}
	if json.Valid(trimmed) {
		if text, ok, err := parseAgyDoc(trimmed); ok || err != nil {
			return text, err
		}
	}
	lines := bytes.Split(trimmed, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' || !json.Valid(line) {
			continue
		}
		if text, ok, err := parseAgyDoc(line); ok || err != nil {
			return text, err
		}
	}
	msg := "antigravity: salida sin resultado reconocible"
	if hint := classifyFailure(string(stderr) + "\n" + string(stdout)); hint != "" {
		msg += ": " + hint
	}
	return "", errors.New(msg)
}

// parseAgyDoc reads one document. ok is false for documents that carry no answer (other
// events such as init or step_update).
func parseAgyDoc(data []byte) (string, bool, error) {
	var doc agyDoc
	if uerr := json.Unmarshal(data, &doc); uerr != nil {
		return "", false, nil
	}
	r := doc.agyResult
	switch {
	case doc.Event == "result":
		if doc.Result == nil {
			return "", false, nil
		}
		r = *doc.Result
	case doc.Event != "":
		return "", false, nil
	case doc.Result != nil:
		r = *doc.Result
	case r.Status == "" && r.Response == "" && len(r.Error) == 0:
		return "", false, nil
	}
	status := strings.ToUpper(strings.TrimSpace(r.Status))
	if status != "" && status != "SUCCESS" {
		msg := agyErrorText(r.Error)
		if msg == "" {
			msg = "estado " + status
		}
		if hint := classifyFailure(status + " " + msg); hint != "" {
			msg = hint + " (" + msg + ")"
		}
		return "", true, fmt.Errorf("antigravity: %s", msg)
	}
	return strings.TrimSpace(r.Response), true, nil
}

// agyErrorText turns the error field (a string or an object) into a short message.
func agyErrorText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var msg string
	if err := json.Unmarshal(raw, &msg); err != nil {
		var obj struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(raw, &obj); err != nil {
			return ""
		}
		msg = strings.TrimSpace(obj.Type + " " + obj.Message)
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if utf8.RuneCountInString(msg) > 300 {
		return clipRunes(msg, 300) + "…"
	}
	return msg
}

// writeAntigravityMCP registers the companion's MCP server in the workspace file
// <workDir>\agy\.agents\mcp_config.json. Other servers and keys already in the file are kept.
func writeAntigravityMCP(path, url string) error {
	doc, existing, err := readJSONObject(path)
	if err != nil {
		return err
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[mcpServerName] = map[string]any{
		"serverUrl":      url,
		"timeoutSeconds": antigravityMCPTimeoutSec,
	}
	doc["mcpServers"] = servers
	return writeJSONIfChanged(path, doc, existing)
}

// writeAntigravitySettings writes the per-session settings passed with --settings. The file is
// owned by the companion, so it is rewritten whole. The user's global settings are untouched.
// The screens folder is allowed for reading; each protected file is denied by its exact path.
// A deny on a folder would also cover the screens folder when the folder is inside it, so only
// files are denied.
func writeAntigravitySettings(path, screens string, protected []string) error {
	allow := append(append([]string{}, antigravityAllow...), readRule(screens))
	deny := append([]string{}, antigravityDeny...)
	for _, p := range protected {
		deny = append(deny, readRule(p))
	}
	doc := map[string]any{
		"toolPermission": "request-review",
		"permissions": map[string]any{
			"allow": allow,
			"deny":  deny,
		},
	}
	_, existing, err := readJSONObject(path)
	if err != nil {
		return err
	}
	return writeJSONIfChanged(path, doc, existing)
}

// readRule is the permission rule that grants or denies reading one path.
func readRule(path string) string {
	return "read_file(" + filepath.Clean(path) + ")"
}

// readJSONObject reads a JSON object from path. A missing or blank file is an empty object.
func readJSONObject(path string) (map[string]any, []byte, error) {
	doc := map[string]any{}
	existing, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return doc, nil, nil
	case err != nil:
		return nil, nil, fmt.Errorf("leer %s: %w", path, err)
	case len(bytes.TrimSpace(existing)) == 0:
		return doc, existing, nil
	}
	if err := json.Unmarshal(existing, &doc); err != nil {
		return nil, nil, fmt.Errorf("parsear %s: %w", path, err)
	}
	return doc, existing, nil
}

// writeJSONIfChanged writes doc as indented JSON unless it matches the existing bytes.
func writeJSONIfChanged(path string, doc map[string]any, existing []byte) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("codificar %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	if bytes.Equal(data, existing) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("crear %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("escribir %s: %w", path, err)
	}
	return nil
}

// resolveAntigravity finds agy on PATH, falling back to the installer's location.
func resolveAntigravity(name string) (string, error) {
	return resolveAntigravityWithFallback(name, defaultAgyPath())
}

// defaultAgyPath is where the Antigravity installer puts agy on Windows.
func defaultAgyPath() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return ""
	}
	return filepath.Join(local, "agy", "bin", "agy.exe")
}

func resolveAntigravityWithFallback(name, fallback string) (string, error) {
	p, err := resolveCommand(name)
	if err == nil {
		return p, nil
	}
	if fallback != "" {
		if fi, statErr := os.Stat(fallback); statErr == nil && !fi.IsDir() {
			return fallback, nil
		}
	}
	return "", fmt.Errorf("%w; tampoco está en %s", err, fallback)
}

// clipRunes shortens s to at most n runes.
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}
