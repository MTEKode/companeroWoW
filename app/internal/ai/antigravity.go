package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

const (
	// antigravityPromptLimit caps the prompt passed as the -p argument (in runes); a
	// Windows command line holds about 32k UTF-16 units.
	antigravityPromptLimit = 24000
	// antigravityMCPTimeoutSec is the per-call timeout agy applies to our MCP tools.
	antigravityMCPTimeoutSec = 120
	// antigravityImageNote replaces the screenshot: headless agy has no image attach flag,
	// so the model is told to fetch the capture with the MCP tool instead.
	antigravityImageNote = "El usuario ha adjuntado una captura: usa la herramienta capturar_pantalla para verla."
	// antigravitySettingsFile is the per-session settings file passed with --settings.
	antigravitySettingsFile = "antigravity-settings.json"
)

// antigravityAllow lets the model use only the companion's MCP server.
var antigravityAllow = []string{"mcp(" + mcpServerName + "/*)"}

// antigravityDeny overrides any allow rule (deny > ask > allow), including the user's global
// settings: shell and any command, unsandboxed execution, file writes, file reads (the model
// must not read the user's .env or other files) and URL fetches. Other tools default to ask,
// and headless runs soft-deny an approval they cannot obtain.
var antigravityDeny = []string{
	"command(*)", "unsandboxed(*)", "write_file(*)", "read_file(*)", "read_url(*)", "execute_url(*)",
}

// antigravity is the Antigravity CLI (agy) in headless mode: one `agy -p` process per question,
// with cwd = workDir, the companion's MCP server registered in the workspace and a session
// settings file that restricts tools. The user's global agy settings are not modified.
type antigravity struct {
	command  string
	workDir  string
	mcpURL   string
	timeout  time.Duration
	run      runner
	lookPath func(string) (string, error)
}

// NewAntigravity returns the Antigravity CLI provider. Its command is cfg.AntigravityCommand
// (default "agy") on PATH, falling back to %LOCALAPPDATA%\agy\bin\agy.exe.
func NewAntigravity(cfg *config.Config, workDir string) Provider {
	return newAntigravity(cfg, workDir, execRunner{}, resolveAntigravity)
}

func newAntigravity(cfg *config.Config, workDir string, r runner, lookPath func(string) (string, error)) *antigravity {
	return &antigravity{
		command:  cfgString(cfg, func(c *config.Config) string { return c.AntigravityCommand }, "agy"),
		workDir:  workDir,
		mcpURL:   mcpURL(cfg),
		timeout:  aiTimeout(cfg),
		run:      r,
		lookPath: lookPath,
	}
}

func (a *antigravity) Name() string { return "antigravity" }

func (a *antigravity) Available() error {
	if _, err := a.lookPath(a.command); err != nil {
		return fmt.Errorf("CLI antigravity (agy) no disponible: %w", err)
	}
	return nil
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
	if err := os.MkdirAll(a.workDir, 0o755); err != nil {
		return Response{}, fmt.Errorf("crear carpeta de trabajo: %w", err)
	}
	if err := writeAntigravityMCP(filepath.Join(a.workDir, ".agents", "mcp_config.json"), a.mcpURL); err != nil {
		return Response{}, err
	}
	settingsPath := filepath.Join(a.workDir, antigravitySettingsFile)
	if err := writeAntigravitySettings(settingsPath); err != nil {
		return Response{}, err
	}

	var imageLine string
	if len(req.ImagePNG) > 0 {
		imageLine = antigravityImageNote
	}
	prompt := buildPromptCapped(req, imageLine, antigravityPromptLimit)

	args := []string{
		"--settings", settingsPath,
		"--output-format", "json",
		"-p", prompt,
	}
	res, err := a.run.run(ctx, cmdSpec{
		Path: exe,
		Args: args,
		Dir:  a.workDir,
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
// <workDir>\.agents\mcp_config.json. Other servers and keys already in the file are kept.
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
func writeAntigravitySettings(path string) error {
	doc := map[string]any{
		"toolPermission": "request-review",
		"permissions": map[string]any{
			"allow": antigravityAllow,
			"deny":  antigravityDeny,
		},
	}
	_, existing, err := readJSONObject(path)
	if err != nil {
		return err
	}
	return writeJSONIfChanged(path, doc, existing)
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
