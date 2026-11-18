package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

func toolsFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTheoryDataFromTheSiteFeeds(t *testing.T) {
	talents, err := websearch.ParseWFTTalents(toolsFixture(t, filepath.Join("wft", "tips-warrior.json")), "warrior")
	if err != nil {
		t.Fatal(err)
	}
	abil, err := websearch.ParseWFTAbilities(string(toolsFixture(t, filepath.Join("wft", "abilities-warrior.html"))), "warrior")
	if err != nil {
		t.Fatal(err)
	}
	d := theoryDataFrom(talents, abil)
	if len(d.Talents) != 53 {
		t.Fatalf("talents = %d", len(d.Talents))
	}
	if len(d.Abilities["Rend"]) != 7 || d.Abilities["Rend"][0].Level != 4 {
		t.Fatalf("Rend ranks = %+v", d.Abilities["Rend"])
	}
	for _, tl := range d.Talents {
		if tl.Name == "Mortal Strike" && tl.Prereq != "1 point in Sweeping Strikes" {
			t.Fatalf("Mortal Strike prerequisite = %q", tl.Prereq)
		}
	}
}

func TestTheoryBuildArgumentsAreTolerant(t *testing.T) {
	a, err := parseArgs(json.RawMessage(`{"talentos":{"Cruelty":5},"rotacion":"Overpower, Rend, hs","hs_sobre":30,"fuerza":10,"velocidad_arma":-0.1}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := theoryBuild(a)
	if err != nil {
		t.Fatal(err)
	}
	if b.Talents["Cruelty"] != 5 {
		t.Errorf("talents = %v", b.Talents)
	}
	if strings.Join(b.Rotation, ",") != "overpower,rend,hs" {
		t.Errorf("rotation = %v", b.Rotation)
	}
	if b.Params["hs_sobre"] != 30 {
		t.Errorf("params = %v", b.Params)
	}
	if b.Delta.Str != 10 || b.Delta.MHSpeed != -0.1 {
		t.Errorf("delta = %+v", b.Delta)
	}
	list, err := parseArgs(json.RawMessage(`{"rotacion":["mortal_strike","rend"]}`))
	if err != nil {
		t.Fatal(err)
	}
	b2, err := theoryBuild(list)
	if err != nil || strings.Join(b2.Rotation, ",") != "mortal_strike,rend" {
		t.Fatalf("array rotation = %v, %v", b2.Rotation, err)
	}
}

func TestTheoryToolsNeedGameData(t *testing.T) {
	def := calcularDPS(nil, nil)
	_, err := def.Handler(t.Context(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("without game data the tool must answer with an error")
	}
}

func TestTheoryRegistersItsTools(t *testing.T) {
	r := Build(nil, nil)
	for _, name := range []string{"calcular_dps", "pesos_estadisticas", "comparar_objetos", "optimizar_talentos", "optimizar_rotacion"} {
		found := false
		for _, d := range r.Defs() {
			if d.Name == name {
				found = true
			}
		}
		if !found {
			t.Errorf("tool %s not registered", name)
		}
	}
}
