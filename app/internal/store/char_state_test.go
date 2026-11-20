package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

func openCharStateStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// The newest copy of a section wins, whatever the order of arrival: a sheet that arrives late with an
// older time does not replace the one the store has.
func TestCharStateKeepsTheNewestSection(t *testing.T) {
	st := openCharStateStore(t)
	if _, err := st.IngestMessage(bridge.MsgSheet, []byte(`{"level":20,"class":"WARRIOR"}`), "G-Reino", time.Unix(200, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.IngestMessage(bridge.MsgSheet, []byte(`{"level":18,"class":"WARRIOR"}`), "G-Reino", time.Unix(100, 0)); err != nil {
		t.Fatal(err)
	}
	cs, err := st.CharState("G-Reino")
	if err != nil {
		t.Fatal(err)
	}
	sheet, _ := cs.Sections["sheet"].(map[string]any)
	if sheet["level"] != float64(20) {
		t.Fatalf("sheet = %v, want the level 20 one", sheet)
	}
	if !cs.Latest.Equal(time.Unix(200, 0)) {
		t.Fatalf("latest = %v", cs.Latest)
	}
	keys, err := st.CharKeysState()
	if err != nil {
		t.Fatal(err)
	}
	if !keys["G-Reino"].Equal(time.Unix(200, 0)) {
		t.Fatalf("keys = %v", keys)
	}
}

// The outbox of schema 2 writes the state with the time each item was learned; the legacy blocks of
// schema 1 write none (the readers take them last).
func TestCharStateFromOutboxAndNotFromLegacyBlocks(t *testing.T) {
	st := openCharStateStore(t)
	sv := &bridge.SavedVars{Raw: map[string]any{
		"schema": float64(2),
		"characters": map[string]any{
			"G-Reino": map[string]any{
				"updatedAt": float64(100),
				"sheet":     map[string]any{"level": float64(9), "class": "WARRIOR"},
			},
		},
		"outbox": map[string]any{"items": []any{
			map[string]any{"seq": float64(1), "msgType": float64(12), "at": float64(1000), "payload": map[string]any{
				"p": []any{"sheet", float64(20777)}, "oseq": float64(1), "v": map[string]any{"lv": float64(21), "class": "WARRIOR"},
			}},
		}},
	}}
	if _, err := st.IngestSavedVars(sv, time.Unix(5000, 0)); err != nil {
		t.Fatal(err)
	}
	cs, err := st.CharState("G-Reino")
	if err != nil {
		t.Fatal(err)
	}
	sheet, _ := cs.Sections["sheet"].(map[string]any)
	if sheet["level"] != float64(21) {
		t.Fatalf("sheet = %v, want the outbox record (level 21, lv expanded)", sheet)
	}
	if !cs.Latest.Equal(time.Unix(1000, 0)) {
		t.Fatalf("the outbox item is stamped with the time it was learned: %v", cs.Latest)
	}
	if len(cs.Sections) != 1 {
		t.Fatalf("sections = %v: the legacy blocks must not write the state", cs.Sections)
	}
}
