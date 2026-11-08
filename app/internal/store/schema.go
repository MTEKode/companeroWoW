package store

import (
	"database/sql"
	"fmt"
)

// migrations[i] upgrades the schema from user_version i to i+1.
var migrations = [][]string{
	{
		`CREATE TABLE quests (
			quest_id          INTEGER PRIMARY KEY,
			title             TEXT    NOT NULL DEFAULT '',
			objectives_text   TEXT    NOT NULL DEFAULT '',
			description_text  TEXT    NOT NULL DEFAULT '',
			level             INTEGER NOT NULL DEFAULT 0,
			giver_npc_id      INTEGER NOT NULL DEFAULT 0,
			giver_name        TEXT    NOT NULL DEFAULT '',
			giver_map_id      INTEGER NOT NULL DEFAULT 0,
			giver_x           REAL    NOT NULL DEFAULT 0,
			giver_y           REAL    NOT NULL DEFAULT 0,
			turnin_npc_id     INTEGER NOT NULL DEFAULT 0,
			turnin_name       TEXT    NOT NULL DEFAULT '',
			turnin_map_id     INTEGER NOT NULL DEFAULT 0,
			turnin_x          REAL    NOT NULL DEFAULT 0,
			turnin_y          REAL    NOT NULL DEFAULT 0,
			rewards_json      TEXT    NOT NULL DEFAULT '',
			first_seen        INTEGER NOT NULL DEFAULT 0,
			last_seen         INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE npcs (
			npc_id           INTEGER PRIMARY KEY,
			name             TEXT    NOT NULL DEFAULT '',
			map_id           INTEGER NOT NULL DEFAULT 0,
			x                REAL    NOT NULL DEFAULT 0,
			y                REAL    NOT NULL DEFAULT 0,
			is_quest_giver   INTEGER NOT NULL DEFAULT 0,
			is_vendor        INTEGER NOT NULL DEFAULT 0,
			is_trainer       INTEGER NOT NULL DEFAULT 0,
			trainer_type     TEXT    NOT NULL DEFAULT '',
			reaction         TEXT    NOT NULL DEFAULT '',
			first_seen       INTEGER NOT NULL DEFAULT 0,
			last_seen        INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX npcs_map ON npcs(map_id)`,
		`CREATE INDEX npcs_name ON npcs(name)`,
		`CREATE INDEX quests_title ON quests(title)`,
		`CREATE TABLE maps (
			map_id INTEGER PRIMARY KEY,
			name   TEXT NOT NULL
		)`,
		`CREATE TABLE cache (
			key        TEXT PRIMARY KEY,
			value      TEXT    NOT NULL,
			fetched_at INTEGER NOT NULL
		)`,
		`CREATE TABLE messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			role       TEXT    NOT NULL,
			content    TEXT    NOT NULL,
			provider   TEXT    NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
	},
}

// latestVersion is the user_version after all migrations have run.
var latestVersion = len(migrations)

func migrate(db *sql.DB) error {
	var current int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current > latestVersion {
		return fmt.Errorf("database schema version %d is newer than this app supports (%d)", current, latestVersion)
	}
	for v := current; v < latestVersion; v++ {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", v+1, err)
		}
		for _, stmt := range migrations[v] {
			if _, err := tx.Exec(stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration %d: %w", v+1, err)
			}
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", v+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("set schema version %d: %w", v+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", v+1, err)
		}
	}
	return nil
}
