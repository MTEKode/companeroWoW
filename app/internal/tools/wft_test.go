package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

func TestSpellsUpToKeepsRanksAtOrBelowTheLevel(t *testing.T) {
	schools := []websearch.WFTSchool{{
		Nombre: "Holy",
		Hechizos: []websearch.WFTSpell{
			{Nombre: "Lesser Heal", Rangos: []websearch.WFTRank{{Rango: "Rank 1", Nivel: 1}, {Rango: "Rank 2", Nivel: 4}, {Rango: "Rank 3", Nivel: 10}}},
			{Nombre: "Heal", Rangos: []websearch.WFTRank{{Rango: "Rank 1", Nivel: 16}}},
		},
	}}
	got := spellsUpTo(schools, 5)
	if len(got) != 1 || len(got[0].Hechizos) != 1 {
		t.Fatalf("spellsUpTo = %+v, want only Lesser Heal", got)
	}
	if n := len(got[0].Hechizos[0].Rangos); n != 2 {
		t.Errorf("ranks up to level 5 = %d, want 2", n)
	}
	if none := spellsUpTo(schools, 0); len(none) != 0 {
		t.Errorf("level 0 should keep nothing, got %+v", none)
	}
}

func TestWFTJSONCompactsALongBlockAndNamesTheLevelToAsk(t *testing.T) {
	var spells []websearch.WFTSpell
	for i := 0; i < 400; i++ {
		spells = append(spells, websearch.WFTSpell{
			Nombre: fmt.Sprintf("Hechizo muy largo número %d", i),
			Estado: "cambiado",
			Rangos: []websearch.WFTRank{{Rango: "Rank 1", Nivel: i%60 + 1, Texto: strings.Repeat("texto ", 20)}},
		})
	}
	res, err := wftJSON(map[string]any{"escuelas": []websearch.WFTSchool{{Nombre: "Holy", Hechizos: spells}}, "nota": sourceNote})
	if err != nil {
		t.Fatal(err)
	}
	text := wftText(t, res)
	if !strings.Contains(text, "pide un nivel") {
		t.Errorf("a long block should ask for a level:\n%.300s", text)
	}
	if strings.Contains(text, "texto texto texto") {
		t.Error("the compact list must not carry the rank texts")
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(text), &back); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
}

func TestWithWFTKeepsTheGameDataAndAddsTheSiteBlock(t *testing.T) {
	res, err := withWFT(map[string]any{"available": false, "hint": "sync"}, map[string]any{"fuente": wftSource})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(wftText(t, res)), &m); err != nil {
		t.Fatal(err)
	}
	if m["hint"] != "sync" || m["available"] != false {
		t.Errorf("game data changed: %v", m)
	}
	site, _ := m["wowforever"].(map[string]any)
	if site["fuente"] != wftSource {
		t.Errorf("wowforever block = %v", m["wowforever"])
	}
	// A non-map answer is kept under "datos".
	res, _ = withWFT("texto", map[string]any{"a": 1})
	if !strings.Contains(wftText(t, res), `"datos": "texto"`) {
		t.Errorf("non-map answer lost: %s", wftText(t, res))
	}
}

func TestIsUnavailableReadsTheGamedataFlags(t *testing.T) {
	if !isUnavailable(map[string]any{"available": false}) || !isUnavailable(map[string]any{"encontrada": false}) {
		t.Error("available:false and encontrada:false are unavailable")
	}
	if isUnavailable(map[string]any{"available": true}) || isUnavailable("texto") {
		t.Error("available data is not unavailable")
	}
}

func TestSiteClassAndRoleNamesAreTranslated(t *testing.T) {
	cases := map[string]string{"Sacerdote": "priest", "PRIEST": "priest", "Chamán": "shaman", "Dragón": ""}
	for in, want := range cases {
		if got := wftClass(in); got != want {
			t.Errorf("wftClass(%q) = %q, want %q", in, got, want)
		}
	}
	if got := translateRol("Disciplina"); got != "discipline" {
		t.Errorf("translateRol = %q", got)
	}
}

func TestNewSiteToolsValidateTheirArguments(t *testing.T) {
	reg := Build(nil, nil)
	ctx := context.Background()
	for _, tc := range []struct{ name, args, want string }{
		{"hechizos_clase", `{}`, "falta clase"},
		{"hechizos_clase", `{"clase":"Dragón"}`, "no es una clase"},
		{"hechizos_clase", `{"clase":"priest"}`, "no está configurada"},
		{"builds_clase", `{"clase":"priest"}`, "no está configurada"},
		{"builds_clase", `{}`, "falta clase"},
	} {
		res, err := reg.Call(ctx, tc.name, json.RawMessage(tc.args))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !res.IsError || !strings.Contains(wftText(t, res), tc.want) {
			t.Errorf("%s %s: got %q, want error containing %q", tc.name, tc.args, wftText(t, res), tc.want)
		}
	}
}

// wftText joins the text content of a result.
func wftText(t *testing.T, res Result) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}
