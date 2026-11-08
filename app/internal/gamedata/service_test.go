package gamedata

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

const fixture = "../../testdata/CompaneroWoW.lua"

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// fixtureService returns a service over the fixture SavedVariables with knowledge imported.
func fixtureService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st := openStore(t)
	w := bridge.NewSVWatcher(fixture, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll fixture: %v", err)
	}
	if _, _, err := st.ImportKnowledge(w.Current()); err != nil {
		t.Fatalf("import knowledge: %v", err)
	}
	return NewService(nil, w, st), st
}

func objOf(t *testing.T, get func() (any, error)) map[string]any {
	t.Helper()
	v, err := get()
	if err != nil {
		t.Fatalf("getter returned error: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("getter returned %T, want map", v)
	}
	return m
}

func TestQuestsEnrichedWithKnowledge(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, svc.Quests)
	if m["available"] != true || m["source"] != "savedvariables" {
		t.Fatalf("envelope = %#v", m)
	}
	if m["updatedAt"] == nil {
		t.Error("updatedAt missing")
	}
	quests, ok := m["quests"].([]map[string]any)
	if !ok || len(quests) != 2 {
		t.Fatalf("quests = %#v", m["quests"])
	}
	if m["count"] != 2 || m["trackedQuestId"] != int64(90001) {
		t.Errorf("count/tracked = %v / %v", m["count"], m["trackedQuestId"])
	}
	q := quests[0]
	if q["questID"] != float64(90001) || q["title"] != "The Warchief's Orders" {
		t.Errorf("first quest = %#v", q)
	}
	giver, ok := q["giver"].(map[string]any)
	if !ok || giver["name"] != "Garrosh Hellscream" || giver["npcId"] != int64(3296) {
		t.Fatalf("giver enrichment missing: %#v", q["giver"])
	}
	if q["descriptionText"] == nil || q["rewards"] == nil {
		t.Errorf("knowledge description/rewards missing: %#v", q)
	}
	objectives, ok := q["objectives"].([]any)
	if !ok || len(objectives) != 2 {
		t.Errorf("log objectives must be preserved: %#v", q["objectives"])
	}
}

func TestQuestOneEntry(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, func() (any, error) { return svc.Quest(90002) })
	if m["inLog"] != true || m["title"] != "Herbs for the Shamans" {
		t.Fatalf("quest 90002 = %#v", m)
	}
	if turnIn, ok := m["turnIn"].(map[string]any); !ok || turnIn["name"] != "Herbalist Ruth" {
		t.Errorf("turnIn = %#v", m["turnIn"])
	}

	unknown := objOf(t, func() (any, error) { return svc.Quest(4242) })
	if unknown["available"] != false || !strings.Contains(unknown["hint"].(string), "4242") {
		t.Errorf("unknown quest = %#v", unknown)
	}
}

func TestMissingDataReturnsHint(t *testing.T) {
	st := openStore(t)
	w := bridge.NewSVWatcher(filepath.Join(t.TempDir(), "missing.lua"), time.Hour)
	svc := NewService(nil, w, st)
	for name, get := range map[string]func() (any, error){
		"quests":    svc.Quests,
		"character": svc.Character,
		"inventory": svc.Inventory,
		"money":     svc.Money,
		"location":  svc.Location,
	} {
		m := objOf(t, get)
		if m["available"] != false {
			t.Errorf("%s: available = %v, want false", name, m["available"])
		}
		if m["hint"] != HintSync {
			t.Errorf("%s: hint = %v", name, m["hint"])
		}
	}
	if m := objOf(t, svc.Target); m["available"] != false || m["hint"] != HintLive {
		t.Errorf("target without bridge = %#v", m)
	}
}

