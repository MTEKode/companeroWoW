// Package skill holds the "companero" skill: the Spanish instructions that shape every
// answer. SKILL.md is the single source of truth. This package renders it for each AI
// provider: AGENTS.md for Codex, the workspace skill and AGENTS.md for Antigravity, the
// system prompt for the API providers and the condensed MCP instructions.
package skill

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Name is the skill name: the folder name for Antigravity and the front-matter name.
const Name = "companero"

//go:embed SKILL.md
var source string

const (
	mcpStart = "<!-- mcp:start -->"
	mcpEnd   = "<!-- mcp:end -->"
	// frontMatterDelim opens and closes the YAML front matter.
	frontMatterDelim = "---"
)

// Markdown returns the full SKILL.md, front matter included, with Unix line endings.
func Markdown() string {
	return strings.ReplaceAll(source, "\r\n", "\n")
}

// Body returns the instructions without the front matter and the MCP-only block: the
// text that the model reads on every question.
func Body() string {
	md := Markdown()
	if strings.HasPrefix(md, frontMatterDelim+"\n") {
		if end := strings.Index(md[len(frontMatterDelim)+1:], "\n"+frontMatterDelim+"\n"); end >= 0 {
			md = md[len(frontMatterDelim)+1+end+len("\n"+frontMatterDelim+"\n"):]
		}
	}
	if s := strings.Index(md, mcpStart); s >= 0 {
		if e := strings.Index(md, mcpEnd); e > s {
			md = md[:s] + md[e+len(mcpEnd):]
		}
	}
	return strings.TrimSpace(md)
}

// Description returns the front-matter description, which Antigravity uses to decide
// when the skill applies.
func Description() string {
	for _, line := range strings.Split(Markdown(), "\n") {
		if v, ok := strings.CutPrefix(line, "description:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// MCPInstructions returns the condensed playbook that the MCP server sends to clients as
// its instructions. It is the block between the mcp markers of SKILL.md.
func MCPInstructions() string {
	md := Markdown()
	s := strings.Index(md, mcpStart)
	e := strings.Index(md, mcpEnd)
	if s < 0 || e <= s {
		return Body()
	}
	return strings.TrimSpace(md[s+len(mcpStart) : e])
}

// Version is a short fingerprint of the skill text, useful in logs and reports.
func Version() string {
	sum := sha256.Sum256([]byte(Markdown()))
	return hex.EncodeToString(sum[:])[:12]
}

// SystemPrompt is the skill as an API system prompt. The per-question state is added by
// the caller.
func SystemPrompt() string {
	return Body()
}

// AgentsMarkdown is the content of AGENTS.md: the skill body, which Codex reads from the
// working directory and Antigravity reads as an always-on rules file.
func AgentsMarkdown() string {
	return Body() + "\n"
}

// WorkspaceSkillPath is where Antigravity looks for workspace skills under workDir.
func WorkspaceSkillPath(workDir string) string {
	return filepath.Join(workDir, ".agents", "skills", Name, "SKILL.md")
}

// AgentsPath is the AGENTS.md file under workDir.
func AgentsPath(workDir string) string {
	return filepath.Join(workDir, "AGENTS.md")
}

// DeployCodex writes AGENTS.md into workDir, the codex workspace (the folder Codex runs in). It reports
// whether the file was written.
func DeployCodex(workDir string) (bool, error) {
	return writeIfChanged(AgentsPath(workDir), []byte(AgentsMarkdown()))
}

// DeployAntigravity writes the workspace skill and AGENTS.md under workDir. The skill is
// activated by its description; AGENTS.md is the always-on fallback.
func DeployAntigravity(workDir string) (bool, error) {
	changed, err := writeIfChanged(WorkspaceSkillPath(workDir), []byte(Markdown()))
	if err != nil {
		return false, err
	}
	ch2, err := writeIfChanged(AgentsPath(workDir), []byte(AgentsMarkdown()))
	if err != nil {
		return changed, err
	}
	return changed || ch2, nil
}

// Workspace folders under the data directory: agy runs in agyWorkspace and codex in
// codexWorkspace. Each provider deploys its own copy of the skill there.
const (
	agyWorkspace   = "agy"
	codexWorkspace = "codex"
	// staleAgentsDir is the root-level .agents folder of earlier versions. Antigravity now runs
	// in agyWorkspace, so this folder is removed on startup.
	staleAgentsDir = ".agents"
)

// DeployAll deploys the skill to every file-based provider: the agy workspace (the workspace
// skill and AGENTS.md) and the codex workspace (AGENTS.md). It also removes the stale root
// .agents folder of earlier versions, and nothing else of the data directory. It is called on
// startup; providers call the deploy functions again before each question, so a deleted or
// outdated file is repaired.
func DeployAll(workDir string) error {
	if err := os.RemoveAll(filepath.Join(workDir, staleAgentsDir)); err != nil {
		return fmt.Errorf("quitar %s: %w", filepath.Join(workDir, staleAgentsDir), err)
	}
	if _, err := DeployCodex(filepath.Join(workDir, codexWorkspace)); err != nil {
		return err
	}
	_, err := DeployAntigravity(filepath.Join(workDir, agyWorkspace))
	return err
}

// Installed reports whether the files written by DeployAll exist in both workspaces and match
// the current skill text.
func Installed(workDir string) bool {
	agy := filepath.Join(workDir, agyWorkspace)
	codex := filepath.Join(workDir, codexWorkspace)
	return upToDate(WorkspaceSkillPath(agy), Markdown()) &&
		upToDate(AgentsPath(agy), AgentsMarkdown()) &&
		upToDate(AgentsPath(codex), AgentsMarkdown())
}

func upToDate(path, want string) bool {
	got, err := os.ReadFile(path)
	return err == nil && bytes.Equal(got, []byte(want))
}

// writeIfChanged writes data to path unless the file already holds exactly that content.
func writeIfChanged(path string, data []byte) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("crear %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, fmt.Errorf("escribir %s: %w", path, err)
	}
	return true, nil
}
