package gamedata

import (
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

func TestSheetReadsFromSavedVariables(t *testing.T) {
	svc := svService(t, charLua(`["character"] = { ["class"] = "Warrior", ["level"] = 20 },
		["sheet"] = { ["exportedAt"] = 1795000000, ["level"] = 20, ["class"] = "WARRIOR",
			["melee"] = { ["mhMin"] = 44, ["mhMax"] = 62, ["mhSpeed"] = 3.5, ["apBase"] = 100 },
			["weaponSkills"] = { { ["name"] = "Two-Handed Swords", ["rank"] = 100, ["max"] = 100 } } }`))
	raw, at, ok := svc.SheetJSON()
	if !ok {
		t.Fatal("the sheet block was not read")
	}
	if at.IsZero() {
		t.Error("the export time must be set")
	}
	sh, err := theory.ParseSheet(raw)
	if err != nil {
		t.Fatalf("the sheet does not parse: %v", err)
	}
	if sh.Level != 20 || sh.Class != "WARRIOR" || sh.Melee.MHMax != 62 || sh.Melee.APBase != 100 {
		t.Fatalf("sheet = %+v", sh)
	}
	if kind, twoHanded, ok := sh.WeaponKind(); !ok || kind != "sword" || !twoHanded {
		t.Fatalf("weapon = %q %v %v", kind, twoHanded, ok)
	}
}

func TestNoSheetWhenTheAddonDoesNotExportOne(t *testing.T) {
	svc := svService(t, charLua(`["character"] = { ["class"] = "Warrior", ["level"] = 20 }`))
	if _, _, ok := svc.SheetJSON(); ok {
		t.Fatal("a character without a sheet block must report none")
	}
}

func TestLiveSheetReplacesTheSavedOne(t *testing.T) {
	merged := map[string]any{"sheet": map[string]any{"level": float64(19)}}
	live := map[string]any{"level": float64(20), "class": "WARRIOR"}
	if !applyLiveBlock(merged, bridge.MsgSheet, live) {
		t.Fatal("a live sheet must be applied")
	}
	sh := asMap(merged["sheet"])
	if numberOf(sh["level"]) != 20 {
		t.Fatalf("sheet level after the live message = %v", sh["level"])
	}
	if applyLiveBlock(merged, bridge.MsgSheet, "no es un objeto") {
		t.Fatal("a non-object sheet must be ignored")
	}
}
