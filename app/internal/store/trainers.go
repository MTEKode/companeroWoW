package store

import "fmt"

// Trainers returns up to limit known trainers (is_trainer), ordered by name. The
// profession plan matches them to the player's professions by trainer type and name.
func (s *Store) Trainers(limit int) ([]KNpc, error) {
	rows, err := s.db.Query(
		`SELECT `+npcCols+` FROM npcs WHERE is_trainer = 1 ORDER BY name, npc_id LIMIT ?`,
		clampLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list trainers: %w", err)
	}
	return collectNpcs(rows)
}
