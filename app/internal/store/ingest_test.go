package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

var testNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.Local)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func loadFixture(t *testing.T, name string) *bridge.SavedVars {
	t.Helper()
	sv, err := bridge.LoadSavedVariables(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return sv
}

// count returns the rows of a table (or view) for a quick assertion.
func count(t *testing.T, s *Store, table string, where ...string) int {
	t.Helper()
	q := "SELECT COUNT(*) FROM " + table
	if len(where) > 0 {
		q += " WHERE " + where[0]
	}
	var n int
	if err := s.db.QueryRow(q).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// dump is a deterministic text of every base table, to prove a second ingestion changes nothing.
func dump(t *testing.T, s *Store) string {
	t.Helper()
	var b strings.Builder
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	for _, tb := range tables {
		if tb == "messages" || tb == "cache" || tb == "ingest_log" {
			continue
		}
		r, err := s.db.Query(`SELECT * FROM ` + tb)
		if err != nil {
			t.Fatalf("dump %s: %v", tb, err)
		}
		cols, _ := r.Columns()
		fmt.Fprintf(&b, "== %s\n", tb)
		var lines []string
		for r.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			_ = r.Scan(ptrs...)
			lines = append(lines, fmt.Sprint(vals...))
		}
		r.Close()
		// The rows come back in no fixed order between two reads of the same table only if the table
		// changed; sort them so the comparison is about content.
		sortStrings(lines)
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

func sortStrings(v []string) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

func TestIngestSchema1Fixture(t *testing.T) {
	s := openTestStore(t)
	sv := loadFixture(t, "sv_schema1.lua")
	st, err := s.IngestSavedVars(sv, testNow)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if st.Failed != 0 {
		t.Fatalf("failed items: %+v", st)
	}

	checks := map[string][2]any{}
	add := func(name string, query string, want int) {
		checks[name] = [2]any{query, want}
	}
	add("characters", `SELECT COUNT(*) FROM characters WHERE char_key = 'Fixture-Realm' AND level = 12 AND money = 25000`, 1)
	add("snapshots", `SELECT COUNT(*) FROM character_snapshots WHERE char_key = 'Fixture-Realm'`, 1)
	add("quest_log", `SELECT COUNT(*) FROM quest_log WHERE complete = 1 OR complete = 0`, 2)
	add("quest_complete", `SELECT COUNT(*) FROM quest_log WHERE quest_id = 1002 AND complete = 1`, 1)
	add("objectives", `SELECT COUNT(*) FROM quest_objectives WHERE fulfilled = 3 AND required = 8`, 1)
	add("completed", `SELECT COUNT(*) FROM completed_quests`, 3)
	add("equipment_history", `SELECT COUNT(*) FROM equipment_history`, 2)
	add("item_name_from_gear", `SELECT COUNT(*) FROM items WHERE item_id = 12345 AND name = 'Yelmo de prueba' AND equip_loc = 'INVTYPE_HEAD'`, 1)
	add("weapon_damage", `SELECT COUNT(*) FROM items WHERE item_id = 54321 AND weapon_min = 5 AND weapon_max = 9`, 1)
	add("item_stats", `SELECT COUNT(*) FROM item_stats WHERE item_id = 12345 AND value = 9`, 1)
	add("combats", `SELECT COUNT(*) FROM combats WHERE char_key = 'Fixture-Realm'`, 2)
	add("combat_spells", `SELECT COUNT(*) FROM combat_spells WHERE combat_id = 1 AND kind = 'dmg' AND hits = 3`, 1)
	add("combat_taken", `SELECT COUNT(*) FROM combat_taken`, 2)
	add("deaths", `SELECT COUNT(*) FROM deaths WHERE combat_id = 2 AND killer = 'Jabali'`, 1)
	add("action_bars", `SELECT COUNT(*) FROM action_bars WHERE char_key = 'Fixture-Realm' AND item_id = 78`, 1)
	add("macros", `SELECT COUNT(*) FROM macros`, 2)
	add("sessions_addon", `SELECT COUNT(*) FROM sessions WHERE char_key = 'Fixture-Realm'`, 2)
	add("session_current_open", `SELECT COUNT(*) FROM sessions WHERE char_key = 'Fixture-Realm' AND open = 1 AND end_level = 12`, 1)
	add("knowledge_quest", `SELECT COUNT(*) FROM quests WHERE quest_id = 127 AND giver_name = 'Guardia'`, 1)
	add("quest_rewards", `SELECT COUNT(*) FROM quest_rewards WHERE quest_id = 127`, 3)
	add("npc_roles", `SELECT COUNT(*) FROM npc_roles WHERE npc_id = 900`, 2)
	add("flight_nodes", `SELECT COUNT(*) FROM flight_nodes`, 2)
	add("flight_route", `SELECT COUNT(*) FROM flight_routes WHERE from_id = 10 AND to_id = 11 AND seconds = 45`, 1)
	add("flight_known", `SELECT COUNT(*) FROM flight_known WHERE char_key = 'Fixture-Realm'`, 2)
	add("vendor_items", `SELECT COUNT(*) FROM vendor_items WHERE npc_id = 900 AND item_id = 2592`, 1)
	add("trainer_services", `SELECT COUNT(*) FROM trainer_services WHERE npc_id = 950 AND cost = 100`, 1)
	add("quest_chain", `SELECT COUNT(*) FROM quest_chains WHERE quest_id = 1002 AND next_id = 1003`, 1)
	add("consumable", `SELECT COUNT(*) FROM consumables WHERE item_id = 4601 AND health = 100`, 1)
	add("maps", `SELECT COUNT(*) FROM maps WHERE map_id = 1429`, 1)

	for name, c := range checks {
		var n int
		if err := s.db.QueryRow(c[0].(string)).Scan(&n); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if n != c[1].(int) {
			t.Errorf("%s: got %d rows, want %d", name, n, c[1])
		}
	}
	// The views read what was ingested.
	for view, want := range map[string]int{"v_combates": 2, "v_muertes": 1, "v_macros": 2, "v_mazmorras": 0, "v_vuelos": 1} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + view).Scan(&n); err != nil {
			t.Errorf("view %s: %v", view, err)
			continue
		}
		if n != want {
			t.Errorf("view %s: %d rows, want %d", view, n, want)
		}
	}
}

func TestIngestSchema2OutboxIsIdempotent(t *testing.T) {
	s := openTestStore(t)
	sv := loadFixture(t, "sv_schema2.lua")
	first, err := s.IngestSavedVars(sv, testNow)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if first.Applied == 0 || first.Failed != 0 || first.Skipped != 0 {
		t.Fatalf("first ingest stats: %+v", first)
	}
	before := dump(t, s)

	second, err := s.IngestSavedVars(sv, testNow)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.Skipped != 4 {
		t.Errorf("second ingest skipped %d outbox items, want 4 (%+v)", second.Skipped, second)
	}
	if after := dump(t, s); after != before {
		t.Errorf("second ingestion changed the tables")
	}

	// The outbox content landed on the active character.
	if n := count(t, s, "combats", "char_key = 'Fixture-Realm' AND combat_id = 7"); n != 1 {
		t.Errorf("combat from outbox: %d rows", n)
	}
	if n := count(t, s, "completed_quests", "char_key = 'Fixture-Realm' AND quest_id = 127"); n != 1 {
		t.Errorf("completed quest from outbox: %d rows", n)
	}
	if n := count(t, s, "vendor_items", "npc_id = 901 AND item_id = 4604"); n != 1 {
		t.Errorf("vendor item from world delta: %d rows", n)
	}
	if n := count(t, s, "quests", "quest_id = 127"); n != 1 {
		t.Errorf("knowledge quest from outbox: %d rows", n)
	}
}

func TestIngestMessageSameOseqTwice(t *testing.T) {
	s := openTestStore(t)
	body := []byte(`{"oseq":42,"combat":{"id":5,"startedAt":1795000000,"duration":4,"zone":"Dun Morogh",` +
		`"damageDone":{"total":10,"spells":[]},"healingDone":{"total":0,"spells":[]},"damageTaken":{"total":0,"sources":[]}}}`)
	first, err := s.IngestMessage(bridge.MsgCombat, body, "Fixture-Realm", testNow)
	if err != nil || first.Applied != 1 {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := s.IngestMessage(bridge.MsgCombat, body, "Fixture-Realm", testNow)
	if err != nil || second.Skipped != 1 || second.Applied != 0 {
		t.Fatalf("second: %+v %v", second, err)
	}
	if n := count(t, s, "combats"); n != 1 {
		t.Fatalf("combats = %d, want 1", n)
	}
	// The same oseq with a different body is new data (a reset addon outbox), so it is applied.
	other := []byte(`{"oseq":42,"combat":{"id":6,"startedAt":1795000001,"duration":4,"zone":"Dun Morogh"}}`)
	third, err := s.IngestMessage(bridge.MsgCombat, other, "Fixture-Realm", testNow)
	if err != nil || third.Applied != 1 {
		t.Fatalf("third: %+v %v", third, err)
	}
	if n := count(t, s, "combats"); n != 2 {
		t.Fatalf("combats = %d, want 2", n)
	}
}

func TestIngestMessageWithoutOseqUpsertsByNaturalKey(t *testing.T) {
	s := openTestStore(t)
	body := []byte(`{"combat":{"id":9,"startedAt":1795000000,"duration":4,"zone":"A","damageDone":{"total":1,"spells":[]}}}`)
	for i := 0; i < 2; i++ {
		if _, err := s.IngestMessage(bridge.MsgCombat, body, "Fixture-Realm", testNow); err != nil {
			t.Fatalf("ingest %d: %v", i, err)
		}
	}
	if n := count(t, s, "combats"); n != 1 {
		t.Fatalf("combats = %d, want 1", n)
	}
}

func TestIngestCompletedQuestsListBody(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.IngestMessage(bridge.MsgCompletedQuests, []byte(`[1,2,3]`), "Fixture-Realm", testNow); err != nil {
		t.Fatalf("full list: %v", err)
	}
	if _, err := s.IngestMessage(bridge.MsgCompletedQuests, []byte(`{"add":[4]}`), "Fixture-Realm", testNow); err != nil {
		t.Fatalf("add: %v", err)
	}
	if n := count(t, s, "completed_quests"); n != 4 {
		t.Fatalf("completed = %d, want 4", n)
	}
	if _, err := s.IngestMessage(bridge.MsgCompletedQuests, []byte(`[9]`), "Fixture-Realm", testNow); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if n := count(t, s, "completed_quests"); n != 1 {
		t.Fatalf("after replace = %d, want 1", n)
	}
}

func TestMigrationOnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	// A database as the app left it at schema version 4 (before the normalized tables), with data.
	for v := 0; v < 4; v++ {
		for _, stmt := range migrations[v] {
			if _, err := raw.Exec(stmt); err != nil {
				t.Fatalf("migration %d: %v", v+1, err)
			}
		}
	}
	if _, err := raw.Exec(`PRAGMA user_version = 4`); err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO quests (quest_id, title, first_seen, last_seen) VALUES (127, 'Antigua', 1, 2)`); err != nil {
		t.Fatalf("seed quest: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO messages (role, content, created_at) VALUES ('user', 'hola', 1)`); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO sessions (started_at) VALUES (1795000000)`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	_ = raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open migrated: %v", err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != latestVersion {
		t.Fatalf("user_version = %d (%v), want %d", version, err, latestVersion)
	}
	q, err := s.GetQuest(127)
	if err != nil || q == nil || q.Title != "Antigua" {
		t.Fatalf("quest after migration: %+v %v", q, err)
	}
	msgs, err := s.RecentMessages(5)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("messages after migration: %v %v", msgs, err)
	}
	if n := count(t, s, "sessions", "char_key = ''"); n != 1 {
		t.Fatalf("old session lost its default key: %d", n)
	}
	for _, v := range viewNames {
		if n := count(t, s, "sqlite_master", "type = 'view' AND name = '"+v+"'"); n != 1 {
			t.Errorf("view %s missing after migration", v)
		}
	}
	// A second Open applies nothing.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = s2.Close()
}

func TestRetentionKeepsNewestCombats(t *testing.T) {
	s := openTestStore(t)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	const total = maxCombats + 5
	for i := 1; i <= total; i++ {
		if _, err := tx.Exec(`INSERT INTO combats (char_key, combat_id, started_at) VALUES ('C', ?, ?)`, i, 1795000000+i); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
		if _, err := tx.Exec(`INSERT INTO combat_spells (char_key, combat_id, kind, spell_id, name) VALUES ('C', ?, 'dmg', 1, 'x')`, i); err != nil {
			t.Fatalf("spell %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	n, snaps, err := s.trimHistory(testNow)
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	if n != 5 || snaps != 0 {
		t.Fatalf("trimmed %d combats and %d snapshots, want 5 and 0", n, snaps)
	}
	if got := count(t, s, "combats"); got != maxCombats {
		t.Fatalf("combats kept = %d, want %d", got, maxCombats)
	}
	if got := count(t, s, "combat_spells"); got != maxCombats {
		t.Fatalf("spells kept = %d, want %d", got, maxCombats)
	}
	if got := count(t, s, "combats", "combat_id = 1"); got != 0 {
		t.Fatalf("the oldest combat was kept")
	}
	var newest int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM combats WHERE combat_id = ?`, total).Scan(&newest); err != nil || newest != 1 {
		t.Fatalf("the newest combat was removed")
	}
}

