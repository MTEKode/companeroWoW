package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/store"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

const fixture = "../../testdata/CompaneroWoW.lua"

var wantNames = []string{
	"contexto_jugador",
	"guia_mision",
	"plan_profesiones",
	"capturar_pantalla",
	"estado_conexion",
	"obtener_ubicacion",
	"obtener_personaje",
	"obtener_misiones",
	"obtener_mision",
	"obtener_objetivo",
	"obtener_inventario",
	"obtener_equipo",
	"obtener_grupo",
	"obtener_hechizos",
	"obtener_talentos",
	"obtener_profesiones",
	"obtener_reputaciones",
	"obtener_dinero",
	"buscar_conocimiento",
	"npcs_en_mapa",
	"buscar_web",
	"leer_pagina",
}

// fixtureService builds a gamedata service over the SavedVariables fixture with the
// knowledge imported, and no live bridge.
func fixtureService(t *testing.T) *gamedata.Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w := bridge.NewSVWatcher(fixture, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll fixture: %v", err)
	}
	if _, _, err := st.ImportKnowledge(w.Current()); err != nil {
		t.Fatalf("import knowledge: %v", err)
	}
	return gamedata.NewService(nil, w, st)
}

func call(t *testing.T, reg *tools.Registry, name string, args string) tools.Result {
	t.Helper()
	res, err := reg.Call(context.Background(), name, json.RawMessage(args))
	if err != nil {
		t.Fatalf("Call(%s): unexpected error %v", name, err)
	}
	return res
}

func textOf(t *testing.T, res tools.Result) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}

func TestBuildRegistersAllTools(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	defs := reg.Defs()
	if len(defs) != len(wantNames) {
		t.Fatalf("got %d tools, want %d", len(defs), len(wantNames))
	}
	for i, d := range defs {
		if d.Name != wantNames[i] {
			t.Errorf("tool %d = %q, want %q", i, d.Name, wantNames[i])
		}
		if d.Description == "" || d.InputSchema == nil || d.Handler == nil {
			t.Errorf("tool %q incomplete", d.Name)
		}
	}
}

func TestObtenerMisionesReturnsJSON(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	res := call(t, reg, "obtener_misiones", "")
	if res.IsError {
		t.Fatalf("IsError: %s", textOf(t, res))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res)), &m); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, textOf(t, res))
	}
	if m["available"] != true {
		t.Errorf("available = %v", m["available"])
	}
	if !bytes.Contains([]byte(textOf(t, res)), []byte("\n  ")) {
		t.Error("result is not 2-space indented JSON")
	}
}

func TestObtenerMisionReturnsQuestAndMissingIDIsError(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)

	res := call(t, reg, "obtener_mision", `{"quest_id": 90001}`)
	if res.IsError || !strings.Contains(textOf(t, res), "The Warchief's Orders") {
		t.Fatalf("quest 90001 not returned: %+v", res)
	}

	res = call(t, reg, "obtener_mision", `{}`)
	if !res.IsError {
		t.Fatal("missing quest_id must be an error result")
	}
	if !strings.Contains(textOf(t, res), "quest_id") {
		t.Errorf("error message should name quest_id: %q", textOf(t, res))
	}
}

func TestCapturarPantallaUsesOverridableCapture(t *testing.T) {
	orig := tools.CaptureFunc
	t.Cleanup(func() { tools.CaptureFunc = orig })
	png := []byte("\x89PNG\r\n\x1a\nfake")
	tools.CaptureFunc = func() ([]byte, error) { return png, nil }

	reg := tools.Build(nil, nil)
	res := call(t, reg, "capturar_pantalla", "")
	if res.IsError || len(res.Content) < 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	img := res.Content[0]
	if img.Type != "image" || img.MimeType != "image/png" || !bytes.Equal(img.Data, png) {
		t.Errorf("first content = %+v, want the PNG image", img)
	}

	tools.CaptureFunc = func() ([]byte, error) { return nil, errors.New("sin ventana") }
	res = call(t, reg, "capturar_pantalla", "")
	if !res.IsError {
		t.Error("capture failure must be an error result")
	}
}

