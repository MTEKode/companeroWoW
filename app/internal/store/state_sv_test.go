package store

import (
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// Schema 2.1: characters[k].state is the newest copy of each block of the character. Its sections are
// stored under the readers' names, stamped with the time the addon saved them (state.at).
func TestCharStateFromSavedVarsStateBlock(t *testing.T) {
	st := openCharStateStore(t)
	sv := &bridge.SavedVars{Raw: map[string]any{
		"schema":      float64(2),
		"schemaMinor": float64(1),
		"characters": map[string]any{
			"G-Reino": map[string]any{
				"updatedAt": float64(100),
				"state": map[string]any{
					"at": float64(5000),
					"sheet": map[string]any{
						"level": float64(25), "class": "WARRIOR",
						"talents": map[string]any{"tabs": []any{map[string]any{"name": "Armas", "talents": []any{
							map[string]any{"name": "Golpe mortal", "spellID": float64(12294), "rank": float64(5), "maxRank": float64(5)},
						}}}},
						"spells": []any{map[string]any{"spellID": float64(78), "name": "Golpe heroico", "cost": float64(15)}},
					},
					"equipment": map[string]any{"1": map[string]any{"itemID": float64(2), "link": "|Hitem:2|h[Casco]|h|r"}},
					"location":  map[string]any{"mapID": float64(1429), "zone": "Elwynn"},
					"training":  map[string]any{"available": true, "source": "WhatsTraining"},
				},
			},
		},
	}}
	if _, err := st.IngestSavedVars(sv, time.Unix(9000, 0)); err != nil {
		t.Fatal(err)
	}
	cs, err := st.CharState("G-Reino")
	if err != nil {
		t.Fatal(err)
	}
	sheet, _ := cs.Sections["sheet"].(map[string]any)
	if sheet["level"] != float64(25) {
		t.Fatalf("sheet = %v, want the state's level 25", sheet)
	}
	tabs := sheet["talents"].(map[string]any)["tabs"].([]any)
	talent := tabs[0].(map[string]any)["talents"].([]any)[0].(map[string]any)
	if talent["name"] != "Golpe mortal" || talent["spellID"] != float64(12294) {
		t.Fatalf("talent = %v: the Spanish name and the spell id must survive", talent)
	}
	if eq, _ := cs.Sections["equipment"].(map[string]any); eq == nil || eq["1"] == nil {
		t.Fatalf("equipment = %v", cs.Sections["equipment"])
	}
	if loc, _ := cs.Sections["location"].(map[string]any); loc["zone"] != "Elwynn" {
		t.Fatalf("location = %v", cs.Sections["location"])
	}
	if _, ok := cs.Sections["training"]; !ok {
		t.Fatalf("training section missing: %v", cs.Sections)
	}
	if _, ok := cs.Sections["at"]; ok {
		t.Fatalf("the time of the state is not a section: %v", cs.Sections)
	}
	if !cs.Latest.Equal(time.Unix(5000, 0)) {
		t.Fatalf("latest = %v, want the state's time 5000", cs.Latest)
	}
}

// The state takes part in the newest-wins rule: a channel sheet learned after the save wins over the
// saved state, and a saved state learned after the channel sheet wins over it.
func TestCharStateNewestWinsBetweenChannelAndSavedVarsState(t *testing.T) {
	st := openCharStateStore(t)
	if _, err := st.IngestMessage(bridge.MsgSheet, []byte(`{"level":26,"class":"WARRIOR"}`), "G-Reino", time.Unix(6000, 0)); err != nil {
		t.Fatal(err)
	}
	sv := func(at float64, level float64) *bridge.SavedVars {
		return &bridge.SavedVars{Raw: map[string]any{
			"schema": float64(2),
			"characters": map[string]any{
				"G-Reino": map[string]any{"state": map[string]any{
					"at":    at,
					"sheet": map[string]any{"level": level, "class": "WARRIOR"},
				}},
			},
		}}
	}
	if _, err := st.IngestSavedVars(sv(5000, 25), time.Unix(9000, 0)); err != nil {
		t.Fatal(err)
	}
	cs, err := st.CharState("G-Reino")
	if err != nil {
		t.Fatal(err)
	}
	if sheet, _ := cs.Sections["sheet"].(map[string]any); sheet["level"] != float64(26) {
		t.Fatalf("older saved state replaced the newer channel sheet: %v", sheet)
	}
	if _, err := st.IngestSavedVars(sv(7000, 27), time.Unix(9000, 0)); err != nil {
		t.Fatal(err)
	}
	cs, err = st.CharState("G-Reino")
	if err != nil {
		t.Fatal(err)
	}
	if sheet, _ := cs.Sections["sheet"].(map[string]any); sheet["level"] != float64(27) {
		t.Fatalf("newer saved state did not replace the channel sheet: %v", sheet)
	}
}
