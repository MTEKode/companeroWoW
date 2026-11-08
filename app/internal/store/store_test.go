package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

const fixture = "../../testdata/CompaneroWoW.lua"

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenCreatesSchemaInWALMode(t *testing.T) {
	s := openTemp(t)
	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != latestVersion {
		t.Fatalf("user_version = %d, want %d", version, latestVersion)
	}
}

func TestReopenKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "companero.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMapName(85, "Orgrimmar"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if name, err := s2.MapName(85); err != nil || name != "Orgrimmar" {
		t.Fatalf("MapName after reopen = %q, %v", name, err)
	}
}

func TestUpsertQuestMergeSemantics(t *testing.T) {
	s := openTemp(t)
	if err := s.UpsertQuest(KQuest{
		QuestID: 1, Title: "Old title", ObjectivesText: "Kill 8 boars",
		GiverNpcID: 10, GiverName: "Giver", GiverX: 0.4, GiverY: 0.6,
		FirstSeen: 200, LastSeen: 300,
	}); err != nil {
		t.Fatal(err)
	}
	// Incoming: new title, empty objectives (must keep old), earlier FirstSeen, later LastSeen.
	if err := s.UpsertQuest(KQuest{
		QuestID: 1, Title: "New title", ObjectivesText: "",
		TurnInNpcID: 11, TurnInName: "Turner", TurnInX: 0.1, TurnInY: 0.2,
		FirstSeen: 100, LastSeen: 500,
	}); err != nil {
		t.Fatal(err)
	}
	q, err := s.GetQuest(1)
	if err != nil || q == nil {
		t.Fatalf("GetQuest: %v %v", q, err)
	}
	if q.Title != "New title" {
		t.Errorf("Title = %q, want overwritten", q.Title)
	}
	if q.ObjectivesText != "Kill 8 boars" {
		t.Errorf("ObjectivesText = %q, empty incoming must keep old", q.ObjectivesText)
	}
	if q.GiverName != "Giver" || q.GiverNpcID != 10 || q.GiverX != 0.4 {
		t.Errorf("giver fields lost: %+v", q)
	}
	if q.TurnInName != "Turner" || q.TurnInX != 0.1 {
		t.Errorf("turn-in fields not written: %+v", q)
	}
	if q.FirstSeen != 100 || q.LastSeen != 500 {
		t.Errorf("FirstSeen/LastSeen = %d/%d, want 100/500", q.FirstSeen, q.LastSeen)
	}

	// A later, smaller LastSeen must not move LastSeen backwards.
	if err := s.UpsertQuest(KQuest{QuestID: 1, LastSeen: 50}); err != nil {
		t.Fatal(err)
	}
	q, _ = s.GetQuest(1)
	if q.LastSeen != 500 || q.FirstSeen != 100 {
		t.Errorf("merge moved timestamps: first=%d last=%d", q.FirstSeen, q.LastSeen)
	}
}

func TestGetMissingReturnsNil(t *testing.T) {
	s := openTemp(t)
	if q, err := s.GetQuest(999); q != nil || err != nil {
		t.Fatalf("GetQuest missing = %v, %v", q, err)
	}
	if n, err := s.GetNpc(999); n != nil || err != nil {
		t.Fatalf("GetNpc missing = %v, %v", n, err)
	}
	if name, err := s.MapName(999); name != "" || err != nil {
		t.Fatalf("MapName missing = %q, %v", name, err)
	}
}

