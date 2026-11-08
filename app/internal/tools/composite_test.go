package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

func TestContextoJugadorAnswersSituationInOneCall(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	res := call(t, reg, "contexto_jugador", "")
	if res.IsError {
		t.Fatalf("IsError: %s", textOf(t, res))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res)), &m); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	for _, key := range []string{"ubicacion", "personaje", "misionRastreada", "dinero", "profesiones", "datos"} {
		if _, ok := m[key]; !ok {
			t.Errorf("contexto_jugador lacks %q", key)
		}
	}
	mr := m["misionRastreada"].(map[string]any)
	step := mr["siguientePaso"].(map[string]any)
	if step["marca"] != "/compa marca 85 31.0 55.0 Gnoll Raider" {
		t.Errorf("next step marca = %v", step["marca"])
	}
}

func TestGuiaMisionDefaultsToTrackedQuest(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	res := call(t, reg, "guia_mision", "")
	if res.IsError || !strings.Contains(textOf(t, res), `"questId": 90001`) {
		t.Fatalf("default guia must be the tracked quest: %s", textOf(t, res))
	}
	res = call(t, reg, "guia_mision", `{"quest_id": 90002}`)
	if res.IsError || !strings.Contains(textOf(t, res), "Herbalist Ruth") {
		t.Fatalf("guia for 90002 = %s", textOf(t, res))
	}
	res = call(t, reg, "guia_mision", `{"quest_id": -4}`)
	if !res.IsError {
		t.Error("negative quest_id must be an error")
	}
}

func TestPlanProfesionesListsTrainers(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	res := call(t, reg, "plan_profesiones", "")
	if res.IsError {
		t.Fatalf("IsError: %s", textOf(t, res))
	}
	text := textOf(t, res)
	for _, want := range []string{"Herbalism", "Hanna Rel", "Herbalist Ruth", "entrenarYa"} {
		if !strings.Contains(text, want) {
			t.Errorf("plan lacks %q", want)
		}
	}
}

func TestCompositeToolsHaveSpanishDescriptionsAndSchemas(t *testing.T) {
	reg := tools.Build(nil, nil)
	want := map[string]bool{"contexto_jugador": false, "guia_mision": false, "plan_profesiones": false}
	for _, d := range reg.Defs() {
		if _, ok := want[d.Name]; ok {
			want[d.Name] = true
			if !strings.Contains(d.Description, "Úsala") {
				t.Errorf("%s description should say when to use it: %q", d.Name, d.Description)
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %s is not registered", name)
		}
	}
	res := call(t, reg, "contexto_jugador", "")
	if !res.IsError {
		t.Error("without game data the composite must answer with an error, not data")
	}
}

func TestObserverSeesEveryCallWithArgs(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	var mu sync.Mutex
	var got []tools.CallRecord
	reg.SetObserver(func(r tools.CallRecord) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, r)
	})
	_ = call(t, reg, "guia_mision", `{"quest_id": 90002}`)
	_, _ = reg.Call(context.Background(), "no_existe", nil)
	if len(got) != 2 {
		t.Fatalf("observer saw %d calls, want 2", len(got))
	}
	if got[0].Name != "guia_mision" || got[0].Args != `{"quest_id": 90002}` || got[0].IsError || !strings.Contains(got[0].Text, "90002") {
		t.Errorf("first record = %+v", got[0])
	}
	if got[1].Err == "" {
		t.Errorf("unknown tool must be recorded with its error: %+v", got[1])
	}
	reg.SetObserver(nil)
	_ = call(t, reg, "estado_conexion", "")
	if len(got) != 2 {
		t.Error("removed observer must not be called")
	}
}
