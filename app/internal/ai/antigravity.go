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
	"github.com/MTEKode/companeroWoW/app/internal/mcpserver"
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
// settings: shell and any command, unsandboxed execution, file writes and URL fetches. The
// secret files are denied by path in protectedFiles. Reads outside the workspace need approval
// anyway, which a headless run refuses; the deny list holds even if the user's global settings
// allow more.
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
	models    []string      // --model, in the order they are tried; never empty
	cooldown  time.Duration // how long a model that ran out of quota is left out
	effort    string        // --effort, for a model slug without an effort suffix; empty leaves it out
	timeout   time.Duration // the timeout of a model without a timeout of its own
	run       runner
	lookPath  func(string) (string, error)
}

// NewAntigravity returns the Antigravity CLI provider. Its command is cfg.AntigravityCommand
// (default "agy") on PATH, falling back to %LOCALAPPDATA%\agy\bin\agy.exe.
func NewAntigravity(cfg *config.Config, workDir string) Provider {
	return newAntigravity(cfg, workDir, execRunner{}, resolveAntigravity)
}

// antigravityModelList is the configured order of models. A file that still has only the
// single antigravityModel is read as a one-model list; an invalid or empty list gets the default.
func antigravityModelList(cfg *config.Config) []string {
	if cfg == nil {
		return append([]string(nil), config.DefaultAntigravityModels...)
	}
	list := cfg.AntigravityModels
	if len(list) == 0 && strings.TrimSpace(cfg.AntigravityModel) != "" {
		list = []string{cfg.AntigravityModel}
	}
	return config.NormalizeAntigravityModels(list)
}

// antigravityCooldown is the configured cooldown of a model out of quota.
func antigravityCooldown(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.AntigravityQuotaCooldownMin <= 0 {
		return config.DefaultQuotaCooldownMin * time.Minute
	}
	return time.Duration(cfg.AntigravityQuotaCooldownMin) * time.Minute
}

func newAntigravity(cfg *config.Config, workDir string, r runner, lookPath func(string) (string, error)) *antigravity {
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
		models:    antigravityModelList(cfg),
		cooldown:  antigravityCooldown(cfg),
		effort:    effort,
		timeout:   aiTimeout(cfg),
		run:       r,
		lookPath:  lookPath,
	}
}

func (a *antigravity) Name() string { return "antigravity" }

// Budget is the time the chain gives this provider: the timeouts of its models together. The
// chain's own per-provider timeout would cut Hydrogen short.
func (a *antigravity) Budget() time.Duration {
	var total time.Duration
	for _, m := range a.models {
		total += modelTimeout(m, a.timeout)
	}
	return total
}

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

// Ask answers with the first model that has quota. Each model has its own timeout. A model
// that runs out of quota goes on cooldown and the next model is tried at once; any other
// failure ends the provider, so that the chain moves on to codex or the API.
func (a *antigravity) Ask(ctx context.Context, req Request) (Response, error) {
	return a.ask(ctx, req, Stream{})
}

// AskStream is Ask with the answer written to s as agy produces it (stream-json).
func (a *antigravity) AskStream(ctx context.Context, req Request, s Stream) (Response, error) {
	return a.ask(ctx, req, s)
}

