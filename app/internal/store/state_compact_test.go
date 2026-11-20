package store

import (
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// The addon writes the quest window and the gear candidates of the state in the compact encoding (short keys
// of Storage.KEYS: qd = questID, n = name, lv = level, rw = rewards, ch = choice, m = mapID, ...). The store
// keeps them under the long names the readers use, and the Questie summary as it is.
func TestCharStateReadsTheCompactQuestWindow(t *testing.T) {
	st := openCharStateStore(t)
	sv := &bridge.SavedVars{Raw: map[string]any{
		"schema":      float64(2),
		"schemaMinor": float64(1),
		"characters": map[string]any{
			"G-Reino": map[string]any{
				"updatedAt": float64(100),
				"state": map[string]any{
					"at": float64(5000),
					"questWindow": map[string]any{
						"available": true, "level": float64(25), "minLevel": float64(20), "maxLevel": float64(28),
						"quests": map[string]any{
							"10005": map[string]any{
								"qd": float64(10005), "n": "Mision 5", "lv": float64(25), "requiredLevel": float64(22),
								"accessibleNow": true, "source": "questie",
								"starter": map[string]any{"n": "Encargado", "id": float64(1005), "m": float64(1429), "x": 40.1, "y": 55.5},
								"rw":      map[string]any{"ch": []any{float64(5005)}, "fixed": []any{float64(6005)}},
								"zone":    "Elwynn",
							},
						},
					},
					"gearCandidates": map[string]any{
						"available": true,
						"it": map[string]any{
							"3005": map[string]any{
								"i": float64(3005), "name": "Peto de prueba", "itemLevel": float64(40),
								"stats":   map[string]any{"ITEM_MOD_STRENGTH_SHORT": float64(5)},
								"sources": []any{map[string]any{"kind": "quest", "qd": float64(10005), "choice": true}},
							},
						},
					},
					"questieStatus": map[string]any{"available": true, "version": "12.10.0", "quests": float64(27)},
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
	qw, _ := cs.Sections["questWindow"].(map[string]any)
	quests, _ := qw["quests"].(map[string]any)
	q, _ := quests["10005"].(map[string]any)
	if q == nil {
		t.Fatalf("questWindow = %v: the quest 10005 is missing", qw)
	}
	if q["questID"] != float64(10005) || q["name"] != "Mision 5" || q["level"] != float64(25) {
		t.Fatalf("quest = %v: the short keys must come back as the long names", q)
	}
	if q["requiredLevel"] != float64(22) || q["zone"] != "Elwynn" || q["accessibleNow"] != true {
		t.Fatalf("quest = %v: the keys that are not short must stay as they are", q)
	}
	starter, _ := q["starter"].(map[string]any)
	if starter["name"] != "Encargado" || starter["mapID"] != float64(1429) || starter["id"] != float64(1005) {
		t.Fatalf("starter = %v", starter)
	}
	rewards, _ := q["rewards"].(map[string]any)
	if choice, _ := rewards["choice"].([]any); len(choice) != 1 || choice[0] != float64(5005) {
		t.Fatalf("rewards = %v, want the choice 5005", rewards)
	}
	cw, _ := cs.Sections["gearCandidates"].(map[string]any)
	items, _ := cw["items"].(map[string]any)
	item, _ := items["3005"].(map[string]any)
	if item == nil || item["itemID"] != float64(3005) || item["itemLevel"] != float64(40) {
		t.Fatalf("gearCandidates = %v: the item must come back with itemID and itemLevel", cw)
	}
	if stats, _ := item["stats"].(map[string]any); stats["ITEM_MOD_STRENGTH_SHORT"] != float64(5) {
		t.Fatalf("stats = %v: the stat names must stay", stats)
	}
	src, _ := item["sources"].([]any)
	if len(src) != 1 || src[0].(map[string]any)["questID"] != float64(10005) {
		t.Fatalf("sources = %v: the source must carry questID", src)
	}
	qs, _ := cs.Sections["questieStatus"].(map[string]any)
	if qs["available"] != true || qs["version"] != "12.10.0" || qs["quests"] != float64(27) {
		t.Fatalf("questieStatus = %v: the summary is stored as it is", qs)
	}
}
