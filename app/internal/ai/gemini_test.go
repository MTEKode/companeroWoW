package ai

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func argValue(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func TestGeminiBuildsHeadlessCallAndSettings(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{Stdout: []byte(`{"response":"  Eldoria está al norte.  ","stats":{}}`)}, nil
	}}
	cfg := testConfig()
	p := newGemini(&cfg, workDir, fr, found)

	req := Request{
		System:  "Eres un compañero.",
		History: []Message{{Role: "user", Content: "hola"}, {Role: "assistant", Content: "¡hola!"}},
		Prompt:  "¿dónde está Eldoria?",
	}
	resp, err := p.Ask(context.Background(), req)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Text != "Eldoria está al norte." || resp.Provider != "gemini" {
		t.Fatalf("resp = %+v", resp)
	}

	if len(fr.calls) != 1 {
		t.Fatalf("runner called %d times", len(fr.calls))
	}
	spec := fr.calls[0]
	if spec.Path != "/fake/bin/gemini" {
		t.Errorf("Path = %q", spec.Path)
	}
	if spec.Dir != workDir {
		t.Errorf("Dir = %q, want %q", spec.Dir, workDir)
	}
	if got := argValue(spec.Args, "--output-format"); got != "json" {
		t.Errorf("--output-format = %q", got)
	}
	if got := argValue(spec.Args, "--approval-mode"); got != "default" {
		t.Errorf("--approval-mode = %q (must never be yolo)", got)
	}
	if !hasArg(spec.Args, "--skip-trust") || !hasArg(spec.Args, "-p") {
		t.Errorf("args missing -p / --skip-trust: %v", spec.Args)
	}
	for _, a := range spec.Args {
		if strings.Contains(a, "Eldoria") {
			t.Errorf("question leaked into argv: %q", a)
		}
	}
	for _, want := range []string{"Eres un compañero.", "Historial:", "Usuario: hola", "Asistente: ¡hola!", "Pregunta: ¿dónde está Eldoria?"} {
		if !strings.Contains(spec.Stdin, want) {
			t.Errorf("stdin missing %q:\n%s", want, spec.Stdin)
		}
	}

	raw, err := os.ReadFile(filepath.Join(workDir, ".gemini", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	var doc struct {
		MCPServers map[string]struct {
			HTTPURL string `json:"httpUrl"`
			Trust   bool   `json:"trust"`
		} `json:"mcpServers"`
		Tools struct {
			Exclude []string `json:"exclude"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("settings.json invalid: %v\n%s", err, raw)
	}
	srv, ok := doc.MCPServers["companero"]
	if !ok {
		t.Fatalf("mcpServers.companero missing:\n%s", raw)
	}
	if srv.HTTPURL != "http://127.0.0.1:47321/mcp" || !srv.Trust {
		t.Errorf("companero server = %+v", srv)
	}
	for _, tool := range []string{"run_shell_command", "write_file", "replace"} {
		if !containsString(doc.Tools.Exclude, tool) {
			t.Errorf("tools.exclude %v missing %q", doc.Tools.Exclude, tool)
		}
	}
}

func TestGeminiSettingsKeepsOtherKeysAndDoesNotDuplicate(t *testing.T) {
	workDir := t.TempDir()
	dir := filepath.Join(workDir, ".gemini")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	existing := `{"theme":"dark","tools":{"exclude":["write_file"]},"mcpServers":{"other":{"command":"x"}}}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{Stdout: []byte("texto plano")}, nil
	}}
	cfg := testConfig()
	p := newGemini(&cfg, workDir, fr, found)
	for i := 0; i < 2; i++ {
		if _, err := p.Ask(context.Background(), Request{Prompt: "x"}); err != nil {
			t.Fatalf("Ask %d: %v", i, err)
		}
	}
	raw, _ := os.ReadFile(path)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["theme"] != "dark" {
		t.Errorf("theme lost: %s", raw)
	}
	servers := doc["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Errorf("other MCP server lost: %s", raw)
	}
	excl := doc["tools"].(map[string]any)["exclude"].([]any)
	seen := map[any]int{}
	for _, v := range excl {
		seen[v]++
	}
	for v, n := range seen {
		if n > 1 {
			t.Errorf("exclude entry %v duplicated %d times", v, n)
		}
	}
	if len(excl) != 3 {
		t.Errorf("exclude = %v, want the 3 unique entries", excl)
	}
}

func TestGeminiAuthFailureReturnsError(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{ExitCode: 41, Stderr: []byte("Please run the login command: not authenticated")}, nil
	}}
	cfg := testConfig()
	_, err := newGemini(&cfg, workDir, fr, found).Ask(context.Background(), Request{Prompt: "x"})
	if err == nil {
		t.Fatal("expected auth error")
	}
	if !strings.Contains(err.Error(), "autenticación") || !strings.Contains(err.Error(), "41") {
		t.Fatalf("error = %v", err)
	}
}

