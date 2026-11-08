package store

import (
	"fmt"
	"testing"
	"time"
)

func TestMaintainKeepsNewestMessages(t *testing.T) {
	s := openTemp(t)
	for i := 0; i < keepChatMessages+30; i++ {
		if _, err := s.AddMessage(ChatMsg{Role: "user", Content: fmt.Sprintf("m%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := s.Maintain()
	if err != nil {
		t.Fatalf("Maintain: %v", err)
	}
	if res.MessagesDeleted != 30 {
		t.Fatalf("deleted %d messages, want 30", res.MessagesDeleted)
	}
	msgs, err := s.RecentMessages(keepChatMessages + 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != keepChatMessages {
		t.Fatalf("kept %d messages, want %d", len(msgs), keepChatMessages)
	}
	if msgs[0].Content != "m30" || msgs[len(msgs)-1].Content != fmt.Sprintf("m%d", keepChatMessages+29) {
		t.Fatalf("kept the wrong messages: first %q last %q", msgs[0].Content, msgs[len(msgs)-1].Content)
	}
}

func TestMaintainExpiresCacheByKind(t *testing.T) {
	s := openTemp(t)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base }
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.CachePut("search:all:old", "x"))
	must(s.CachePut("page:https://a/old", "x"))
	must(s.CachePut("other:kept", "x"))

	// 25 hours later: the search is past its 24 h, the page is not past its 7 days.
	s.now = func() time.Time { return base.Add(25 * time.Hour) }
	must(s.CachePut("page:https://a/fresh", "x"))
	res, err := s.Maintain()
	if err != nil {
		t.Fatalf("Maintain: %v", err)
	}
	if res.CacheDeleted != 1 {
		t.Fatalf("deleted %d cache rows after 25 h, want 1 (the search)", res.CacheDeleted)
	}
	if _, ok, _ := s.CacheGet("page:https://a/old", 0); !ok {
		t.Error("page within 7 days was deleted")
	}

	// 8 days after the page was stored: it expires; the unknown-prefix row never does.
	s.now = func() time.Time { return base.Add(8 * 24 * time.Hour) }
	if _, err := s.Maintain(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.CacheGet("page:https://a/old", 0); ok {
		t.Error("page older than 7 days was kept")
	}
	if _, ok, _ := s.CacheGet("other:kept", 0); !ok {
		t.Error("row with an unknown prefix was deleted")
	}
}

func TestMaintainVacuumsAtMostWeekly(t *testing.T) {
	s := openTemp(t)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base }
	res, err := s.Maintain()
	if err != nil || !res.Vacuumed {
		t.Fatalf("first run: vacuumed=%v err=%v, want a VACUUM", res.Vacuumed, err)
	}
	s.now = func() time.Time { return base.Add(6 * 24 * time.Hour) }
	if res, _ := s.Maintain(); res.Vacuumed {
		t.Error("VACUUM ran again after 6 days")
	}
	s.now = func() time.Time { return base.Add(vacuumEvery) }
	if res, _ := s.Maintain(); !res.Vacuumed {
		t.Error("no VACUUM after a week")
	}
}
