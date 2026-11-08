package ai

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/mcpserver"
)

// npmShim is the text of the codex shim that npm writes on Windows (abridged to the lines that matter).
const npmShim = `@ECHO off
IF EXIST "%dp0%\node.exe" (
  SET "_prog=%dp0%\node.exe"
) ELSE (
  SET "_prog=node"
)
"%_prog%"  "%dp0%\node_modules\@openai\codex\bin\codex.js" %*
`

func TestCodexShimRunsNodeWithScriptNotCmd(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "codex.cmd")
	script := filepath.Join(dir, "node_modules", "@openai", "codex", "bin", "codex.js")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shim, []byte(npmShim), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("// codex"), 0o644); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(dir, "node.exe")
	if err := os.WriteFile(node, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}

	exe, prefix, err := codexLaunch(shim, found)
	if err != nil {
		t.Fatalf("codexLaunch: %v", err)
	}
	if exe != node {
		t.Errorf("exe = %q, want node.exe next to the shim", exe)
	}
	if len(prefix) != 1 || prefix[0] != script {
		t.Errorf("prefix = %v, want [%s]", prefix, script)
	}
	if strings.HasSuffix(strings.ToLower(exe), ".cmd") {
		t.Error("codex must never run through cmd.exe")
	}
}

func TestCodexShimWithoutScriptIsAnError(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "codex.cmd")
	if err := os.WriteFile(shim, []byte("@echo hola\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := codexLaunch(shim, found); err == nil {
		t.Fatal("a shim without a script must fail, not run cmd.exe")
	}
}

func TestCodexExecutableIsRunDirectly(t *testing.T) {
	exe, prefix, err := codexLaunch(filepath.Join("bin", "codex.exe"), found)
	if err != nil || exe != filepath.Join("bin", "codex.exe") || prefix != nil {
		t.Fatalf("codexLaunch(.exe) = %q %v %v", exe, prefix, err)
	}
}

func TestBatchFilesAreRefused(t *testing.T) {
	for _, p := range []string{`C:\npm\codex.cmd`, `C:\x\agy.BAT`} {
		if err := refuseBatchFile(p); err == nil {
			t.Errorf("refuseBatchFile(%q) = nil, want error", p)
		}
	}
	for _, p := range []string{`C:\agy\bin\agy.exe`, "agy"} {
		if err := refuseBatchFile(p); err != nil {
			t.Errorf("refuseBatchFile(%q) = %v", p, err)
		}
	}
}

func TestAntigravityRefusesBatchShim(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		t.Fatal("agy must not run through a batch file")
		return cmdResult{}, nil
	}}
	cfg := testConfig()
	shim := func(string) (string, error) { return `C:\npm\agy.cmd`, nil }
	p := newAntigravity(&cfg, workDir, fr, shim)
	if _, err := p.Ask(context.Background(), Request{Prompt: "x"}); err == nil {
		t.Fatal("Ask with agy.cmd = nil error")
	}
	if len(fr.calls) != 0 {
		t.Fatalf("runner called %d times", len(fr.calls))
	}
}

func TestAntigravityMCPConfigCarriesTheBearerToken(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{Stdout: []byte(envelopeSuccess)}, nil
	}}
	cfg := testConfig()
	p := newAntigravity(&cfg, workDir, fr, found)
	if _, err := p.Ask(context.Background(), Request{Prompt: "x"}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	doc, _, err := readJSONObject(filepath.Join(workDir, "agy", ".agents", "mcp_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	servers := doc["mcpServers"].(map[string]any)
	entry := servers[mcpServerName].(map[string]any)
	headers, _ := entry["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer "+mcpserver.Token() {
		t.Errorf("headers = %v, want the bearer token", headers)
	}
}

func TestAntigravityProtectedFilesCoverTheDataFolder(t *testing.T) {
	workDir := t.TempDir()
	for _, name := range []string{"companero.db-wal", "companero.log", "eval-run1.json", "window.json", "config.json"} {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"agy", "screens", "webview2"} {
		if err := os.MkdirAll(filepath.Join(workDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := testConfig()
	a := newAntigravity(&cfg, workDir, &fakeRunner{}, found)
	files := a.protectedFiles()

	for _, want := range []string{
		filepath.Join(workDir, "companero.db"),
		filepath.Join(workDir, "companero.db-wal"),
		filepath.Join(workDir, "companero.db-shm"),
		filepath.Join(workDir, "companero.log.1"),
		filepath.Join(workDir, "eval-run1.json"),
		filepath.Join(workDir, "window.json"),
		filepath.Join(workDir, "webview2"),
		filepath.Join(a.workspace, ".agents", "mcp_config.json"),
	} {
		if !slices.Contains(files, want) {
			t.Errorf("protected files miss %s", want)
		}
	}
	for _, keep := range []string{a.workspace, a.screensDir()} {
		if slices.Contains(files, keep) {
			t.Errorf("%s must stay readable, but it is protected", keep)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, want := range []string{".ssh", ".aws", ".gemini", ".env"} {
			if !slices.Contains(files, filepath.Join(home, want)) {
				t.Errorf("protected files miss ~\\%s", want)
			}
		}
	}
}

func TestAntigravitySettingsDenyTheProtectedPaths(t *testing.T) {
	workDir := t.TempDir()
	cfg := testConfig()
	a := newAntigravity(&cfg, workDir, &fakeRunner{}, found)
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := writeAntigravitySettings(path, a.screensDir(), a.protectedFiles()); err != nil {
		t.Fatal(err)
	}
	doc, _, err := readJSONObject(path)
	if err != nil {
		t.Fatal(err)
	}
	perms := doc["permissions"].(map[string]any)
	allow := toStrings(perms["allow"])
	deny := toStrings(perms["deny"])
	if !slices.Contains(allow, "read_file("+a.screensDir()+")") {
		t.Errorf("allow %v lacks the screens folder", allow)
	}
	if !slices.Contains(deny, "read_file("+filepath.Join(workDir, "config.json")+")") {
		t.Errorf("deny %v lacks config.json", deny)
	}
	if runtime.GOOS == "windows" && slices.Contains(deny, "read_file("+a.screensDir()+")") {
		t.Error("the screens folder is denied")
	}
}

func TestCodexLaunchAcceptsDirectCodexPath(t *testing.T) {
	cfg := testConfig()
	cfg.CodexCommand = "codex"
	var fr fakeRunner
	fr.fn = func(cmdSpec) (cmdResult, error) { return cmdResult{}, nil }
	p := newCodex(&cfg, t.TempDir(), &fr, found)
	exe, prefix, err := p.launch()
	if err != nil || exe != "/fake/bin/codex" || prefix != nil {
		t.Fatalf("launch = %q %v %v", exe, prefix, err)
	}
}

// toStrings converts a decoded JSON array of strings.
func toStrings(v any) []string {
	var out []string
	for _, s := range v.([]any) {
		out = append(out, s.(string))
	}
	return out
}
