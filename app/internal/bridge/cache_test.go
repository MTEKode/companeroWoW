package bridge

import (
	"testing"
	"time"
)

func TestLiveDataExportImportKeepsLatestOfEachType(t *testing.T) {
	src := NewLiveData()
	at := time.Date(2026, 11, 18, 18, 0, 0, 0, time.UTC)
	_ = src.Put(MsgCharacter, []byte(`{"level":19}`), at)
	_ = src.Put(MsgCharacter, []byte(`{"level":20}`), at.Add(time.Second)) // latest wins
	_ = src.Put(MsgSession, []byte(`{"playedSec":600}`), at)
	items := src.Export()
	if len(items) != 2 {
		t.Fatalf("exported %d items, want 2", len(items))
	}

	dst := NewLiveData()
	if n := dst.Import(items); n != 2 {
		t.Fatalf("imported %d, want 2", n)
	}
	it, ok := dst.Get(MsgCharacter)
	if !ok {
		t.Fatal("character not imported")
	}
	if m, _ := it.Value.(map[string]any); m["level"] != float64(20) {
		t.Fatalf("value = %#v, want level 20", it.Value)
	}
	if !it.At.Equal(at.Add(time.Second)) {
		t.Fatalf("At = %v, want %v", it.At, at.Add(time.Second))
	}
	if it.Name != "character" {
		t.Fatalf("Name = %q", it.Name)
	}
}

func TestImportSkipsBadItemsAndKeepsNewerLive(t *testing.T) {
	d := NewLiveData()
	now := time.Date(2026, 11, 18, 18, 0, 0, 0, time.UTC)
	_ = d.Put(MsgQuests, []byte(`{"live":true}`), now)
	n := d.Import([]CachedItem{
		{Type: 0, At: now, Value: []byte(`{}`)},                                      // no type
		{Type: MsgCharacter, Value: []byte(`{}`)},                                    // no time
		{Type: MsgCharacter, At: now, Value: []byte(`{not json`)},                    // undecodable
		{Type: MsgQuests, At: now.Add(-time.Hour), Value: []byte(`{"cached":true}`)}, // older than live
	})
	if n != 0 {
		t.Fatalf("imported %d, want 0", n)
	}
	if _, ok := d.Get(MsgCharacter); ok {
		t.Fatal("an undecodable or untimed item was stored")
	}
	it, _ := d.Get(MsgQuests)
	if m, _ := it.Value.(map[string]any); m["live"] != true {
		t.Fatalf("live quests were replaced by an older cached one: %#v", it.Value)
	}
}

func TestSessionTypeIsNamedAndFreshnessListsIt(t *testing.T) {
	if MsgNames[MsgSession] != "session" || MsgSession != 14 {
		t.Fatalf("session type = %d %q, want 14 session", MsgSession, MsgNames[MsgSession])
	}
	c := NewChannel()
	now := time.Now()
	_ = c.Data.Put(MsgSession, []byte(`{"playedSec":1}`), now)
	found := false
	for _, f := range c.Freshness(now) {
		if f.Type == MsgSession && f.Name == "session" {
			found = true
		}
	}
	if !found {
		t.Fatal("Freshness does not list the session message")
	}
}
