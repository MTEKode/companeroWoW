package websearch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseWFTTalentsFromTheFeed(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wft", "tips-warrior.json"))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := ParseWFTTalents(raw, "warrior")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Talentos) != 53 || tree.Build != "1.60.1.70938" {
		t.Fatalf("talents = %d, build = %q", len(tree.Talentos), tree.Build)
	}
	var mortal WFTTalent
	for _, tl := range tree.Talentos {
		if tl.Slug == "mortal-strike" {
			mortal = tl
		}
	}
	if mortal.Arbol != "Arms" || mortal.Fila != 7 || mortal.Maximo != 1 || len(mortal.Rangos) != 1 {
		t.Fatalf("Mortal Strike = %+v", mortal)
	}
	points, name, ok := PrerequisiteOf(mortal.Requisito)
	if !ok || points != 1 || name != "Sweeping Strikes" {
		t.Fatalf("prerequisite = %d %q %v", points, name, ok)
	}
	if _, err := ParseWFTTalents([]byte(`{"b":"x","t":{}}`), "warrior"); err == nil {
		t.Fatal("a feed without talents must fail")
	}
	if _, err := ParseWFTTalents([]byte(`{broken`), "warrior"); err == nil {
		t.Fatal("broken JSON must fail")
	}
}
