package store

import (
	"fmt"
	"time"
)

// Retention of the history the addon sends: the newest maxCombats encounters (all characters) and
// the daily snapshots of the last snapshotDays days. Everything else is kept.
const (
	maxCombats    = 5000
	snapshotDays  = 365
	svImportsKeep = 200 // the newest imports of the SavedVariables file are kept (it is saved often)
)

// trimHistory deletes the combats beyond the newest maxCombats (with their spells, damage taken and
// deaths) and the snapshots older than snapshotDays. It returns the number of combats and snapshots removed.
func (s *Store) trimHistory(now time.Time) (combats, snapshots int64, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("trim history: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	res, err := tx.Exec(`DELETE FROM character_snapshots WHERE taken_at < ?`,
		now.AddDate(0, 0, -snapshotDays).Unix())
	if err != nil {
		return 0, 0, fmt.Errorf("trim snapshots: %w", err)
	}
	snapshots, _ = res.RowsAffected()

	// The imports of the SavedVariables file: the newest svImportsKeep are kept (the file is saved often).
	if _, err = tx.Exec(`DELETE FROM sv_imports WHERE id NOT IN (SELECT id FROM sv_imports ORDER BY id DESC LIMIT ?)`, svImportsKeep); err != nil {
		return 0, 0, fmt.Errorf("trim imports: %w", err)
	}

	const keep = `SELECT char_key, combat_id FROM combats ORDER BY started_at DESC, combat_id DESC LIMIT ` + "5000"
	for _, table := range []string{"combat_spells", "combat_taken", "deaths"} {
		if _, err = tx.Exec(`DELETE FROM ` + table + ` WHERE (char_key, combat_id) NOT IN (` + keep + `)`); err != nil {
			return 0, 0, fmt.Errorf("trim %s: %w", table, err)
		}
	}
	res, err = tx.Exec(`DELETE FROM combats WHERE (char_key, combat_id) NOT IN (` + keep + `)`)
	if err != nil {
		return 0, 0, fmt.Errorf("trim combats: %w", err)
	}
	combats, _ = res.RowsAffected()
	if err = tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("trim history: %w", err)
	}
	return combats, snapshots, nil
}