func TestUpsertNpcMergeAndFlags(t *testing.T) {
	s := openTemp(t)
	if err := s.UpsertNpc(KNpc{NpcID: 5, Name: "Guard", MapID: 85, X: 0.5, Y: 0.5, IsVendor: true, FirstSeen: 10, LastSeen: 10}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNpc(KNpc{NpcID: 5, IsQuestGiver: true, TrainerType: "", Reaction: "Friendly", FirstSeen: 20, LastSeen: 30}); err != nil {
		t.Fatal(err)
	}
	n, err := s.GetNpc(5)
	if err != nil || n == nil {
		t.Fatalf("GetNpc: %v %v", n, err)
	}
	if !n.IsVendor || !n.IsQuestGiver || n.IsTrainer {
		t.Errorf("flags = vendor:%v giver:%v trainer:%v", n.IsVendor, n.IsQuestGiver, n.IsTrainer)
	}
	if n.Name != "Guard" || n.MapID != 85 || n.Reaction != "Friendly" {
		t.Errorf("merged npc = %+v", n)
	}
	if n.FirstSeen != 10 || n.LastSeen != 30 {
		t.Errorf("FirstSeen/LastSeen = %d/%d", n.FirstSeen, n.LastSeen)
	}
}

func TestSearchQuestsAndNpcs(t *testing.T) {
	s := openTemp(t)
	for _, q := range []KQuest{
		{QuestID: 1, Title: "Wolves at the Gate", ObjectivesText: "Kill wolves", LastSeen: 1},
		{QuestID: 2, Title: "Herbs", ObjectivesText: "Collect 50% peacebloom_x", LastSeen: 2},
		{QuestID: 3, Title: "Other", ObjectivesText: "Nothing", LastSeen: 3},
	} {
		if err := s.UpsertQuest(q); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.SearchQuests("wolves", 10)
	if err != nil || len(got) != 1 || got[0].QuestID != 1 {
		t.Fatalf("title search = %+v, %v", got, err)
	}
	got, err = s.SearchQuests("peacebloom", 10)
	if err != nil || len(got) != 1 || got[0].QuestID != 2 {
		t.Fatalf("objectives search = %+v, %v; want quest 2 only", got, err)
	}
	got, err = s.SearchQuests("collect 50%", 10)
	if err != nil || len(got) != 1 || got[0].QuestID != 2 {
		t.Fatalf("wildcard-literal search = %+v, %v", got, err)
	}
	got, err = s.SearchQuests("", 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("limit search = %d rows, %v", len(got), err)
	}

	for _, n := range []KNpc{
		{NpcID: 1, Name: "Thrall", MapID: 85, LastSeen: 1},
		{NpcID: 2, Name: "Thrall's Scout", MapID: 1, LastSeen: 2},
		{NpcID: 3, Name: "Garrosh", MapID: 85, LastSeen: 3},
	} {
		if err := s.UpsertNpc(n); err != nil {
			t.Fatal(err)
		}
	}
	ns, err := s.SearchNpcs("thrall", 10)
	if err != nil || len(ns) != 2 {
		t.Fatalf("SearchNpcs = %+v, %v", ns, err)
	}
	onMap, err := s.NpcsOnMap(85, 10)
	if err != nil || len(onMap) != 2 {
		t.Fatalf("NpcsOnMap = %+v, %v", onMap, err)
	}
}

func TestImportKnowledgeFromFixture(t *testing.T) {
	s := openTemp(t)
	sv, err := bridge.LoadSavedVariables(fixture)
	if err != nil {
		t.Fatal(err)
	}
	quests, npcs, err := s.ImportKnowledge(sv)
	if err != nil {
		t.Fatalf("ImportKnowledge: %v", err)
	}
	if quests != 2 || npcs != 2 {
		t.Fatalf("imported %d quests, %d npcs; want 2, 2", quests, npcs)
	}
	q, err := s.GetQuest(90001)
	if err != nil || q == nil {
		t.Fatalf("GetQuest 90001: %v %v", q, err)
	}
	if q.Title != "The Warchief's Orders" || q.GiverName != "Garrosh Hellscream" || q.GiverMapID != 85 {
		t.Errorf("quest = %+v", q)
	}
	if q.GiverX != 0.513 || q.TurnInNpcID != 3296 {
		t.Errorf("coords/turn-in wrong: %+v", q)
	}
	if !strings.Contains(q.RewardsJSON, `"money":45000`) || !strings.Contains(q.RewardsJSON, "Gnoll Hide") {
		t.Errorf("RewardsJSON = %s", q.RewardsJSON)
	}
	n, err := s.GetNpc(3001)
	if err != nil || n == nil {
		t.Fatalf("GetNpc 3001: %v %v", n, err)
	}
	if !n.IsVendor || !n.IsTrainer || n.TrainerType != "Herbalism" {
		t.Errorf("npc = %+v", n)
	}
	if name, _ := s.MapName(85); name != "Orgrimmar" {
		t.Errorf("MapName(85) = %q", name)
	}

	// Importing again is idempotent.
	if _, _, err := s.ImportKnowledge(sv); err != nil {
		t.Fatalf("second import: %v", err)
	}
	if _, _, err := s.ImportKnowledge(nil); err == nil {
		t.Fatal("nil saved variables must be an error")
	}
}

func TestCacheExpiry(t *testing.T) {
	s := openTemp(t)
	base := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base }
	if err := s.CachePut("search:forever", "result-body"); err != nil {
		t.Fatal(err)
	}

	v, ok, err := s.CacheGet("search:forever", time.Hour)
	if err != nil || !ok || v != "result-body" {
		t.Fatalf("fresh get = %q %v %v", v, ok, err)
	}
	s.now = func() time.Time { return base.Add(2 * time.Hour) }
	if _, ok, err := s.CacheGet("search:forever", time.Hour); err != nil || ok {
		t.Fatalf("expired entry returned ok=%v err=%v", ok, err)
	}
	if v, ok, err := s.CacheGet("search:forever", 0); err != nil || !ok || v != "result-body" {
		t.Fatalf("maxAge 0 should disable expiry: %q %v %v", v, ok, err)
	}
	if _, ok, err := s.CacheGet("missing", time.Hour); err != nil || ok {
		t.Fatalf("missing key: ok=%v err=%v", ok, err)
	}
}

func TestChatHistoryOrder(t *testing.T) {
	s := openTemp(t)
	base := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	roles := []string{"user", "assistant", "user", "system", "assistant"}
	for i, r := range roles {
		if _, err := s.AddMessage(ChatMsg{Role: r, Content: string(rune('a' + i)), Provider: "gemini", CreatedAt: base.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddMessage(ChatMsg{Role: "bogus", Content: "x"}); err == nil {
		t.Fatal("invalid role must be rejected")
	}

	msgs, err := s.RecentMessages(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	if msgs[0].Content != "c" || msgs[2].Content != "e" {
		t.Fatalf("expected oldest→newest c,d,e; got %q..%q", msgs[0].Content, msgs[2].Content)
	}
	if msgs[0].Role != "user" || msgs[0].Provider != "gemini" || !msgs[0].CreatedAt.Equal(base.Add(2*time.Second)) {
		t.Errorf("message fields wrong: %+v", msgs[0])
	}
	if err := s.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	msgs, err = s.RecentMessages(10)
	if err != nil || len(msgs) != 0 {
		t.Fatalf("after clear: %d messages, %v", len(msgs), err)
	}
}

func TestErrorsWrapContext(t *testing.T) {
	s := openTemp(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetQuest(1)
	if err == nil || errors.Unwrap(err) == nil {
		t.Fatalf("error on closed DB should wrap the cause, got %v", err)
	}
}
