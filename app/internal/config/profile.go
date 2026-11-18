package config

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// Profile is what the player says about how they play. It is stored in the configuration,
// injected compactly into every prompt ("Perfil del jugador: …") and read by the tools that
// rank things (valor_misiones and the like), so that the answers fit the player's goals.
type Profile struct {
	// Goals are the ways the player wants to play, from the Goal* values below, in the order
	// the player chose them. Empty means no stated goal.
	Goals []string `json:"goals"`
	// Notes is free text about the playstyle, at most MaxProfileNotes characters.
	Notes string `json:"notes"`
	// Spec is the preferred specialization or role ("Mejora, DPS", "Sanador"…). Free text.
	Spec string `json:"spec"`
	// Tone is ToneBrief or ToneDetailed: how long the answers should be.
	Tone string `json:"tone"`
	// Hardcore means the character has a single life: deaths weigh more in the advice.
	Hardcore bool `json:"hardcore"`
}

// The goals a player can pick.
const (
	GoalLevel       = "subir"       // subir de nivel rápido
	GoalPvP         = "jcj"         // jugador contra jugador
	GoalDungeons    = "mazmorras"   // mazmorras y bandas
	GoalProfessions = "profesiones" // subir profesiones
	GoalGold        = "oro"         // ganar oro
	GoalExplore     = "explorar"    // explorar el mundo y las misiones de historia
)

// The tones of the answers.
const (
	ToneBrief    = "breve"
	ToneDetailed = "detallado"
)

// MaxProfileNotes is the longest free text of the profile, in characters.
const MaxProfileNotes = 300

// maxProfileSpec caps the spec text, which is injected in every prompt.
const maxProfileSpec = 60

// goalLabels are the Spanish labels shown in the settings and in the prompt.
var goalLabels = map[string]string{
	GoalLevel:       "subir de nivel rápido",
	GoalPvP:         "JcJ",
	GoalDungeons:    "mazmorras",
	GoalProfessions: "profesiones",
	GoalGold:        "oro",
	GoalExplore:     "explorar",
}

// AllGoals lists the goals in the order the settings show them.
var AllGoals = []string{GoalLevel, GoalPvP, GoalDungeons, GoalProfessions, GoalGold, GoalExplore}

// GoalLabel is the Spanish name of a goal, or the goal itself when unknown.
func GoalLabel(goal string) string {
	if l, ok := goalLabels[goal]; ok {
		return l
	}
	return goal
}

// ValidGoal reports whether goal is one of the AllGoals values.
func ValidGoal(goal string) bool {
	_, ok := goalLabels[goal]
	return ok
}

// ValidTone reports whether t is ToneBrief or ToneDetailed.
func ValidTone(t string) bool {
	return t == ToneBrief || t == ToneDetailed
}

// HasGoal reports whether the player picked goal.
func (p Profile) HasGoal(goal string) bool {
	return slices.Contains(p.Goals, goal)
}

// Empty reports whether the profile says nothing the answers could use.
func (p Profile) Empty() bool {
	return len(p.Goals) == 0 && p.Notes == "" && p.Spec == "" && !p.Hardcore && p.Tone == ToneBrief
}

// Normalize returns the profile cleaned up: unknown goals and duplicates dropped, text trimmed
// and capped, an unknown tone replaced by ToneBrief. It never fails; the settings screen
// reports too-long text before saving.
func (p Profile) Normalize() Profile {
	var goals []string
	for _, g := range p.Goals {
		g = strings.ToLower(strings.TrimSpace(g))
		if ValidGoal(g) && !slices.Contains(goals, g) {
			goals = append(goals, g)
		}
	}
	out := Profile{
		Goals:    goals,
		Notes:    truncateRunes(strings.TrimSpace(p.Notes), MaxProfileNotes),
		Spec:     truncateRunes(strings.TrimSpace(p.Spec), maxProfileSpec),
		Tone:     strings.ToLower(strings.TrimSpace(p.Tone)),
		Hardcore: p.Hardcore,
	}
	if !ValidTone(out.Tone) {
		out.Tone = ToneBrief
	}
	return out
}

// Compact is the one-line description of the profile that every prompt carries, or "" when the
// profile says nothing beyond the defaults. It is short on purpose: it shares the prompt
// with the player context.
func (p Profile) Compact() string {
	n := p.Normalize()
	if n.Empty() {
		return ""
	}
	var parts []string
	if len(n.Goals) > 0 {
		labels := make([]string, 0, len(n.Goals))
		for _, g := range n.Goals {
			labels = append(labels, GoalLabel(g))
		}
		parts = append(parts, "objetivos: "+strings.Join(labels, ", "))
	}
	if n.Spec != "" {
		parts = append(parts, "rol: "+n.Spec)
	}
	if n.Hardcore {
		parts = append(parts, "hardcore: sí (una sola vida)")
	}
	parts = append(parts, "tono: "+n.Tone)
	if n.Notes != "" {
		parts = append(parts, "estilo: "+n.Notes)
	}
	return "Perfil del jugador: " + strings.Join(parts, "; ") + "."
}

// truncateRunes keeps at most n runes of s.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// migrateProfile brings a stored profile into shape (see Normalize) and reports whether it
// changed. A file from before the profile existed has the zero value, which Normalize turns
// into the defaults, so nothing is lost.
func (c *Config) migrateProfile() bool {
	before := c.Profile
	after := before.Normalize()
	if !profilesEqual(before, after) {
		c.Profile = after
		return true
	}
	return false
}

func profilesEqual(a, b Profile) bool {
	return slices.Equal(a.Goals, b.Goals) && a.Notes == b.Notes && a.Spec == b.Spec &&
		a.Tone == b.Tone && a.Hardcore == b.Hardcore
}

// Equal reports whether two profiles say the same thing once normalized.
func (p Profile) Equal(o Profile) bool {
	return profilesEqual(p.Normalize(), o.Normalize())
}
