package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSessionRoundTripAndUpdate(t *testing.T) {
	s := openTemp(t)
	start := time.Unix(1793880000, 0)
	se := &Session{
		StartedAt:       start,
		LastDataAt:      start.Add(time.Minute),
		Open:            true,
		StartLevel:      38,
		EndLevel:        39,
		LevelsGained:    1,
		StartXPPct:      90,
		EndXPPct:        3,
		XPGainedPct:     13.5,
		MoneyStart:      1000,
		MoneyEnd:        3500,
		QuestsCompleted: []int64{90002},
		QuestsTurnedIn:  []int64{90001, 90002},
		ZoneMapIDs:      []int{85, 1},
		Deaths:          2,
	}
	if err := s.SaveSession(se); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if se.ID == 0 {
		t.Fatal("insert must set the ID")
	}
	got, err := s.LastSession()
	if err != nil || got == nil {
		t.Fatalf("LastSession = %v, %v", got, err)
	}
	if got.ID != se.ID || !got.Open || got.XPGainedPct != 13.5 || got.MoneyEnd != 3500 || got.Deaths != 2 {
		t.Errorf("round trip = %+v", got)
	}
	if !got.StartedAt.Equal(start) || !got.EndedAt.IsZero() {
		t.Errorf("times = %v / %v", got.StartedAt, got.EndedAt)
	}
	if !reflect.DeepEqual(got.QuestsTurnedIn, []int64{90001, 90002}) || !reflect.DeepEqual(got.ZoneMapIDs, []int{85, 1}) {
		t.Errorf("lists = %v / %v", got.QuestsTurnedIn, got.ZoneMapIDs)
	}

	se.Open = false
	se.EndedAt = start.Add(time.Hour)
	se.Deaths = 3
	if err := s.SaveSession(se); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.LastSession()
	if got.Open || got.Deaths != 3 || !got.EndedAt.Equal(start.Add(time.Hour)) {
		t.Errorf("updated = %+v", got)
	}
}

func TestEmptySessionListsStoreAsEmpty(t *testing.T) {
	s := openTemp(t)
	se := &Session{StartedAt: time.Unix(100, 0)}
	if err := s.SaveSession(se); err != nil {
		t.Fatal(err)
	}
	got, err := s.LastSession()
	if err != nil || got == nil {
		t.Fatalf("LastSession = %v, %v", got, err)
	}
	if got.QuestsCompleted == nil || len(got.ZoneMapIDs) != 0 {
		t.Errorf("empty lists = %#v", got)
	}
}

func TestLastSessionIsTheNewestStarted(t *testing.T) {
	s := openTemp(t)
	if last, err := s.LastSession(); err != nil || last != nil {
		t.Fatalf("no sessions: %v, %v", last, err)
	}
	for _, sec := range []int64{300, 100, 200} {
		if err := s.SaveSession(&Session{StartedAt: time.Unix(sec, 0)}); err != nil {
			t.Fatal(err)
		}
	}
	last, _ := s.LastSession()
	if last.StartedAt.Unix() != 300 {
		t.Errorf("last = %v, want the one started at 300", last.StartedAt)
	}
}

func TestCloseOpenSessionsEndsAtLastData(t *testing.T) {
	s := openTemp(t)
	crashed := &Session{StartedAt: time.Unix(1000, 0), LastDataAt: time.Unix(1500, 0), Open: true}
	noData := &Session{StartedAt: time.Unix(2000, 0), Open: true}
	done := &Session{StartedAt: time.Unix(3000, 0), LastDataAt: time.Unix(3100, 0), EndedAt: time.Unix(3100, 0)}
	for _, se := range []*Session{crashed, noData, done} {
		if err := s.SaveSession(se); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.CloseOpenSessions()
	if err != nil || n != 2 {
		t.Fatalf("closed = %d, %v; want 2", n, err)
	}
	last, _ := s.LastSession()
	if last.StartedAt.Unix() != 3000 || last.Open {
		t.Errorf("finished session changed: %+v", last)
	}
	if err := s.SaveSession(&Session{}); err != nil {
		t.Fatal(err)
	}
	var open int
	var ended int64
	if err := s.db.QueryRow(`SELECT open, ended_at FROM sessions WHERE id = ?`, crashed.ID).Scan(&open, &ended); err != nil {
		t.Fatal(err)
	}
	if open != 0 || ended != 1500 {
		t.Errorf("crashed session: open=%d ended=%d, want 0 and 1500", open, ended)
	}
	if n, _ := s.CloseOpenSessions(); n != 0 {
		t.Errorf("second close must find nothing, closed %d", n)
	}
}

func TestSaveSessionRejectsNil(t *testing.T) {
	s := openTemp(t)
	if err := s.SaveSession(nil); err == nil {
		t.Error("nil session must be an error")
	}
}

// TestMigrationFromVersionOneKeepsMessages opens a database as an app of the first
// schema left it (no sessions table), then checks that the migration adds the table and
// keeps the chat history.
func TestMigrationFromVersionOneKeepsMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "companero.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMessage(ChatMsg{Role: "user", Content: "¿qué hago?"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DROP TABLE sessions; DROP TABLE feedback; PRAGMA user_version = 1`); err != nil {
		t.Fatalf("downgrade fixture: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen after v1: %v", err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != latestVersion || latestVersion < 2 {
		t.Fatalf("user_version = %d, want %d", version, latestVersion)
	}
	if err := s.SaveSession(&Session{StartedAt: time.Unix(10, 0)}); err != nil {
		t.Fatalf("sessions table after migration: %v", err)
	}
	msgs, err := s.RecentMessages(10)
	if err != nil || len(msgs) != 1 || msgs[0].Content != "¿qué hago?" {
		t.Fatalf("messages after migration = %+v, %v", msgs, err)
	}
}
