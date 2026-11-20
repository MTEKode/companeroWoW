package gamedata

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// newStateService opens a store in a temp dir and a service that reads the SavedVariables file at path.
func newStateService(t *testing.T, path string) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return reopenService(t, path, st), st
}

// reopenService is a service over the store and the file at path, with the file read (as Start does).
func reopenService(t *testing.T, path string, st *store.Store) *Service {
	t.Helper()
	watcher := bridge.NewSVWatcherFunc(func() (string, error) { return path, nil }, 0)
	g := NewService(nil, watcher, st)
	if err := g.RefreshSavedVariables(); err != nil {
		t.Fatal(err)
	}
	return g
}

// writeSV writes a SavedVariables file with the given body (the table's fields, without the global).
func writeSV(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("CompaneroWoWDB = {\n"+body+"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// legacyChar is a schema-1 character table with a sheet of the given level and class.
func legacyChar(key string, updatedAt, loggedOutAt int, level int, class string) string {
	return `["` + key + `"] = {
	["character"] = { ["name"] = "X", ["realm"] = "Reino", ["level"] = ` + strconv.Itoa(level) + `, ["classFile"] = "` + class + `" },
	["updatedAt"] = ` + strconv.Itoa(updatedAt) + `,
	["loggedOutAt"] = ` + strconv.Itoa(loggedOutAt) + `,
	["sheet"] = { ["level"] = ` + strconv.Itoa(level) + `, ["class"] = "` + class + `" },
}`
}

func TestActiveCharacterIsTheOneWithTheNewestActivity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	writeSV(t, path, `["characters"] = {
`+legacyChar("A-Reino", 100, 150, 10, "MAGE")+`,
`+legacyChar("B-Reino", 200, 210, 20, "WARRIOR")+`,
`+legacyChar("C-Reino", 50, 0, 5, "PRIEST")+`
}`)
	g, st := newStateService(t, path)
	if got := g.ActiveCharacterKey(); got != "B-Reino" {
		t.Fatalf("active = %q, want B-Reino (newest updatedAt)", got)
	}

	// A logout later than the others' updates makes that character the active one.
	writeSV(t, path, `["characters"] = {
`+legacyChar("A-Reino", 100, 900, 10, "MAGE")+`,
`+legacyChar("B-Reino", 200, 210, 20, "WARRIOR")+`
}`)
	g2, _ := newStateService(t, path)
	if got := g2.ActiveCharacterKey(); got != "A-Reino" {
		t.Fatalf("active = %q, want A-Reino (newest loggedOutAt)", got)
	}

	// The store knows a newer state for C than the file knows for anyone: C is the active one.
	if _, err := st.IngestMessage(bridge.MsgSheet, []byte(`{"level":30,"class":"PALADIN"}`), "C-Reino", time.Unix(5000, 0)); err != nil {
		t.Fatal(err)
	}
	g3 := reopenService(t, path, st)
	if got := g3.ActiveCharacterKey(); got != "C-Reino" {
		t.Fatalf("active = %q, want C-Reino (newest stored state)", got)
	}
}

func TestStoredSectionsBeatLegacyBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	writeSV(t, path, `["characters"] = {
`+legacyChar("A-Reino", 100, 0, 10, "MAGE")+`
}`)
	g, st := newStateService(t, path)
	raw, _, ok := g.SheetJSON()
	if !ok || !strings.Contains(string(raw), `"level":10`) {
		t.Fatalf("without store state the legacy sheet is read: ok=%v raw=%s", ok, raw)
	}

	// The channel delivers a newer sheet: it wins over the legacy block.
	if _, err := st.IngestMessage(bridge.MsgSheet, []byte(`{"level":12,"class":"WARRIOR"}`), "A-Reino", time.Unix(300, 0)); err != nil {
		t.Fatal(err)
	}
	g = reopenService(t, path, st)
	raw, _, ok = g.SheetJSON()
	if !ok || !strings.Contains(string(raw), `"level":12`) || !strings.Contains(string(raw), `"class":"WARRIOR"`) {
		t.Fatalf("the stored sheet beats the legacy one: ok=%v raw=%s", ok, raw)
	}

	// A section the table lacks is read from the store alone (no legacy equipment block).
	if _, err := st.IngestMessage(bridge.MsgEquipment, []byte(`{"equipment":{"16":{"itemID":2000,"name":"Espada"}}}`), "A-Reino", time.Unix(310, 0)); err != nil {
		t.Fatal(err)
	}
	g = reopenService(t, path, st)
	if v, _, ok := g.section("equipment"); !ok || asMap(v) == nil {
		t.Fatalf("equipment from the store: ok=%v v=%v", ok, v)
	}
}

func TestSchema2OutboxIsReimportedWhenTheFileChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	item := func(seq, at, lv int) string {
		return `{ ["seq"] = ` + strconv.Itoa(seq) + `, ["msgType"] = 12, ["at"] = ` + strconv.Itoa(at) + `, ["payload"] = {
	["p"] = { "sheet", 20777 }, ["oseq"] = ` + strconv.Itoa(seq) + `, ["v"] = { ["lv"] = ` + strconv.Itoa(lv) + `, ["class"] = "WARRIOR" } } }`
	}
	writeSV(t, path, `["schema"] = 2,
["characters"] = { ["W-Reino"] = { ["updatedAt"] = 100, ["character"] = { ["name"] = "W", ["realm"] = "Reino", ["level"] = 10, ["classFile"] = "WARRIOR" } } },
["outbox"] = { ["seq"] = 1, ["win"] = 1, ["items"] = { `+item(1, 1000, 10)+` } }`)
	g, _ := newStateService(t, path)
	if err := g.ImportSavedVariablesNow(); err != nil {
		t.Fatal(err)
	}
	raw, _, ok := g.SheetJSON()
	if !ok || !strings.Contains(string(raw), `"level":10`) {
		t.Fatalf("first import: ok=%v raw=%s", ok, raw)
	}

	// The file grows: a newer sheet item. The import runs again on the change and the reader sees it.
	writeSV(t, path, `["schema"] = 2,
["characters"] = { ["W-Reino"] = { ["updatedAt"] = 100, ["character"] = { ["name"] = "W", ["realm"] = "Reino", ["level"] = 10, ["classFile"] = "WARRIOR" } } },
["outbox"] = { ["seq"] = 2, ["win"] = 1, ["items"] = { `+item(1, 1000, 10)+`, `+item(2, 2000, 11)+` } }`)
	if err := g.ImportSavedVariablesNow(); err != nil {
		t.Fatal(err)
	}
	raw, _, ok = g.SheetJSON()
	if !ok || !strings.Contains(string(raw), `"level":11`) {
		t.Fatalf("after the change: ok=%v raw=%s", ok, raw)
	}

	// The same content again is not read again (its hash is the last import): nothing changes.
	if err := g.ImportSavedVariablesNow(); err != nil {
		t.Fatal(err)
	}
	raw, _, _ = g.SheetJSON()
	if !strings.Contains(string(raw), `"level":11`) {
		t.Fatalf("same content: raw=%s", raw)
	}
}