func (a *antigravity) ask(ctx context.Context, req Request, s Stream) (Response, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Response{}, errors.New("antigravity: la pregunta está vacía")
	}
	exe, err := a.lookPath(a.command)
	if err != nil {
		return Response{}, fmt.Errorf("CLI antigravity (agy) no disponible: %w", err)
	}
	if err := refuseBatchFile(exe); err != nil {
		return Response{}, err
	}
	if err := os.MkdirAll(a.workspace, 0o755); err != nil {
		return Response{}, fmt.Errorf("crear carpeta de trabajo: %w", err)
	}
	if err := writeAntigravityMCP(filepath.Join(a.workspace, ".agents", "mcp_config.json"), a.mcpURL, mcpserver.Token()); err != nil {
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

	candidates := a.candidates(time.Now())
	var lastErr error
	for i, model := range candidates {
		text, err := a.askModel(ctx, exe, settingsPath, prompt, model, s)
		if err == nil {
			log.Printf("antigravity: respondió %s", model)
			return Response{Text: text, Provider: a.Name() + ":" + model, Model: model}, nil
		}
		var qe *quotaError
		if !errors.As(err, &qe) {
			return Response{}, err
		}
		until := time.Now().Add(a.cooldown)
		quota.exhaust(model, until)
		lastErr = err
		// The next model answers from the start: what the exhausted one streamed is void.
		s.restart()
		if i+1 < len(candidates) {
			log.Printf("antigravity: cuota agotada en %s hasta %s; pruebo %s", model, until.Format("15:04"), candidates[i+1])
		} else {
			log.Printf("antigravity: cuota agotada en %s hasta %s; no quedan modelos", model, until.Format("15:04"))
		}
	}
	return Response{}, fmt.Errorf("antigravity: cuota agotada en todos los modelos (%s): %w", strings.Join(candidates, ", "), lastErr)
}

// askModel runs agy with one model and returns its answer. The model's own timeout bounds the
// run; when the question's context ends first, the error is the context's. With s, agy writes
// stream-json and the text of its answer steps goes to s as it comes. When such a run yields no
// answer and wrote none, the model is asked once more in plain JSON.
func (a *antigravity) askModel(ctx context.Context, exe, settingsPath, prompt, model string, s Stream) (string, error) {
	timeout := modelTimeout(model, a.timeout)
	mctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	format := "json"
	var onLine func([]byte)
	sent := false
	if s.on() {
		format = "stream-json"
		last := -1
		onLine = func(line []byte) {
			step, text := agyStepText(line)
			if text == "" {
				return
			}
			// A new answer step replaces the text of the previous one: only the last step is the answer.
			if last >= 0 && step != last {
				s.restart()
			}
			last = step
			sent = true
			s.delta(text)
		}
	}
	text, err := a.runAgy(mctx, exe, settingsPath, prompt, model, format, onLine)
	if err != nil && s.on() && !sent && errors.Is(err, errNoResult) && ctx.Err() == nil && mctx.Err() == nil {
		log.Printf("antigravity: stream-json sin respuesta con %s; pido la respuesta en JSON", model)
		text, err = a.runAgy(mctx, exe, settingsPath, prompt, model, "json", nil)
	}
	if err != nil {
		if ctx.Err() == nil && errors.Is(mctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("antigravity: tiempo agotado tras %s con %s", timeout, model)
		}
		return "", err
	}
	return text, nil
}

