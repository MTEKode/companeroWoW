package gamedata

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// schema2Sheet is a schema-2 outbox file whose one sheet item (seq) reads the given level. Each call with a
// different level writes different bytes, so the file's hash changes.
func schema2Sheet(t *testing.T, path string, seqs ...int) {
	t.Helper()
	items := ""
	for i, lv := range seqs {
		if i > 0 {
			items += ", "
		}
		items += `{ ["seq"] = ` + strconv.Itoa(i+1) + `, ["msgType"] = 12, ["at"] = ` + strconv.Itoa(1000+i) + `, ["payload"] = {
	["p"] = { "sheet", 20777 }, ["oseq"] = ` + strconv.Itoa(i+1) + `, ["v"] = { ["lv"] = ` + strconv.Itoa(lv) + `, ["class"] = "WARRIOR" } } }`
	}
	writeSV(t, path, `["schema"] = 2,
["characters"] = { ["W-Reino"] = { ["updatedAt"] = 100, ["character"] = { ["name"] = "W", ["realm"] = "Reino", ["level"] = 10, ["classFile"] = "WARRIOR" } } },
["outbox"] = { ["seq"] = `+strconv.Itoa(len(seqs))+`, ["win"] = 1, ["items"] = { `+items+` } }`)
}

// storedSHA is the content hash of the file the store imported last.
func storedSHA(t *testing.T, g *Service, path string) string {
	t.Helper()
	last, ok, err := g.Store().LastSVImport(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return ""
	}
	return last.SHA256
}

// The on-demand mode pauses the watcher, and nothing asks for a while: the idle check still stores a file
// that changed, so the store holds it before the next question.
func TestIdleCheckStoresAChangedFileInOnDemand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	schema2Sheet(t, path, 10)
	g, _ := newStateService(t, path)
	g.SetOnDemand(true)
	if err := g.RefreshSavedVariables(); err != nil {
		t.Fatal(err)
	}

	schema2Sheet(t, path, 10, 11)
	sv, err := bridge.LoadSavedVariables(path)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.RunSavedVariablesCheck(ctx, 5*time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for storedSHA(t, g, path) != sv.SHA256 {
		if time.Now().After(deadline) {
			t.Fatalf("the idle check did not store the changed file")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	raw, _, ok := g.SheetJSON()
	if !ok || !strings.Contains(string(raw), `"level":11`) {
		t.Fatalf("the idle check stored the file, but the sheet is not the new one: ok=%v raw=%s", ok, raw)
	}
}

// A question is answered while the file may change: a read made after the write stores the new file at once,
// without any refresh call and without waiting for the idle check.
func TestReadAtQuestionTimeStoresTheNewestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	schema2Sheet(t, path, 10)
	g, _ := newStateService(t, path)
	g.SetOnDemand(true)
	g.checkEvery = 0 // every read looks at the file

	schema2Sheet(t, path, 10, 11)
	if _, err := g.PlayerContext(); err != nil {
		t.Fatal(err)
	}
	raw, _, ok := g.SheetJSON()
	if !ok || !strings.Contains(string(raw), `"level":11`) {
		t.Fatalf("the read at question time did not store the file: ok=%v raw=%s", ok, raw)
	}
}

// Within checkEvery a read does not look at the file again: the check is cheap, and a burst of tool calls
// costs one stat.
func TestReadsLookAtTheFileAtMostOncePerInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	schema2Sheet(t, path, 10)
	g, _ := newStateService(t, path)
	g.checkEvery = time.Hour
	g.checkSavedVariables() // the first look is due

	schema2Sheet(t, path, 10, 11)
	if raw, _, _ := g.SheetJSON(); strings.Contains(string(raw), `"level":11`) {
		t.Fatalf("a read within the interval looked at the file again")
	}
}

// The context shows the sheet whichever source holds it: the store alone, the file alone, and no source at all.
func TestPlayerContextShowsTheSheetFromAnySource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	writeSV(t, path, `["characters"] = {
["A-Reino"] = { ["character"] = { ["name"] = "A", ["realm"] = "Reino", ["level"] = 12, ["classFile"] = "WARRIOR" }, ["updatedAt"] = 100 }
}`)
	g, st := newStateService(t, path)

	// No source has a sheet: the context says so and asks for the sync.
	ctx, err := g.PlayerContext()
	if err != nil {
		t.Fatal(err)
	}
	m := asMap(ctx)
	if h := asMap(m["hoja"]); h == nil || truthy(h["disponible"]) {
		t.Fatalf("without a sheet the context must say so: hoja=%v", m["hoja"])
	}
	if !strings.Contains(strings.Join(listStrings(m["avisos"]), " "), "Falta la hoja") {
		t.Fatalf("without a sheet the notes must ask for the sync: avisos=%v", m["avisos"])
	}

	// The store has the sheet (the channel wrote it), the file has none: the context shows it from the store.
	sheet := `{"level":12,"class":"WARRIOR","race":"Humano","talents":{"tabs":[{"points":5},{"points":3},{"points":0}]}}`
	if _, err := st.IngestMessage(bridge.MsgSheet, []byte(sheet), "A-Reino", time.Unix(300, 0)); err != nil {
		t.Fatal(err)
	}
	g = reopenService(t, path, st)
	ctx, err = g.PlayerContext()
	if err != nil {
		t.Fatal(err)
	}
	h := asMap(asMap(ctx)["hoja"])
	if !truthy(h["disponible"]) || str(h["fuente"]) != sheetFromStore {
		t.Fatalf("the sheet of the store is not in the context: %v", h)
	}
	if str(h["clase"]) != "WARRIOR" || numberOf(h["nivel"]) != 12 || numberOf(h["puntosTalento"]) != 8 {
		t.Fatalf("the sheet summary is wrong: %v", h)
	}
	if _, ok := asMap(asMap(ctx)["datos"])["masRecienteHaceSeg"]; !ok {
		t.Fatalf("the freshness lacks the age of the newest data: %v", asMap(ctx)["datos"])
	}
}

// listStrings reads a list of strings from a value the getters return (empty for anything else).
func listStrings(v any) []string {
	if s, ok := v.([]string); ok {
		return s
	}
	var out []string
	for _, e := range listOf(v) {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
