package session

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// fakeClock is the tracker's time source; tests move it by hand.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }
func (c *fakeClock) advance(d time.Duration) {
	c.t = c.t.Add(d)
}

// memStore keeps the saved sessions by value, as the store would.
type memStore struct {
	saves  int
	nextID int64
	rows   []store.Session
	fail   error
}

func (m *memStore) SaveSession(s *store.Session) error {
	if m.fail != nil {
		return m.fail
	}
	m.saves++
	if s.ID == 0 {
		m.nextID++
		s.ID = m.nextID
		m.rows = append(m.rows, *s)
		return nil
	}
	for i := range m.rows {
		if m.rows[i].ID == s.ID {
			m.rows[i] = *s
		}
	}
	return nil
}

func (m *memStore) last() store.Session { return m.rows[len(m.rows)-1] }

func newTest(t *testing.T) (*Tracker, *fakeClock, *memStore) {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 11, 7, 18, 0, 0, 0, time.UTC)}
	ms := &memStore{}
	return NewTracker(clock.now, ms), clock, ms
}

// state returns a live state with the fields the tracker reads.
func state(level, xp uint8, money uint32, mapID uint16) bridge.LiveState {
	return bridge.LiveState{Level: level, XPPct: xp, Money: money, MapID: mapID}
}

// event returns a state after an event with the given code, argument and sequence number.
func event(base bridge.LiveState, seq, code uint8, arg uint32) bridge.LiveState {
	base.EventSeq = seq
	base.LastEventCode = code
	base.EventArg = arg
	return base
}

func TestFirstStateIsBaselineAndCountsNothing(t *testing.T) {
	tr, _, _ := newTest(t)
	tr.Observe(event(state(38, 90, 1000, 85), 7, 3, 90001))
	cur, ok := tr.Current()
	if !ok || !cur.Open {
		t.Fatal("first data must open a session")
	}
	if cur.StartLevel != 38 || cur.StartXPPct != 90 || cur.MoneyStart != 1000 {
		t.Errorf("baseline = %+v", cur)
	}
	if len(cur.QuestsTurnedIn) != 0 {
		t.Error("events of the first state are not replayed")
	}
	if !reflect.DeepEqual(cur.ZoneMapIDs, []int{85}) {
		t.Errorf("zones = %v, want the start map", cur.ZoneMapIDs)
	}
}

func TestXPLevelUpAndMoney(t *testing.T) {
	tr, clock, _ := newTest(t)
	tr.Observe(state(38, 90, 1000, 85))
	clock.advance(time.Second)
	tr.Observe(state(38, 95, 1000, 85)) // +5 %
	clock.advance(time.Second)
	tr.Observe(state(39, 3, 3500, 85)) // level up: 5 to finish + 3 = 8 %
	cur, _ := tr.Current()
	if cur.LevelsGained != 1 || cur.EndLevel != 39 || cur.EndXPPct != 3 {
		t.Errorf("levels = %+v", cur)
	}
	if cur.XPGainedPct != 13 {
		t.Errorf("XP = %v, want 13 percentage points", cur.XPGainedPct)
	}
	if cur.MoneyEnd-cur.MoneyStart != 2500 {
		t.Errorf("money delta = %d", cur.MoneyEnd-cur.MoneyStart)
	}
}

func TestSkippedLevelsCountEachLevel(t *testing.T) {
	tr, clock, _ := newTest(t)
	tr.Observe(state(10, 50, 0, 0))
	clock.advance(time.Second)
	tr.Observe(state(12, 20, 0, 0)) // 50 to finish 10, one full level, then 20
	cur, _ := tr.Current()
	if cur.LevelsGained != 2 || cur.XPGainedPct != 170 {
		t.Errorf("two levels = %+v", cur)
	}
}

func TestEventsQuestsZonesAndDeaths(t *testing.T) {
	tr, clock, _ := newTest(t)
	base := state(38, 10, 0, 85)
	tr.Observe(base)

	clock.advance(time.Second)
	tr.Observe(event(base, 1, 2, 90002)) // objectives complete
	clock.advance(time.Second)
	tr.Observe(event(base, 2, 3, 90001)) // turned in
	clock.advance(time.Second)
	tr.Observe(event(base, 3, 3, 90001)) // same quest again: once
	clock.advance(time.Second)
	tr.Observe(event(base, 4, 4, 1)) // zone changed to Durotar
	clock.advance(time.Second)
	tr.Observe(event(base, 5, 7, 0)) // death
	clock.advance(time.Second)
	tr.Observe(event(base, 6, 7, 0)) // second death

	cur, _ := tr.Current()
	if !reflect.DeepEqual(cur.QuestsCompleted, []int64{90002}) {
		t.Errorf("completed = %v", cur.QuestsCompleted)
	}
	if !reflect.DeepEqual(cur.QuestsTurnedIn, []int64{90001}) {
		t.Errorf("turned in = %v", cur.QuestsTurnedIn)
	}
	if !reflect.DeepEqual(cur.ZoneMapIDs, []int{85, 1}) {
		t.Errorf("zones = %v", cur.ZoneMapIDs)
	}
	if cur.Deaths != 2 {
		t.Errorf("deaths = %d", cur.Deaths)
	}
}

