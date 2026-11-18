package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBodyStripsFrontMatterAndMCPBlock(t *testing.T) {
	body := Body()
	if strings.HasPrefix(body, "---") {
		t.Fatalf("front matter still present: %q", body[:20])
	}
	if strings.Contains(body, mcpStart) || strings.Contains(body, "Herramientas de WoW: Forever. Responde siempre") {
		t.Fatalf("MCP-only block leaked into the body")
	}
	if !strings.HasPrefix(body, "# Compañero WoW: Forever") {
		t.Fatalf("body should start with the title, got %q", body[:30])
	}
	for _, want := range []string{"Responde SIEMPRE en castellano", "contexto_jugador", "/compa marca", "Playbooks"} {
		if !strings.Contains(body, want) {
			t.Errorf("body misses %q", want)
		}
	}
}

func TestSystemPromptIsDerivedFromSkill(t *testing.T) {
	if SystemPrompt() != Body() {
		t.Fatal("system prompt must be the skill body, not a copy of it")
	}
}

func TestDescriptionAndFrontMatter(t *testing.T) {
	if !strings.HasPrefix(Markdown(), "---\nname: companero\n") {
		t.Fatalf("front matter missing name: %q", Markdown()[:40])
	}
	if d := Description(); d == "" || !strings.Contains(d, "castellano") {
		t.Fatalf("description = %q", d)
	}
}

func TestMCPInstructionsAreCondensed(t *testing.T) {
	mcp := MCPInstructions()
	if !strings.HasPrefix(mcp, "Herramientas de WoW: Forever.") {
		t.Fatalf("mcp instructions = %q", mcp)
	}
	if len(mcp) >= len(Body()) {
		t.Fatalf("mcp instructions (%d bytes) should be shorter than the body (%d)", len(mcp), len(Body()))
	}
	if strings.Contains(mcp, "<!--") {
		t.Fatal("markers must not reach the MCP instructions")
	}
}

func TestVersionIsStableAndShort(t *testing.T) {
	if v := Version(); len(v) != 12 || v != Version() {
		t.Fatalf("version = %q", v)
	}
}

func TestDeployCodexWritesAgentsMD(t *testing.T) {
	dir := t.TempDir()
	changed, err := DeployCodex(dir)
	if err != nil || !changed {
		t.Fatalf("first deploy: changed=%v err=%v", changed, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != AgentsMarkdown() {
		t.Fatal("AGENTS.md does not match the skill body")
	}
	if changed, _ := DeployCodex(dir); changed {
		t.Error("second deploy with the same text must not rewrite the file")
	}
}

func TestDeployAntigravityWritesWorkspaceSkill(t *testing.T) {
	dir := t.TempDir()
	if _, err := DeployAntigravity(dir); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, ".agents", "skills", "companero", "SKILL.md")
	if WorkspaceSkillPath(dir) != want {
		t.Fatalf("skill path = %s, want %s", WorkspaceSkillPath(dir), want)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("skill file missing: %v", err)
	}
	if !strings.HasPrefix(string(data), "---\n") || !strings.Contains(string(data), "description:") {
		t.Fatal("workspace skill must keep its front matter (name, description)")
	}
	if _, err := os.Stat(AgentsPath(dir)); err != nil {
		t.Fatalf("AGENTS.md missing: %v", err)
	}
}

func TestDeployAllRepairsDeletedAndOutdatedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := DeployAll(dir); err != nil {
		t.Fatal(err)
	}
	if !Installed(dir) {
		t.Fatal("freshly deployed files must count as installed")
	}
	if err := os.WriteFile(AgentsPath(filepath.Join(dir, "agy")), []byte("viejo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Installed(dir) {
		t.Fatal("an outdated AGENTS.md must not count as installed")
	}
	if err := DeployAll(dir); err != nil {
		t.Fatal(err)
	}
	if !Installed(dir) {
		t.Fatal("DeployAll must repair an outdated file")
	}
	if err := os.Remove(WorkspaceSkillPath(filepath.Join(dir, "agy"))); err != nil {
		t.Fatal(err)
	}
	if err := DeployAll(dir); err != nil || !Installed(dir) {
		t.Fatalf("DeployAll must recreate a deleted skill: %v", err)
	}
}

func TestDeployFailsWhenWorkDirIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DeployAll(file); err == nil {
		t.Fatal("deploying under a regular file must fail with an error, not panic")
	}
}

func TestDeployAllWritesOnlyIntoWorkspacesAndRemovesStaleAgents(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, ".agents", "skills", "companero")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "SKILL.md"), []byte("viejo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DeployAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents")); !os.IsNotExist(err) {
		t.Error("the stale root .agents folder must be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("no AGENTS.md may be written to the data folder root")
	}
	if _, err := os.Stat(WorkspaceSkillPath(filepath.Join(dir, "agy"))); err != nil {
		t.Errorf("agy workspace skill missing: %v", err)
	}
	if _, err := os.Stat(AgentsPath(filepath.Join(dir, "codex"))); err != nil {
		t.Errorf("codex workspace AGENTS.md missing: %v", err)
	}
	if _, err := os.Stat(AgentsPath(filepath.Join(dir, "agy"))); err != nil {
		t.Errorf("agy workspace AGENTS.md missing: %v", err)
	}
}

// The expert tools, their playbooks and the profile rules are part of the instructions the
// providers read: each must be in the skill.
func TestExpertToolsPlaybooksAndProfileAreInTheSkill(t *testing.T) {
	md := Markdown()
	for _, want := range []string{
		"`ritmo_subida`", "`valor_misiones`", "`info_enemigo`",
		"(x1)", "(x2)", "(x3)",
		"## Perfil del jugador", "Perfil del jugador: …",
		"## Estimaciones",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("SKILL.md lacks %q", want)
		}
	}
}