func TestRetentionDropsOldSnapshots(t *testing.T) {
	s := openTestStore(t)
	old := testNow.AddDate(-2, 0, 0).Unix()
	recent := testNow.AddDate(0, -1, 0).Unix()
	if _, err := s.db.Exec(`INSERT INTO character_snapshots (char_key, day, taken_at) VALUES ('C', 'old', ?), ('C', 'recent', ?)`, old, recent); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, snaps, err := s.trimHistory(testNow)
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	if snaps != 1 || count(t, s, "character_snapshots") != 1 {
		t.Fatalf("snapshots removed %d, kept %d", snaps, count(t, s, "character_snapshots"))
	}
}

func TestViewsDocumentedInVIEWSMD(t *testing.T) {
	doc := ViewsDoc()
	if doc == "" {
		t.Fatal("VIEWS.md is empty or not embedded")
	}
	for _, v := range viewNames {
		if !strings.Contains(doc, v) {
			t.Errorf("VIEWS.md does not document %s", v)
		}
	}
}

func TestViewsContentOnFixtures(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.IngestSavedVars(loadFixture(t, "sv_schema1.lua"), testNow); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	ctx := context.Background()
	res, err := s.QueryReadOnly(ctx, `SELECT personaje, nombre, nivel FROM v_personaje`, 0)
	if err != nil || len(res.Rows) != 1 || res.Rows[0][2] != int64(12) {
		t.Fatalf("v_personaje: %+v %v", res, err)
	}
	res, err = s.QueryReadOnly(ctx, `SELECT mision_id, estado FROM v_misiones WHERE personaje = 'Fixture-Realm' ORDER BY mision_id`, 0)
	if err != nil || len(res.Rows) != 2 || res.Rows[1][1] != "lista_para_entregar" {
		t.Fatalf("v_misiones: %+v %v", res, err)
	}
	res, err = s.QueryReadOnly(ctx, `SELECT origen, destino, segundos FROM v_vuelos`, 0)
	if err != nil || len(res.Rows) != 1 || res.Rows[0][2] != 45.0 {
		t.Fatalf("v_vuelos: %+v %v", res, err)
	}
	res, err = s.QueryReadOnly(ctx, `SELECT nombre, estadisticas FROM v_objetos WHERE objeto_id = 12345`, 0)
	if err != nil || len(res.Rows) != 1 || !strings.Contains(fmt.Sprint(res.Rows[0][1]), "ITEM_MOD_STAMINA_SHORT") {
		t.Fatalf("v_objetos: %+v %v", res, err)
	}
	res, err = s.QueryReadOnly(ctx, `SELECT tipo, hechizo, cantidad FROM v_combate_hechizos`, 0)
	if err != nil || len(res.Rows) != 1 || res.Rows[0][0] != "dano" {
		t.Fatalf("v_combate_hechizos: %+v %v", res, err)
	}
	res, err = s.QueryReadOnly(ctx, `SELECT vedor FROM v_vuelos`, 0)
	if err == nil {
		t.Fatalf("a misspelled column was accepted: %+v", res)
	}
	res, err = s.QueryReadOnly(ctx, `SELECT actual, objeto FROM v_equipo WHERE actual = 1 ORDER BY ranura_id`, 0)
	if err != nil || len(res.Rows) != 2 {
		t.Fatalf("v_equipo: %+v %v", res, err)
	}
}

