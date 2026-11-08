// Package session follows one play session from the live bridge: when it starts and ends,
// the XP and money it gained, the quests done, the zones visited and the deaths. It keeps
// the session in memory while it runs and saves it to the store at checkpoints, at the end
// and on Close, so a session survives a restart as far as its last checkpoint.
//
// A session starts with the first data after IdleAfter without any (or at the very first
// data), and ends after IdleAfter without data, or when Close is called (app exit).
package session

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

const (
	// IdleAfter is the silence that ends a session, and the gap that starts a new one.
	IdleAfter = 10 * time.Minute
	// checkpointEvery is the least time between two saves of an open session.
	checkpointEvery = 30 * time.Second
	// tickEvery is how often Run checks for idleness and saves.
	tickEvery = 5 * time.Second
	// maxPercent is the highest value of the XP percentage the addon reports.
	maxPercent = 100
)

// Event codes that the session reads (the codes of the pixel strip, byte 24).
const (
	evQuestObjectivesComplete = 2
	evQuestTurnedIn           = 3
	evZoneChanged             = 4
	evPlayerDied              = 7
)

// Persister saves a session. *store.Store implements it.
type Persister interface {
	SaveSession(s *store.Session) error
}

// Tracker accumulates the session. Observe is called from the reader's poll goroutine;
// Tick and Close from other goroutines. All methods are safe for concurrent use.
type Tracker struct {
	now  func() time.Time
	idle time.Duration
	st   Persister // may be nil: the session is then only kept in memory

	mu       sync.Mutex
	cur      *store.Session // the open session, nil when none
	prev     bridge.LiveState
	lastData time.Time // when the last state arrived
	dirty    bool      // cur changed since the last save
	lastSave time.Time
}

// NewTracker returns a tracker that reads time from now and saves to st (may be nil).
func NewTracker(now func() time.Time, st Persister) *Tracker {
	if now == nil {
		now = time.Now
	}
	return &Tracker{now: now, idle: IdleAfter, st: st}
}

// Observe feeds one valid state. It is the callback for bridge.Reader.Subscribe.
func (t *Tracker) Observe(st bridge.LiveState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if t.cur != nil && now.Sub(t.lastData) >= t.idle {
		t.endLocked(t.lastData)
	}
	if t.cur == nil {
		t.startLocked(st, now)
	} else {
		t.updateLocked(st)
	}
	t.prev = st
	t.lastData = now
	t.cur.LastDataAt = now
	t.dirty = true
}

// startLocked opens a session. The first state is the baseline: it is not counted as
// progress, and its events (if any) are not replayed.
func (t *Tracker) startLocked(st bridge.LiveState, now time.Time) {
	s := &store.Session{
		StartedAt:  now,
		LastDataAt: now,
		Open:       true,
		StartLevel: int(st.Level),
		EndLevel:   int(st.Level),
		StartXPPct: validPct(st.XPPct),
		EndXPPct:   validPct(st.XPPct),
		MoneyStart: int64(st.Money),
		MoneyEnd:   int64(st.Money),
	}
	if st.MapID != 0 {
		s.ZoneMapIDs = appendUnique(s.ZoneMapIDs, int(st.MapID))
	}
	t.cur = s
	t.lastSave = time.Time{}
}

// updateLocked counts what changed since the previous state of the session.
func (t *Tracker) updateLocked(st bridge.LiveState) {
	s := t.cur
	prev := t.prev

	if st.Level > 0 && prev.Level > 0 && st.XPPct <= maxPercent && prev.XPPct <= maxPercent {
		switch {
		case st.Level > prev.Level:
			levels := int(st.Level) - int(prev.Level)
			s.LevelsGained += levels
			s.XPGainedPct += float64(maxPercent-int(prev.XPPct)) + float64(levels-1)*maxPercent + float64(st.XPPct)
		case st.Level == prev.Level && st.XPPct > prev.XPPct:
			s.XPGainedPct += float64(st.XPPct - prev.XPPct)
		}
	}
	if st.Level > 0 {
		s.EndLevel = int(st.Level)
		s.EndXPPct = validPct(st.XPPct)
	}
	s.MoneyEnd = int64(st.Money)

	if st.MapID != 0 && st.MapID != prev.MapID {
		s.ZoneMapIDs = appendUnique(s.ZoneMapIDs, int(st.MapID))
	}
	if st.EventSeq != prev.EventSeq {
		t.eventLocked(st.LastEventCode, st.EventArg)
	}
}

// eventLocked applies one event of the strip: the quest, zone or death it reports.
func (t *Tracker) eventLocked(code uint8, arg uint32) {
	s := t.cur
	switch code {
	case evQuestObjectivesComplete:
		if arg != 0 {
			s.QuestsCompleted = appendUniqueID(s.QuestsCompleted, int64(arg))
		}
	case evQuestTurnedIn:
		if arg != 0 {
			s.QuestsTurnedIn = appendUniqueID(s.QuestsTurnedIn, int64(arg))
		}
	case evZoneChanged:
		if arg != 0 {
			s.ZoneMapIDs = appendUnique(s.ZoneMapIDs, int(arg))
		}
	case evPlayerDied:
		s.Deaths++
	}
}

// Tick ends the session when it has been silent for IdleAfter, and saves an open session
// that changed since the last checkpoint.
func (t *Tracker) Tick() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cur == nil {
		return
	}
	now := t.now()
	if now.Sub(t.lastData) >= t.idle {
		t.endLocked(t.lastData)
		return
	}
	if t.dirty && now.Sub(t.lastSave) >= checkpointEvery {
		t.saveLocked(now)
	}
}

// Close ends the open session now (app exit) and saves it. It is safe to call twice.
func (t *Tracker) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cur == nil {
		return
	}
	at := t.now()
	if at.Before(t.lastData) {
		at = t.lastData
	}
	t.endLocked(at)
}

// Run calls Tick every few seconds until ctx ends. It does not close the session: the
// owner calls Close when the app exits.
func (t *Tracker) Run(ctx context.Context) {
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.Tick()
		}
	}
}

// Current returns a copy of the open session, if there is one.
func (t *Tracker) Current() (store.Session, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cur == nil {
		return store.Session{}, false
	}
	return copySession(*t.cur), true
}

// endLocked closes the open session at the given time and saves it.
func (t *Tracker) endLocked(at time.Time) {
	s := t.cur
	s.EndedAt = at
	s.Open = false
	t.cur = nil
	t.dirty = true
	t.saveSession(s, at)
}

func (t *Tracker) saveLocked(now time.Time) {
	t.saveSession(t.cur, now)
}

// saveSession writes s through the persister. A failure is logged; the next checkpoint
// tries again while the session is open.
func (t *Tracker) saveSession(s *store.Session, now time.Time) {
	t.lastSave = now
	if t.st == nil {
		t.dirty = false
		return
	}
	if err := t.st.SaveSession(s); err != nil {
		log.Printf("sesión: guardar: %v", err)
		t.dirty = true
		return
	}
	t.dirty = false
}

func validPct(v uint8) int {
	if v > maxPercent {
		return 0
	}
	return int(v)
}

func appendUnique(list []int, v int) []int {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func appendUniqueID(list []int64, v int64) []int64 {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func copySession(s store.Session) store.Session {
	s.QuestsCompleted = append([]int64(nil), s.QuestsCompleted...)
	s.QuestsTurnedIn = append([]int64(nil), s.QuestsTurnedIn...)
	s.ZoneMapIDs = append([]int(nil), s.ZoneMapIDs...)
	return s
}
