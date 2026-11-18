package warrior

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

// The fixtures are the saved site feeds (testdata/wft) and the sheet of a level 20 warrior
// (testdata/sheet-warrior-l20.json). They are read with the same parsers the live client uses.

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return raw
}

// siteData reads the warrior talent tree and ability ranks from the saved feeds.
func siteData(t *testing.T) theory.Data {
	t.Helper()
	talents, err := websearch.ParseWFTTalents(fixture(t, filepath.Join("wft", "tips-warrior.json")), "warrior")
	if err != nil {
		t.Fatal(err)
	}
	abil, err := websearch.ParseWFTAbilities(string(fixture(t, filepath.Join("wft", "abilities-warrior.html"))), "warrior")
	if err != nil {
		t.Fatal(err)
	}
	d := theory.Data{Abilities: map[string][]theory.AbilityRank{}}
	for _, tl := range talents.Talentos {
		td := theory.TalentDef{Slug: tl.Slug, Name: tl.Nombre, Tree: tl.Arbol, Row: tl.Fila, Max: tl.Maximo,
			Need: tl.Necesita, Prereq: tl.Requisito, Status: tl.Estado, RankText: map[int]string{}}
		for _, r := range tl.Rangos {
			td.RankText[r.Rango] = r.Texto
		}
		d.Talents = append(d.Talents, td)
	}
	for _, school := range abil.Escuelas {
		for _, sp := range school.Hechizos {
			for i, r := range sp.Rangos {
				d.Abilities[sp.Nombre] = append(d.Abilities[sp.Nombre], theory.AbilityRank{Rank: i + 1, Level: r.Nivel, Cost: r.Coste, Text: r.Texto})
			}
		}
	}
	return d
}

// sheetL20 is the level 20 warrior sheet.
func sheetL20(t *testing.T) theory.Sheet {
	t.Helper()
	sh, err := theory.ParseSheet(fixture(t, "sheet-warrior-l20.json"))
	if err != nil {
		t.Fatal(err)
	}
	return sh
}

// inputL20 is the sheet, the site data and the sheet's own build.
func inputL20(t *testing.T) theory.Input {
	t.Helper()
	sh := sheetL20(t)
	return theory.Input{Sheet: sh, Data: siteData(t), Opt: theory.Options{Fights: 200}}
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }
