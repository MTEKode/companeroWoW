package tools_test

import (
	"os"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// Each alias must reach its handler: the handler then fails for a reason other than the
// missing argument (or succeeds). The "falta ..." messages name a missing argument.

func TestTextAliasesReachBuscarConocimiento(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	for _, key := range []string{"texto", "consulta", "query", "q", "text", "busqueda", "búsqueda", "search"} {
		res := call(t, reg, "buscar_conocimiento", `{"`+key+`":"Garrosh"}`)
		if strings.Contains(textOf(t, res), "falta texto") {
			t.Errorf("alias %q not read: %s", key, textOf(t, res))
		}
	}
}

func TestTextAliasesReachBuscarWeb(t *testing.T) {
	// Without a web client the handler answers "no está configurada" once the query is read.
	reg := tools.Build(nil, nil)
	for _, key := range []string{"consulta", "query", "q", "texto", "text", "busqueda", "búsqueda", "search"} {
		res := call(t, reg, "buscar_web", `{"`+key+`":"Herbalism trainer"}`)
		if !strings.Contains(textOf(t, res), "no está configurada") {
			t.Errorf("alias %q not read: %s", key, textOf(t, res))
		}
	}
	res := call(t, reg, "buscar_web", `{"otra":"x"}`)
	if !strings.Contains(textOf(t, res), "falta consulta") {
		t.Errorf("unknown key must not satisfy consulta: %s", textOf(t, res))
	}
}

func TestURLAliasesReachLeerPagina(t *testing.T) {
	reg := tools.Build(nil, nil)
	for _, key := range []string{"url", "link", "enlace"} {
		res := call(t, reg, "leer_pagina", `{"`+key+`":"https://warcraft.wiki.gg/wiki/Forever_mounts"}`)
		if !strings.Contains(textOf(t, res), "no está configurada") {
			t.Errorf("alias %q not read: %s", key, textOf(t, res))
		}
	}
}

func TestQuestIDAliasesReachObtenerMisionAndGuia(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	for _, key := range []string{"quest_id", "id", "questID", "questId", "mision"} {
		res := call(t, reg, "obtener_mision", `{"`+key+`":90001}`)
		if !strings.Contains(textOf(t, res), "The Warchief's Orders") {
			t.Errorf("obtener_mision alias %q not read: %s", key, textOf(t, res))
		}
		res = call(t, reg, "guia_mision", `{"`+key+`":90001}`)
		if res.IsError {
			t.Errorf("guia_mision alias %q failed: %s", key, textOf(t, res))
		}
	}
	res := call(t, reg, "obtener_mision", `{"quest_id":"90001"}`)
	if !strings.Contains(textOf(t, res), "The Warchief's Orders") {
		t.Errorf("quest id as text not read: %s", textOf(t, res))
	}
}

func TestMapAliasesReachNpcsEnMapa(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	for _, key := range []string{"map_id", "mapID", "mapid", "mapa"} {
		res := call(t, reg, "npcs_en_mapa", `{"`+key+`":1429}`)
		if res.IsError || !strings.Contains(textOf(t, res), `"mapId": 1429`) {
			t.Errorf("map alias %q not read: %s", key, textOf(t, res))
		}
	}
}

func TestLimitAliasesAndUnknownKeysAreTolerated(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	for _, key := range []string{"limite", "limit", "max"} {
		res := call(t, reg, "buscar_conocimiento", `{"texto":"Garrosh","`+key+`":1,"extra":true}`)
		if res.IsError {
			t.Errorf("limit alias %q failed: %s", key, textOf(t, res))
		}
	}
	// A limit that is not a number falls back to the default instead of failing.
	res := call(t, reg, "buscar_conocimiento", `{"texto":"Garrosh","limite":"muchos","inventado":{"a":1}}`)
	if res.IsError {
		t.Errorf("bad limit must not fail the call: %s", textOf(t, res))
	}
}

func TestCanonicalNamesStillInSchemas(t *testing.T) {
	reg := tools.Build(nil, nil)
	want := map[string]string{"buscar_web": "consulta", "leer_pagina": "url", "obtener_mision": "quest_id", "npcs_en_mapa": "map_id", "buscar_conocimiento": "texto"}
	for _, d := range reg.Defs() {
		key, ok := want[d.Name]
		if !ok {
			continue
		}
		props, _ := d.InputSchema["properties"].(map[string]any)
		if _, ok := props[key]; !ok {
			t.Errorf("%s schema lost canonical %q", d.Name, key)
		}
	}
}

func TestCapturarPantallaSavesFileAndKeepsNewest(t *testing.T) {
	orig, origDir := tools.CaptureFunc, tools.ScreenshotDir
	t.Cleanup(func() { tools.CaptureFunc, tools.ScreenshotDir = orig, origDir })
	tools.CaptureFunc = func() ([]byte, error) { return []byte("png"), nil }
	dir := t.TempDir()
	tools.ScreenshotDir = dir
	reg := tools.Build(nil, nil)
	var last string
	for i := 0; i < 12; i++ {
		res := call(t, reg, "capturar_pantalla", "")
		if res.IsError {
			t.Fatalf("capturar_pantalla: %s", textOf(t, res))
		}
		text := textOf(t, res)
		if !strings.Contains(text, "Archivo: ") || !strings.Contains(text, "read_file") {
			t.Fatalf("answer must name the file and how to read it: %s", text)
		}
		last = text
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 10 {
		t.Errorf("kept %d captures, want 10", len(entries))
	}
	if !strings.Contains(last, dir) {
		t.Errorf("last answer does not name %s: %s", dir, last)
	}
}
