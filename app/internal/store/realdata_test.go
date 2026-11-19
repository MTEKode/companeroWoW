package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// TestRealSavedVariablesReport ingests a COPY of a real SavedVariables file into a database and
// reports its size, the rows per table and a few queries. It runs only when COMPANERO_SV_COPY names
// the copy (never the original); COMPANERO_DB_OUT optionally names the database file to write.
//
//	COMPANERO_SV_COPY=<copy of CompaneroWoW.lua> go test ./internal/store -run RealSavedVariables -v
func TestRealSavedVariablesReport(t *testing.T) {
	src := os.Getenv("COMPANERO_SV_COPY")
	if src == "" {
		t.Skip("COMPANERO_SV_COPY not set")
	}
	dbPath := os.Getenv("COMPANERO_DB_OUT")
	if dbPath == "" {
		dbPath = filepath.Join(t.TempDir(), "real.db")
	}
	info, err := os.Stat(src)
	if err != nil {
		t.Fatalf("stat copy: %v", err)
	}
	t.Logf("SavedVariables copy: %d bytes", info.Size())

	start := time.Now()
	sv, err := bridge.LoadSavedVariables(src)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Logf("evaluated in %v", time.Since(start).Round(time.Millisecond))

	_ = os.Remove(dbPath)
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	start = time.Now()
	imp, err := s.ImportSavedVariables(sv, time.Now())
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	t.Logf("imported in %v (schema %d, sha256 %s): %+v", time.Since(start).Round(time.Millisecond), imp.Schema, imp.SHA256, imp.Stats)
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	for _, table := range []string{"characters", "character_snapshots", "quests", "quest_log", "quest_objectives",
		"quest_rewards", "npcs", "npc_roles", "vendors", "vendor_items", "trainers", "trainer_services",
		"flight_nodes", "flight_routes", "items", "item_stats", "consumables", "equipment_history",
		"gear_candidates", "combats", "combat_spells", "combat_taken", "deaths", "sessions", "macros",
		"action_bars", "dungeons", "dungeon_bosses", "dungeon_loot", "maps"} {
		t.Logf("rows %-20s %d", table, count(t, s, table))
	}
	var pages, pageSize int64
	_ = s.db.QueryRow(`PRAGMA page_count`).Scan(&pages)
	_ = s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
	t.Logf("database file: %d bytes (%d pages of %d)", pages*pageSize, pages, pageSize)
	if fi, err := os.Stat(dbPath); err == nil {
		t.Logf("database file on disk: %d bytes", fi.Size())
	}

	ctx := context.Background()
	for _, q := range []string{
		`SELECT personaje, nombre, nivel, dinero_cobre FROM v_personaje`,
		`SELECT zona, COUNT(*) AS combates, ROUND(AVG(dps), 1) AS dps_medio FROM v_combates GROUP BY zona ORDER BY combates DESC`,
		`SELECT mision_id, titulo, nivel, entrega_npc, entrega_zona FROM v_misiones_conocidas WHERE nivel BETWEEN 1 AND 10 ORDER BY nivel, mision_id`,
	} {
		res, err := s.QueryReadOnly(ctx, q, 3)
		if err != nil {
			t.Logf("query %q: %v", q, err)
			continue
		}
		b, _ := json.Marshal(res)
		t.Logf("query: %s\n  -> %s", q, b)
	}
}
