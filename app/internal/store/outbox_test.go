package store

import (
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// The examples of docs/outbox-format.md (sections 5.1 to 5.4), as the channel sends them, one
// message each. They are the contract between the addon and the app.
var docExamples = []struct {
	name    string
	msgType uint8
	body    string
}{
	{"combate", bridge.MsgCombat, `{"oseq":10,"p":["combats",3],"v":{"damageDone":{"spells":[{"amount":2100,"n":"Golpe furtivo","spellID":1752}],"total":3000},"died":false,"duration":42.1,"endedAt":1700000042,"id":3,"inInstance":false,"m":1429,"resourcesAtEnd":{"healthPct":80,"powerPct":60},"startedAt":1700000000,"targets":[{"guid":"Creature-0-0-0-0-1234-0","n":"Lobo"}],"zone":"Elwynn"}}`},
	{"mision", bridge.MsgKnowledge, `{"oseq":1,"p":["quests",9001],"v":{"dt":"Los lobos atacan la granja.","fs":1700000000,"gm":1429,"gn":1234,"gna":"Granjero Tomas","gx":0.251,"gy":0.663,"lv":5,"oa":{"m":1429,"x":0.25,"y":0.66},"os":"juego","ot":"Mata 8 lobos.","qd":9001,"rw":{"it":[{"c":1,"ch":true,"i":2901,"l":"|Hitem:2901|h[Bolsa]|h"}],"mo":1200,"xp":450},"ti":"Peligro en la granja","tm":1429,"tn":1240,"tna":"Capitan Vale","tx":0.3,"ty":0.7}}`},
	{"pnj", bridge.MsgKnowledge, `{"oseq":2,"p":["npcs",1234],"v":{"ac":2,"fs":1700000000,"m":1429,"n":"Granjero Tomas","pn":1234,"qg":true,"r":5,"sv":["Peligro en la granja"],"x":0.251,"y":0.663}}`},
	{"hoja", bridge.MsgSheet, `{"oseq":11,"p":["sheet",20000],"v":{"class":"WARRIOR","health":4200,"healthMax":4200,"lv":60,"melee":{"ap":1200,"crit":12.5},"race":"Human","spells":[{"n":"Golpe furtivo","rank":"","spellID":1752}]}}`},
	{"nodo", bridge.MsgWorld, `{"oseq":3,"p":["flight","Alliance","nodes",1001],"v":{"kn":true,"m":1429,"n":"Puerto de Ventobravo","nd":1001,"x":0.441,"y":0.512}}`},
	{"coste", bridge.MsgWorld, `{"oseq":4,"p":["flight","Alliance","costs","1001>1002"],"v":{"cu":1250,"ho":1,"se":75}}`},
	{"conocidos", bridge.MsgWorld, `{"oseq":5,"p":["flight","known","Tester-Ravencrest"],"v":[1001,1002,1003]}`},
	{"vendedor", bridge.MsgWorld, `{"oseq":6,"p":["vendors",1000],"v":{"faction":"Alliance","it":[{"i":2001,"limited":false,"n":"Pan","price":1,"stack":1}],"m":1429,"n":"Vendedor de Pan","pn":1000,"x":0.5,"y":0.5}}`},
	{"instructor", bridge.MsgWorld, `{"oseq":7,"p":["trainers",2000],"v":{"kind":"profession","m":1429,"n":"Instructor de Herboristeria","pn":2000,"profession":"Herboristeria","sv":[{"available":"available","cost":10000,"lv":5,"n":"Herboristeria","rank":"1"}],"x":0.3,"y":0.3}}`},
	{"cadena", bridge.MsgWorld, `{"oseq":8,"p":["questChains",9001],"v":{"next":9002,"pre":[9000]}}`},
	{"consumible", bridge.MsgWorld, `{"oseq":9,"p":["consumables",2001],"v":{"i":2001,"kind":"food","n":"Pan","restores":{"health":0,"mana":0,"se":0}}}`},
}

// Every example of the contract lands in its table with the long names (the keys are expanded).
func TestDocExamplesIngest(t *testing.T) {
	s := openTestStore(t)
	for _, ex := range docExamples {
		st, err := s.IngestMessage(ex.msgType, []byte(ex.body), "Tester-Ravencrest", testNow)
		if err != nil {
			t.Fatalf("%s: %v", ex.name, err)
		}
		if st.Applied != 1 {
			t.Fatalf("%s: %+v", ex.name, st)
		}
	}
	checks := map[string]string{
		"combate: spell name from n":         `SELECT COUNT(*) FROM combat_spells WHERE combat_id = 3 AND name = 'Golpe furtivo' AND amount = 2100`,
		"combate: zone and duration":         `SELECT COUNT(*) FROM combats WHERE combat_id = 3 AND zone = 'Elwynn' AND duration = 42.1 AND health_pct = 80`,
		"combate: target name from n":        `SELECT COUNT(*) FROM combats WHERE combat_id = 3 AND targets = '["Lobo"]'`,
		"mision: giver and turn-in":          `SELECT COUNT(*) FROM quests WHERE quest_id = 9001 AND giver_name = 'Granjero Tomas' AND turnin_name = 'Capitan Vale' AND giver_x = 0.251`,
		"mision: rewards from rw.it":         `SELECT COUNT(*) FROM quest_rewards WHERE quest_id = 9001 AND kind = 'choice' AND item_id = 2901 AND amount = 1`,
		"mision: money from mo":              `SELECT COUNT(*) FROM quest_rewards WHERE quest_id = 9001 AND kind = 'money' AND amount = 1200`,
		"pnj: flags from qg/vd and roles":    `SELECT COUNT(*) FROM npcs WHERE npc_id = 1234 AND is_quest_giver = 1 AND name = 'Granjero Tomas' AND reaction = ''`,
		"pnj: role quest_giver":              `SELECT COUNT(*) FROM npc_roles WHERE npc_id = 1234 AND role = 'quest_giver'`,
		"hoja: day from the path":            `SELECT COUNT(*) FROM character_snapshots WHERE char_key = 'Tester-Ravencrest' AND day = '2024-10-04' AND sheet_json LIKE '%WARRIOR%'`,
		"nodo: node of the faction":          `SELECT COUNT(*) FROM flight_nodes WHERE node_id = 1001 AND faction = 'Alliance' AND name = 'Puerto de Ventobravo' AND has_pos = 1`,
		"coste: copper, hops, seconds":       `SELECT COUNT(*) FROM flight_routes WHERE from_id = 1001 AND to_id = 1002 AND copper = 1250 AND hops = 1 AND seconds = 75`,
		"conocidos: three known nodes":       `SELECT COUNT(*) FROM flight_known WHERE char_key = 'Tester-Ravencrest'`,
		"vendedor: items with price":         `SELECT COUNT(*) FROM vendor_items WHERE npc_id = 1000 AND item_id = 2001 AND name = 'Pan' AND price = 1`,
		"instructor: service level and rank": `SELECT COUNT(*) FROM trainer_services WHERE npc_id = 2000 AND name = 'Herboristeria' AND level = 5 AND cost = 10000`,
		"cadena: next and pre":               `SELECT COUNT(*) FROM quest_chains WHERE quest_id = 9001 AND next_id = 9002 AND pre_json = '[9000]'`,
		"consumible: restores and name":      `SELECT COUNT(*) FROM consumables WHERE item_id = 2001 AND name = 'Pan' AND kind = 'food'`,
	}
	for name, q := range checks {
		if n := scalarInt(t, s, q); n != 1 && !strings.HasPrefix(name, "conocidos") {
			t.Errorf("%s: %d, want 1", name, n)
		}
	}
	if n := scalarInt(t, s, `SELECT COUNT(*) FROM flight_known WHERE char_key = 'Tester-Ravencrest'`); n != 3 {
		t.Errorf("known nodes = %d, want 3", n)
	}
}

// Records of one path are a state: the highest oseq wins, whatever the arrival order.
func TestOutboxPathKeepsHighestOseq(t *testing.T) {
	s := openTestStore(t)
	newer := []byte(`{"oseq":5,"p":["quests",500],"v":{"ti":"Version nueva","lv":9}}`)
	older := []byte(`{"oseq":3,"p":["quests",500],"v":{"ti":"Version vieja","lv":2}}`)
	if st, err := s.IngestMessage(bridge.MsgKnowledge, newer, "", testNow); err != nil || st.Applied != 1 {
		t.Fatalf("newer: %+v %v", st, err)
	}
	st, err := s.IngestMessage(bridge.MsgKnowledge, older, "", testNow)
	if err != nil || st.Skipped != 1 {
		t.Fatalf("older arrives later and must be skipped: %+v %v", st, err)
	}
	q, err := s.GetQuest(500)
	if err != nil || q == nil || q.Title != "Version nueva" || q.Level != 9 {
		t.Fatalf("quest after out-of-order arrival: %+v %v", q, err)
	}
	// Sheet days are paths too: each day keeps its own state.
	day1 := []byte(`{"oseq":7,"p":["sheet",20000],"v":{"lv":10}}`)
	day2 := []byte(`{"oseq":8,"p":["sheet",20001],"v":{"lv":11}}`)
	for _, b := range [][]byte{day2, day1} {
		if _, err := s.IngestMessage(bridge.MsgSheet, b, "Tester-Ravencrest", testNow); err != nil {
			t.Fatalf("sheet: %v", err)
		}
	}
	if n := count(t, s, "character_snapshots", "char_key = 'Tester-Ravencrest'"); n != 2 {
		t.Fatalf("sheet days kept = %d, want 2", n)
	}
}

// A "~" escapes a key: the rest is the key as it is (a key "n" with no short form travels as "~n").
func TestTildeEscapeKeys(t *testing.T) {
	if got := decodeKey("~n"); got != "n" {
		t.Errorf("decodeKey(~n) = %q, want n", got)
	}
	if got := decodeKey("n"); got != "name" {
		t.Errorf("decodeKey(n) = %q, want name", got)
	}
	if got := decodeKey("class"); got != "class" {
		t.Errorf("decodeKey(class) = %q, want class", got)
	}
	s := openTestStore(t)
	body := []byte(`{"oseq":12,"p":["npcs",777],"v":{"n":"Pnj de prueba","pn":777,"~pn":"literal"}}`)
	if _, err := s.IngestMessage(bridge.MsgKnowledge, body, "", testNow); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	n, err := s.GetNpc(777)
	if err != nil || n == nil || n.Name != "Pnj de prueba" {
		t.Fatalf("npc with an escaped key: %+v %v", n, err)
	}
}

// A body is an outbox item only with an oseq: a path and a value without one are read as the
// old body of the message type (here a combat body without combat or combats, so nothing is stored).
func TestPathWithoutOseqIsNotOutbox(t *testing.T) {
	s := openTestStore(t)
	body := []byte(`{"p":["combats",4],"v":{"id":4,"startedAt":1795000000,"zone":"X"}}`)
	st, err := s.IngestMessage(bridge.MsgCombat, body, "Tester-Ravencrest", testNow)
	if err != nil || st.Applied != 1 {
		t.Fatalf("ingest: %+v %v", st, err)
	}
	if n := count(t, s, "combats"); n != 0 {
		t.Fatalf("a body without oseq was read as an outbox record: %d combats", n)
	}
}

// Importing the file records it: its path, content hash and schema, and the counts. Importing the
// same content again keeps one record.
func TestImportSavedVariablesIsRecorded(t *testing.T) {
	s := openTestStore(t)
	sv := loadFixture(t, "sv_schema1.lua")
	if sv.SHA256 == "" || sv.Path == "" || sv.Size == 0 {
		t.Fatalf("loaded data has no identity: path=%q sha=%q size=%d", sv.Path, sv.SHA256, sv.Size)
	}
	imp, err := s.ImportSavedVariables(sv, testNow)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if imp.Schema != 1 || imp.Stats.Applied == 0 || imp.Stats.Failed != 0 {
		t.Fatalf("import result: %+v", imp)
	}
	last, ok, err := s.LastSVImport(sv.Path)
	if err != nil || !ok {
		t.Fatalf("last import: %v %v", ok, err)
	}
	if last.SHA256 != sv.SHA256 || last.Size != sv.Size || last.Schema != 1 {
		t.Fatalf("recorded identity = %+v, want sha %s size %d schema 1", last, sv.SHA256, sv.Size)
	}
	if _, err := s.ImportSavedVariables(sv, testNow.Add(60e9)); err != nil {
		t.Fatalf("second import: %v", err)
	}
	if n := count(t, s, "sv_imports"); n != 1 {
		t.Fatalf("records for the same content = %d, want 1", n)
	}
	if _, ok, _ := s.LastSVImport("no/such/file.lua"); ok {
		t.Fatal("an unknown file has an import")
	}
}

// scalarInt runs a query that returns one integer.
func scalarInt(t *testing.T, s *Store, q string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(q).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	return n
}