func TestGeminiQuotaFailureFromJSONError(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{Stdout: []byte(`{"error":{"type":"ApiError","message":"RESOURCE_EXHAUSTED (429)"}}`)}, nil
	}}
	cfg := testConfig()
	_, err := newGemini(&cfg, workDir, fr, found).Ask(context.Background(), Request{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "cuota") {
		t.Fatalf("err = %v, want quota error", err)
	}
}

func TestGeminiQuotaFailureFromExitStderr(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{ExitCode: 1, Stderr: []byte("HTTP 429 Too Many Requests")}, nil
	}}
	cfg := testConfig()
	_, err := newGemini(&cfg, workDir, fr, found).Ask(context.Background(), Request{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "cuota") {
		t.Fatalf("err = %v, want quota error", err)
	}
}

func TestGeminiAttachesScreenshotAndCleansUp(t *testing.T) {
	workDir := t.TempDir()
	var attachPath string
	fr := &fakeRunner{fn: func(spec cmdSpec) (cmdResult, error) {
		at := strings.Index(spec.Stdin, "@")
		if at < 0 {
			t.Errorf("stdin has no @ reference:\n%s", spec.Stdin)
			return cmdResult{Stdout: []byte("ok")}, nil
		}
		line := spec.Stdin[at+1:]
		attachPath = strings.SplitN(line, "\n", 2)[0]
		if data, err := os.ReadFile(attachPath); err != nil || string(data) != "\x89PNG-fake" {
			t.Errorf("attachment not readable during the call: %v", err)
		}
		return cmdResult{Stdout: []byte(`{"response":"veo un mapa"}`)}, nil
	}}
	cfg := testConfig()
	_, err := newGemini(&cfg, workDir, fr, found).Ask(context.Background(), Request{Prompt: "¿qué ves?", ImagePNG: []byte("\x89PNG-fake")})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if attachPath == "" {
		t.Fatal("no attachment path seen")
	}
	if _, err := os.Stat(attachPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("attachment not deleted after call: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(attachPath), "attach-") {
		t.Errorf("attachment name %q", attachPath)
	}
}

func TestGeminiAvailableReportsMissingBinary(t *testing.T) {
	cfg := testConfig()
	p := newGemini(&cfg, t.TempDir(), &fakeRunner{}, func(string) (string, error) {
		return "", errors.New("not found")
	})
	if err := p.Available(); err == nil {
		t.Fatal("Available should fail when the binary is missing")
	}
}

func TestParseGeminiOutputSkipsLogLinesBeforeJSON(t *testing.T) {
	text, err := parseGeminiOutput([]byte("Loaded cached credentials.\n{\"response\":\"respuesta limpia\"}\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if text != "respuesta limpia" {
		t.Fatalf("text = %q", text)
	}
	plain, err := parseGeminiOutput([]byte("solo texto\nsegunda línea"))
	if err != nil || plain != "solo texto\nsegunda línea" {
		t.Fatalf("plain output = %q, %v", plain, err)
	}
}

func TestGeminiInvalidJSONIsError(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{Stdout: []byte(`{"response": `)}, nil
	}}
	cfg := testConfig()
	if _, err := newGemini(&cfg, workDir, fr, found).Ask(context.Background(), Request{Prompt: "x"}); err == nil {
		t.Fatal("expected error for invalid JSON output")
	}
}
