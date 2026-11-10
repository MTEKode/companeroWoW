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
	// TipsMode is how proactive tips reach the player: TipsPanel, TipsBandeja or TipsOff.
	TipsMode string `json:"tipsMode"`
	// TipsSound plays the Windows notification sound with the tray balloon of a tip.
	TipsSound bool `json:"tipsSound"`
	// Proactive is true unless TipsMode is off. It mirrors TipsMode for older versions that
	// read this key; TipsMode is the setting that counts.
	Proactive    bool     `json:"proactive"`
	WebSources   []string `json:"webSources"`
	PixelPollMs  int      `json:"pixelPollMs"`
	AITimeoutSec int      `json:"aiTimeoutSec"`
	// HideInCombat hides the panel while the character is in combat and shows it again
	// two seconds after combat ends.
	HideInCombat bool `json:"hideInCombat"`
	// StartWithWoW starts the app minimized to the tray at Windows logon (HKCU Run key).
	StartWithWoW bool `json:"startWithWoW"`
	// Opacity is the panel opacity in percent, from MinOpacity to 100.
	Opacity int `json:"opacity"`
	// FontSize is the panel's text size in points, from MinFontSize to MaxFontSize.
	FontSize float64 `json:"fontSize"`
}

// The ways proactive tips reach the player (TipsMode).
const (
	// TipsPanel opens the panel without taking the focus when a tip arrives (the old behaviour).
	TipsPanel = "panel"
	// TipsBandeja keeps the panel closed: a tray balloon and a badge on the tray icon announce
	// the tip, and the panel shows it when it is opened. It is the default.
	TipsBandeja = "bandeja"
	// TipsOff produces no proactive tips.
	TipsOff = "off"
)

// ValidTipsMode reports whether s is one of the TipsMode values.
func ValidTipsMode(s string) bool {
	return s == TipsPanel || s == TipsBandeja || s == TipsOff
}

// Limits of the appearance settings.
const (
	MinOpacity  = 60
	MaxOpacity  = 100
	MinFontSize = 9.0
	MaxFontSize = 14.0
)

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
		TipsMode:          TipsBandeja,
		TipsSound:         false,
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
		HideInCombat: true,
		StartWithWoW: false,
		Opacity:      90,
		FontSize:     10,
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
	c, _, err := LoadInfo(path)
	return c, err
}

// LoadInfo is Load that also reports whether the file was just created, which is how
// the first-run wizard knows it should open.
func LoadInfo(path string) (c *Config, created bool, err error) {
	d := Defaults()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := d.Save(path); err != nil {
			return nil, false, fmt.Errorf("write default config %s: %w", path, err)
		}
		return &d, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read config %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return &d, false, nil
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, false, fmt.Errorf("parse config %s: %w", path, err)
	}
	// A file from before the appearance and combat settings lacks their keys; it is
	// rewritten once so that the keys show up (and can be edited) in config.json.
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(data, &raw)
	_, hasKey := raw["hideInCombat"]
	// A file from before tipsMode existed takes its mode from proactive (see syncTips).
	if _, hasTips := raw["tipsMode"]; !hasTips {
		d.TipsMode = ""
		hasKey = false
	}
	if d.migrate() || !hasKey {
		if err := d.Save(path); err != nil {
			return nil, false, fmt.Errorf("save migrated config %s: %w", path, err)
		}
	}
	return &d, false, nil
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
	if c.clampAppearance() {
		changed = true
	}
	if c.syncTips() {
		changed = true
	}
	return changed
}

// syncTips makes TipsMode a valid mode and Proactive agree with it. When TipsMode is missing
// or unknown, it comes from Proactive: false means off, anything else bandeja. It reports
// whether a value changed.
func (c *Config) syncTips() bool {
	beforeMode, beforeProactive := c.TipsMode, c.Proactive
	if !ValidTipsMode(c.TipsMode) {
		if c.Proactive {
			c.TipsMode = TipsBandeja
		} else {
			c.TipsMode = TipsOff
		}
	}
	c.Proactive = c.TipsMode != TipsOff
	return beforeMode != c.TipsMode || beforeProactive != c.Proactive
}

// SyncTips is syncTips for values set by the settings screen.
func (c *Config) SyncTips() { c.syncTips() }

// clampAppearance brings opacity and font size into their ranges. A zero value (a file
// written before these fields existed, or an explicit 0) gets the default. It reports
// whether a value changed.
func (c *Config) clampAppearance() bool {
	d := Defaults()
	before := *c
	if c.Opacity == 0 {
		c.Opacity = d.Opacity
	}
	c.Opacity = min(max(c.Opacity, MinOpacity), MaxOpacity)
	if c.FontSize == 0 {
		c.FontSize = d.FontSize
	}
	c.FontSize = min(max(c.FontSize, MinFontSize), MaxFontSize)
	return before.Opacity != c.Opacity || before.FontSize != c.FontSize
}

// ClampAppearance is clampAppearance for values set by the settings screen.
func (c *Config) ClampAppearance() { c.clampAppearance() }

// Save writes the config as pretty JSON (2-space indent). The write is atomic:
// a temporary file is written and then renamed over path.
func (c *Config) Save(path string) error {
	out := *c
	out.syncTips()
	data, err := json.MarshalIndent(out, "", "  ")
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

// ErrNoSavedVariables means WoW's account folders exist but none holds the addon's data
// yet. It is cleared by running /compa sync in game.
var ErrNoSavedVariables = errors.New("ningún personaje tiene datos del addon (SavedVariables/CompaneroWoW.lua)")

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
		return "", fmt.Errorf("%w under %s", ErrNoSavedVariables, accountsDir)
	}
	return best, nil
}