// runAgy runs agy once in the given output format and returns the final answer. onLine, when set,
// gets each stdout line while agy runs.
func (a *antigravity) runAgy(mctx context.Context, exe, settingsPath, prompt, model, format string, onLine func([]byte)) (string, error) {
	args := []string{
		"--settings", settingsPath,
		"--output-format", format,
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	// The slug names its effort; --effort is only for a slug without one.
	if a.effort != "" && !hasEffortSuffix(model) {
		args = append(args, "--effort", a.effort)
	}
	args = append(args, "-p", prompt)
	res, err := a.run.run(mctx, cmdSpec{
		Path:   exe,
		Args:   args,
		Dir:    a.workspace,
		OnLine: onLine,
	})
	if err != nil {
		return "", fmt.Errorf("antigravity: %w", err)
	}
	if res.ExitCode != 0 {
		return "", agyExitError(res)
	}
	return parseAntigravityOutput(res.Stdout, res.Stderr)
}

// agyStepText returns the step index and the text of a stream-json event that carries the
// answer: the text_delta of an "agent_response" step. For any other line the text is empty.
func agyStepText(line []byte) (int, string) {
	var ev struct {
		Event string `json:"event"`
		Step  struct {
			Index     int    `json:"step_index"`
			Type      string `json:"step_type"`
			TextDelta string `json:"text_delta"`
		} `json:"step_update"`
	}
	if json.Unmarshal(line, &ev) != nil || ev.Event != "step_update" || ev.Step.Type != "agent_response" {
		return -1, ""
	}
	return ev.Step.Index, ev.Step.TextDelta
}

// appDataFileNames are the files of the data folder that never leave the PC, listed by name so
// that they are denied even before they exist (the log rotates to companero.log.1).
var appDataFileNames = []string{
	"companero.db", "companero.db-wal", "companero.db-shm",
	"companero.log", "companero.log.1", "config.json", "window.json",
}

// userSecretFolders are the folders of the user's profile that hold keys and credentials.
var userSecretFolders = []string{".ssh", ".aws", ".gemini"}

// protectedFiles lists what agy must never read, as exact paths (a deny on a folder covers
// everything below it, and a deny beats an allow):
//   - every .env path that may hold API keys (see config.DotenvPaths) and ~\.env;
//   - the companion's database, log, configuration, window state and evaluation reports, which
//     are every file of the data folder: the folder is listed at each question;
//   - the user's ~\.ssh, ~\.aws and ~\.gemini folders;
//   - the MCP registration of the workspace, which holds the bearer token of this session.
//
// The data folder's sub-folders are denied too, except the workspace (agy) and the screens
// folder that agy reads on purpose: a deny on the data folder would also cover the screens.
func (a *antigravity) protectedFiles() []string {
	files := append([]string{}, config.DotenvPaths()...)
	for _, name := range appDataFileNames {
		files = append(files, filepath.Join(a.workDir, name))
	}
	files = append(files, a.dataFolderDenies()...)
	files = append(files, filepath.Join(a.workspace, ".agents", "mcp_config.json"))
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		files = append(files, filepath.Join(home, ".env"))
		for _, name := range userSecretFolders {
			files = append(files, filepath.Join(home, name))
		}
	}
	return files
}

// dataFolderDenies lists the entries of the data folder that agy may not read: every file, and
// every folder except the workspace and the screens folder.
func (a *antigravity) dataFolderDenies() []string {
	entries, err := os.ReadDir(a.workDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && (e.Name() == antigravityWorkspaceDir || e.Name() == filepath.Base(a.screensDir())) {
			continue
		}
		out = append(out, filepath.Join(a.workDir, e.Name()))
	}
	return out
}

// errNoResult means the output of agy held no answer.
var errNoResult = errors.New("antigravity: salida sin resultado reconocible")

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
	if hint := classifyFailure(string(stderr) + "\n" + string(stdout)); hint != "" {
		return "", fmt.Errorf("%w: %s", errNoResult, hint)
	}
	return "", errNoResult
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
		quota := isQuotaFailure(status + " " + msg)
		if hint := classifyFailure(status + " " + msg); hint != "" {
			msg = hint + " (" + msg + ")"
		}
		err := fmt.Errorf("antigravity: %s", msg)
		if quota {
			return "", true, &quotaError{err}
		}
		return "", true, err
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
// <workDir>\agy\.agents\mcp_config.json, with the session's bearer token in its headers. Other
// servers and keys already in the file are kept.
func writeAntigravityMCP(path, url, token string) error {
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
		"headers":        map[string]any{"Authorization": "Bearer " + token},
		"timeoutSeconds": antigravityMCPTimeoutSec,
	}
	doc["mcpServers"] = servers
	return writeJSONIfChanged(path, doc, existing)
}

// writeAntigravitySettings writes the per-session settings passed with --settings. The file is
// owned by the companion, so it is rewritten whole. The user's global settings are untouched.
// The screens folder is allowed for reading. Each protected path is denied: files by name and
// folders such as ~.ssh recursively. The data folder is never denied as a whole, because a deny
// on it would also cover the screens folder (deny beats allow); protectedFiles lists its entries.
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