func TestNilSourcesDoNotPanic(t *testing.T) {
	svc := NewService(nil, nil, nil)
	if _, ok := svc.Live(); ok {
		t.Fatal("Live must be !ok without reader")
	}
	for _, get := range []func() (any, error){
		svc.Location, svc.Character, svc.Quests, svc.Target, svc.Inventory, svc.Equipment,
		svc.Group, svc.Spells, svc.Talents, svc.Professions, svc.Reputations, svc.Money, svc.Status,
	} {
		if _, err := get(); err != nil {
			t.Fatalf("nil-source getter returned error: %v", err)
		}
	}
	st := objOf(t, svc.Status)
	if st["bridgeConnected"] != false || st["savedVariablesLoaded"] != false {
		t.Fatalf("status = %#v", st)
	}
	if svc.Store() != nil {
		t.Fatal("Store() should be nil")
	}
	svc.Start(context.Background()) // must not panic
}

func TestCharacterAndSections(t *testing.T) {
	svc, _ := fixtureService(t)

	c := objOf(t, svc.Character)
	if c["name"] != "Thrall" || c["level"] != float64(38) || c["key"] != "Thrall-Forever" {
		t.Errorf("character = %#v", c)
	}

	inv := objOf(t, svc.Inventory)
	if items, ok := inv["items"].([]any); !ok || len(items) != 3 || inv["freeSlots"] != float64(9) {
		t.Errorf("inventory = %#v", inv)
	}

	eq := objOf(t, svc.Equipment)
	if items, ok := eq["items"].([]map[string]any); !ok || len(items) != 3 || items[0]["slotID"] != float64(1) {
		t.Errorf("equipment = %#v", eq["items"])
	}

	grp := objOf(t, svc.Group)
	if grp["inGroup"] != false {
		t.Errorf("group = %#v", grp)
	}

	if spells := objOf(t, svc.Spells)["spells"].([]any); len(spells) != 2 {
		t.Errorf("spells = %#v", spells)
	}
	if profs := objOf(t, svc.Professions)["professions"].([]any); len(profs) != 3 {
		t.Errorf("professions = %#v", profs)
	}
	if reps := objOf(t, svc.Reputations)["reputations"].([]any); len(reps) != 1 {
		t.Errorf("reputations = %#v", reps)
	}
	if tal := objOf(t, svc.Talents); tal["talents"] == nil {
		t.Errorf("talents = %#v", tal)
	}
}

func TestMoneySplitsCopper(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, svc.Money)
	if m["copper"] != int64(1234567) || m["gold"] != int64(123) || m["silver"] != int64(45) || m["copperRemainder"] != int64(67) {
		t.Errorf("money = %#v", m)
	}
}

func TestLocationUsesSavedVariablesWithoutLive(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, svc.Location)
	if m["zone"] != "Orgrimmar" || m["subZone"] != "Valley of Strength" || m["mapId"] != 85 {
		t.Errorf("location = %#v", m)
	}
	if m["source"] != "savedvariables" {
		t.Errorf("source = %v", m["source"])
	}
}

func TestSavedVariablesFileAgeStatus(t *testing.T) {
	svc, _ := fixtureService(t)
	st := objOf(t, svc.Status)
	if st["savedVariablesLoaded"] != true || st["characterKey"] != "Thrall-Forever" {
		t.Errorf("status = %#v", st)
	}
	if _, ok := st["savedVariablesAgeSec"]; !ok {
		t.Errorf("savedVariablesAgeSec missing: %#v", st)
	}
}

func TestStartImportsKnowledgeInBackground(t *testing.T) {
	st := openStore(t)
	w := bridge.NewSVWatcher(fixture, 50*time.Millisecond)
	svc := NewService(nil, w, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	svc.Start(ctx) // idempotent

	deadline := time.Now().Add(5 * time.Second)
	for {
		q, err := st.GetQuest(90001)
		if err != nil {
			t.Fatal(err)
		}
		if q != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("knowledge was not imported after Start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if svc.Store() != st {
		t.Fatal("Store() must return the store")
	}
}
