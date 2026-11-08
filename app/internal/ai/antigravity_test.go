package ai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

const envelopeSuccess = `{"status":"SUCCESS","response":"  Eldoria está al norte.  ","num_turns":1}`

func TestAntigravityBuildsHeadlessCall(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{Stdout: []byte(envelopeSuccess)}, nil
	}}
	cfg := testConfig()
	p := newAntigravity(&cfg, workDir, fr, found)

	req := Request{
		System:  "Eres un compañero.",
		History: []Message{{Role: "user", Content: "hola"}, {Role: "assistant", Content: "¡hola!"}},
		Prompt:  "¿dónde está Eldoria?",
	}
	resp, err := p.Ask(context.Background(), req)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Text != "Eldoria está al norte." || resp.Provider != "antigravity" {
		t.Fatalf("resp = %+v", resp)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("runner called %d times", len(fr.calls))
	}
	spec := fr.calls[0]
	if spec.Path != "/fake/bin/agy" {
		t.Errorf("Path = %q, want /fake/bin/agy", spec.Path)
	}
	if want := filepath.Join(workDir, antigravityWorkspaceDir); spec.Dir != want {
		t.Errorf("Dir = %q, want the workspace %q", spec.Dir, want)
	}
	if spec.Stdin != "" {
		t.Errorf("Stdin = %q, want empty (prompt travels in -p)", spec.Stdin)
	}
	if got := argValue(spec.Args, "--output-format"); got != "json" {
		t.Errorf("--output-format = %q, want json", got)
	}
	if got := filepath.Base(argValue(spec.Args, "--settings")); got != antigravitySettingsFile {
		t.Errorf("--settings file = %q, want %s", got, antigravitySettingsFile)
	}
	prompt := argValue(spec.Args, "-p")
	for _, want := range []string{"Eres un compañero.", "Historial:", "Usuario: hola", "Asistente: ¡hola!", "Pregunta: ¿dónde está Eldoria?"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("-p prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, a := range spec.Args {
		if strings.Contains(a, "dangerously") || strings.Contains(a, "yolo") || strings.Contains(a, "always-proceed") {
			t.Errorf("unsafe argument %q", a)
		}
	}
}

func TestAntigravityMCPConfigMergesAndKeepsOtherKeys(t *testing.T) {
	workDir := t.TempDir()
	dir := filepath.Join(workDir, antigravityWorkspaceDir, ".agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mcp_config.json")
	existing := `{"theme":"x","mcpServers":{"otro":{"command":"tool"},"companero":{"serverUrl":"http://127.0.0.1:1/mcp"}}}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{Stdout: []byte(envelopeSuccess)}, nil
	}}
	cfg := testConfig()
	p := newAntigravity(&cfg, workDir, fr, found)
	for i := 0; i < 2; i++ {
		if _, err := p.Ask(context.Background(), Request{Prompt: "x"}); err != nil {
			t.Fatalf("Ask %d: %v", i, err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Theme      string `json:"theme"`
		MCPServers map[string]struct {
			ServerURL      string `json:"serverUrl"`
			TimeoutSeconds int    `json:"timeoutSeconds"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("mcp_config invalid: %v\n%s", err, raw)
	}
	if doc.Theme != "x" {
		t.Errorf("other top-level key lost: %s", raw)
	}
	if _, ok := doc.MCPServers["otro"]; !ok {
		t.Errorf("other server lost: %s", raw)
	}
	srv := doc.MCPServers["companero"]
	if srv.ServerURL != "http://127.0.0.1:47321/mcp" || srv.TimeoutSeconds != 120 {
		t.Errorf("companero = %+v", srv)
	}
	if strings.Contains(string(raw), "httpUrl") {
		t.Errorf("httpUrl is not supported by agy: %s", raw)
	}
}

func TestParseAntigravityStreamJSONResult(t *testing.T) {
	out := strings.Join([]string{
		`{"event":"init","conversation_id":"c1","init":{"cwd":"x","tools":["run_command"]}}`,
		`{"event":"step_update","step_update":{"state":"ACTIVE"}}`,
		`{"event":"result","result":{"conversation_id":"c1","status":"SUCCESS","response":"Respuesta final","num_turns":1}}`,
	}, "\n") + "\n"
	text, err := parseAntigravityOutput([]byte(out), nil)
	if err != nil || text != "Respuesta final" {
		t.Fatalf("text = %q, err = %v", text, err)
	}
}

func TestParseAntigravityEnvelopeVariants(t *testing.T) {
	multiline := "{\n  \"status\": \"SUCCESS\",\n  \"response\": \"varias líneas\"\n}\n"
	text, err := parseAntigravityOutput([]byte(multiline), nil)
	if err != nil || text != "varias líneas" {
		t.Fatalf("pretty envelope: text = %q, err = %v", text, err)
	}
	if text, err := parseAntigravityOutput([]byte("  \n"), nil); err != nil || text != "" {
		t.Fatalf("empty output: text = %q, err = %v", text, err)
	}
	if _, err := parseAntigravityOutput([]byte("texto suelto sin json"), nil); err == nil {
		t.Fatal("plain text without a result must be an error")
	}
}

