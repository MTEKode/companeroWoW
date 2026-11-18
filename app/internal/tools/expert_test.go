package tools_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/store"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// gearService is a service over the gear fixture: a level 19 priest with quests in its window.
func gearService(t *testing.T) *gamedata.Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w := bridge.NewSVWatcher("../../testdata/CompaneroWoW_gear.lua", time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll gear fixture: %v", err)
	}
	return gamedata.NewService(nil, w, st)
}

// withProfile sets the profile source for one test and restores it afterwards.
func withProfile(t *testing.T, p config.Profile) {
	t.Helper()
	tools.SetProfileSource(func() config.Profile { return p })
	t.Cleanup(func() { tools.SetProfileSource(nil) })
}

func TestRitmoSubidaAnswersFromTheFixtureCharacter(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	res := call(t, reg, "ritmo_subida", `{"nivel": 40}`)
	if res.IsError {
		t.Fatalf("ritmo_subida failed: %s", textOf(t, res))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res)), &m); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if m["nivel"] != 38.0 {
		t.Errorf("nivel = %v, want 38 from the fixture", m["nivel"])
	}
	if m["available"] != true {
		t.Errorf("available = %v", m["available"])
	}
	// The level argument goes through the same lenient reading as the others.
	res = call(t, reg, "ritmo_subida", `{"level": "40"}`)
	if res.IsError {
		t.Errorf("a numeric text level must be accepted: %s", textOf(t, res))
	}
	if res := call(t, reg, "ritmo_subida", `{"nivel": "alto"}`); !res.IsError {
		t.Error("a level that is not a number must be an error")
	}
}

func TestValorMisionesRanksAndUsesTheProfile(t *testing.T) {
	reg := tools.Build(gearService(t), nil)
	res := call(t, reg, "valor_misiones", `{"ventana": "ambas"}`)
	if res.IsError {
		t.Fatalf("valor_misiones failed: %s", textOf(t, res))
	}
	if !strings.Contains(textOf(t, res), `"criterio": "xp por minuto"`) {
		t.Errorf("default criterion must be XP per minute: %s", textOf(t, res))
	}

	// A player whose goal is gold and not leveling is ranked by gold per minute.
	withProfile(t, config.Profile{Goals: []string{config.GoalGold}, Tone: config.ToneBrief})
	res = call(t, reg, "valor_misiones", `{"ventana": "ambas"}`)
	if !strings.Contains(textOf(t, res), `"criterio": "oro por minuto"`) {
		t.Errorf("a gold profile must rank by gold: %s", textOf(t, res))
	}
	// An explicit criterion wins over the profile.
	res = call(t, reg, "valor_misiones", `{"ventana": "ambas", "criterio": "xp"}`)
	if !strings.Contains(textOf(t, res), `"criterio": "xp por minuto"`) {
		t.Errorf("an explicit criterion must win: %s", textOf(t, res))
	}
	if res := call(t, reg, "valor_misiones", `{"ventana": "nunca"}`); !res.IsError {
		t.Error("an unknown window must be an error")
	}
}

func TestInfoEnemigoArgumentsAndNoTarget(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	// No live target and no name: the answer says what is missing, not an error.
	res := call(t, reg, "info_enemigo", `{}`)
	if res.IsError || !strings.Contains(textOf(t, res), "objetivo") {
		t.Errorf("no target must explain itself: %s", textOf(t, res))
	}
	if res := call(t, reg, "info_enemigo", `{"grupo": 5}`); !res.IsError {
		t.Error("a group of five is outside 1 to 3 and must be an error")
	}
	if res := call(t, reg, "info_enemigo", `{"npc_id": -4}`); !res.IsError {
		t.Error("a negative npc_id must be an error")
	}
	res = call(t, reg, "info_enemigo", `{"nombre": "Enemigo de prueba", "grupo": 2}`)
	if res.IsError {
		t.Fatalf("a named mob must answer: %s", textOf(t, res))
	}
	if !strings.Contains(textOf(t, res), `"grupoAnalizado": 2`) {
		t.Errorf("the group argument must reach the verdict: %s", textOf(t, res))
	}
}

func TestCapturesSeeOnlyCallsMadeWhileOpen(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	caps := tools.NewCaptures()
	reg.AddObserver(caps.Observe)

	before := caps.Begin()
	_ = call(t, reg, "estado_conexion", `{}`)
	first := before.End()
	if len(first) != 1 || first[0].Name != "estado_conexion" {
		t.Fatalf("first capture = %+v", first)
	}
	if again := before.End(); again != nil {
		t.Errorf("a second End must return nothing, got %d calls", len(again))
	}

	// Two captures open at once both see a call; a call after both closed is seen by neither.
	a := caps.Begin()
	b := caps.Begin()
	_ = call(t, reg, "obtener_dinero", `{}`)
	if n := len(a.End()); n != 1 {
		t.Errorf("capture a saw %d calls, want 1", n)
	}
	if n := len(b.End()); n != 1 {
		t.Errorf("capture b saw %d calls, want 1", n)
	}
	_ = call(t, reg, "obtener_dinero", `{}`)
	if n := len(caps.Begin().End()); n != 0 {
		t.Errorf("a capture opened later must not see earlier calls, got %d", n)
	}
}

func TestRegistryObserverAndAddObserverBothRun(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	var set, added int
	reg.SetObserver(func(tools.CallRecord) { set++ })
	reg.AddObserver(func(tools.CallRecord) { added++ })
	_ = call(t, reg, "estado_conexion", `{}`)
	if set != 1 || added != 1 {
		t.Errorf("observers ran %d and %d times, want 1 and 1", set, added)
	}
	_ = context.Background()
}
