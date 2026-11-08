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

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

const (
	defaultMCPPort = 47321
	mcpServerName  = "companero"
	// geminiPromptArg is the -p text; the real question arrives on stdin and is appended to it.
	geminiPromptArg = "Responde a la pregunta del texto recibido por stdin, en español."
)

// geminiExcludedTools are built-in tools the model must never get: shell and file edits.
// Everything else the model can use is either read-only, web search, or an MCP tool.
var geminiExcludedTools = []string{"run_shell_command", "write_file", "replace"}

type gemini struct {
	command  string
	workDir  string
	mcpURL   string
	timeout  time.Duration
	run      runner
	lookPath func(string) (string, error)
}

// NewGemini returns the Gemini CLI provider. It runs headless with cwd = workDir and a
// workspace .gemini/settings.json that registers the companion's MCP server.
func NewGemini(cfg *config.Config, workDir string) Provider {
	return newGemini(cfg, workDir, execRunner{}, resolveCommand)
}

func newGemini(cfg *config.Config, workDir string, r runner, lookPath func(string) (string, error)) *gemini {
	return &gemini{
		command:  cfgString(cfg, func(c *config.Config) string { return c.GeminiCommand }, "gemini"),
		workDir:  workDir,
		mcpURL:   mcpURL(cfg),
		timeout:  aiTimeout(cfg),
		run:      r,
		lookPath: lookPath,
	}
}

func (g *gemini) Name() string { return "gemini" }

func (g *gemini) Available() error {
	if _, err := g.lookPath(g.command); err != nil {
		return fmt.Errorf("CLI gemini no disponible: %w", err)
	}
	return nil
}

func (g *gemini) Ask(ctx context.Context, req Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	exe, err := g.lookPath(g.command)
	if err != nil {
		return Response{}, fmt.Errorf("CLI gemini no disponible: %w", err)
	}
	if err := os.MkdirAll(g.workDir, 0o755); err != nil {
		return Response{}, fmt.Errorf("crear carpeta de trabajo: %w", err)
	}
	if err := writeGeminiSettings(filepath.Join(g.workDir, ".gemini", "settings.json"), g.mcpURL); err != nil {
		return Response{}, err
	}

	var imageLine string
	if len(req.ImagePNG) > 0 {
		path, cleanup, err := writeAttachment(g.workDir, req.ImagePNG)
		if err != nil {
			return Response{}, err
		}
		defer cleanup()
		imageLine = "Captura de pantalla adjunta: @" + path
	}

	args := []string{
		"-p", geminiPromptArg,
		"--output-format", "json",
		"--approval-mode", "default",
		"--skip-trust",
	}
	res, err := g.run.run(ctx, cmdSpec{
		Path:  exe,
		Args:  args,
		Dir:   g.workDir,
		Stdin: buildPrompt(req, imageLine),
	})
	if err != nil {
		return Response{}, fmt.Errorf("gemini: %w", err)
	}
	if res.ExitCode != 0 {
		return Response{}, cliFailure("gemini", res)
	}
	text, err := parseGeminiOutput(res.Stdout)
	if err != nil {
		return Response{}, err
	}
	return Response{Text: text, Provider: g.Name()}, nil
}

// parseGeminiOutput reads the answer from `--output-format json` output: the "response"
// field, or an "error" object. Plain text output is returned as is.
func parseGeminiOutput(stdout []byte) (string, error) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return "", nil
	}
	if trimmed[0] != '{' {
		// Log lines may precede the JSON document; use the last line that opens an object.
		idx := bytes.LastIndex(trimmed, []byte("\n{"))
		if idx < 0 || !json.Valid(trimmed[idx+1:]) {
			return string(trimmed), nil
		}
		trimmed = trimmed[idx+1:]
	}
	var doc struct {
		Response string `json:"response"`
		Error    *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return "", fmt.Errorf("gemini: salida JSON inválida: %w", err)
	}
	if doc.Error != nil {
		msg := doc.Error.Message
		if hint := classifyFailure(doc.Error.Type + " " + msg); hint != "" {
			msg = hint + " (" + msg + ")"
		}
		return "", fmt.Errorf("gemini: %s", msg)
	}
	return strings.TrimSpace(doc.Response), nil
}

// writeGeminiSettings makes sure the workspace settings register the MCP server and
// exclude the shell and file-edit tools. Other keys already in the file are kept.
func writeGeminiSettings(path, mcpURL string) error {
	doc := map[string]any{}
	existing, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return fmt.Errorf("leer %s: %w", path, err)
	case len(bytes.TrimSpace(existing)) > 0:
		if err := json.Unmarshal(existing, &doc); err != nil {
			return fmt.Errorf("parsear %s: %w", path, err)
		}
	}

	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[mcpServerName] = map[string]any{
		"httpUrl":     mcpURL,
		"trust":       true,
		"timeout":     60000,
		"description": "Herramientas del juego (CompaneroWoW)",
	}
	doc["mcpServers"] = servers

	tools, _ := doc["tools"].(map[string]any)
	if tools == nil {
		tools = map[string]any{}
	}
	var exclude []string
	if prev, ok := tools["exclude"].([]any); ok {
		for _, v := range prev {
			if s, ok := v.(string); ok {
				exclude = append(exclude, s)
			}
		}
	}
	for _, name := range geminiExcludedTools {
		if !containsString(exclude, name) {
			exclude = append(exclude, name)
		}
	}
	tools["exclude"] = exclude
	doc["tools"] = tools

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("codificar settings de gemini: %w", err)
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

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// cfgString returns the field of cfg, or def when cfg is nil or the field is blank.
func cfgString(cfg *config.Config, field func(*config.Config) string, def string) string {
	if cfg == nil {
		return def
	}
	if v := strings.TrimSpace(field(cfg)); v != "" {
		return v
	}
	return def
}

// mcpURL is the streamable HTTP endpoint of the companion's MCP server.
func mcpURL(cfg *config.Config) string {
	port := defaultMCPPort
	if cfg != nil && cfg.MCPPort > 0 {
		port = cfg.MCPPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
}
