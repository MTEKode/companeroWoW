package tools_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/store"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// rotationFixture is the priest with ten recorded combats and bound action bars.
var rotationFixture = filepath.Join("..", "..", "testdata", "CompaneroWoW_rotacion.lua")

func rotationTools(t *testing.T) *tools.Registry {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w := bridge.NewSVWatcher(rotationFixture, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll rotation fixture: %v", err)
	}
	return tools.Build(gamedata.NewService(nil, w, st), nil)
}

func jsonOf(t *testing.T, res tools.Result) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatalf("IsError: %s", textOf(t, res))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res)), &m); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	return m
}

func TestAnalizarRotacionAnswersWithSpellbook(t *testing.T) {
	reg := rotationTools(t)
	m := jsonOf(t, call(t, reg, "analizar_rotacion", ""))
	if m["clase"] != "Priest" || m["nivel"] != float64(19) {
		t.Errorf("clase/nivel = %v/%v", m["clase"], m["nivel"])
	}
	spells, _ := m["hechizos"].([]any)
	if len(spells) != 13 {
		t.Errorf("hechizos = %d, want 13", len(spells))
	}
	if !strings.Contains(textOf(t, call(t, reg, "analizar_rotacion", "")), "Smite") {
		t.Error("the spellbook must list Smite")
	}
}

func TestRevisarCombatesArgumentsAreTolerant(t *testing.T) {
	reg := rotationTools(t)

	deaths := jsonOf(t, call(t, reg, "revisar_combates", `{"muertes": true}`))
	if list, _ := deaths["combates"].([]any); len(list) != 1 {
		t.Errorf("muertes: %d combats, want 1", len(list))
	}

	last := jsonOf(t, call(t, reg, "revisar_combates", `{"ultimo": "sí"}`))
	if list, _ := last["combates"].([]any); len(list) != 1 {
		t.Errorf("ultimo as text: %d combats, want 1", len(list))
	}

	three := jsonOf(t, call(t, reg, "revisar_combates", `{"n": "3"}`))
	if three["analizados"] != float64(3) {
		t.Errorf("n as text: analizados = %v", three["analizados"])
	}

	fallback := jsonOf(t, call(t, reg, "revisar_combates", `{"n": "muchos"}`))
	if fallback["analizados"] != float64(10) {
		t.Errorf("bad n must fall back to 10, got %v", fallback["analizados"])
	}
}

func TestRevisarCombatesTellsAboutTheDeath(t *testing.T) {
	reg := rotationTools(t)
	m := jsonOf(t, call(t, reg, "revisar_combates", `{"muertes": true}`))
	text := textOf(t, call(t, reg, "revisar_combates", `{"muertes": true}`))
	if !strings.Contains(text, "Defias Pillager") || !strings.Contains(text, "defensivos conocidos") {
		t.Errorf("death answer lacks the killer or the defensives hint: %s", text)
	}
	pistas, _ := m["pistas"].([]any)
	if len(pistas) == 0 {
		t.Error("pistas must not be empty for the death window")
	}
}

func TestConsejoBarrasProposesAndBuildsCommand(t *testing.T) {
	reg := rotationTools(t)
	m := jsonOf(t, call(t, reg, "consejo_barras", ""))
	if _, ok := m["disposicion"].([]any); !ok {
		t.Fatalf("disposicion missing: %#v", m)
	}
	cmds, _ := m["comandos"].([]any)
	if len(cmds) != 1 || cmds[0] != "/compa barras aplicar 6:2054" {
		t.Errorf("comandos = %v", cmds)
	}
}

func TestGenerarComandoBarrasValidatesInput(t *testing.T) {
	reg := rotationTools(t)

	ok := jsonOf(t, call(t, reg, "generar_comando_barras",
		`{"asignaciones": [{"ranura": "6", "hechizo": "heal"}, {"ranura": 7, "id": 8092}]}`))
	cmds, _ := ok["comandos"].([]any)
	if len(cmds) != 1 || cmds[0] != "/compa barras aplicar 6:2054,7:8092" {
		t.Errorf("comandos = %v", cmds)
	}

	for name, args := range map[string]string{
		"sin lista":      `{}`,
		"no es lista":    `{"asignaciones": "6 Heal"}`,
		"sin ranura":     `{"asignaciones": [{"hechizo": "Heal"}]}`,
		"sin hechizo":    `{"asignaciones": [{"ranura": 6}]}`,
		"lista vacía":    `{"asignaciones": []}`,
		"id no numérico": `{"asignaciones": [{"ranura": 6, "id": "x"}]}`,
	} {
		res, err := reg.Call(context.Background(), "generar_comando_barras", json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !res.IsError {
			t.Errorf("%s: want an error answer, got %s", name, textOf(t, res))
		}
	}
}

func TestRotationToolsWithoutDataSayWhatIsMissing(t *testing.T) {
	reg := tools.Build(nil, nil)
	for _, name := range []string{"analizar_rotacion", "revisar_combates", "consejo_barras"} {
		res := call(t, reg, name, "")
		if !res.IsError {
			t.Errorf("%s without game data must answer an error, got %s", name, textOf(t, res))
		}
	}
}
