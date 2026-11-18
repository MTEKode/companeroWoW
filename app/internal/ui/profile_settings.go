package ui

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// The player's profile and the answer verifier are edited in their own screen, opened from
// the settings. These are the platform-neutral parts: the values of the screen, their checks
// and the sentences the settings report.

// maxProfileSpec is the longest role text the profile screen accepts.
const maxProfileSpec = 60

// profileForm is the profile screen's values as the player left them.
type profileForm struct {
	Goals    []string
	Spec     string
	Notes    string
	Tone     string
	Hardcore bool
	Verify   bool
}

// formFromProfile fills the screen from a profile and the verifier setting.
func formFromProfile(p config.Profile, verify bool) profileForm {
	n := p.Normalize()
	return profileForm{
		Goals:    append([]string(nil), n.Goals...),
		Spec:     n.Spec,
		Notes:    n.Notes,
		Tone:     n.Tone,
		Hardcore: n.Hardcore,
		Verify:   verify,
	}
}

// profile checks the screen's values and returns the profile they describe. The error is in
// Spanish and names the field. Goals that the settings do not know are dropped.
func (f profileForm) profile() (config.Profile, error) {
	spec := strings.TrimSpace(f.Spec)
	if utf8.RuneCountInString(spec) > maxProfileSpec {
		return config.Profile{}, fmt.Errorf("Rol preferido: como mucho %d caracteres", maxProfileSpec)
	}
	notes := strings.TrimSpace(f.Notes)
	if utf8.RuneCountInString(notes) > config.MaxProfileNotes {
		return config.Profile{}, fmt.Errorf("Estilo de juego: como mucho %d caracteres", config.MaxProfileNotes)
	}
	tone := strings.TrimSpace(f.Tone)
	if tone == "" {
		// No radio checked: the brief tone is the default.
		tone = config.ToneBrief
	}
	if !config.ValidTone(tone) {
		return config.Profile{}, errors.New("Tono de las respuestas: elige breve o detallado")
	}
	goals := make([]string, 0, len(f.Goals))
	for _, g := range f.Goals {
		if config.ValidGoal(g) {
			goals = append(goals, g)
		}
	}
	return config.Profile{Goals: goals, Spec: spec, Notes: notes, Tone: tone, Hardcore: f.Hardcore}.Normalize(), nil
}

// profileChangeText names what changed in the profile and the verifier, for the settings
// line that says what took effect now.
func profileChangeText(old, next config.Profile, oldVerify, nextVerify bool) []string {
	var out []string
	if !old.Equal(next) {
		out = append(out, "perfil del jugador")
	}
	if oldVerify != nextVerify {
		out = append(out, "verificación de respuestas")
	}
	return out
}

// goalsLabels are the sentences of the goals in the order the screen lists them.
func goalsLabels() []string {
	out := make([]string, 0, len(config.AllGoals))
	for _, g := range config.AllGoals {
		out = append(out, config.GoalLabel(g))
	}
	return out
}

// profileSummary is the one-line text the settings screen shows under the profile button.
func profileSummary(p config.Profile) string {
	n := p.Normalize()
	if n.Empty() {
		return "Perfil: sin definir"
	}
	var parts []string
	if len(n.Goals) > 0 {
		labels := make([]string, 0, len(n.Goals))
		for _, g := range n.Goals {
			labels = append(labels, config.GoalLabel(g))
		}
		parts = append(parts, strings.Join(labels, ", "))
	}
	if n.Hardcore {
		parts = append(parts, "hardcore")
	}
	parts = append(parts, "tono "+n.Tone)
	return "Perfil: " + strings.Join(parts, " · ")
}