// The lean outbox written by the addon's own encoder (testdata/gen_outbox_fixture.lua): payloads
// { oseq, p, v } with short keys, which the ingestion expands and nests by path.
func TestOutboxLeanFixtureFromAddonEncoder(t *testing.T) {
	s := openTestStore(t)
	sv := loadFixture(t, "sv_outbox_lean.lua")
	first, err := s.IngestSavedVars(sv, testNow)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if first.Failed != 0 || first.Applied != 11 || first.Skipped != 0 {
		t.Fatalf("first ingest: %+v, want 11 applied (10 outbox items and the character)", first)
	}
	checks := []struct {
		name, query string
		want        int
	}{
		{"quest title expanded", `SELECT COUNT(*) FROM quests WHERE quest_id = 127 AND title = 'Mision de prueba' AND level = 3`, 1},
		{"quest giver expanded", `SELECT COUNT(*) FROM quests WHERE quest_id = 127 AND giver_name = 'Guardia' AND turnin_npc_id = 901`, 1},
		{"quest coordinates rounded", `SELECT COUNT(*) FROM quests WHERE quest_id = 127 AND giver_x = 0.501`, 1},
		{"quest rewards from rw/it/mo/xp", `SELECT COUNT(*) FROM quest_rewards WHERE quest_id = 127`, 3},
		{"choice reward", `SELECT COUNT(*) FROM quest_rewards WHERE quest_id = 127 AND kind = 'choice' AND item_id = 6948 AND amount = 1`, 1},
		{"npc expanded", `SELECT COUNT(*) FROM npcs WHERE npc_id = 900 AND name = 'Guardia' AND is_vendor = 1 AND is_quest_giver = 1`, 1},
		{"npc roles", `SELECT COUNT(*) FROM npc_roles WHERE npc_id = 900`, 2},
		{"combat keyed by path", `SELECT COUNT(*) FROM combats WHERE char_key = 'Fixture-Realm' AND combat_id IN (1, 2)`, 2},
		{"combat spell name expanded from n", `SELECT COUNT(*) FROM combat_spells WHERE combat_id = 1 AND name = 'Golpe heroico' AND hits = 3`, 1},
		{"damage taken source from n", `SELECT COUNT(*) FROM combat_taken WHERE combat_id = 1 AND source = 'Lobo'`, 1},
		{"death from the record", `SELECT COUNT(*) FROM deaths WHERE combat_id = 2 AND killer = 'Jabali'`, 1},
		{"vendor nested by path", `SELECT COUNT(*) FROM vendor_items WHERE npc_id = 901 AND item_id = 4604 AND price = 10`, 1},
		{"flight nodes by faction path", `SELECT COUNT(*) FROM flight_nodes WHERE node_id IN (10, 11) AND faction = 'Alliance'`, 2},
		{"flight route key", `SELECT COUNT(*) FROM flight_routes WHERE from_id = 10 AND to_id = 11 AND seconds = 45 AND copper = 120`, 1},
		{"known flights by character path", `SELECT COUNT(*) FROM flight_known WHERE char_key = 'Fixture-Realm'`, 2},
		{"sheet day from the path", `SELECT COUNT(*) FROM character_snapshots WHERE day = '2024-10-04' AND sheet_json LIKE '%stamina%'`, 1},
		{"character from the block", `SELECT COUNT(*) FROM characters WHERE char_key = 'Fixture-Realm' AND level = 13`, 1},
		{"knowledge last_seen filled", `SELECT COUNT(*) FROM quests WHERE quest_id = 127 AND last_seen > 0`, 1},
	}
	for _, c := range checks {
		var n int
		if err := s.db.QueryRow(c.query).Scan(&n); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if n != c.want {
			t.Errorf("%s: %d, want %d", c.name, n, c.want)
		}
	}
	before := dump(t, s)
	second, err := s.IngestSavedVars(sv, testNow)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.Skipped != 10 || second.Applied != 1 {
		t.Fatalf("second ingest: %+v, want 10 skipped (the oseq items) and the character applied", second)
	}
	if dump(t, s) != before {
		t.Fatal("reading the same outbox again changed the tables")
	}
}

