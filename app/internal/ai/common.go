package ai

import (
	"fmt"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

const (
	defaultMCPPort = 47321
	// mcpServerName is the key under which the companion's MCP server is registered in
	// every CLI's configuration.
	mcpServerName = "companero"
)

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

// effortOf returns the configured reasoning effort, trimmed. Without a configuration the
// default is "low"; with one, an empty field means "no effort", not the default.
func effortOf(cfg *config.Config, field func(*config.Config) string) string {
	if cfg == nil {
		return "low"
	}
	return strings.TrimSpace(field(cfg))
}

// mcpURL is the streamable HTTP endpoint of the companion's MCP server.
func mcpURL(cfg *config.Config) string {
	port := defaultMCPPort
	if cfg != nil && cfg.MCPPort > 0 {
		port = cfg.MCPPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
}
