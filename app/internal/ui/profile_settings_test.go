package ui

import (
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

func TestProfileFormRoundTrip(t *testing.T) {
	p := config.Profile{Goals: []string{config.GoalGold, config.GoalPvP}, Spec: "DPS", Notes: "me gusta explorar", Tone: config.ToneDetailed, Hardcore: true}
	f := formFromProfile(p, false)
	back, err := f.profile()
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if !back.Equal(p) {
		t.Errorf("round trip = %+v, want %+v", back, p)
	}
}

func TestProfileFormDropsUnknownGoalsAndSetsTheDefaultTone(t *testing.T) {
	f := profileForm{Goals: []string{"magia", config.GoalLevel}, Tone: "", Spec: "  Tanque  "}
	p, err := f.profile()
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if len(p.Goals) != 1 || p.Goals[0] != config.GoalLevel {
		t.Errorf("goals = %v", p.Goals)
	}
	if p.Tone != config.ToneBrief || p.Spec != "Tanque" {
		t.Errorf("tone %q, spec %q", p.Tone, p.Spec)
	}
}

func TestProfileFormRefusesTooLongTextAndBadTone(t *testing.T) {
	long := strings.Repeat("a", config.MaxProfileNotes+1)
	if _, err := (profileForm{Tone: config.ToneBrief, Notes: long}).profile(); err == nil || !strings.Contains(err.Error(), "300") {
		t.Errorf("notes of %d characters must be refused: %v", len(long), err)
	}
	if _, err := (profileForm{Tone: config.ToneBrief, Spec: strings.Repeat("b", maxProfileSpec+1)}).profile(); err == nil || !strings.Contains(err.Error(), "Rol") {
		t.Errorf("a long role must be refused: %v", err)
	}
	if _, err := (profileForm{Tone: "largo"}).profile(); err == nil || !strings.Contains(err.Error(), "Tono") {
		t.Errorf("an unknown tone must be refused: %v", err)
	}
	// Exactly the limit is accepted (counted in characters, not bytes).
	ok := strings.Repeat("ñ", config.MaxProfileNotes)
	if _, err := (profileForm{Tone: config.ToneBrief, Notes: ok}).profile(); err != nil {
		t.Errorf("%d characters must pass: %v", config.MaxProfileNotes, err)
	}
}

func TestProfileChangeTextNamesWhatChanged(t *testing.T) {
	a := config.Profile{Tone: config.ToneBrief}
	b := config.Profile{Tone: config.ToneDetailed}
	if got := profileChangeText(a, a, true, true); len(got) != 0 {
		t.Errorf("no change = %v", got)
	}
	got := profileChangeText(a, b, true, false)
	if len(got) != 2 || got[0] != "perfil del jugador" || got[1] != "verificación de respuestas" {
		t.Errorf("changes = %v", got)
	}
}

func TestSettingsApplyCarriesProfileAndVerifier(t *testing.T) {
	base := config.Defaults()
	base.Profile = config.Profile{Goals: []string{config.GoalLevel}, Tone: config.ToneBrief}
	form := formFromConfig(base)
	if !form.VerifyAnswers || len(form.Profile.Goals) != 1 {
		t.Fatalf("form does not carry the config: %+v", form)
	}
	form.Profile = config.Profile{Goals: []string{config.GoalDungeons}, Tone: config.ToneDetailed, Hardcore: true}
	form.VerifyAnswers = false
	form.Hotkey = base.Hotkey
	form.Opacity = "90"
	form.FontSize = "10"
	form.ProviderOrder = []string{"antigravity"}
	form.WoWPath = base.WoWPath
	form.TipsMode = config.TipsBandeja
	next, err := form.apply(base)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !next.Profile.HasGoal(config.GoalDungeons) || !next.Profile.Hardcore || next.VerifyAnswers {
		t.Errorf("applied = %+v / verify %v", next.Profile, next.VerifyAnswers)
	}
	live, _ := settingsChanges(base, next)
	joined := strings.Join(live, ", ")
	if !strings.Contains(joined, "perfil del jugador") || !strings.Contains(joined, "verificación de respuestas") {
		t.Errorf("live changes = %v", live)
	}
}

func TestProfileSummaryAndGoalLabels(t *testing.T) {
	if got := profileSummary(config.Profile{}); got != "Perfil: sin definir" {
		t.Errorf("empty summary = %q", got)
	}
	got := profileSummary(config.Profile{Goals: []string{config.GoalGold}, Hardcore: true, Tone: config.ToneDetailed})
	if !strings.Contains(got, "oro") || !strings.Contains(got, "hardcore") || !strings.Contains(got, "tono detallado") {
		t.Errorf("summary = %q", got)
	}
	if labels := goalsLabels(); len(labels) != len(config.AllGoals) || labels[0] != config.GoalLabel(config.GoalLevel) {
		t.Errorf("goal labels = %v", labels)
	}
}
