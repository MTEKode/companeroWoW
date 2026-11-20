package gamedata

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSV2FixtureWarriorIsTheActiveCharacter reads the anonymised copy of a real schema-2 file
// (testdata/sv2_warrior.lua): the sheet is in the outbox, not in the character table, and the active
// character is the warrior Guerrera-Reino (the newest activity of the three).
func TestSV2FixtureWarriorIsTheActiveCharacter(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "sv2_warrior.lua")
	g, _ := newStateService(t, path)
	if err := g.ImportSavedVariablesNow(); err != nil {
		t.Fatal(err)
	}
	if got := g.ActiveCharacterKey(); got != "Guerrera-Reino" {
		t.Fatalf("active = %q, want Guerrera-Reino", got)
	}
	raw, _, ok := g.SheetJSON()
	if !ok {
		t.Fatal("the sheet of the active character is not read")
	}
	s := string(raw)
	for _, want := range []string{`"class":"WARRIOR"`, `"level":25`, `"name":"Crueldad"`, `"spellID":12320`} {
		if !strings.Contains(s, want) {
			t.Errorf("sheet lacks %s", want)
		}
	}
	// The sheet's spells and talents are the book and the talents of the character too.
	if v, _, ok := g.section("spells"); !ok || len(listOf(v)) == 0 {
		t.Errorf("spells of the sheet: ok=%v", ok)
	}
	if v, _, ok := g.section("talents"); !ok || asMap(v) == nil {
		t.Errorf("talents of the sheet: ok=%v", ok)
	}
	// Every name in the file is anonymised.
	for _, personal := range []string{"Ezpatak", "Inguma", "Trumoi", "CAMWOW", "Legio"} {
		if strings.Contains(s, personal) {
			t.Errorf("the sheet holds %q", personal)
		}
	}
}
