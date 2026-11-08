package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/store"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// fakeAsker records the requests and answers with a fixed response or error.
type fakeAsker struct {
	reqs []ai.Request
	resp ai.Response
	err  error
}

func (f *fakeAsker) Ask(_ context.Context, req ai.Request) (ai.Response, error) {
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return ai.Response{}, f.err
	}
	return f.resp, nil
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestSystemPromptKeyPhrases(t *testing.T) {
	for _, want := range []string{
		"World of Warcraft: Forever",
		"castellano",
		"buscar_conocimiento",
		"capturar_pantalla",
		"obtener_ubicacion",
		"Compañero",
	} {
		if !strings.Contains(SystemPrompt, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
}

func TestAskPersistsTurnsAndSendsHistoryInOrder(t *testing.T) {
	st := openStore(t)
	for _, m := range []store.ChatMsg{
		{Role: "user", Content: "primera pregunta"},
		{Role: "assistant", Content: "primera respuesta", Provider: "gemini"},
		{Role: "user", Content: "segunda pregunta"},
	} {
		if _, err := st.AddMessage(m); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	fake := &fakeAsker{resp: ai.Response{Text: "respuesta nueva", Provider: "codex"}}
	a := newWithAsker(fake, nil, st, nil)

	resp, err := a.Ask(context.Background(), "  ¿dónde estoy?  ", false)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Text != "respuesta nueva" || resp.Provider != "codex" {
		t.Fatalf("response = %+v", resp)
	}
	if len(fake.reqs) != 1 {
		t.Fatalf("chain called %d times, want 1", len(fake.reqs))
	}
	req := fake.reqs[0]
	if req.Prompt != "¿dónde estoy?" {
		t.Errorf("prompt = %q (should be trimmed)", req.Prompt)
	}
	wantHist := []ai.Message{
		{Role: "user", Content: "primera pregunta"},
		{Role: "assistant", Content: "primera respuesta"},
		{Role: "user", Content: "segunda pregunta"},
	}
	if len(req.History) != len(wantHist) {
		t.Fatalf("history = %+v", req.History)
	}
	for i := range wantHist {
		if req.History[i] != wantHist[i] {
			t.Errorf("history[%d] = %+v, want %+v", i, req.History[i], wantHist[i])
		}
	}
	if !strings.Contains(req.System, "Estado actual") {
		t.Errorf("system prompt lacks the state block")
	}

	msgs, err := st.RecentMessages(2)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("persisted tail = %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "¿dónde estoy?" {
		t.Errorf("user message = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "respuesta nueva" || msgs[1].Provider != "codex" {
		t.Errorf("assistant message = %+v", msgs[1])
	}
}

func TestAskLimitsHistoryToTenMessages(t *testing.T) {
	st := openStore(t)
	for i := 0; i < 25; i++ {
		if _, err := st.AddMessage(store.ChatMsg{Role: "user", Content: string(rune('a' + i%26))}); err != nil {
			t.Fatal(err)
		}
	}
	fake := &fakeAsker{resp: ai.Response{Text: "ok", Provider: "gemini"}}
	a := newWithAsker(fake, nil, st, nil)
	if _, err := a.Ask(context.Background(), "x", false); err != nil {
		t.Fatal(err)
	}
	if got := len(fake.reqs[0].History); got != historyTurns {
		t.Fatalf("history length = %d, want %d", got, historyTurns)
	}
}

func TestAskFailureStoresNothing(t *testing.T) {
	st := openStore(t)
	fake := &fakeAsker{err: errors.New("sin proveedores")}
	a := newWithAsker(fake, nil, st, nil)
	if _, err := a.Ask(context.Background(), "hola", false); err == nil {
		t.Fatal("expected an error")
	}
	msgs, _ := a.History(10)
	if len(msgs) != 0 {
		t.Fatalf("stored %d messages after a failed ask", len(msgs))
	}
}

func TestAskRejectsEmptyQuestion(t *testing.T) {
	a := newWithAsker(&fakeAsker{}, nil, nil, nil)
	if _, err := a.Ask(context.Background(), "   ", false); err == nil {
		t.Fatal("expected an error for an empty question")
	}
}

func TestAskWithScreenshotAttachesPNG(t *testing.T) {
	orig := CaptureFunc
	t.Cleanup(func() { CaptureFunc = orig })
	CaptureFunc = func() ([]byte, error) { return []byte("png-bytes"), nil }

	fake := &fakeAsker{resp: ai.Response{Text: "ok", Provider: "gemini"}}
	a := newWithAsker(fake, nil, nil, nil)
	if _, err := a.Ask(context.Background(), "mira la pantalla", true); err != nil {
		t.Fatal(err)
	}
	if string(fake.reqs[0].ImagePNG) != "png-bytes" {
		t.Errorf("screenshot not attached")
	}
}

func TestAskWithFailedScreenshotStillAsks(t *testing.T) {
	orig := CaptureFunc
	t.Cleanup(func() { CaptureFunc = orig })
	CaptureFunc = func() ([]byte, error) { return nil, errors.New("WoW no está abierto") }

	fake := &fakeAsker{resp: ai.Response{Text: "ok", Provider: "gemini"}}
	a := newWithAsker(fake, nil, nil, nil)
	if _, err := a.Ask(context.Background(), "¿qué ves?", true); err != nil {
		t.Fatal(err)
	}
	req := fake.reqs[0]
	if req.ImagePNG != nil {
		t.Error("no image expected")
	}
	if !strings.Contains(req.Prompt, "¿qué ves?") || !strings.Contains(req.Prompt, "WoW no está abierto") {
		t.Errorf("prompt = %q", req.Prompt)
	}
}

func TestClearHistoryAndHistory(t *testing.T) {
	st := openStore(t)
	a := newWithAsker(&fakeAsker{resp: ai.Response{Text: "ok", Provider: "gemini"}}, nil, st, nil)
	if _, err := a.Ask(context.Background(), "uno", false); err != nil {
		t.Fatal(err)
	}
	msgs, err := a.History(10)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("History = %d msgs, err %v", len(msgs), err)
	}
	if err := a.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	msgs, _ = a.History(10)
	if len(msgs) != 0 {
		t.Fatalf("history not cleared: %d", len(msgs))
	}
}

func TestNilChainReportsNoProviders(t *testing.T) {
	a := New(nil, nil, nil, nil)
	if _, err := a.Ask(context.Background(), "hola", false); err == nil || !strings.Contains(err.Error(), "proveedores") {
		t.Fatalf("err = %v", err)
	}
}

func TestStateBlockWithoutGameData(t *testing.T) {
	a := New(nil, nil, nil, nil)
	if got := a.stateBlock(); !strings.Contains(got, "sin datos del juego") {
		t.Errorf("state = %q", got)
	}
}

func TestDisplayCoordAndLimitSentences(t *testing.T) {
	if got := displayCoord(0.4567); got < 45.66 || got > 45.67 {
		t.Errorf("displayCoord(0.4567) = %v", got)
	}
	if got := displayCoord(32.5); got != 32.5 {
		t.Errorf("displayCoord(32.5) = %v", got)
	}
	in := "Primera frase.\n\nSegunda frase!\nTercera?\nCuarta frase."
	if got := limitLines(in, 3); got != "Primera frase.\nSegunda frase!\nTercera?" {
		t.Errorf("limitLines = %q", got)
	}
	marca := "Vale 1.5 de oro.\n📍 /compa marca 45.2 67.1 Entregar"
	if got := limitLines(marca, 3); got != marca {
		t.Errorf("command line must stay intact: %q", got)
	}
}

func TestToolAdapterMapsDefsAndResults(t *testing.T) {
	reg := tools.NewRegistry()
	schema := map[string]any{"type": "object"}
	reg.Add(tools.Def{
		Name:        "eco",
		Description: "Repite el texto",
		InputSchema: schema,
		Handler: func(_ context.Context, args json.RawMessage) (tools.Result, error) {
			return tools.Result{Content: []tools.Content{
				{Type: "text", Text: "uno"},
				{Type: "image", Data: []byte("img")},
				{Type: "text", Text: "dos"},
			}}, nil
		},
	})
	reg.Add(tools.Def{
		Name:        "falla",
		Description: "Siempre falla",
		Handler: func(_ context.Context, _ json.RawMessage) (tools.Result, error) {
			return tools.Result{IsError: true, Content: []tools.Content{{Type: "text", Text: "mal"}}}, nil
		},
	})
	ad := ToolAdapter(reg)

	specs := ad.Specs()
	if len(specs) != 2 || specs[0].Name != "eco" || specs[0].Description != "Repite el texto" {
		t.Fatalf("specs = %+v", specs)
	}
	if specs[0].Parameters["type"] != "object" {
		t.Errorf("parameters not mapped: %+v", specs[0].Parameters)
	}

	out, err := ad.Execute(context.Background(), "eco", `{"a":1}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Text != "uno\ndos" || string(out.ImagePNG) != "img" || out.IsError {
		t.Errorf("output = %+v", out)
	}

	out, err = ad.Execute(context.Background(), "falla", "")
	if err != nil || !out.IsError || out.Text != "mal" {
		t.Errorf("failing tool output = %+v, err %v", out, err)
	}

	if _, err := ad.Execute(context.Background(), "no-existe", "{}"); err == nil {
		t.Error("unknown tool should be an error")
	}
}

// ---- proactive tips --------------------------------------------------------

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestProactor(t *testing.T, fake *fakeAsker, clock *fakeClock, inCombat *bool, got *[]Tip) *proactor {
	t.Helper()
	st := openStore(t)
	a := newWithAsker(fake, nil, st, nil)
	a.now = clock.now
	p := a.newProactor(func(tip Tip) { *got = append(*got, tip) })
	p.combat = func() bool { return *inCombat }
	return p
}

func event(code uint8) bridge.LiveState {
	return bridge.LiveState{LastEventCode: code}
}

func TestProactiveDebounceCoalescesAndPicksPriority(t *testing.T) {
	fake := &fakeAsker{resp: ai.Response{Text: "Ve a Dalaran. Entrena hechizos.", Provider: "gemini"}}
	clock := &fakeClock{t: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)}
	combat := false
	var tips []Tip
	p := newTestProactor(t, fake, clock, &combat, &tips)

	p.onEvent(event(4)) // zone changed
	clock.advance(3 * time.Second)
	p.onEvent(event(1)) // level up
	p.onEvent(event(1)) // duplicate in the same batch
	clock.advance(4 * time.Second)
	p.tick(context.Background())
	if len(fake.reqs) != 0 {
		t.Fatalf("asked before the 8 s window ended")
	}
	clock.advance(1 * time.Second)
	p.tick(context.Background())
	if len(fake.reqs) != 1 {
		t.Fatalf("asks = %d, want 1 coalesced ask", len(fake.reqs))
	}
	if !strings.Contains(fake.reqs[0].Prompt, "subido de nivel") && !strings.Contains(fake.reqs[0].Prompt, "subir de nivel") {
		t.Errorf("prompt for level up expected, got %q", fake.reqs[0].Prompt)
	}
	if len(tips) != 1 {
		t.Fatalf("tips = %d", len(tips))
	}
	if tips[0].EventCode != 1 || tips[0].Title != "¡Subiste de nivel!" {
		t.Errorf("tip = %+v", tips[0])
	}
	if tips[0].Text != "Ve a Dalaran. Entrena hechizos." {
		t.Errorf("tip text = %q", tips[0].Text)
	}
}

func TestProactiveMaxOneTipPerMinute(t *testing.T) {
	fake := &fakeAsker{resp: ai.Response{Text: "Consejo.", Provider: "gemini"}}
	clock := &fakeClock{t: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)}
	combat := false
	var tips []Tip
	p := newTestProactor(t, fake, clock, &combat, &tips)

	p.onEvent(event(3))
	clock.advance(8 * time.Second)
	p.tick(context.Background())
	if len(tips) != 1 {
		t.Fatalf("first tip missing")
	}

	clock.advance(10 * time.Second)
	p.onEvent(event(6))
	clock.advance(10 * time.Second) // 28 s after the first tip: too soon
	p.tick(context.Background())
	if len(tips) != 1 {
		t.Fatalf("second tip sent within 60 s")
	}

	clock.advance(40 * time.Second) // 68 s after the first tip
	p.tick(context.Background())
	if len(tips) != 2 {
		t.Fatalf("queued tip not sent after the gap; tips = %d", len(tips))
	}
}

func TestProactiveQueuesDuringCombat(t *testing.T) {
	fake := &fakeAsker{resp: ai.Response{Text: "Entrega la misión.", Provider: "codex"}}
	clock := &fakeClock{t: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)}
	combat := true
	var tips []Tip
	p := newTestProactor(t, fake, clock, &combat, &tips)

	p.onEvent(event(2))
	clock.advance(10 * time.Second)
	p.tick(context.Background())
	clock.advance(time.Minute)
	p.tick(context.Background())
	if len(fake.reqs) != 0 || len(tips) != 0 {
		t.Fatalf("tip produced during combat")
	}

	combat = false
	p.tick(context.Background())
	if len(tips) != 1 || tips[0].EventCode != 2 {
		t.Fatalf("queued tip not delivered after combat: %+v", tips)
	}
}

func TestProactiveIgnoresCode7AndDropsStale(t *testing.T) {
	fake := &fakeAsker{resp: ai.Response{Text: "Algo.", Provider: "gemini"}}
	clock := &fakeClock{t: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)}
	combat := true
	var tips []Tip
	p := newTestProactor(t, fake, clock, &combat, &tips)

	p.onEvent(event(7))
	if p.hasWindow {
		t.Fatal("code 7 opened a window")
	}

	p.onEvent(event(5))
	clock.advance(6 * time.Minute) // older than maxPendingAge
	combat = false
	p.tick(context.Background())
	if len(fake.reqs) != 0 || len(tips) != 0 {
		t.Fatalf("stale event produced a tip")
	}
}

func TestProactiveTipIsStoredInHistory(t *testing.T) {
	fake := &fakeAsker{resp: ai.Response{Text: "Entrena cocina.", Provider: "gemini"}}
	clock := &fakeClock{t: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)}
	combat := false
	var tips []Tip
	st := openStore(t)
	a := newWithAsker(fake, nil, st, nil)
	a.now = clock.now
	p := a.newProactor(func(tip Tip) { tips = append(tips, tip) })
	p.combat = func() bool { return combat }

	p.onEvent(event(5))
	clock.advance(8 * time.Second)
	p.tick(context.Background())

	msgs, err := st.RecentMessages(1)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("messages = %v, err %v", msgs, err)
	}
	if msgs[0].Role != "assistant" || msgs[0].Provider != "proactivo:gemini" {
		t.Errorf("stored tip = %+v", msgs[0])
	}
	if !strings.Contains(msgs[0].Content, "Entrena cocina.") {
		t.Errorf("stored content = %q", msgs[0].Content)
	}
}

func TestToolAdapterNilRegistry(t *testing.T) {
	ad := ToolAdapter(nil)
	if len(ad.Specs()) != 0 {
		t.Error("nil registry should offer no specs")
	}
}