func TestSameEventSequenceIsNotCountedTwice(t *testing.T) {
	tr, clock, _ := newTest(t)
	base := state(38, 10, 0, 85)
	tr.Observe(base)
	clock.advance(time.Second)
	death := event(base, 9, 7, 0)
	tr.Observe(death)
	clock.advance(time.Second)
	tr.Observe(death) // the strip still shows the same event
	cur, _ := tr.Current()
	if cur.Deaths != 1 {
		t.Errorf("deaths = %d, want 1", cur.Deaths)
	}
}

func TestSessionEndsAfterIdleAndNewOneStarts(t *testing.T) {
	tr, clock, ms := newTest(t)
	tr.Observe(state(38, 10, 0, 85))
	clock.advance(time.Minute)
	tr.Observe(state(38, 20, 0, 85))
	clock.advance(IdleAfter - time.Second)
	tr.Tick()
	if _, ok := tr.Current(); !ok {
		t.Fatal("session must still run before IdleAfter")
	}

	// Silence reaches IdleAfter: Tick ends the session at its last data.
	clock.advance(time.Second)
	tr.Tick()
	if _, ok := tr.Current(); ok {
		t.Fatal("session must end after IdleAfter without data")
	}
	ended := ms.last()
	if ended.Open || ended.EndedAt.IsZero() || !ended.EndedAt.Equal(clock.t.Add(-IdleAfter)) {
		t.Errorf("ended session = %+v", ended)
	}
	if ended.XPGainedPct != 10 {
		t.Errorf("XP of the ended session = %v", ended.XPGainedPct)
	}

	// Data after a long silence starts a new session with its own baseline.
	clock.advance(time.Hour)
	tr.Observe(state(40, 5, 0, 1))
	cur, ok := tr.Current()
	if !ok || cur.StartLevel != 40 || cur.XPGainedPct != 0 {
		t.Fatalf("new session = %+v, %v", cur, ok)
	}
	if cur.ID == ended.ID {
		t.Error("the new session must be a new row")
	}
}

func TestObserveAfterLongSilenceClosesOldSession(t *testing.T) {
	tr, clock, ms := newTest(t)
	tr.Observe(state(38, 10, 0, 85))
	clock.advance(IdleAfter + time.Minute)
	tr.Observe(state(38, 30, 0, 85)) // no Tick ran in between
	if len(ms.rows) != 1 || ms.rows[0].Open {
		t.Fatalf("old session must be closed on the next data: %+v", ms.rows)
	}
	cur, _ := tr.Current()
	if cur.ID == ms.rows[0].ID || cur.XPGainedPct != 0 {
		t.Errorf("new session = %+v", cur)
	}
}

func TestCheckpointSavesOpenSessionPeriodically(t *testing.T) {
	tr, clock, ms := newTest(t)
	tr.Observe(state(38, 10, 0, 85))
	tr.Tick()
	if ms.saves != 1 || ms.last().ID == 0 {
		t.Fatalf("first tick must insert the session (saves=%d)", ms.saves)
	}
	clock.advance(time.Second)
	tr.Observe(state(38, 15, 0, 85))
	tr.Tick() // less than checkpointEvery since the last save
	if ms.saves != 1 {
		t.Fatalf("no save before the checkpoint interval (saves=%d)", ms.saves)
	}
	clock.advance(checkpointEvery)
	tr.Tick()
	if ms.saves != 2 || ms.last().XPGainedPct != 5 {
		t.Fatalf("checkpoint must store progress: saves=%d last=%+v", ms.saves, ms.last())
	}
}

func TestCloseEndsRunningSessionOnce(t *testing.T) {
	tr, clock, ms := newTest(t)
	tr.Observe(state(38, 10, 0, 85))
	clock.advance(5 * time.Minute)
	tr.Observe(state(38, 12, 0, 85))
	tr.Close()
	tr.Close()
	if _, ok := tr.Current(); ok {
		t.Fatal("Close must end the session")
	}
	row := ms.last()
	if row.Open || row.EndedAt.IsZero() || row.XPGainedPct != 2 {
		t.Errorf("closed row = %+v", row)
	}
	if ms.saves != 1 {
		t.Errorf("Close must save the open session exactly once (saves=%d)", ms.saves)
	}
}

func TestPersistFailureKeepsSessionDirty(t *testing.T) {
	tr, clock, ms := newTest(t)
	ms.fail = errors.New("disk full")
	tr.Observe(state(38, 10, 0, 85))
	tr.Tick()
	ms.fail = nil
	clock.advance(checkpointEvery)
	tr.Tick()
	if len(ms.rows) != 1 {
		t.Fatalf("the failed save must be retried at the next checkpoint: rows=%d", len(ms.rows))
	}
}

func TestNilPersisterKeepsSessionInMemory(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	tr := NewTracker(clock.now, nil)
	tr.Observe(state(1, 1, 0, 0))
	tr.Close()
	if _, ok := tr.Current(); ok {
		t.Fatal("Close must work without a store")
	}
}

func TestCurrentReturnsCopy(t *testing.T) {
	tr, clock, _ := newTest(t)
	tr.Observe(state(38, 10, 0, 85))
	clock.advance(time.Second)
	tr.Observe(event(state(38, 10, 0, 85), 1, 2, 90001))
	cur, _ := tr.Current()
	cur.QuestsCompleted[0] = 1
	cur.ZoneMapIDs[0] = 999
	again, _ := tr.Current()
	if again.ZoneMapIDs[0] != 85 || len(again.QuestsCompleted) != 1 || again.QuestsCompleted[0] != 90001 {
		t.Errorf("Current must not share slices with the tracker: %+v", again)
	}
}
