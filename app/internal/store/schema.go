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
	{
		// One row per play session (see internal/session). Times are Unix seconds; the
		// lists are JSON arrays of ids.
		`CREATE TABLE sessions (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			started_at       INTEGER NOT NULL,
			last_data_at     INTEGER NOT NULL DEFAULT 0,
			ended_at         INTEGER NOT NULL DEFAULT 0,
			open             INTEGER NOT NULL DEFAULT 0,
			start_level      INTEGER NOT NULL DEFAULT 0,
			end_level        INTEGER NOT NULL DEFAULT 0,
			levels_gained    INTEGER NOT NULL DEFAULT 0,
			start_xp_pct     INTEGER NOT NULL DEFAULT 0,
			end_xp_pct       INTEGER NOT NULL DEFAULT 0,
			xp_gained_pct    REAL    NOT NULL DEFAULT 0,
			money_start      INTEGER NOT NULL DEFAULT 0,
			money_end        INTEGER NOT NULL DEFAULT 0,
			quests_completed TEXT    NOT NULL DEFAULT '[]',
			quests_turned_in TEXT    NOT NULL DEFAULT '[]',
			zones            TEXT    NOT NULL DEFAULT '[]',
			deaths           INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX sessions_started ON sessions(started_at)`,
	},
	{
		// One row per 👍/👎 the user gives an answer (see feedback.go). Times are Unix
		// nanoseconds, like messages; tool_calls is a JSON array of tool names, empty when
		// the provider did not report them.
		`CREATE TABLE feedback (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at    INTEGER NOT NULL,
			rating        INTEGER NOT NULL,
			reason        TEXT    NOT NULL DEFAULT '',
			question      TEXT    NOT NULL DEFAULT '',
			answer        TEXT    NOT NULL DEFAULT '',
			provider      TEXT    NOT NULL DEFAULT '',
			skill_version TEXT    NOT NULL DEFAULT '',
			tool_calls    TEXT    NOT NULL DEFAULT '[]'
		)`,
		`CREATE INDEX feedback_created ON feedback(created_at)`,
	},
	{
		// wowforevertalents.com (see websearch/wft*.go). wft_pages keeps the raw HTML of each
		// page with its validators (ETag, Last-Modified) and the patch it was read under;
		// wft_sitemap is the URL index; wft_meta holds timestamps and the current patch;
		// wft_parsed caches the structured data the parsers extract from a page.
		`CREATE TABLE wft_pages (
			url           TEXT    PRIMARY KEY,
			body          TEXT    NOT NULL,
			etag          TEXT    NOT NULL DEFAULT '',
			last_modified TEXT    NOT NULL DEFAULT '',
			patch         TEXT    NOT NULL DEFAULT '',
			fetched_at    INTEGER NOT NULL
		)`,
		`CREATE TABLE wft_sitemap (
			url     TEXT PRIMARY KEY,
			slug    TEXT NOT NULL DEFAULT '',
			title   TEXT NOT NULL DEFAULT '',
			section TEXT NOT NULL DEFAULT '',
			cls     TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX wft_sitemap_section ON wft_sitemap(section, cls)`,
		`CREATE TABLE wft_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE wft_parsed (
			key        TEXT    PRIMARY KEY,
			kind       TEXT    NOT NULL,
			json       TEXT    NOT NULL,
			patch      TEXT    NOT NULL DEFAULT '',
			fetched_at INTEGER NOT NULL
		)`,
	},
	appSchemaMigration(), // migration 5: normalized tables, session key, read-only views (schema_app.go, views.go)
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