func TestParseAntigravityErrorStatusIsError(t *testing.T) {
	out := `{"event":"result","result":{"status":"ERROR","error":{"type":"AUTH","message":"authentication required, run agy to log in"}}}`
	_, err := parseAntigravityOutput([]byte(out), nil)
	if err == nil {
		t.Fatal("ERROR status must be an error")
	}
	if !strings.Contains(err.Error(), "sesión no iniciada") || !strings.Contains(err.Error(), "authentication required") {
		t.Fatalf("auth failure not classified: %v", err)
	}
}

func TestParseAntigravityQuotaStatus(t *testing.T) {
	out := `{"status":"ERROR","error":"429 RESOURCE_EXHAUSTED: quota exceeded"}`
	_, err := parseAntigravityOutput([]byte(out), nil)
	if err == nil || !strings.Contains(err.Error(), "cuota") {
		t.Fatalf("quota failure not classified: %v", err)
	}
}

func TestAntigravityAuthFailureOnExitCodeIsError(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{ExitCode: 1, Stderr: []byte("Error: authentication required. Please sign in.")}, nil
	}}
	cfg := testConfig()
	p := newAntigravity(&cfg, workDir, fr, found)
	_, err := p.Ask(context.Background(), Request{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "sesión no iniciada") {
		t.Fatalf("err = %v, want classified auth failure", err)
	}
}

func TestAntigravityImageIsNotAttachedButNoted(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{Stdout: []byte(envelopeSuccess)}, nil
	}}
	cfg := testConfig()
	p := newAntigravity(&cfg, workDir, fr, found)
	if _, err := p.Ask(context.Background(), Request{Prompt: "¿qué ves?", ImagePNG: []byte("\x89PNG fake")}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	prompt := argValue(fr.calls[0].Args, "-p")
	if !strings.Contains(prompt, filepath.Join(workDir, "screens")) || !strings.Contains(prompt, "read_file") {
		t.Errorf("image path note missing:\n%s", prompt)
	}
	if n, _ := filepath.Glob(filepath.Join(workDir, "screens", "screenshot-*.png")); len(n) != 1 {
		t.Errorf("screenshot file count = %d, want 1", len(n))
	}
	entries, _ := os.ReadDir(workDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "attach-") {
			t.Errorf("screenshot was written to %s", e.Name())
		}
	}
}

func TestAntigravityPromptIsCappedDroppingOldestHistory(t *testing.T) {
	var hist []Message
	for i := 0; i < 10; i++ {
		hist = append(hist, Message{Role: "user", Content: "turno-" + string(rune('a'+i)) + strings.Repeat("x", 4000)})
	}
	req := Request{System: "Sistema", History: hist, Prompt: "pregunta-final"}
	text := buildPromptCapped(req, "", antigravityPromptLimit)
	if n := utf8.RuneCountInString(text); n > antigravityPromptLimit {
		t.Fatalf("prompt has %d runes, limit %d", n, antigravityPromptLimit)
	}
	if !strings.Contains(text, "pregunta-final") {
		t.Fatal("the question was dropped")
	}
	if strings.Contains(text, "turno-a") {
		t.Fatal("oldest turn should be dropped first")
	}
	if !strings.Contains(text, "turno-j") {
		t.Fatal("newest turn should be kept")
	}
}

func TestAntigravityPromptCapWithHugeSystemPrompt(t *testing.T) {
	req := Request{System: strings.Repeat("ñ", 30000), Prompt: "fin"}
	text := buildPromptCapped(req, "", antigravityPromptLimit)
	if n := utf8.RuneCountInString(text); n > antigravityPromptLimit {
		t.Fatalf("prompt has %d runes, limit %d", n, antigravityPromptLimit)
	}
	if !utf8.ValidString(text) {
		t.Fatal("cut split a rune")
	}
}

func TestAntigravityUnavailableWhenMissing(t *testing.T) {
	cfg := testConfig()
	p := newAntigravity(&cfg, t.TempDir(), &fakeRunner{}, func(string) (string, error) {
		return "", os.ErrNotExist
	})
	if err := p.Available(); err == nil || !strings.Contains(err.Error(), "agy") {
		t.Fatalf("Available = %v", err)
	}
}

func TestAntigravityCommandFromConfig(t *testing.T) {
	cfg := testConfig()
	cfg.AntigravityCommand = `C:\custom\agy.exe`
	var asked string
	p := newAntigravity(&cfg, t.TempDir(), &fakeRunner{}, func(name string) (string, error) {
		asked = name
		return name, nil
	})
	if err := p.Available(); err != nil {
		t.Fatalf("Available: %v", err)
	}
	if asked != `C:\custom\agy.exe` {
		t.Fatalf("looked up %q", asked)
	}
}

func TestResolveAntigravityFallbackPath(t *testing.T) {
	fallback := filepath.Join(t.TempDir(), "agy.exe")
	if err := os.WriteFile(fallback, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveAntigravityWithFallback("companero-no-existe-agy", fallback)
	if err != nil || got != fallback {
		t.Fatalf("fallback = %q, %v", got, err)
	}
	if _, err := resolveAntigravityWithFallback("companero-no-existe-agy", filepath.Join(t.TempDir(), "nope.exe")); err == nil {
		t.Fatal("missing command and fallback must be an error")
	}
}

func TestChainSkipsRetiredGemini(t *testing.T) {
	cfg := testConfig()
	cfg.ProviderOrder = []string{"gemini", "antigravity", "codex"}
	c := BuildChain(&cfg, t.TempDir(), nil)
	want := []string{"antigravity", "codex"}
	if got := c.Providers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Providers() = %v, want %v", got, want)
	}
}
