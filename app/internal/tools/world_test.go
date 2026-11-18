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

// worldRegistry is the registry over the world fixture: a level 19 human priest in Elwynn.
func worldRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w := bridge.NewSVWatcher("../../testdata/CompaneroWoW_world.lua", time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll world fixture: %v", err)
	}
	return tools.Build(gamedata.NewService(nil, w, st), nil)
}

// callWorld runs one tool and returns its text and whether it failed.
func callWorld(t *testing.T, r *tools.Registry, name, args string) (string, bool) {
	t.Helper()
	res, err := r.Call(context.Background(), name, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), res.IsError
}

func TestWorldToolsAreRegistered(t *testing.T) {
	r := worldRegistry(t)
	have := map[string]bool{}
	for _, d := range r.Defs() {
		have[d.Name] = true
	}
	for _, name := range []string{"ruta_viaje", "donde_comprar", "coste_entrenamiento", "cadena_mision", "consumibles"} {
		if !have[name] {
			t.Errorf("tool %s is not registered", name)
		}
	}
}

func TestRutaViajeAnswersWithTheFlightAndAMarca(t *testing.T) {
	r := worldRegistry(t)
	text, failed := callWorld(t, r, "ruta_viaje", `{"destino":"Forjaz"}`)
	if failed {
		t.Fatalf("ruta_viaje failed: %s", text)
	}
	for _, want := range []string{`"destino": "Forjaz"`, "Toma vuelo en Ventormenta hacia Forjaz", "/compa marca 37 ", `"medido": true`} {
		if !strings.Contains(text, want) {
			t.Errorf("ruta_viaje lacks %q in:\n%s", want, text)
		}
	}
}

func TestRutaViajeTolerantArgsAndMissingDestination(t *testing.T) {
	r := worldRegistry(t)
	// The alias "lugar" and a number as text are accepted.
	text, failed := callWorld(t, r, "ruta_viaje", `{"lugar":"Villa Darkshire"}`)
	if failed || !strings.Contains(text, "Villa Darkshire") {
		t.Errorf("alias lugar: failed %v text %s", failed, text)
	}
	if _, failed := callWorld(t, r, "ruta_viaje", `{}`); !failed {
		t.Error("a call without destino must fail")
	}
}

func TestDondeComprarNeedsNameOrItem(t *testing.T) {
	r := worldRegistry(t)
	text, failed := callWorld(t, r, "donde_comprar", `{"item_id":1251}`)
	if failed || !strings.Contains(text, "Hermano Rodrigo") {
		t.Errorf("by item id: failed %v text %s", failed, text)
	}
	if _, failed := callWorld(t, r, "donde_comprar", `{}`); !failed {
		t.Error("a call without nombre or item_id must fail")
	}
}

func TestCosteEntrenamientoAnswersWithTrainerCost(t *testing.T) {
	r := worldRegistry(t)
	text, failed := callWorld(t, r, "coste_entrenamiento", `{"profesion":"Sastrería"}`)
	if failed || !strings.Contains(text, `"costeAhora": 800`) || !strings.Contains(text, "Lana Hilotejido") {
		t.Errorf("profession cost: failed %v text %s", failed, text)
	}
}

func TestCadenaMisionByNumberAndName(t *testing.T) {
	r := worldRegistry(t)
	text, failed := callWorld(t, r, "cadena_mision", `{"quest_id":"90102"}`)
	if failed || !strings.Contains(text, "Segundo encargo") || !strings.Contains(text, "Encargo de la Hermana Lurdes") {
		t.Errorf("chain by id: failed %v text %s", failed, text)
	}
	if _, failed := callWorld(t, r, "cadena_mision", `{}`); !failed {
		t.Error("a call without quest_id or nombre must fail")
	}
}

func TestConsumiblesReadsPeleas(t *testing.T) {
	r := worldRegistry(t)
	text, failed := callWorld(t, r, "consumibles", `{"peleas":"5"}`)
	if failed || !strings.Contains(text, `"vidaNecesaria": 2000`) {
		t.Errorf("consumables: failed %v text %s", failed, text)
	}
	if _, failed := callWorld(t, r, "consumibles", `{"peleas":-2}`); !failed {
		t.Error("a negative peleas must fail")
	}
}
