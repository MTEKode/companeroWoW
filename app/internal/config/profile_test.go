package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeDropsUnknownAndDuplicateGoals(t *testing.T) {
	p := Profile{Goals: []string{" Oro ", "magia", "subir", "oro", ""}, Tone: "largo"}.Normalize()
	want := []string{GoalGold, GoalLevel}
	if strings.Join(p.Goals, ",") != strings.Join(want, ",") {
		t.Fatalf("goals = %v, want %v", p.Goals, want)
	}
	if p.Tone != ToneBrief {
		t.Errorf("tone = %q, want %q for an unknown tone", p.Tone, ToneBrief)
	}
}

func TestNormalizeCapsNotesInRunes(t *testing.T) {
	long := strings.Repeat("ñ", MaxProfileNotes+50)
	p := Profile{Notes: long}.Normalize()
	if n := utf8.RuneCountInString(p.Notes); n != MaxProfileNotes {
		t.Fatalf("notes runes = %d, want %d", n, MaxProfileNotes)
	}
}

func TestCompactIsEmptyForDefaultProfile(t *testing.T) {
	if got := (Profile{Tone: ToneBrief}).Compact(); got != "" {
		t.Errorf("default profile compact = %q, want empty", got)
	}
	if got := (Profile{}).Compact(); got != "" {
		t.Errorf("zero profile compact = %q, want empty", got)
	}
}

func TestCompactNamesGoalsAndTone(t *testing.T) {
	p := Profile{Goals: []string{GoalLevel, GoalGold}, Spec: "DPS", Tone: ToneDetailed, Hardcore: true, Notes: "me gusta explorar"}
	got := p.Compact()
	for _, want := range []string{"Perfil del jugador:", "subir de nivel rápido, oro", "rol: DPS", "hardcore: sí", "tono: detallado", "estilo: me gusta explorar"} {
		if !strings.Contains(got, want) {
			t.Errorf("compact %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "\n") {
		t.Errorf("compact must be one line: %q", got)
	}
}

func TestLoadWritesNewKeysOnceAndKeepsChoices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// A file from before the verifier and the profile: no verifyAnswers, no profile.
	old := `{"tipsMode":"bandeja","wftAdded":true,"hideInCombat":true,"webSources":["x"]}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.VerifyAnswers {
		t.Error("verifyAnswers must default to true for an old file")
	}
	if c.Profile.Tone != ToneBrief || len(c.Profile.Goals) != 0 {
		t.Errorf("profile default = %+v", c.Profile)
	}
	saved, _ := os.ReadFile(path)
	for _, key := range []string{`"verifyAnswers": true`, `"profile": {`} {
		if !strings.Contains(string(saved), key) {
			t.Errorf("migrated file lacks %s:\n%s", key, saved)
		}
	}

	// The player turns the verifier off and sets a profile: a later load keeps both.
	c.VerifyAnswers = false
	c.Profile = Profile{Goals: []string{GoalProfessions}, Tone: ToneDetailed, Hardcore: true}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.VerifyAnswers {
		t.Error("an explicit verifyAnswers=false must stick")
	}
	if !again.Profile.HasGoal(GoalProfessions) || again.Profile.Tone != ToneDetailed || !again.Profile.Hardcore {
		t.Errorf("saved profile lost: %+v", again.Profile)
	}
}

func TestMigrateNormalizesStoredProfile(t *testing.T) {
	c := Defaults()
	c.Profile = Profile{Goals: []string{"nope", GoalDungeons}, Tone: "x"}
	if !c.migrate() {
		t.Fatal("a profile with junk should report a change")
	}
	if len(c.Profile.Goals) != 1 || c.Profile.Goals[0] != GoalDungeons || c.Profile.Tone != ToneBrief {
		t.Errorf("normalized profile = %+v", c.Profile)
	}
	if c.migrate() {
		t.Error("a second migration must not change anything")
	}
}
