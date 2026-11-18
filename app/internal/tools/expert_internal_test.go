package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/store"
	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

func TestTheoryKillTimeNeedsGameDataAndTalentSource(t *testing.T) {
	if theoryKillTime(nil, websearch.New(nil, nil)) != nil {
		t.Error("no game data, no hook")
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g := gamedata.NewService(nil, bridge.NewSVWatcher("../../testdata/CompaneroWoW.lua", time.Hour), st)
	if theoryKillTime(g, nil) != nil {
		t.Error("no talent source, no hook")
	}
}

func TestTheoryKillTimeAnswersNothingWithoutASheetAndDoesNotRetryAtOnce(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	w := bridge.NewSVWatcher("../../testdata/CompaneroWoW.lua", time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatal(err)
	}
	g := gamedata.NewService(nil, w, st)
	hook := theoryKillTime(g, websearch.New(nil, nil))
	if hook == nil {
		t.Fatal("a service with a talent source must get a hook")
	}
	if sec, ok := hook(10); ok || sec != 0 {
		t.Errorf("the fixture has no sheet: hook(10) = %v, %v", sec, ok)
	}
	// The failure is remembered: a second ask within the retry time answers at once, without a
	// second simulation. The answer is the same either way.
	if _, ok := hook(10); ok {
		t.Error("a remembered failure must still answer no")
	}
	if _, ok := hook(0); ok {
		t.Error("level 0 has no answer")
	}
	if _, ok := hook(theoryHookMaxLevel + 1); ok {
		t.Error("levels above the model's range have no answer")
	}
}

func TestProfileNotesLabelTheObjectiveAndKeepTheNumbers(t *testing.T) {
	r := NewRegistry()
	r.Add(Def{Name: "optimizar_talentos", InputSchema: schema(nil), Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
		return jsonResult(map[string]any{"disponible": true, "resultado": map[string]any{"nivel": 38}})
	}})
	r.Add(Def{Name: "otra", InputSchema: schema(nil), Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
		return textResult("sin cambios"), nil
	}})
	withProfileNotes(r)
	defer SetProfileSource(nil)
	SetProfileSource(func() config.Profile { return config.Profile{Hardcore: true, Tone: config.ToneBrief} })
	res, err := r.Call(context.Background(), "optimizar_talentos", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].Text
	for _, want := range []string{`"objetivo": "muertes por hora"`, `"hardcore": true`, `fraccionMuertes`, `"nivel": 38`} {
		if !strings.Contains(text, want) {
			t.Errorf("answer lacks %q:\n%s", want, text)
		}
	}

	SetProfileSource(nil)
	res, _ = r.Call(context.Background(), "optimizar_talentos", json.RawMessage(`{}`))
	if strings.Contains(res.Content[0].Text, "fraccionMuertes") {
		t.Error("a player who is not hardcore gets no survival note")
	}

	// Other tools pass through untouched.
	res, _ = r.Call(context.Background(), "otra", json.RawMessage(`{}`))
	if res.Content[0].Text != "sin cambios" {
		t.Errorf("other tool changed: %q", res.Content[0].Text)
	}

	// An error of the engine is passed through as it is.
	r2 := NewRegistry()
	r2.Add(Def{Name: "optimizar_talentos", InputSchema: schema(nil), Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
		return Result{}, errors.New("sin hoja")
	}})
	withProfileNotes(r2)
	res, _ = r2.Call(context.Background(), "optimizar_talentos", json.RawMessage(`{}`))
	if !res.IsError || !strings.Contains(res.Content[0].Text, "sin hoja") {
		t.Errorf("engine error must pass through: %+v", res)
	}
}

func TestGearProfileNoteCarriesRoleAndHardcore(t *testing.T) {
	r := NewRegistry()
	r.Add(Def{Name: "analizar_equipo", InputSchema: schema(nil), Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
		return jsonResult(map[string]any{"ranuras": []any{}})
	}})
	withProfileNotes(r)
	defer SetProfileSource(nil)
	SetProfileSource(func() config.Profile { return config.Profile{Spec: "Tanque", Hardcore: true, Tone: config.ToneBrief} })
	res, _ := r.Call(context.Background(), "analizar_equipo", json.RawMessage(`{}`))
	text := res.Content[0].Text
	for _, want := range []string{`"rol": "Tanque"`, `"hardcore": true`, "aguante"} {
		if !strings.Contains(text, want) {
			t.Errorf("gear answer lacks %q:\n%s", want, text)
		}
	}
}
