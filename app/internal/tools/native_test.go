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

// nativeFixture has no Questie and no ForeverDungeonJournal block: the tools must answer from the
// game's data ("juego") and what the addon learned ("aprendido"), and name the source when it matters.
const nativeFixture = "../../testdata/CompaneroWoW_native.lua"

func nativeFixtureService(t *testing.T) *gamedata.Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w := bridge.NewSVWatcher(nativeFixture, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll native fixture: %v", err)
	}
	return gamedata.NewService(nil, w, st)
}

func TestNativeOnlyQuestsAnswerFromTheGame(t *testing.T) {
	reg := tools.Build(nativeFixtureService(t), nil)
	text := textOf(t, call(t, reg, "misiones_disponibles", `{"ventana":"ambas"}`))
	for _, want := range []string{"Encargo de la Hermana Lurdes", "Recados del Cuartel", `"fuente": "juego"`, `"fuente": "aprendido"`} {
		if !strings.Contains(text, want) {
			t.Errorf("misiones_disponibles misses %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, "hace falta Questie") || strings.Contains(text, "obligatorio") {
		t.Errorf("Questie must never be a requirement:\n%s", text)
	}
	if strings.Contains(text, "desconocida") {
		t.Errorf("a quest already in the log must not report an unknown place to accept it:\n%s", text)
	}
}

func TestNativeOnlyGearNamesItsSources(t *testing.T) {
	reg := tools.Build(nativeFixtureService(t), nil)
	text := textOf(t, call(t, reg, "analizar_equipo", `{}`))
	if !strings.Contains(text, `"fuentes"`) || !strings.Contains(text, `"juego"`) {
		t.Errorf("analizar_equipo must carry the source summary:\n%s", text)
	}
	if strings.Contains(text, `"questie":true`) || strings.Contains(text, `"fdj":true`) {
		t.Errorf("no Questie or FDJ data in the native fixture:\n%s", text)
	}
}

func TestNativeOnlyDungeonsWithoutLevelsSayTheyHaveNone(t *testing.T) {
	reg := tools.Build(nativeFixtureService(t), nil)
	text := textOf(t, call(t, reg, "info_mazmorra", `{}`))
	if !strings.Contains(text, "sin dato") || strings.Contains(text, "ForeverDungeonJournal") {
		t.Errorf("info_mazmorra must say the level is unknown, without asking for FDJ:\n%s", text)
	}
}
