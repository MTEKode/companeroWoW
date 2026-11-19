package gamedata

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

func openIngestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// serviceOver builds a service whose SavedVariables is the given fixture, with the store attached.
func serviceOver(t *testing.T, st *store.Store, fixture string) *Service {
	t.Helper()
	w := bridge.NewSVWatcher(filepath.Join("..", "..", "testdata", fixture), time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll %s: %v", fixture, err)
	}
	return NewService(nil, w, st)
}

// The combat history comes from the store first: a schema 2 file has no combat block, yet the
// combat the outbox carried is read back.
func TestCombatsReadFromStoreWhenSavedVariablesHaveNone(t *testing.T) {
	st := openIngestStore(t)
	g := serviceOver(t, st, "sv_schema2.lua")
	g.importKnowledge(g.watcher.Current())

	res, err := g.CombatReview(10, false, false)
	if err != nil {
		t.Fatalf("CombatReview: %v", err)
	}
	if n := combatCount(res); n != 1 {
		t.Fatalf("combats from the store = %d, want 1 (%v)", n, res)
	}
}

// Without a store the SavedVariables combats are read as before.
func TestCombatsFallBackToSavedVariables(t *testing.T) {
	g := serviceOver(t, nil, "sv_schema1.lua")
	res, err := g.CombatReview(10, false, false)
	if err != nil {
		t.Fatalf("CombatReview: %v", err)
	}
	if n := combatCount(res); n != 2 {
		t.Fatalf("combats from SavedVariables = %d, want 2", n)
	}
}

// The world block is read from the store: the vendor of the outbox delta is there.
func TestWorldReadsFromStore(t *testing.T) {
	st := openIngestStore(t)
	g := serviceOver(t, st, "sv_schema2.lua")
	g.importKnowledge(g.watcher.Current())

	w, ok := g.loadWorld()
	if !ok {
		t.Fatal("no world block")
	}
	if _, found := w.Vendors[901]; !found {
		t.Fatalf("vendor 901 from the outbox missing; vendors = %v", w.Vendors)
	}
}

// Channel messages are stored by the worker, without blocking the callback.
func TestQueueChannelStoresMessages(t *testing.T) {
	st := openIngestStore(t)
	g := NewService(nil, nil, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.StartIngest(ctx)

	body := []byte(`{"combat":{"id":11,"startedAt":1795000000,"duration":3,"zone":"Elwynn","damageDone":{"total":5,"spells":[]}}}`)
	g.QueueChannel(bridge.MsgCombat, body, time.Now())
	// A type the store does not keep is dropped without a queue entry.
	g.QueueChannel(bridge.MsgMeta, []byte(`{}`), time.Now())

	deadline := time.Now().Add(3 * time.Second)
	for {
		res, err := st.QueryReadOnly(context.Background(), "SELECT combate_id FROM v_combates WHERE combate_id = 11", 0)
		if err == nil && len(res.Rows) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the channel message was not stored")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if g.IngestDropped() != 0 {
		t.Fatalf("dropped = %d", g.IngestDropped())
	}
}

func TestMergeCombatsKeepsNewestUnique(t *testing.T) {
	stored := []any{
		map[string]any{"id": float64(3)},
		map[string]any{"id": float64(1)},
	}
	live := []any{
		map[string]any{"id": float64(4)},
		map[string]any{"id": float64(3)}, // already stored: not repeated
	}
	got := mergeCombats(stored, live)
	if len(got) != 3 {
		t.Fatalf("merged %d combats, want 3", len(got))
	}
	ids := []float64{}
	for _, v := range got {
		ids = append(ids, numberOf(asMap(v)["id"]))
	}
	if ids[0] != 4 || ids[1] != 3 || ids[2] != 1 {
		t.Fatalf("order = %v, want [4 3 1]", ids)
	}
}

// combatCount returns the number of combats in a CombatReview result.
func combatCount(res any) int {
	switch list := asMap(res)["combates"].(type) {
	case []map[string]any:
		return len(list)
	case []any:
		return len(list)
	}
	return -1
}
