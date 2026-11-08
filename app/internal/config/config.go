// Package config loads and saves the companion's JSON configuration.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// APIProvider is an OpenAI-compatible endpoint (NVIDIA, Z.ai, Alibaba...).
type APIProvider struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
}

// Config is the persisted application configuration.
type Config struct {
	WoWPath       string        `json:"wowPath"`
	Account       string        `json:"account"`
	Hotkey        string        `json:"hotkey"`
	Language      string        `json:"language"`
	ProviderOrder []string      `json:"providerOrder"`
	GeminiCommand string        `json:"geminiCommand"`
	CodexCommand  string        `json:"codexCommand"`
	APIProviders  []APIProvider `json:"apiProviders"`
	MCPPort       int           `json:"mcpPort"`
	Proactive     bool          `json:"proactive"`
	WebSources    []string      `json:"webSources"`
	PixelPollMs   int           `json:"pixelPollMs"`
	AITimeoutSec  int           `json:"aiTimeoutSec"`
}

// Defaults returns a Config populated with the default values.
func Defaults() Config {
	return Config{
		WoWPath:       `C:\Program Files (x86)\World of Warcraft\_forever_`,
		Account:       "",
		Hotkey:        "Ctrl+Shift+Space",
		Language:      "es",
		ProviderOrder: []string{"gemini", "codex", "api"},
		GeminiCommand: "gemini",
		CodexCommand:  "codex",
		APIProviders:  []APIProvider{},
		MCPPort:       47321,
		Proactive:     true,
		WebSources: []string{
			"warcraft.wiki.gg",
			"www.warcrafttavern.com",
			"mobalytics.gg",
			"www.icy-veins.com",
			"forevermeta.me",
			"forevermastery.com",
		},
		PixelPollMs:  250,
		AITimeoutSec: 120,
	}
}

// PixelPoll returns PixelPollMs as a duration (falls back to 250ms when unset).
func (c *Config) PixelPoll() time.Duration {
	if c.PixelPollMs <= 0 {
		return 250 * time.Millisecond
	}
	return time.Duration(c.PixelPollMs) * time.Millisecond
}

// DefaultDir returns %APPDATA%\CompaneroWoW.
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "CompaneroWoW"), nil
}

// Load reads the config at path. A missing file is created with the defaults.
// Fields absent from the file keep their default values. Invalid JSON is an error
// that names the path.
func Load(path string) (*Config, error) {
	c := Defaults()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := c.Save(path); err != nil {
			return nil, fmt.Errorf("write default config %s: %w", path, err)
		}
		return &c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return &c, nil
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &c, nil
}

// Save writes the config as pretty JSON (2-space indent). The write is atomic:
// a temporary file is written and then renamed over path.
func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write config %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace config %s: %w", path, err)
	}
	return nil
}

// SavedVariablesPath returns <WoWPath>\WTF\Account\<Account>\SavedVariables\CompaneroWoW.lua.
// When Account is empty, the account folder that contains that file with the most
// recent modification time is used.
func (c *Config) SavedVariablesPath() (string, error) {
	if c.WoWPath == "" {
		return "", errors.New("config: wowPath is empty")
	}
	account := c.Account
	if account == "" {
		detected, err := detectAccount(c.WoWPath)
		if err != nil {
			return "", err
		}
		account = detected
	}
	return filepath.Join(c.WoWPath, "WTF", "Account", account, "SavedVariables", "CompaneroWoW.lua"), nil
}

// savedVariablesRel is the file path below an account folder.
var savedVariablesRel = filepath.Join("SavedVariables", "CompaneroWoW.lua")

func detectAccount(wowPath string) (string, error) {
	accountsDir := filepath.Join(wowPath, "WTF", "Account")
	entries, err := os.ReadDir(accountsDir)
	if err != nil {
		return "", fmt.Errorf("list accounts in %s: %w", accountsDir, err)
	}
	best := ""
	var bestMod time.Time
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := os.Stat(filepath.Join(accountsDir, e.Name(), savedVariablesRel))
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestMod) {
			best = e.Name()
			bestMod = info.ModTime()
		}
	}
	if best == "" {
		return "", fmt.Errorf("no account under %s contains SavedVariables\\CompaneroWoW.lua (run /compa sync in game)", accountsDir)
	}
	return best, nil
}
