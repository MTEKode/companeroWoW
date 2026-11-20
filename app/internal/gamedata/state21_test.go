package gamedata

import (
	"path/filepath"
	"strings"
	"testing"
)

// Schema 2.1: characters[k].state reaches the readers through the store, so the sheet of the active
// character comes with the state's talents (the Spanish name and the spell id of each talent).
func TestSchema21StateSheetReachesTheReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	writeSV(t, path, `["schema"] = 2,
["schemaMinor"] = 1,
["characters"] = {
["Tester-Reino"] = {
	["character"] = { ["name"] = "Tester", ["realm"] = "Reino", ["level"] = 25, ["classFile"] = "WARRIOR" },
	["updatedAt"] = 100,
	["state"] = {
		["at"] = 5000,
		["sheet"] = {
			["level"] = 25, ["class"] = "WARRIOR",
			["talents"] = { ["tabs"] = { { ["name"] = "Armas", ["talents"] = {
				{ ["name"] = "Golpe mortal", ["spellID"] = 12294, ["rank"] = 5, ["maxRank"] = 5 },
			} } } },
		},
	},
},
}`)
	g, _ := newStateService(t, path)
	raw, _, ok := g.SheetJSON()
	if !ok {
		t.Fatal("no sheet from the schema 2.1 state")
	}
	text := string(raw)
	if !strings.Contains(text, "Golpe mortal") || !strings.Contains(text, "12294") {
		t.Fatalf("sheet = %s: the talent's Spanish name and spell id must come through", text)
	}
}
