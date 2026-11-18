package config

import (
	"regexp"
	"slices"
	"strings"
)

// DefaultAntigravityModels is the order in which Antigravity (agy) models are tried: the
// stronger Gemini 4 Hydrogen first, Gemini 3.8 Flash when its quota runs out.
var DefaultAntigravityModels = []string{"gemini-4-hydrogen-high", "gemini-3.8-flash-medium"}

// legacyAntigravityModel is the single model older versions defaulted to. A file that still
// names it gets the new default order instead of it.
const legacyAntigravityModel = "gemini-3.8-flash-low"

// DefaultQuotaCooldownMin is how long a model stays out of use after its quota ran out.
const DefaultQuotaCooldownMin = 30

// Limits of the cooldown setting, in minutes.
const (
	MinQuotaCooldownMin = 1
	MaxQuotaCooldownMin = 720
)

// agyModelRE accepts model slugs such as "gemini-4-hydrogen-high": a flag value never holds
// spaces or quotes.
var agyModelRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,80}$`)

// ValidAntigravityModel reports whether s can be passed to agy as --model.
func ValidAntigravityModel(s string) bool {
	return agyModelRE.MatchString(s)
}

// NormalizeAntigravityModels trims, validates and dedupes a model order, keeping the first
// occurrence. Invalid slugs are dropped. An empty result becomes the default order.
func NormalizeAntigravityModels(models []string) []string {
	var out []string
	for _, m := range models {
		m = strings.TrimSpace(m)
		if !ValidAntigravityModel(m) || slices.Contains(out, m) {
			continue
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return append([]string(nil), DefaultAntigravityModels...)
	}
	return out
}

// migrateAntigravityModels brings the model settings of a file up to date and reports whether
// anything changed. hasList says whether the file has an antigravityModels key. Without it, the
// file predates the ordered list: its single antigravityModel (when it is not the old default)
// becomes the first entry, followed by the default order. The legacy field is then cleared.
func (c *Config) migrateAntigravityModels(hasList bool) bool {
	before := append([]string(nil), c.AntigravityModels...)
	beforeLegacy, beforeCooldown := c.AntigravityModel, c.AntigravityQuotaCooldownMin
	if !hasList {
		var list []string
		if m := strings.TrimSpace(c.AntigravityModel); m != "" && m != legacyAntigravityModel {
			list = append(list, m)
		}
		c.AntigravityModels = append(list, DefaultAntigravityModels...)
	}
	c.AntigravityModels = NormalizeAntigravityModels(c.AntigravityModels)
	c.AntigravityModel = ""
	if c.AntigravityQuotaCooldownMin <= 0 {
		c.AntigravityQuotaCooldownMin = DefaultQuotaCooldownMin
	}
	return !slices.Equal(before, c.AntigravityModels) || beforeLegacy != "" || beforeCooldown != c.AntigravityQuotaCooldownMin || !hasList
}
