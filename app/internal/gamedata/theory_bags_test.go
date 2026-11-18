package gamedata

// Tests of what the engine reads from the bags and the combat records: weapons with their damage and speed,
// equippable bag items with their stats, the spell ids of the combat records, and the stored calibration.

import (
	"testing"
)

func TestBagItemsAreScoredWithTheirStatsAndWeapons(t *testing.T) {
	svc := svService(t, charLua(`["character"] = { ["class"] = "Warrior", ["level"] = 20 },
		["gearCandidates"] = { ["available"] = true, ["items"] = {} },
		["gear"] = { [16] = { ["itemID"] = 100, ["name"] = "Espada vieja", ["itemLevel"] = 10,
			["equipLoc"] = "INVTYPE_2HWEAPON", ["class"] = "Weapon", ["subclass"] = "Two-Handed Swords",
			["weapon"] = { ["min"] = 44, ["max"] = 62, ["speed"] = 3.5 } } },
		["inventory"] = { ["items"] = {
			{ ["itemID"] = 200, ["name"] = "Pechera de prueba", ["equipLoc"] = "INVTYPE_CHEST", ["class"] = "Armor",
				["subclass"] = "Mail", ["itemLevel"] = 18, ["stats"] = { ["ITEM_MOD_STRENGTH_SHORT"] = 9 } },
			{ ["itemID"] = 201, ["name"] = "Espada nueva", ["equipLoc"] = "INVTYPE_2HWEAPON", ["class"] = "Weapon",
				["subclass"] = "Two-Handed Swords", ["itemLevel"] = 22,
				["weapon"] = { ["min"] = 60, ["max"] = 80, ["speed"] = 3.4 } },
			{ ["itemID"] = 202, ["name"] = "Basura", ["equipLoc"] = "INVTYPE_HEAD" },
		} }`))
	items, _, ok := svc.TheoryCandidates()
	if !ok {
		t.Fatal("the candidates block must be known")
	}
	byName := map[string]bool{}
	for _, it := range items {
		byName[it.Name] = true
		if !it.Bag {
			t.Errorf("%s: a bag item must say so", it.Name)
		}
		switch it.Name {
		case "Espada nueva":
			if it.Weapon == nil || it.Weapon.Min != 60 || it.Weapon.Max != 80 || it.Weapon.Speed != 3.4 {
				t.Errorf("the weapon's damage and speed are lost: %+v", it.Weapon)
			}
			if it.Subclass != "Two-Handed Swords" {
				t.Errorf("subclass = %q", it.Subclass)
			}
		case "Pechera de prueba":
			if it.Stats["ITEM_MOD_STRENGTH_SHORT"] != 9 {
				t.Errorf("the bag item's stats are lost: %v", it.Stats)
			}
		}
	}
	if !byName["Espada nueva"] || !byName["Pechera de prueba"] {
		t.Fatalf("bag items missing: %v", byName)
	}
	if byName["Basura"] {
		t.Fatal("an item with no stats and no weapon cannot be scored")
	}
}

func TestRecentFightsCarryTheSpellIDs(t *testing.T) {
	svc := svService(t, charLua(`["character"] = { ["class"] = "Warrior", ["level"] = 20 },
		["combats"] = { {
			["duration"] = 12, ["died"] = false,
			["damageDone"] = { ["total"] = 900, ["spells"] = {
				{ ["spellID"] = 1464, ["name"] = "Aplastar", ["amount"] = 400 },
				{ ["spellID"] = 78, ["name"] = "Golpe heroico", ["amount"] = 300 },
				{ ["spellID"] = 0, ["name"] = "Golpe automático", ["amount"] = 200 },
			} },
		} }`))
	fights := svc.RecentFights(1)
	if len(fights) != 1 {
		t.Fatalf("fights = %d", len(fights))
	}
	got := fights[0].Hechizos
	if len(got) != 2 || got[0] != 1464 || got[1] != 78 {
		t.Fatalf("spell ids = %v, want [1464 78] (the auto attack has no id)", got)
	}
	if len(fights[0].Habilidades) != 3 {
		t.Fatalf("names are kept for display: %v", fights[0].Habilidades)
	}
}

func TestCalibrationIsStoredPerExportedSheet(t *testing.T) {
	svc := svService(t, charLua(`["character"] = { ["class"] = "Warrior", ["level"] = 20 },
		["sheet"] = { ["exportedAt"] = 1795000000, ["level"] = 20, ["class"] = "WARRIOR" }`))
	key, ok := svc.CalibrationKey()
	if !ok {
		t.Fatal("a character with a sheet must have a calibration key")
	}
	if _, found := svc.StoredCalibration(key); found {
		t.Fatal("nothing is stored before the first check")
	}
	svc.StoreCalibration(key, "modelo calibrado ✓")
	line, found := svc.StoredCalibration(key)
	if !found || line != "modelo calibrado ✓" {
		t.Fatalf("stored line = %q, %v", line, found)
	}
	other := svService(t, charLua(`["character"] = { ["class"] = "Warrior", ["level"] = 20 }`))
	if _, ok := other.CalibrationKey(); ok {
		t.Fatal("a character without a sheet has no calibration key")
	}
}
