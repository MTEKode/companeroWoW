package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const fixturePath = "../../testdata/CompaneroWoW.lua"

func TestLoadFixture(t *testing.T) {
	sv, err := LoadSavedVariables(fixturePath)
	if err != nil {
		t.Fatalf("LoadSavedVariables: %v", err)
	}
	if sv.ModTime.IsZero() {
		t.Error("ModTime not set")
	}
	if sv.Raw["schema"] != float64(1) {
		t.Errorf("schema = %v (%T)", sv.Raw["schema"], sv.Raw["schema"])
	}
	settings, ok := sv.Raw["settings"].(map[string]any)
	if !ok || settings["strip"] != true {
		t.Errorf("settings = %#v", sv.Raw["settings"])
	}

	chars, ok := sv.Raw["characters"].(map[string]any)
	if !ok {
		t.Fatalf("characters is %T", sv.Raw["characters"])
	}
	char, ok := chars["Thrall-Forever"].(map[string]any)
	if !ok {
		t.Fatalf("Thrall-Forever is %T", chars["Thrall-Forever"])
	}
	if char["updatedAt"] != float64(1793880000) {
		t.Errorf("updatedAt = %v", char["updatedAt"])
	}

	// Sparse numeric keys (questIDs) must become a map with stringified keys.
	quests, ok := char["quests"].(map[string]any)
	if !ok {
		t.Fatalf("quests is %T, want map", char["quests"])
	}
	q, ok := quests["90001"].(map[string]any)
	if !ok {
		t.Fatalf("quest 90001 is %T", quests["90001"])
	}
	if q["isSuperTracked"] != true || q["level"] != float64(37) {
		t.Errorf("quest fields wrong: %#v", q)
	}
	objectives, ok := q["objectives"].([]any)
	if !ok || len(objectives) != 2 {
		t.Fatalf("objectives = %#v", q["objectives"])
	}
	obj, ok := objectives[0].(map[string]any)
	if !ok || obj["numRequired"] != float64(8) || obj["finished"] != false || obj["text"] != "Slay Gnoll Raiders" {
		t.Errorf("first objective = %#v", objectives[0])
	}

	// Consecutive 1..n keys become []any.
	inv, ok := char["inventory"].(map[string]any)
	if !ok {
		t.Fatalf("inventory is %T", char["inventory"])
	}
	items, ok := inv["items"].([]any)
	if !ok || len(items) != 3 {
		t.Fatalf("inventory items = %#v", inv["items"])
	}
	if inv["freeSlots"] != float64(9) {
		t.Errorf("freeSlots = %v", inv["freeSlots"])
	}

	// Equipment slots 1, 2, 16 are not consecutive: map.
	equip, ok := char["equipment"].(map[string]any)
	if !ok {
		t.Fatalf("equipment is %T, want map", char["equipment"])
	}
	if _, ok := equip["16"].(map[string]any); !ok {
		t.Errorf("equipment[16] missing: %#v", equip)
	}

	group, ok := char["group"].(map[string]any)
	if !ok {
		t.Fatalf("group is %T", char["group"])
	}
	if members, ok := group["members"].([]any); !ok || len(members) != 0 {
		t.Errorf("empty members = %#v", group["members"])
	}

	maps, ok := sv.Raw["maps"].(map[string]any)
	if !ok || maps["85"] != "Orgrimmar" {
		t.Errorf("maps = %#v", sv.Raw["maps"])
	}

	knowledge, ok := sv.Raw["knowledge"].(map[string]any)
	if !ok {
		t.Fatalf("knowledge is %T", sv.Raw["knowledge"])
	}
	kq, ok := knowledge["quests"].(map[string]any)
	if !ok || len(kq) != 2 {
		t.Fatalf("knowledge.quests = %#v", knowledge["quests"])
	}
	k1, ok := kq["90001"].(map[string]any)
	if !ok {
		t.Fatalf("knowledge quest 90001 is %T", kq["90001"])
	}
	rewards, ok := k1["rewards"].(map[string]any)
	if !ok {
		t.Fatalf("rewards is %T", k1["rewards"])
	}
	if rItems, ok := rewards["items"].([]any); !ok || len(rItems) != 1 {
		t.Errorf("reward items = %#v", rewards["items"])
	}
	kn, ok := knowledge["npcs"].(map[string]any)
	if !ok {
		t.Fatalf("knowledge.npcs is %T", knowledge["npcs"])
	}
	if npc, ok := kn["3296"].(map[string]any); !ok || npc["isQuestGiver"] != true {
		t.Errorf("npc 3296 = %#v", kn["3296"])
	}
}