// An outbox payload that arrives on the channel is decoded the same way, and its oseq makes it
// idempotent: the second copy (a later share window) is skipped.
func TestChannelOutboxPayloadIsIdempotentByOseq(t *testing.T) {
	s := openTestStore(t)
	body := []byte(`{"oseq":77,"p":["combats",9],"v":{"id":9,"startedAt":1795000900,"duration":4,"zone":"Dun Morogh",` +
		`"damageDone":{"total":12,"spells":[{"n":"Disparo","spellID":2764,"amount":12,"hits":2}]}}}`)
	first, err := s.IngestMessage(bridge.MsgCombat, body, "Fixture-Realm", testNow)
	if err != nil || first.Applied != 1 {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := s.IngestMessage(bridge.MsgCombat, body, "Fixture-Realm", testNow)
	if err != nil || second.Skipped != 1 {
		t.Fatalf("second: %+v %v", second, err)
	}
	if n := count(t, s, "combat_spells", "combat_id = 9 AND name = 'Disparo' AND amount = 12"); n != 1 {
		t.Fatalf("spell expanded from the channel payload: %d rows", n)
	}
}

// Every example query of VIEWS.md runs (against an ingested fixture) and passes the validator.
func TestViewsDocExamplesRun(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.IngestSavedVars(loadFixture(t, "sv_schema1.lua"), testNow); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	re := regexp.MustCompile("Ejemplo: `([^`]+)`")
	matches := re.FindAllStringSubmatch(ViewsDoc(), -1)
	if len(matches) < len(viewNames) {
		t.Fatalf("VIEWS.md has %d examples, want at least one per view (%d)", len(matches), len(viewNames))
	}
	for _, m := range matches {
		if _, err := s.QueryReadOnly(context.Background(), m[1], 0); err != nil {
			t.Errorf("example %q: %v", m[1], err)
		}
	}
}
