package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Session is one play session as stored: what happened between the first data of a
// session and its end. Money is in copper. The session package fills it in; the store
// only keeps it.
type Session struct {
	ID              int64
	StartedAt       time.Time
	LastDataAt      time.Time
	EndedAt         time.Time // zero while the session is open
	Open            bool
	StartLevel      int
	EndLevel        int
	LevelsGained    int
	StartXPPct      int
	EndXPPct        int
	XPGainedPct     float64 // percentage points of a level, summed over the session
	MoneyStart      int64
	MoneyEnd        int64
	QuestsCompleted []int64 // objectives completed (event 2), unique, in order
	QuestsTurnedIn  []int64 // turned in (event 3), unique, in order
	ZoneMapIDs      []int   // zones visited, unique, in order
	Deaths          int
}

const sessionCols = `id, started_at, last_data_at, ended_at, open, start_level, end_level,
	levels_gained, start_xp_pct, end_xp_pct, xp_gained_pct, money_start, money_end,
	quests_completed, quests_turned_in, zones, deaths`

// SaveSession inserts a new session (ID 0, which it sets) or updates the stored one.
func (s *Store) SaveSession(se *Session) error {
	if se == nil {
		return errors.New("save session: nil session")
	}
	completed, err := json.Marshal(nonNilIDs(se.QuestsCompleted))
	if err != nil {
		return fmt.Errorf("save session: encode quests: %w", err)
	}
	turned, err := json.Marshal(nonNilIDs(se.QuestsTurnedIn))
	if err != nil {
		return fmt.Errorf("save session: encode turn-ins: %w", err)
	}
	zones, err := json.Marshal(nonNilZones(se.ZoneMapIDs))
	if err != nil {
		return fmt.Errorf("save session: encode zones: %w", err)
	}
	vals := []any{
		unixOrZero(se.StartedAt), unixOrZero(se.LastDataAt), unixOrZero(se.EndedAt), boolInt(se.Open),
		se.StartLevel, se.EndLevel, se.LevelsGained, se.StartXPPct, se.EndXPPct, se.XPGainedPct,
		se.MoneyStart, se.MoneyEnd, string(completed), string(turned), string(zones), se.Deaths,
	}
	if se.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO sessions (started_at, last_data_at, ended_at, open, start_level,
			end_level, levels_gained, start_xp_pct, end_xp_pct, xp_gained_pct, money_start, money_end,
			quests_completed, quests_turned_in, zones, deaths)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, vals...)
		if err != nil {
			return fmt.Errorf("insert session: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("session id: %w", err)
		}
		se.ID = id
		return nil
	}
	_, err = s.db.Exec(`UPDATE sessions SET started_at = ?, last_data_at = ?, ended_at = ?, open = ?,
		start_level = ?, end_level = ?, levels_gained = ?, start_xp_pct = ?, end_xp_pct = ?,
		xp_gained_pct = ?, money_start = ?, money_end = ?, quests_completed = ?, quests_turned_in = ?,
		zones = ?, deaths = ? WHERE id = ?`, append(vals, se.ID)...)
	if err != nil {
		return fmt.Errorf("update session %d: %w", se.ID, err)
	}
	return nil
}

// LastSession returns the most recently started session, or nil when there is none.
func (s *Store) LastSession() (*Session, error) {
	row := s.db.QueryRow(`SELECT ` + sessionCols + ` FROM sessions ORDER BY started_at DESC, id DESC LIMIT 1`)
	se, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read last session: %w", err)
	}
	return &se, nil
}

// CloseOpenSessions marks sessions left open by an earlier run (a crash, a kill) as
// ended at their last data time. It returns how many were closed.
func (s *Store) CloseOpenSessions() (int64, error) {
	res, err := s.db.Exec(`UPDATE sessions SET open = 0,
		ended_at = CASE WHEN last_data_at > 0 THEN last_data_at ELSE started_at END
		WHERE open = 1`)
	if err != nil {
		return 0, fmt.Errorf("close open sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count closed sessions: %w", err)
	}
	return n, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(sc rowScanner) (Session, error) {
	var (
		se                       Session
		started, lastData, ended int64
		open                     int
		completed, turned, zones string
	)
	err := sc.Scan(&se.ID, &started, &lastData, &ended, &open, &se.StartLevel, &se.EndLevel,
		&se.LevelsGained, &se.StartXPPct, &se.EndXPPct, &se.XPGainedPct, &se.MoneyStart, &se.MoneyEnd,
		&completed, &turned, &zones, &se.Deaths)
	if err != nil {
		return Session{}, err
	}
	se.StartedAt = timeOf(started)
	se.LastDataAt = timeOf(lastData)
	se.EndedAt = timeOf(ended)
	se.Open = open != 0
	if err := json.Unmarshal([]byte(completed), &se.QuestsCompleted); err != nil {
		return Session{}, fmt.Errorf("session %d quests: %w", se.ID, err)
	}
	if err := json.Unmarshal([]byte(turned), &se.QuestsTurnedIn); err != nil {
		return Session{}, fmt.Errorf("session %d turn-ins: %w", se.ID, err)
	}
	if err := json.Unmarshal([]byte(zones), &se.ZoneMapIDs); err != nil {
		return Session{}, fmt.Errorf("session %d zones: %w", se.ID, err)
	}
	return se, nil
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func timeOf(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

func nonNilIDs(v []int64) []int64 {
	if v == nil {
		return []int64{}
	}
	return v
}

func nonNilZones(v []int) []int {
	if v == nil {
		return []int{}
	}
	return v
}
