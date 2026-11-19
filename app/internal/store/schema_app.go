package store

// The normalized schema of the app's system of record (migration 5). The addon ships its
// knowledge, world data, combats, sessions and macros through the channel or in its
// SavedVariables outbox; ingest.go writes them here by natural key, and the read-only views
// (views.go) are the only part of the database the AI may query (see consultar_db and VIEWS.md).
//
// Conventions: char_key is the addon's character key ("Name-RealmNoSpaces"; "" when the
// character is not known yet). Times are Unix seconds unless a column says otherwise. Money is
// copper. Positions are the addon's 0..1 map coordinates.

// normalizedTables are the base tables. Only this package reads them; the AI reads the views.
var normalizedTables = []string{
	// Characters: the core fields of the newest data, and one snapshot per day (history).
	`CREATE TABLE characters (
		char_key   TEXT PRIMARY KEY,
		name       TEXT    NOT NULL DEFAULT '',
		realm      TEXT    NOT NULL DEFAULT '',
		race       TEXT    NOT NULL DEFAULT '',
		class      TEXT    NOT NULL DEFAULT '',
		level      INTEGER NOT NULL DEFAULT 0,
		xp         INTEGER NOT NULL DEFAULT 0,
		xp_max     INTEGER NOT NULL DEFAULT 0,
		money      INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0,
		seen_at    INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE character_snapshots (
		char_key   TEXT    NOT NULL,
		day        TEXT    NOT NULL,
		level      INTEGER NOT NULL DEFAULT 0,
		xp         INTEGER NOT NULL DEFAULT 0,
		money      INTEGER NOT NULL DEFAULT 0,
		sheet_json TEXT    NOT NULL DEFAULT '',
		taken_at   INTEGER NOT NULL,
		PRIMARY KEY (char_key, day)
	)`,
	`CREATE INDEX character_snapshots_taken ON character_snapshots(taken_at)`,
	// Quest log per character (what the character carries now) and its objectives.
	`CREATE TABLE quest_log (
		char_key  TEXT    NOT NULL,
		quest_id  INTEGER NOT NULL,
		title     TEXT    NOT NULL DEFAULT '',
		level     INTEGER NOT NULL DEFAULT 0,
		complete  INTEGER NOT NULL DEFAULT 0,
		seen_at   INTEGER NOT NULL,
		PRIMARY KEY (char_key, quest_id)
	)`,
	`CREATE TABLE quest_objectives (
		char_key  TEXT    NOT NULL,
		quest_id  INTEGER NOT NULL,
		idx       INTEGER NOT NULL,
		text      TEXT    NOT NULL DEFAULT '',
		fulfilled INTEGER NOT NULL DEFAULT 0,
		required  INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (char_key, quest_id, idx)
	)`,
	// Quests the character can take now (the addon's quest window), with starter and finisher.
	`CREATE TABLE quest_window (
		char_key       TEXT    NOT NULL,
		quest_id       INTEGER NOT NULL,
		name           TEXT    NOT NULL DEFAULT '',
		level          INTEGER NOT NULL DEFAULT 0,
		required_level INTEGER NOT NULL DEFAULT 0,
		zone           TEXT    NOT NULL DEFAULT '',
		accessible     INTEGER NOT NULL DEFAULT 0,
		source         TEXT    NOT NULL DEFAULT '',
		in_log         INTEGER NOT NULL DEFAULT 0,
		missing_json   TEXT    NOT NULL DEFAULT '[]',
		starter_json   TEXT    NOT NULL DEFAULT '{}',
		finisher_json  TEXT    NOT NULL DEFAULT '{}',
		rewards_json   TEXT    NOT NULL DEFAULT '{}',
		seen_at        INTEGER NOT NULL,
		PRIMARY KEY (char_key, quest_id)
	)`,
	`CREATE TABLE completed_quests (
		char_key TEXT    NOT NULL,
		quest_id INTEGER NOT NULL,
		seen_at  INTEGER NOT NULL,
		PRIMARY KEY (char_key, quest_id)
	)`,
	// Rewards of the knowledge quests: kind is reward | choice | money | xp; item_id is 0 for money and xp.
	`CREATE TABLE quest_rewards (
		quest_id INTEGER NOT NULL,
		kind     TEXT    NOT NULL,
		item_id  INTEGER NOT NULL DEFAULT 0,
		amount   INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (quest_id, kind, item_id)
	)`,
	`CREATE TABLE quest_chains (
		quest_id   INTEGER PRIMARY KEY,
		next_id    INTEGER NOT NULL DEFAULT 0,
		chain_name TEXT    NOT NULL DEFAULT '',
		name       TEXT    NOT NULL DEFAULT '',
		pre_json   TEXT    NOT NULL DEFAULT '[]'
	)`,
	// Roles of the NPCs: quest_giver | turnin | vendor | trainer. The npcs flags stay in step.
	`CREATE TABLE npc_roles (
		npc_id INTEGER NOT NULL,
		role   TEXT    NOT NULL,
		map_id INTEGER NOT NULL DEFAULT 0,
		x      REAL    NOT NULL DEFAULT 0,
		y      REAL    NOT NULL DEFAULT 0,
		PRIMARY KEY (npc_id, role)
	)`,
	`CREATE TABLE vendors (
		npc_id     INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL DEFAULT '',
		map_id     INTEGER NOT NULL DEFAULT 0,
		x          REAL    NOT NULL DEFAULT 0,
		y          REAL    NOT NULL DEFAULT 0,
		faction    TEXT    NOT NULL DEFAULT '',
		updated_at INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE vendor_items (
		npc_id       INTEGER NOT NULL,
		item_id      INTEGER NOT NULL,
		name         TEXT    NOT NULL DEFAULT '',
		price        INTEGER NOT NULL DEFAULT 0,
		stack        INTEGER NOT NULL DEFAULT 0,
		limited      INTEGER NOT NULL DEFAULT 0,
		requires_rep TEXT    NOT NULL DEFAULT '',
		PRIMARY KEY (npc_id, item_id)
	)`,
	`CREATE TABLE trainers (
		npc_id     INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL DEFAULT '',
		map_id     INTEGER NOT NULL DEFAULT 0,
		x          REAL    NOT NULL DEFAULT 0,
		y          REAL    NOT NULL DEFAULT 0,
		kind       TEXT    NOT NULL DEFAULT '',
		profession TEXT    NOT NULL DEFAULT '',
		updated_at INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE trainer_services (
		npc_id     INTEGER NOT NULL,
		name       TEXT    NOT NULL,
		rank       TEXT    NOT NULL DEFAULT '',
		cost       INTEGER NOT NULL DEFAULT 0,
		level      INTEGER NOT NULL DEFAULT 0,
		skill_name TEXT    NOT NULL DEFAULT '',
		skill_rank INTEGER NOT NULL DEFAULT 0,
		available  TEXT    NOT NULL DEFAULT '',
		PRIMARY KEY (npc_id, name, rank)
	)`,
	// Flights: the nodes (per faction), the measured routes, and the nodes each character knows.
	`CREATE TABLE flight_nodes (
		node_id INTEGER PRIMARY KEY,
		name    TEXT    NOT NULL DEFAULT '',
		map_id  INTEGER NOT NULL DEFAULT 0,
		faction TEXT    NOT NULL DEFAULT '',
		x       REAL    NOT NULL DEFAULT 0,
		y       REAL    NOT NULL DEFAULT 0,
		has_pos INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE flight_routes (
		from_id INTEGER NOT NULL,
		to_id   INTEGER NOT NULL,
		copper  INTEGER NOT NULL DEFAULT 0,
		hops    INTEGER NOT NULL DEFAULT 0,
		seconds REAL    NOT NULL DEFAULT 0,
		PRIMARY KEY (from_id, to_id)
	)`,
	`CREATE TABLE flight_known (
		char_key TEXT    NOT NULL,
		node_id  INTEGER NOT NULL,
		PRIMARY KEY (char_key, node_id)
	)`,
	// Items seen on the gear, on vendors and in quests: the record and its stats.
	`CREATE TABLE items (
		item_id        INTEGER PRIMARY KEY,
		name           TEXT    NOT NULL DEFAULT '',
		link           TEXT    NOT NULL DEFAULT '',
		item_level     INTEGER NOT NULL DEFAULT 0,
		required_level INTEGER NOT NULL DEFAULT 0,
		quality        INTEGER NOT NULL DEFAULT 0,
		equip_loc      TEXT    NOT NULL DEFAULT '',
		class          TEXT    NOT NULL DEFAULT '',
		subclass       TEXT    NOT NULL DEFAULT '',
		weapon_min     REAL    NOT NULL DEFAULT 0,
		weapon_max     REAL    NOT NULL DEFAULT 0,
		weapon_speed   REAL    NOT NULL DEFAULT 0,
		updated_at     INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE item_stats (
		item_id INTEGER NOT NULL,
		stat    TEXT    NOT NULL,
		value   REAL    NOT NULL DEFAULT 0,
		PRIMARY KEY (item_id, stat)
	)`,
	`CREATE TABLE consumables (
		item_id        INTEGER PRIMARY KEY,
		name           TEXT    NOT NULL DEFAULT '',
		kind           TEXT    NOT NULL DEFAULT '',
		required_level INTEGER NOT NULL DEFAULT 0,
		health         INTEGER NOT NULL DEFAULT 0,
		mana           INTEGER NOT NULL DEFAULT 0,
		seconds        INTEGER NOT NULL DEFAULT 0,
		skill_name     TEXT    NOT NULL DEFAULT '',
		skill_rank     INTEGER NOT NULL DEFAULT 0,
		updated_at     INTEGER NOT NULL DEFAULT 0
	)`,
	// Equipment history: a row each time the item in a slot changes; the newest row per slot is the current one.
	`CREATE TABLE equipment_history (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		char_key   TEXT    NOT NULL,
		slot       INTEGER NOT NULL,
		item_id    INTEGER NOT NULL,
		changed_at INTEGER NOT NULL
	)`,
	`CREATE INDEX equipment_history_char ON equipment_history(char_key, slot, changed_at)`,
	// Gear candidates of a character: the equippable rewards and loot it can get. Each snapshot replaces the last.
	`CREATE TABLE gear_candidates (
		char_key    TEXT    NOT NULL,
		item_id     INTEGER NOT NULL,
		source_kind TEXT    NOT NULL DEFAULT '',
		quest_id    INTEGER NOT NULL DEFAULT 0,
		dungeon_key TEXT    NOT NULL DEFAULT '',
		boss        TEXT    NOT NULL DEFAULT '',
		source      TEXT    NOT NULL DEFAULT '',
		choice      INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (char_key, item_id, source_kind, quest_id, dungeon_key, boss, source)
	)`,
	// Combats. The addon keeps the last ten per character; the app keeps up to 5000 in total.
	`CREATE TABLE combats (
		char_key   TEXT    NOT NULL,
		combat_id  INTEGER NOT NULL,
		started_at INTEGER NOT NULL DEFAULT 0,
		duration   REAL    NOT NULL DEFAULT 0,
		zone       TEXT    NOT NULL DEFAULT '',
		damage     REAL    NOT NULL DEFAULT 0,
		dps        REAL    NOT NULL DEFAULT 0,
		healing    REAL    NOT NULL DEFAULT 0,
		taken      REAL    NOT NULL DEFAULT 0,
		died       INTEGER NOT NULL DEFAULT 0,
		health_pct INTEGER NOT NULL DEFAULT -1,
		targets    TEXT    NOT NULL DEFAULT '[]',
		seen_at    INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (char_key, combat_id)
	)`,
	`CREATE INDEX combats_started ON combats(started_at)`,
	`CREATE TABLE combat_spells (
		char_key  TEXT    NOT NULL,
		combat_id INTEGER NOT NULL,
		kind      TEXT    NOT NULL,
		spell_id  INTEGER NOT NULL DEFAULT 0,
		name      TEXT    NOT NULL DEFAULT '',
		amount    REAL    NOT NULL DEFAULT 0,
		hits      INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (char_key, combat_id, kind, spell_id, name)
	)`,
	`CREATE TABLE combat_taken (
		char_key  TEXT    NOT NULL,
		combat_id INTEGER NOT NULL,
		source    TEXT    NOT NULL,
		amount    REAL    NOT NULL DEFAULT 0,
		PRIMARY KEY (char_key, combat_id, source)
	)`,
	`CREATE TABLE deaths (
		char_key       TEXT    NOT NULL,
		combat_id      INTEGER NOT NULL,
		died_at        INTEGER NOT NULL DEFAULT 0,
		zone           TEXT    NOT NULL DEFAULT '',
		killer         TEXT    NOT NULL DEFAULT '',
		last_hits_json TEXT    NOT NULL DEFAULT '[]',
		PRIMARY KEY (char_key, combat_id)
	)`,
	// Macros (account and character scope) and action bars: each snapshot replaces the previous one.
	`CREATE TABLE macros (
		scope    TEXT    NOT NULL,
		char_key TEXT    NOT NULL DEFAULT '',
		idx      INTEGER NOT NULL,
		name     TEXT    NOT NULL DEFAULT '',
		body     TEXT    NOT NULL DEFAULT '',
		PRIMARY KEY (scope, char_key, idx)
	)`,
	`CREATE TABLE action_bars (
		char_key TEXT    NOT NULL,
		slot     INTEGER NOT NULL,
		kind     TEXT    NOT NULL DEFAULT '',
		name     TEXT    NOT NULL DEFAULT '',
		item_id  INTEGER NOT NULL DEFAULT 0,
		key      TEXT    NOT NULL DEFAULT '',
		PRIMARY KEY (char_key, slot)
	)`,
	// Dungeons with their bosses and the loot of each boss.
	`CREATE TABLE dungeons (
		dungeon_key TEXT    PRIMARY KEY,
		name        TEXT    NOT NULL DEFAULT '',
		zone        TEXT    NOT NULL DEFAULT '',
		min_level   INTEGER NOT NULL DEFAULT 0,
		max_level   INTEGER NOT NULL DEFAULT 0,
		source      TEXT    NOT NULL DEFAULT '',
		map_id      INTEGER NOT NULL DEFAULT 0,
		x           REAL    NOT NULL DEFAULT 0,
		y           REAL    NOT NULL DEFAULT 0,
		quests_json TEXT    NOT NULL DEFAULT '[]',
		updated_at  INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE dungeon_bosses (
		dungeon_key TEXT NOT NULL,
		boss        TEXT NOT NULL,
		PRIMARY KEY (dungeon_key, boss)
	)`,
	`CREATE TABLE dungeon_loot (
		dungeon_key TEXT    NOT NULL,
		boss        TEXT    NOT NULL,
		item_id     INTEGER NOT NULL,
		PRIMARY KEY (dungeon_key, boss, item_id)
	)`,
	// The state of each outbox path: the highest oseq stored for a path of a message type (ingest_items.go).
	`CREATE TABLE outbox_path (
		msg_type INTEGER NOT NULL,
		path     TEXT    NOT NULL,
		oseq     INTEGER NOT NULL,
		PRIMARY KEY (msg_type, path)
	)`,
	// Each SavedVariables file imported, by content (sha256): when, how big, which schema, and the counts.
	`CREATE TABLE sv_imports (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		path        TEXT    NOT NULL,
		sha256      TEXT    NOT NULL,
		size        INTEGER NOT NULL,
		mtime       INTEGER NOT NULL,
		schema      INTEGER NOT NULL DEFAULT 0,
		imported_at INTEGER NOT NULL,
		applied     INTEGER NOT NULL DEFAULT 0,
		skipped     INTEGER NOT NULL DEFAULT 0,
		failed      INTEGER NOT NULL DEFAULT 0,
		UNIQUE (path, sha256)
	)`,
	// Idempotency: an item carrying oseq is applied once per body digest (see ingest_items.go).
	`CREATE TABLE ingest_log (
		msg_type INTEGER NOT NULL,
		oseq     INTEGER NOT NULL,
		digest   TEXT    NOT NULL,
		at       INTEGER NOT NULL,
		PRIMARY KEY (msg_type, oseq, digest)
	)`,
}

// sessionsMigration gives the sessions table a character key, so the addon's sessions (one per
// character) are ingested next to the ones the app tracks itself (char_key "").
var sessionsMigration = []string{
	`ALTER TABLE sessions ADD COLUMN char_key TEXT NOT NULL DEFAULT ''`,
	`CREATE UNIQUE INDEX sessions_char_start ON sessions(char_key, started_at) WHERE char_key <> ''`,
}

// appSchemaMigration is migration 5: the normalized tables, the session key and the read-only views.
func appSchemaMigration() []string {
	out := make([]string, 0, len(normalizedTables)+len(sessionsMigration)+len(viewStatements))
	out = append(out, normalizedTables...)
	out = append(out, sessionsMigration...)
	out = append(out, viewStatements...)
	return out
}
