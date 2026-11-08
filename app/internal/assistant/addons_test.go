package assistant

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
)

// fixtureAssistant returns an assistant over the fixture game data and a fake provider.
func fixtureAssistant(t *testing.T) (*Assistant, *fakeAsker) {
	t.Helper()
	st := openStore(t)
	w := bridge.NewSVWatcher(fixtureLua, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll fixture: %v", err)
	}
	fake := &fakeAsker{resp: ai.Response{Text: "Entrena Frost Shock.", Provider: "codex"}}
	return newWithAsker(fake, gamedata.NewService(nil, w, st), st, nil), fake
}

// A level-up tip carries what can be trained now, so it can say what to learn; other tips
// do not carry it.
func TestLevelUpTipCarriesTrainingBlockOnlyOnLevelUp(t *testing.T) {
	a, fake := fixtureAssistant(t)
	p := a.newProactor(func(Tip) {})
	if _, _, err := p.generate(context.Background(), levelUpCode, time.Now()); err != nil {
		t.Fatalf("level-up tip: %v", err)
	}
	system := fake.reqs[0].System
	idx := strings.Index(system, trainingHeader)
	if idx < 0 {
		t.Fatalf("level-up tip lacks the training block:\n%s", system)
	}
	if !strings.Contains(system[idx:], "Frost Shock") || !strings.Contains(system[idx:], "Korla Windtotem") {
		t.Errorf("training block misses the spells or the trainer: %s", system[idx:])
	}
	if n := utf8.RuneCountInString(system[idx:]); n > trainingLimit+1 {
		t.Errorf("training block has %d runes, limit %d", n, trainingLimit)
	}

	if _, _, err := p.generate(context.Background(), 3, time.Now()); err != nil {
		t.Fatalf("quest tip: %v", err)
	}
	if strings.Contains(fake.reqs[1].System, trainingHeader) {
		t.Error("only the level-up tip carries the training block")
	}
}

func TestSessionSummaryWithoutSessionReturnsHint(t *testing.T) {
	a, _ := fixtureAssistant(t)
	text, err := a.SessionSummary()
	if err != nil {
		t.Fatalf("no session is a hint, not an error: %v", err)
	}
	if !strings.Contains(text, "Aún no hay sesiones") {
		t.Errorf("summary = %q", text)
	}
}

func TestSessionSummaryWithoutGameDataIsAnError(t *testing.T) {
	a := newWithAsker(&fakeAsker{}, nil, openStore(t), nil)
	if _, err := a.SessionSummary(); err == nil {
		t.Error("no game data must be an error")
	}
}
