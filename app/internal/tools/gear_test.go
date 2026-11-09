package tools_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/store"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// gearFixture is the cloth priest of level 19 with every gear block (see gamedata's tests).
const gearFixture = "../../testdata/CompaneroWoW_gear.lua"

func gearFixtureService(t *testing.T) *gamedata.Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w := bridge.NewSVWatcher(gearFixture, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll gear fixture: %v", err)
	}
	return gamedata.NewService(nil, w, st)
}

func TestGearToolsAreRegisteredWithSpanishDescriptions(t *testing.T) {
	reg := tools.Build(gearFixtureService(t), nil)
	want := map[string]bool{"analizar_equipo": true, "misiones_disponibles": true, "info_mazmorra": true, "buscar_objeto": true}
	for _, d := range reg.Defs() {
		if !want[d.Name] {
			continue
		}
		delete(want, d.Name)
		if !strings.Contains(d.Description, "Úsala") {
			t.Errorf("%s description must say when to use it", d.Name)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing tools: %v", want)
	}
	for _, d := range reg.Defs() {
		if d.Name == "paso_guia" {
			t.Error("paso_guia must be removed: the user does not use RXPGuides")
		}
	}
}

func TestAnalizarEquipoAcceptsAliasesAndDefaultsToBothWindows(t *testing.T) {
	reg := tools.Build(gearFixtureService(t), nil)

	byAlias := textOf(t, call(t, reg, "analizar_equipo", `{"ranura":"pecho","window":"ahora"}`))
	if !strings.Contains(byAlias, "Túnica de Hilo Arcano") || !strings.Contains(byAlias, `"ventana": "ahora"`) {
		t.Errorf("alias call:\n%s", byAlias)
	}
	if strings.Contains(byAlias, "Capucha de Hilo Sagrada") {
		t.Errorf("slot filter must keep only the chest:\n%s", byAlias)
	}

	both := textOf(t, call(t, reg, "analizar_equipo", ""))
	for _, want := range []string{`"ahora"`, `"pronto"`, "Cinturón de Lino Ritual", "Bastón de Brasa Lenta", `"ventana": "ambas"`} {
		if !strings.Contains(both, want) {
			t.Errorf("default call misses %q:\n%s", want, both)
		}
	}
	if !strings.Contains(both, `"clase": "Priest"`) || !strings.Contains(both, `"especializacion": "Disciplina"`) {
		t.Errorf("class and spec must be in the answer:\n%s", both)
	}
}

func TestAnalizarEquipoRejectsBadWindowAndAcceptsSlotNumber(t *testing.T) {
	reg := tools.Build(gearFixtureService(t), nil)
	res := call(t, reg, "analizar_equipo", `{"ventana":"mañana"}`)
	if !res.IsError || !strings.Contains(textOf(t, res), "ventana debe ser") {
		t.Errorf("bad window = %+v", res)
	}
	byNumber := textOf(t, call(t, reg, "analizar_equipo", `{"slot":"5","ventana":"ahora"}`))
	if !strings.Contains(byNumber, `"ranura": "Pecho"`) {
		t.Errorf("slot 5 must be the chest:\n%s", byNumber)
	}
}

func TestMisionesDisponiblesFiltersByZone(t *testing.T) {
	reg := tools.Build(gearFixtureService(t), nil)
	text := textOf(t, call(t, reg, "misiones_disponibles", `{"zona":"ventormenta"}`))
	for _, want := range []string{"Malla del Campeón", "Patrulla de Ventormenta", "/compa marca 84 48.0 58.0 Capitán Lenard"} {
		if !strings.Contains(text, want) {
			t.Errorf("ventormenta misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Encargo de la Hermana Lurdes") {
		t.Errorf("zone filter leaked another zone:\n%s", text)
	}
	all := textOf(t, call(t, reg, "misiones_disponibles", ""))
	if !strings.Contains(all, `"mejoraEquipo"`) || !strings.Contains(all, "Túnica de Hilo Arcano") {
		t.Errorf("reward upgrades must be marked:\n%s", all)
	}
}

func TestInfoMazmorraByNameAndSuggestions(t *testing.T) {
	reg := tools.Build(gearFixtureService(t), nil)
	text := textOf(t, call(t, reg, "info_mazmorra", `{"nombre":"Fortaleza"}`))
	for _, want := range []string{"Fortaleza Colmillo Oscuro", "Lord Godfrey", "Túnica de Hilo Arcano", "/compa marca 10 35.0 62.0"} {
		if !strings.Contains(text, want) {
			t.Errorf("dungeon misses %q:\n%s", want, text)
		}
	}
	sug := textOf(t, call(t, reg, "info_mazmorra", ""))
	if !strings.Contains(sug, "sugerencias") || !strings.Contains(sug, "Minas de Brazaloscura") {
		t.Errorf("suggestions:\n%s", sug)
	}
}

func TestBuscarObjetoByNameIdAndAlias(t *testing.T) {
	reg := tools.Build(gearFixtureService(t), nil)

	byID := textOf(t, call(t, reg, "buscar_objeto", `{"item_id": 91005}`))
	if !strings.Contains(byID, "Coraza de Malla Forjada") || !strings.Contains(byID, `"usable": false`) || !strings.Contains(byID, "Mail") {
		t.Errorf("mail by id:\n%s", byID)
	}
	byAlias := textOf(t, call(t, reg, "buscar_objeto", `{"id": "91001"}`))
	if !strings.Contains(byAlias, "Túnica de Hilo Arcano") || !strings.Contains(byAlias, "Lord Godfrey") {
		t.Errorf("alias id as text:\n%s", byAlias)
	}
	byName := textOf(t, call(t, reg, "buscar_objeto", `{"texto":"Pergamino"}`))
	if !strings.Contains(byName, "Pergamino de Ritual Antiguo") || !strings.Contains(byName, "Recados del Cuartel") {
		t.Errorf("knowledge item:\n%s", byName)
	}
	if res := call(t, reg, "buscar_objeto", `{}`); !res.IsError {
		t.Error("no name and no id must be an error")
	}
	if res := call(t, reg, "buscar_objeto", `{"item_id": -3}`); !res.IsError {
		t.Error("a negative item id must be an error")
	}
}

func TestGearToolsWithoutGameAnswerWithoutPanicking(t *testing.T) {
	reg := tools.Build(nil, nil)
	for _, name := range []string{"analizar_equipo", "misiones_disponibles", "info_mazmorra", "buscar_objeto"} {
		res := call(t, reg, name, `{"nombre":"x","item_id":1}`)
		if !res.IsError {
			t.Errorf("%s without data must be an error result", name)
		}
	}
}
