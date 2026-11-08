package ai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// agySettings reads the session settings that the last Ask wrote into the workspace.
func agySettings(t *testing.T, workDir string) (allow, deny []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(workDir, antigravityWorkspaceDir, antigravitySettingsFile))
	if err != nil {
		t.Fatalf("settings not written: %v", err)
	}
	var doc struct {
		ToolPermission string `json:"toolPermission"`
		Permissions    struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("settings invalid: %v\n%s", err, raw)
	}
	if doc.ToolPermission != "request-review" {
		t.Errorf("toolPermission = %q", doc.ToolPermission)
	}
	return doc.Permissions.Allow, doc.Permissions.Deny
}

func TestAntigravitySettingsAllowScreensAndDenySecretsOnly(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{Stdout: []byte(envelopeSuccess)}, nil
	}}
	cfg := testConfig()
	p := newAntigravity(&cfg, workDir, fr, found)
	if _, err := p.Ask(context.Background(), Request{Prompt: "x"}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	allow, deny := agySettings(t, workDir)

	screens := filepath.Join(workDir, "screens")
	if want := []string{"mcp(companero/*)", "read_file(" + screens + ")"}; !reflect.DeepEqual(allow, want) {
		t.Errorf("allow = %v, want %v", allow, want)
	}
	for _, rule := range []string{"command(*)", "unsandboxed(*)", "write_file(*)", "read_url(*)", "execute_url(*)"} {
		if !containsString(deny, rule) {
			t.Errorf("deny %v misses %q", deny, rule)
		}
	}
	// A deny on the whole read namespace or on the data folder would also deny the screens
	// folder, because deny overrides allow in Antigravity.
	for _, rule := range deny {
		if rule == "read_file(*)" || rule == "read_file("+workDir+")" || rule == "read_file("+filepath.Dir(workDir)+")" {
			t.Errorf("deny rule %q would also block the screens folder", rule)
		}
	}
	for _, secret := range []string{filepath.Join(workDir, "companero.db"), filepath.Join(workDir, "config.json")} {
		if !containsString(deny, "read_file("+secret+")") {
			t.Errorf("deny %v misses the secret %s", deny, secret)
		}
	}
	for _, env := range config.DotenvPaths() {
		if !containsString(deny, "read_file("+filepath.Clean(env)+")") {
			t.Errorf("deny misses the .env file %s", env)
		}
	}
}

func TestAntigravityWorkspaceHoldsNoSecrets(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{Stdout: []byte(envelopeSuccess)}, nil
	}}
	cfg := testConfig()
	p := newAntigravity(&cfg, workDir, fr, found)
	if _, err := p.Ask(context.Background(), Request{Prompt: "x"}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, antigravityWorkspaceDir, ".env")); err == nil {
		t.Error("the workspace must not hold a .env")
	}
	if _, err := os.Stat(filepath.Join(workDir, "companero.db")); err == nil {
		t.Error("the database must not be written by the provider")
	}
}

func TestAntigravityModelAndEffortFlags(t *testing.T) {
	cases := []struct {
		name       string
		model      string
		effort     string
		wantModel  string
		wantEffort string
	}{
		{"defaults", "gemini-3.8-flash-low", "low", "gemini-3.8-flash-low", "low"},
		{"agy default model, no effort", "", "", "", ""},
		{"explicit model and effort", "gemini-4-argon-high", "high", "gemini-4-argon-high", "high"},
		{"model with spaces is dropped", "bad model", "medium", "", "medium"},
		{"model with shell characters is dropped", "x;rm -rf", "", "", ""},
		{"unknown effort is dropped", "gemini-3.8-flash-low", "turbo", "gemini-3.8-flash-low", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			workDir := t.TempDir()
			fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
				return cmdResult{Stdout: []byte(envelopeSuccess)}, nil
			}}
			cfg := testConfig()
			cfg.AntigravityModel = c.model
			cfg.AntigravityEffort = c.effort
			p := newAntigravity(&cfg, workDir, fr, found)
			if _, err := p.Ask(context.Background(), Request{Prompt: "x"}); err != nil {
				t.Fatalf("Ask: %v", err)
			}
			args := fr.calls[0].Args
			if got := argValue(args, "--model"); got != c.wantModel {
				t.Errorf("--model = %q, want %q (args %v)", got, c.wantModel, args)
			}
			if got := argValue(args, "--effort"); got != c.wantEffort {
				t.Errorf("--effort = %q, want %q (args %v)", got, c.wantEffort, args)
			}
			if !hasArg(args, "-p") || args[len(args)-2] != "-p" {
				t.Errorf("the prompt must come last: %v", args)
			}
		})
	}
}

func TestCodexModelAndEffortFlags(t *testing.T) {
	run := func(t *testing.T, model, effort string) []string {
		t.Helper()
		workDir := t.TempDir()
		fr := &fakeRunner{fn: func(spec cmdSpec) (cmdResult, error) {
			return cmdResult{}, writeLastMessage(spec, "ok")
		}}
		cfg := testConfig()
		cfg.CodexModel = model
		cfg.CodexEffort = effort
		p := newCodex(&cfg, workDir, fr, found)
		if _, err := p.Ask(context.Background(), Request{Prompt: "x"}); err != nil {
			t.Fatalf("Ask: %v", err)
		}
		return fr.calls[0].Args
	}

	args := run(t, "", "")
	if hasArg(args, "-m") {
		t.Errorf("no model configured, -m must be absent: %v", args)
	}
	if containsString(args, `model_reasoning_effort="low"`) {
		t.Errorf("no effort configured, the override must be absent: %v", args)
	}

	args = run(t, "gpt-5.5", "low")
	if got := argValue(args, "-m"); got != "gpt-5.5" {
		t.Errorf("-m = %q, want gpt-5.5", got)
	}
	if !containsString(args, `model_reasoning_effort="low"`) {
		t.Errorf("effort override missing: %v", args)
	}
	for i, a := range args {
		if a == "-c" && i+1 < len(args) && strings.HasPrefix(args[i+1], "model_reasoning_effort=") {
			if args[i+1] != `model_reasoning_effort="low"` {
				t.Errorf("effort value = %q", args[i+1])
			}
		}
	}

	args = run(t, "", "Turbo!")
	if containsString(args, `model_reasoning_effort="Turbo!"`) || hasArgPrefix(args, "model_reasoning_effort=") {
		t.Errorf("invalid effort must not reach codex: %v", args)
	}
}

func hasArgPrefix(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}
