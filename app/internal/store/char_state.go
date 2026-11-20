package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// The character state: the newest body of each section of a character, as the readers see it in
// the SavedVariables character table ("sheet", "equipment", "questWindow", "gearCandidates",
// "session", "macros", "actionBars", "talents", ...). The channel messages and the outbox items
// of schema 2 write it (applyItem); the legacy SavedVariables blocks of schema 1 do not, because
// the readers take them as the last resort (live overlay > store > schema 1).
//
// A section is replaced as a whole. Its newest copy is the one with the latest time (at, the time
// the addon learned it, or the time it arrived) and, for the same time, the highest oseq.

// charStateMigration is migration 6: the table of the character state.
var charStateMigration = []string{
	`CREATE TABLE char_state (
		char_key TEXT    NOT NULL,
		section  TEXT    NOT NULL,
		body     TEXT    NOT NULL,
		oseq     INTEGER NOT NULL DEFAULT 0,
		at       INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (char_key, section)
	)`,
	`CREATE INDEX char_state_at ON char_state(char_key, at)`,
}

// stateSections returns the sections a message body of msgType gives to its character, as the
// SavedVariables character table names them. A message with no section (combats, knowledge, the
// world) returns nil: those have their own tables.
func stateSections(msgType uint8, body map[string]any) map[string]any {
	switch msgType {
	case bridge.MsgCombat, bridge.MsgKnowledge, bridge.MsgWorld, bridge.MsgMeta, bridge.MsgQuestion,
		bridge.MsgCompletedQuests:
		return nil
	case bridge.MsgSheet:
		return sectionOf("sheet", body)
	case bridge.MsgMacros:
		return sectionOf("macros", body)
	case bridge.MsgSession:
		return sectionOf("session", body)
	}
	out := make(map[string]any, len(body))
	for k, v := range body {
		if v != nil {
			out[k] = v
		}
	}
	return out
}

// sectionOf names a whole body as one section (a body that is the section itself).
func sectionOf(name string, body map[string]any) map[string]any {
	if len(body) == 0 {
		return nil
	}
	return map[string]any{name: body}
}

// saveState writes the sections of one applied message for charKey, stamped at (and oseq). The
// completed quests are a list (or {"add": [...]}) that the state keeps whole.
func (in *ingester) saveState(msgType uint8, body map[string]any, raw any, charKey string, at time.Time, oseq int64) error {
	if charKey == "" {
		return nil
	}
	sections := stateSections(msgType, body)
	if msgType == bridge.MsgCompletedQuests {
		sections = map[string]any{"completedQuests": completedState(in.tx, charKey, raw)}
	}
	for name, v := range sections {
		if v == nil {
			continue
		}
		if err := in.putSection(charKey, name, v, at, oseq); err != nil {
			return err
		}
	}
	return nil
}

// completedState returns the list of completed quest ids the message gives: a full list, or the
// previous list with the ids of {"add": [...]} added.
func completedState(tx *sql.Tx, charKey string, raw any) any {
	switch t := raw.(type) {
	case []any:
		return t
	case map[string]any:
		if list, ok := t["list"].([]any); ok {
			return list
		}
		add, _ := t["add"].([]any)
		var old []any
		var body string
		if err := tx.QueryRow(`SELECT body FROM char_state WHERE char_key = ? AND section = 'completedQuests'`,
			charKey).Scan(&body); err == nil {
			_ = json.Unmarshal([]byte(body), &old)
		}
		seen := make(map[any]bool, len(old)+len(add))
		out := make([]any, 0, len(old)+len(add))
		for _, id := range append(append([]any{}, old...), add...) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		return out
	}
	return nil
}

// putSection stores one section unless the stored copy is newer.
func (in *ingester) putSection(charKey, section string, v any, at time.Time, oseq int64) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode section %s: %w", section, err)
	}
	if _, err := in.tx.Exec(`
		INSERT INTO char_state (char_key, section, body, oseq, at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(char_key, section) DO UPDATE SET
			body = excluded.body, oseq = excluded.oseq, at = excluded.at
		WHERE excluded.at > char_state.at OR (excluded.at = char_state.at AND excluded.oseq >= char_state.oseq)`,
		charKey, section, string(raw), oseq, at.Unix()); err != nil {
		return fmt.Errorf("write section %s of %s: %w", section, charKey, err)
	}
	return nil
}

// CharState is the stored state of one character: its sections and the newest time any of them was
// learned.
type CharState struct {
	Key      string
	Sections map[string]any
	Latest   time.Time
}

// CharState returns the sections stored for charKey (nil Sections when there are none) and the
// newest time among them.
func (s *Store) CharState(charKey string) (CharState, error) {
	out := CharState{Key: charKey, Sections: map[string]any{}}
	rows, err := s.db.Query(`SELECT section, body, at FROM char_state WHERE char_key = ?`, charKey)
	if err != nil {
		return out, fmt.Errorf("read character state: %w", err)
	}
	defer rows.Close()
	var latest int64
	for rows.Next() {
		var name, body string
		var at int64
		if err := rows.Scan(&name, &body, &at); err != nil {
			return out, fmt.Errorf("read character state: %w", err)
		}
		var v any
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			continue // a broken section is left out; the next message of its kind replaces it
		}
		out.Sections[name] = v
		if at > latest {
			latest = at
		}
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("read character state: %w", err)
	}
	if latest > 0 {
		out.Latest = time.Unix(latest, 0)
	}
	return out, nil
}

// CharKeysState returns the keys of the characters that have stored state, with the newest time of each.
func (s *Store) CharKeysState() (map[string]time.Time, error) {
	rows, err := s.db.Query(`SELECT char_key, MAX(at) FROM char_state WHERE char_key <> '' GROUP BY char_key`)
	if err != nil {
		return nil, fmt.Errorf("read character keys: %w", err)
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var key string
		var at int64
		if err := rows.Scan(&key, &at); err != nil {
			return nil, fmt.Errorf("read character keys: %w", err)
		}
		out[key] = time.Unix(at, 0)
	}
	return out, rows.Err()
}

// ActiveRank ranks a character of the SavedVariables table by how recently the player was on it: the
// newer of its updatedAt and loggedOutAt (Unix seconds, 0 when neither is set). The character with the
// highest rank is the one the player plays now.
func ActiveRank(ch map[string]any) float64 {
	ts := rawNum(ch, "updatedAt")
	if out := rawNum(ch, "loggedOutAt"); out > ts {
		ts = out
	}
	return ts
}