func TestLoadSavedVariablesMissingFile(t *testing.T) {
	_, err := LoadSavedVariables(filepath.Join(t.TempDir(), "nope.lua"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want wrapped os.ErrNotExist", err)
	}
}

func TestSequenceVersusMapConversion(t *testing.T) {
	src := `CompaneroWoWDB = {
		seq = {"a", "b"},
		gap = {[1] = "a", [3] = "c"},
		zero = {[0] = "x"},
		empty = {},
		mixed = {1, 2, name = "n"},
		nested = { { x = 1 }, { x = 2 } },
		flag = false,
	}`
	raw, err := evalSavedVariables(context.Background(), []byte(src), "test.lua")
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := raw["seq"].([]any); !ok || len(s) != 2 || s[1] != "b" {
		t.Errorf("seq = %#v", raw["seq"])
	}
	if g, ok := raw["gap"].(map[string]any); !ok || g["3"] != "c" || g["1"] != "a" {
		t.Errorf("gap = %#v", raw["gap"])
	}
	if z, ok := raw["zero"].(map[string]any); !ok || z["0"] != "x" {
		t.Errorf("zero = %#v", raw["zero"])
	}
	if e, ok := raw["empty"].([]any); !ok || len(e) != 0 {
		t.Errorf("empty = %#v", raw["empty"])
	}
	if m, ok := raw["mixed"].(map[string]any); !ok || m["name"] != "n" || m["1"] != float64(1) {
		t.Errorf("mixed = %#v", raw["mixed"])
	}
	if n, ok := raw["nested"].([]any); !ok || n[1].(map[string]any)["x"] != float64(2) {
		t.Errorf("nested = %#v", raw["nested"])
	}
	if raw["flag"] != false {
		t.Errorf("flag = %#v", raw["flag"])
	}
}

func TestSandboxHasNoFileOrOSAccess(t *testing.T) {
	src := `CompaneroWoWDB = {
		ok = (dofile == nil) and (loadfile == nil) and (load == nil) and
		     (loadstring == nil) and (os == nil) and (io == nil) and
		     (require == nil) and (package == nil) and (string == nil),
	}`
	raw, err := evalSavedVariables(context.Background(), []byte(src), "test.lua")
	if err != nil {
		t.Fatal(err)
	}
	if raw["ok"] != true {
		t.Fatalf("sandbox exposes dangerous globals: %#v", raw)
	}
}

func TestEvaluationTimeout(t *testing.T) {
	src := "CompaneroWoWDB = {}\nwhile true do end\n"
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := evalSavedVariables(ctx, []byte(src), "loop.lua"); err == nil {
		t.Fatal("expected timeout error from infinite loop")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took %v", elapsed)
	}
}

func TestMalformedLuaIsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CompaneroWoW.lua")
	if err := os.WriteFile(path, []byte("CompaneroWoWDB = { broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSavedVariables(path); err == nil {
		t.Fatal("expected error for malformed Lua")
	}
	if err := os.WriteFile(path, []byte("OtherGlobal = 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSavedVariables(path); err == nil {
		t.Fatal("expected error when CompaneroWoWDB is missing")
	}
}

func TestSVWatcherReloadsOnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CompaneroWoW.lua")
	write := func(body string, mt time.Time) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	w := NewSVWatcher(path, time.Hour)
	if err := w.Poll(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: err = %v", err)
	}
	if w.Current() != nil {
		t.Fatal("Current should be nil before any load")
	}

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	write("CompaneroWoWDB = { schema = 1 }", base)
	if err := w.Poll(); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if first := <-w.Changes(); first.Raw["schema"] != float64(1) {
		t.Fatalf("first change = %#v", first.Raw)
	}

	if err := w.Poll(); err != nil {
		t.Fatalf("unchanged poll: %v", err)
	}
	select {
	case <-w.Changes():
		t.Fatal("unchanged file must not emit a change")
	default:
	}

	write("CompaneroWoWDB = { schema = 2 }", base.Add(time.Minute))
	if err := w.Poll(); err != nil {
		t.Fatalf("changed poll: %v", err)
	}
	if got := w.Current(); got == nil || got.Raw["schema"] != float64(2) {
		t.Fatalf("Current after change = %#v", got)
	}

	// A broken write keeps the previous good data and reports the error.
	write("CompaneroWoWDB = { schema = ", base.Add(2*time.Minute))
	if err := w.Poll(); err == nil {
		t.Fatal("expected load error for broken file")
	}
	if got := w.Current(); got == nil || got.Raw["schema"] != float64(2) {
		t.Fatalf("broken write should keep previous data, got %#v", got)
	}
	if w.LastError() == nil {
		t.Fatal("LastError should report the failure")
	}
}
