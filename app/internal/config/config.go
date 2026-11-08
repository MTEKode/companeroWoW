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
	"slices"
	"strings"
	"time"
)

// APIProvider is an OpenAI-compatible endpoint (NVIDIA, Z.ai...).
// The key comes from APIKey (literal), else from the environment variable APIKeyEnv,
// else from the first .env file that defines it (see ResolveKey).
type APIProvider struct {
	Name      string `json:"name"`
	BaseURL   string `json:"baseUrl"`
	APIKey    string `json:"apiKey"`
	APIKeyEnv string `json:"apiKeyEnv"`
	Model     string `json:"model"`
}

// defaultNVIDIAProvider is the API provider used when the config lists none.
var defaultNVIDIAProvider = APIProvider{
	Name:      "nvidia",
	BaseURL:   "https://integrate.api.nvidia.com/v1",
	APIKeyEnv: "NVIDIA_KEY",
	Model:     "meta/llama-4-maverick-17b-128e-instruct",
}

// Config is the persisted application configuration.
type Config struct {
	WoWPath            string   `json:"wowPath"`
	Account            string   `json:"account"`
	Hotkey             string   `json:"hotkey"`
	Language           string   `json:"language"`
	ProviderOrder      []string `json:"providerOrder"`
	AntigravityCommand string   `json:"antigravityCommand"`
	// AntigravityModel is passed as --model to agy when set; empty keeps agy's default.
	AntigravityModel string `json:"antigravityModel"`
	// AntigravityEffort is passed as --effort to agy when set (low, medium, high, xhigh, max).
	AntigravityEffort string `json:"antigravityEffort"`
	CodexCommand      string `json:"codexCommand"`
	// CodexModel is passed as -m to codex when set; empty keeps codex's default.
	CodexModel string `json:"codexModel"`
	// CodexEffort is passed as model_reasoning_effort to codex when set.
	CodexEffort  string        `json:"codexEffort"`
	APIProviders []APIProvider `json:"apiProviders"`
	MCPPort      int           `json:"mcpPort"`
	Proactive    bool          `json:"proactive"`
	WebSources   []string      `json:"webSources"`
	PixelPollMs  int           `json:"pixelPollMs"`
	AITimeoutSec int           `json:"aiTimeoutSec"`
}

// Defaults returns a Config populated with the default values.
func Defaults() Config {
	return Config{
		WoWPath:            `C:\Program Files (x86)\World of Warcraft\_forever_`,
		Account:            "",
		Hotkey:             "Ctrl+Shift+Space",
		Language:           "es",
		ProviderOrder:      []string{"antigravity", "codex", "api"},
		AntigravityCommand: "agy",
		// gemini-3.8-flash-low is the fastest model that `agy models` lists; the answers
		// are short and the tools do the work, so speed matters more than depth.
		AntigravityModel:  "gemini-3.8-flash-low",
		AntigravityEffort: "low",
		CodexCommand:      "codex",
		CodexEffort:       "low",
		APIProviders:      []APIProvider{defaultNVIDIAProvider},
		MCPPort:           47321,
		Proactive:         true,
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

// ScreensDir is the folder under the data directory where screenshots are saved, so that
// the Antigravity sessions can read them with read_file.
func ScreensDir(dir string) string {
	return filepath.Join(dir, "screens")
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
	if c.migrate() {
		if err := c.Save(path); err != nil {
			return nil, fmt.Errorf("save migrated config %s: %w", path, err)
		}
	}
	return &c, nil
}

// migrate upgrades a config written by an earlier version and reports whether it changed
// anything. Gemini CLI is gone (Google ended its personal-account login), so "gemini" in
// ProviderOrder becomes "antigravity" (only once). An empty apiProviders list gets the
// NVIDIA default.
func (c *Config) migrate() bool {
	changed := false
	var order []string
	for _, name := range c.ProviderOrder {
		if strings.EqualFold(strings.TrimSpace(name), "gemini") {
			name = "antigravity"
			changed = true
		}
		if name == "antigravity" && slices.Contains(order, "antigravity") {
			changed = true
			continue
		}
		order = append(order, name)
	}
	if changed {
		c.ProviderOrder = order
	}
	if len(c.APIProviders) == 0 {
		c.APIProviders = []APIProvider{defaultNVIDIAProvider}
		changed = true
	}
	return changed
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
