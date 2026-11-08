package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
)

func TestPromptCarriesPreConsultedContext(t *testing.T) {
	st := openStore(t)
	w := bridge.NewSVWatcher(fixtureLua, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll fixture: %v", err)
	}
	g := gamedata.NewService(nil, w, st)
	fake := &fakeAsker{resp: ai.Response{Text: "ok", Provider: "codex"}}
	a := newWithAsker(fake, g, st, nil)
	if _, err := a.Answer(context.Background(), "¿qué hago ahora?", false); err != nil {
		t.Fatal(err)
	}
	system := fake.reqs[0].System
	idx := strings.Index(system, contextHeader)
	if idx < 0 {
		t.Fatalf("system lacks the pre-consulted context header:\n%s", system)
	}
	block := system[idx:]
	if n := utf8.RuneCountInString(block); n > contextLimit {
		t.Fatalf("context block has %d runes, limit %d", n, contextLimit)
	}
	payload := strings.TrimSpace(strings.TrimPrefix(block, contextHeader))
	var doc map[string]any
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		t.Fatalf("context block is not JSON: %v\n%s", err, payload)
	}
	if _, ok := doc["misionRastreada"]; !ok {
		t.Errorf("tracked quest missing from the context: %v", doc)
	}
	if _, ok := doc["ubicacion"]; !ok {
		t.Errorf("location missing from the context: %v", doc)
	}
}

func TestNoPreConsultWithoutGameData(t *testing.T) {
	fake := &fakeAsker{resp: ai.Response{Text: "ok", Provider: "codex"}}
	a := newWithAsker(fake, nil, openStore(t), nil)
	if _, err := a.Answer(context.Background(), "hola", false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fake.reqs[0].System, contextHeader) {
		t.Error("no game data, so no context block is expected")
	}
}

func TestCompactContextNeverExceedsLimit(t *testing.T) {
	big := strings.Repeat("texto largo ", 200)
	v := map[string]any{
		"ubicacion": map[string]any{"zona": "Orgrimmar", "mapId": 1411},
		"misionRastreada": map[string]any{
			"titulo":    "Misión",
			"objetivos": []any{map[string]any{"texto": big}},
		},
		"avisos":      []any{big},
		"profesiones": []any{map[string]any{"nombre": big}},
	}
	for _, limit := range []int{40, 200, 600, 1000} {
		got := compactContext(v, limit)
		if utf8.RuneCountInString(got) > limit {
			t.Errorf("limit %d: got %d runes", limit, utf8.RuneCountInString(got))
		}
	}
	got := compactContext(v, 600)
	if strings.Contains(got, "objetivos") {
		t.Errorf("objectives must go before the rest of the quest: %s", got)
	}
	if !strings.Contains(got, "Orgrimmar") {
		t.Errorf("location must be kept: %s", got)
	}
}

func TestCompactContextEmptyWithoutMap(t *testing.T) {
	if got := compactContext(nil, 1200); got != "" {
		t.Errorf("nil context = %q", got)
	}
	if got := compactContext(map[string]any{"otra": 1}, 1200); got != "" {
		t.Errorf("context without known parts = %q", got)
	}
}

func TestStateBlockWithoutDataDoesNotAskForSyncOrWindowMode(t *testing.T) {
	a := New(nil, gamedata.NewService(nil, nil, nil), nil, nil)
	block := a.stateBlock()
	for _, noise := range []string{"sync", "ventana"} {
		if strings.Contains(block, noise) {
			t.Errorf("state block must not nag about %q: %s", noise, block)
		}
	}
}
