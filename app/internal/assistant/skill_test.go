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
	"github.com/MTEKode/companeroWoW/app/internal/skill"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

const fixtureLua = "../../testdata/CompaneroWoW.lua"

func TestSystemPromptIsTheSkill(t *testing.T) {
	if SystemPrompt != skill.SystemPrompt() {
		t.Fatal("assistant.SystemPrompt must be derived from the companero skill")
	}
	if strings.Contains(SystemPrompt, "Estado actual") {
		t.Fatal("the per-question state must not be part of the static skill")
	}
}

func TestRequestCarriesSkillAndStateSeparately(t *testing.T) {
	fake := &fakeAsker{resp: ai.Response{Text: "ok", Provider: "codex"}}
	a := newWithAsker(fake, nil, openStore(t), nil)
	if _, err := a.Ask(context.Background(), "¿qué hago?", false); err != nil {
		t.Fatal(err)
	}
	req := fake.reqs[0]
	if req.Skill != SystemPrompt {
		t.Errorf("Skill = %q, want the skill text", req.Skill)
	}
	if !strings.HasPrefix(req.System, "Playbook (misiones):") || !strings.Contains(req.System, "Estado actual") {
		t.Errorf("System = %q, want the playbook of the question first, then the state block", req.System)
	}
}

func TestAnswerSendsNoHistoryAndStoresNothing(t *testing.T) {
	st := openStore(t)
	if _, err := st.AddMessage(store.ChatMsg{Role: "user", Content: "anterior"}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAsker{resp: ai.Response{Text: "respuesta", Provider: "codex"}}
	a := newWithAsker(fake, nil, st, nil)
	if _, err := a.Answer(context.Background(), "pregunta de evaluación", false); err != nil {
		t.Fatal(err)
	}
	if got := len(fake.reqs[0].History); got != 0 {
		t.Errorf("Answer sent %d history turns, want none", got)
	}
	msgs, _ := st.RecentMessages(10)
	if len(msgs) != 1 {
		t.Errorf("Answer stored messages: %+v", msgs)
	}
	if _, err := a.Answer(context.Background(), "   ", false); err == nil {
		t.Error("an empty question must be rejected")
	}
}

func TestHistoryTruncatesLongAnswersOnly(t *testing.T) {
	st := openStore(t)
	long := strings.Repeat("ñ", 1000)
	for _, m := range []store.ChatMsg{
		{Role: "user", Content: long},
		{Role: "assistant", Content: long},
		{Role: "assistant", Content: "corta"},
	} {
		if _, err := st.AddMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	fake := &fakeAsker{resp: ai.Response{Text: "ok", Provider: "codex"}}
	a := newWithAsker(fake, nil, st, nil)
	if _, err := a.Ask(context.Background(), "x", false); err != nil {
		t.Fatal(err)
	}
	h := fake.reqs[0].History
	if len(h) != 3 {
		t.Fatalf("history = %d turns", len(h))
	}
	if h[0].Content != long {
		t.Error("user messages must be sent in full")
	}
	if n := utf8.RuneCountInString(h[1].Content); n != historyAnswerRunes+1 || !strings.HasSuffix(h[1].Content, "…") {
		t.Errorf("long answer = %d runes, want %d with an ellipsis", n, historyAnswerRunes+1)
	}
	if h[2].Content != "corta" {
		t.Errorf("short answer changed: %q", h[2].Content)
	}
}

func TestHistoryTurnsIsTen(t *testing.T) {
	if historyTurns != 6 {
		t.Fatalf("historyTurns = %d, the spec asks for 6", historyTurns)
	}
}

func TestStateBlockHasAtMostEightLines(t *testing.T) {
	st := openStore(t)
	w := bridge.NewSVWatcher(fixtureLua, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll fixture: %v", err)
	}
	g := gamedata.NewService(nil, w, st)
	a := New(nil, g, st, nil)
	block := a.stateBlock()
	if lines := strings.Count(block, "\n") + 1; lines > 8 {
		t.Fatalf("state block has %d lines, limit 8:\n%s", lines, block)
	}
	for _, want := range []string{"Zona: Orgrimmar", "nivel 38", "Misión rastreada: The Warchief's Orders"} {
		if !strings.Contains(block, want) {
			t.Errorf("state block misses %q:\n%s", want, block)
		}
	}
}

func TestProactiveTipUsesSkillAndKeepsMarkLine(t *testing.T) {
	fake := &fakeAsker{resp: ai.Response{
		Text:     "Entrega en Orgrimmar.\nDespués sube a Durotar.\n📍 /compa marca 85 51.3 38.5 Entregar\nSobra esta línea.",
		Provider: "codex",
	}}
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	inCombat := false
	var got []Tip
	p := newTestProactorUnverified(t, fake, clock, &inCombat, &got)
	p.onEvent(event(2))
	clock.advance(debounceWindow + time.Second)
	p.tick(context.Background())

	if len(fake.reqs) != 1 || fake.reqs[0].Skill != SystemPrompt {
		t.Fatalf("tip request must carry the skill: %+v", fake.reqs)
	}
	if len(got) != 1 {
		t.Fatalf("tips = %d", len(got))
	}
	lines := strings.Split(got[0].Text, "\n")
	if len(lines) != maxTipLines || lines[2] != "📍 /compa marca 85 51.3 38.5 Entregar" {
		t.Errorf("tip text = %q", got[0].Text)
	}
}

func TestSkillDeployedFilesMatchAssistantPrompt(t *testing.T) {
	if !strings.Contains(skill.AgentsMarkdown(), SystemPrompt) {
		t.Fatal("AGENTS.md must carry the same instructions the API providers get")
	}
}
