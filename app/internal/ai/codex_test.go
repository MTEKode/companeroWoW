package ai

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/mcpserver"
)

// writeLastMessage simulates codex writing its final message to the -o file.
func writeLastMessage(spec cmdSpec, text string) error {
	out := argValue(spec.Args, "-o")
	return os.WriteFile(out, []byte(text), 0o644)
}

func TestCodexBuildsReadOnlyExecWithMCPOverride(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(spec cmdSpec) (cmdResult, error) {
		if err := writeLastMessage(spec, "  Eldoria está al norte.\n"); err != nil {
			t.Fatal(err)
		}
		return cmdResult{}, nil
	}}
	cfg := testConfig()
	p := newCodex(&cfg, workDir, fr, found)

	resp, err := p.Ask(context.Background(), Request{System: "Sistema", Prompt: "¿dónde?"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Text != "Eldoria está al norte." || resp.Provider != "codex" {
		t.Fatalf("resp = %+v", resp)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("runner called %d times", len(fr.calls))
	}
	spec := fr.calls[0]
	workspace := filepath.Join(workDir, "codex")
	if spec.Path != "/fake/bin/codex" || spec.Dir != workspace {
		t.Errorf("Path=%q Dir=%q", spec.Path, spec.Dir)
	}
	if len(spec.Args) == 0 || spec.Args[0] != "exec" {
		t.Fatalf("args = %v, want exec first", spec.Args)
	}
	for _, want := range [][2]string{
		{"-s", "read-only"},
		{"-C", workspace},
		{"--color", "never"},
	} {
		if got := argValue(spec.Args, want[0]); got != want[1] {
			t.Errorf("%s = %q, want %q", want[0], got, want[1])
		}
	}
	if !hasArg(spec.Args, "--skip-git-repo-check") {
		t.Errorf("missing --skip-git-repo-check: %v", spec.Args)
	}
	overrides := map[string]bool{}
	for i := 0; i+1 < len(spec.Args); i++ {
		if spec.Args[i] == "-c" {
			overrides[spec.Args[i+1]] = true
		}
	}
	for _, want := range []string{
		`mcp_servers.companero.url="http://127.0.0.1:47321/mcp"`,
		`mcp_servers.companero.default_tools_approval_mode="approve"`,
		`approval_policy="never"`,
	} {
		if !overrides[want] {
			t.Errorf("missing -c %s in %v", want, spec.Args)
		}
	}
	if argValue(spec.Args, "-o") == "" {
		t.Errorf("missing -o output file: %v", spec.Args)
	}
	if !strings.Contains(spec.Stdin, "Pregunta: ¿dónde?") || !strings.Contains(spec.Stdin, "Sistema") {
		t.Errorf("stdin = %q", spec.Stdin)
	}
	if left := leftovers(t, workspace); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
	// The token travels in the environment, never in the command line.
	if !slices.Contains(spec.Env, "COMPANERO_MCP_TOKEN="+mcpserver.Token()) {
		t.Errorf("env %v lacks the MCP token", spec.Env)
	}
	for _, a := range spec.Args {
		if strings.Contains(a, mcpserver.Token()) {
			t.Errorf("the MCP token appears in the arguments: %v", spec.Args)
		}
	}
	for _, want := range []string{
		`mcp_servers.companero.bearer_token_env_var="COMPANERO_MCP_TOKEN"`,
		`default_permissions="companero"`,
		`permissions.companero.filesystem={":minimal"="read", ":project_roots"={"."="read"}}`,
	} {
		if !overrides[want] {
			t.Errorf("missing -c %s in %v", want, spec.Args)
		}
	}
}

func TestCodexAuthFailureReturnsError(t *testing.T) {
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{ExitCode: 1, Stderr: []byte("Error: not logged in, run codex login")}, nil
	}}
	cfg := testConfig()
	_, err := newCodex(&cfg, t.TempDir(), fr, found).Ask(context.Background(), Request{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "autenticación") {
		t.Fatalf("err = %v, want auth error", err)
	}
}

func TestCodexQuotaFailureReturnsError(t *testing.T) {
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{ExitCode: 1, Stdout: []byte("ERROR: usage limit reached, rate limit 429")}, nil
	}}
	cfg := testConfig()
	_, err := newCodex(&cfg, t.TempDir(), fr, found).Ask(context.Background(), Request{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "cuota") {
		t.Fatalf("err = %v, want quota error", err)
	}
}

func TestCodexMissingLastMessageIsError(t *testing.T) {
	// codex exits 0 but the -o file is gone: that is an error, not an empty answer.
	fr := &fakeRunner{fn: func(spec cmdSpec) (cmdResult, error) {
		_ = os.Remove(argValue(spec.Args, "-o"))
		return cmdResult{}, nil
	}}
	cfg := testConfig()
	_, err := newCodex(&cfg, t.TempDir(), fr, found).Ask(context.Background(), Request{Prompt: "x"})
	if err == nil {
		t.Fatal("expected error when codex did not write the last message")
	}
}

func TestCodexAttachesImageWithFlag(t *testing.T) {
	workDir := t.TempDir()
	var seen string
	fr := &fakeRunner{fn: func(spec cmdSpec) (cmdResult, error) {
		seen = argValue(spec.Args, "-i")
		if data, err := os.ReadFile(seen); err != nil || string(data) != "\x89PNG-codex" {
			t.Errorf("image not readable during call: %v", err)
		}
		return cmdResult{}, writeLastMessage(spec, "visto")
	}}
	cfg := testConfig()
	resp, err := newCodex(&cfg, workDir, fr, found).Ask(context.Background(), Request{Prompt: "¿qué ves?", ImagePNG: []byte("\x89PNG-codex")})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Text != "visto" {
		t.Fatalf("Text = %q", resp.Text)
	}
	if seen == "" {
		t.Fatal("-i flag missing")
	}
	if _, err := os.Stat(seen); err == nil {
		t.Fatalf("image %s not deleted after call", seen)
	}
}

func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		// AGENTS.md is the skill the provider deploys on purpose, not a leftover.
		if e.Name() == "AGENTS.md" {
			continue
		}
		names = append(names, e.Name())
	}
	return names
}
