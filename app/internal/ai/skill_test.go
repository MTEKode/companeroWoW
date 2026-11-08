package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/skill"
)

// skillText is a fragment of the companero skill that must reach the model one way or another.
const skillText = "Responde SIEMPRE en castellano"

func TestCodexReadsSkillFromAgentsFileAndKeepsItOutOfPrompt(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(spec cmdSpec) (cmdResult, error) {
		return cmdResult{}, writeLastMessage(spec, "Ve a Orgrimmar (51.2, 38.7).")
	}}
	cfg := testConfig()
	p := newCodex(&cfg, workDir, fr, found)

	req := Request{Skill: skill.SystemPrompt(), System: "Estado actual:\n- Zona: Orgrimmar", Prompt: "¿qué hago?"}
	if _, err := p.Ask(context.Background(), req); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	stdin := fr.calls[0].Stdin
	if strings.Contains(stdin, skillText) {
		t.Error("the skill must not be repeated in the prompt when AGENTS.md carries it")
	}
	if !strings.Contains(stdin, "Estado actual") {
		t.Error("the per-question state must still travel in the prompt")
	}
	data, err := os.ReadFile(filepath.Join(workDir, "AGENTS.md"))
	if err != nil || !strings.Contains(string(data), skillText) {
		t.Fatalf("AGENTS.md missing the skill: %v", err)
	}
}

func TestCodexSkillInlineWhenAgentsFileCannotBeWritten(t *testing.T) {
	// AGENTS.md held by a folder makes the deploy fail: the skill then travels inside the
	// prompt, so the answer still follows the rules.
	workDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workDir, "AGENTS.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	fr := &fakeRunner{fn: func(spec cmdSpec) (cmdResult, error) {
		return cmdResult{}, writeLastMessage(spec, "Respuesta.")
	}}
	cfg := testConfig()
	p := newCodex(&cfg, workDir, fr, found)
	req := Request{Skill: skill.SystemPrompt(), System: "Estado actual:\n- Sin datos", Prompt: "hola"}
	_, _ = p.Ask(context.Background(), req)
	if len(fr.calls) == 0 {
		t.Fatal("the CLI must still be called")
	}
	if !strings.Contains(fr.calls[0].Stdin, skillText) {
		t.Error("with no AGENTS.md the skill must be inline in the prompt")
	}
}

func TestAntigravityDeploysWorkspaceSkillAndNamesIt(t *testing.T) {
	workDir := t.TempDir()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{Stdout: []byte(envelopeSuccess)}, nil
	}}
	cfg := testConfig()
	p := newAntigravity(&cfg, workDir, fr, found)
	req := Request{Skill: skill.SystemPrompt(), System: "Estado actual:\n- Sin datos", Prompt: "hola"}
	if _, err := p.Ask(context.Background(), req); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if _, err := os.Stat(skill.WorkspaceSkillPath(workDir)); err != nil {
		t.Fatalf("workspace skill not deployed: %v", err)
	}
	prompt := argValue(fr.calls[0].Args, "-p")
	if !strings.Contains(prompt, `Aplica la skill "companero"`) {
		t.Errorf("agy must be asked to apply the skill by name:\n%s", prompt)
	}
	if strings.Contains(prompt, skillText) {
		t.Error("the skill text must not be repeated in the prompt when the workspace carries it")
	}
}

func TestAPIProviderSendsSkillAsSystemPrompt(t *testing.T) {
	req := Request{Skill: "SKILL-TEXT", System: "Estado actual:\n- Zona: Orgrimmar"}
	got := systemText(req)
	if got != "SKILL-TEXT\n\nEstado actual:\n- Zona: Orgrimmar" {
		t.Errorf("systemText = %q", got)
	}
	if systemText(Request{System: "solo estado"}) != "solo estado" {
		t.Error("without a skill the state alone is the system text")
	}
}