func TestUnknownToolIsError(t *testing.T) {
	reg := tools.Build(nil, nil)
	if _, err := reg.Call(context.Background(), "no_existe", nil); err == nil {
		t.Fatal("unknown tool did not return an error")
	}
}

func TestNilServicesAnswerInsteadOfPanicking(t *testing.T) {
	reg := tools.Build(nil, nil)
	for _, d := range reg.Defs() {
		res, err := reg.Call(context.Background(), d.Name, nil)
		if err != nil {
			t.Errorf("%s: unexpected error %v", d.Name, err)
		}
		if d.Name != "capturar_pantalla" && !res.IsError {
			t.Errorf("%s with no data should be an error result", d.Name)
		}
	}
}

func TestBuscarConocimientoFindsLearnedQuestAndNpc(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	res := call(t, reg, "buscar_conocimiento", `{"texto": "Garrosh"}`)
	if res.IsError {
		t.Fatalf("IsError: %s", textOf(t, res))
	}
	if !strings.Contains(textOf(t, res), "Garrosh Hellscream") {
		t.Errorf("NPC not found:\n%s", textOf(t, res))
	}

	res = call(t, reg, "buscar_conocimiento", `{"texto": ""}`)
	if !res.IsError {
		t.Error("empty texto must be an error result")
	}
}

func TestNpcsEnMapaUsesRequestedAndSavedVariablesMap(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)

	res := call(t, reg, "npcs_en_mapa", `{"map_id": 1}`)
	if res.IsError || !strings.Contains(textOf(t, res), "Herbalist Ruth") {
		t.Fatalf("map 1 NPCs missing: %+v", res)
	}

	// Without map_id the current map comes from SavedVariables (Orgrimmar, 85).
	res = call(t, reg, "npcs_en_mapa", "")
	if res.IsError || !strings.Contains(textOf(t, res), "Garrosh Hellscream") {
		t.Fatalf("current map NPCs missing: %+v", res)
	}
}

func TestBuscarWebWithoutClientIsError(t *testing.T) {
	reg := tools.Build(nil, nil)
	res := call(t, reg, "buscar_web", `{"consulta": "herbs"}`)
	if !res.IsError {
		t.Error("buscar_web without a client must be an error result")
	}
	res = call(t, reg, "leer_pagina", `{"url": "https://warcraft.wiki.gg/wiki/X"}`)
	if !res.IsError {
		t.Error("leer_pagina without a client must be an error result")
	}
}

func TestHandlerPanicBecomesErrorResult(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Add(tools.Def{
		Name:    "explota",
		Handler: func(context.Context, json.RawMessage) (tools.Result, error) { panic("boom") },
	})
	res, err := reg.Call(context.Background(), "explota", nil)
	if err != nil {
		t.Fatalf("panic leaked as error: %v", err)
	}
	if !res.IsError {
		t.Fatal("panic must become IsError")
	}
}

func TestRegistryAddReplacesInPlace(t *testing.T) {
	reg := tools.NewRegistry()
	ok := func(s string) tools.Def {
		return tools.Def{Name: "a", Handler: func(context.Context, json.RawMessage) (tools.Result, error) {
			return tools.Result{Content: []tools.Content{{Type: "text", Text: s}}}, nil
		}}
	}
	reg.Add(ok("uno"))
	reg.Add(tools.Def{Name: "b"})
	reg.Add(ok("dos"))
	defs := reg.Defs()
	if len(defs) != 2 || defs[0].Name != "a" {
		t.Fatalf("defs = %+v", defs)
	}
	res, _ := reg.Call(context.Background(), "a", nil)
	if res.Content[0].Text != "dos" {
		t.Errorf("replacement not used: %q", res.Content[0].Text)
	}
}
