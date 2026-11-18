package assistant

import "github.com/MTEKode/companeroWoW/app/internal/config"

// The player's profile reaches every prompt as one line ("Perfil del jugador: …"), with the
// length the tone asks for. The tools that rank things read the same profile through
// tools.SetProfileSource (the assistant's Profile method).

// SetProfile replaces the player's profile. The next question and the next tip carry it; the
// settings screen calls it when the player saves.
func (a *Assistant) SetProfile(p config.Profile) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prof = p.Normalize()
}

// Profile returns the player's profile as the assistant holds it.
func (a *Assistant) Profile() config.Profile {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.prof
}

// profileBlock is the profile line of the prompt, or "" when the profile says nothing.
func (a *Assistant) profileBlock() string {
	p := a.Profile()
	line := p.Compact()
	if line == "" {
		return ""
	}
	if p.Tone == config.ToneDetailed {
		return line + " Responde con el detalle que pide el perfil: hasta 8 líneas, con los pasos."
	}
	return line + " Responde breve: como mucho 5 líneas."
}

// SetAnswerSettings replaces the player's profile and the answer verifier setting together; the
// settings screen calls it when the player saves them.
func (a *Assistant) SetAnswerSettings(p config.Profile, verify bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prof = p.Normalize()
	a.verify = verify
}
