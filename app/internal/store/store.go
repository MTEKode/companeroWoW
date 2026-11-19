// Package store persists the self-learning WoW Forever knowledge base, the web
// cache and the chat history in a single SQLite file (pure Go driver, no CGO).
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"

	_ "modernc.org/sqlite"
)

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 500
)

// KQuest is a quest learned from WoW Forever.
type KQuest struct {
	QuestID         int64
	Title           string
	ObjectivesText  string
	DescriptionText string
	Level           int
	GiverNpcID      int64
	GiverName       string
	GiverMapID      int
	GiverX, GiverY  float64
	TurnInNpcID     int64
	TurnInName      string
	TurnInMapID     int
	TurnInX         float64
	TurnInY         float64
	RewardsJSON     string
	FirstSeen       int64
	LastSeen        int64
}

// KNpc is an NPC learned from WoW Forever.
type KNpc struct {
	NpcID        int64
	Name         string
	MapID        int
	X, Y         float64
	IsQuestGiver bool
	IsVendor     bool
	IsTrainer    bool
	TrainerType  string
	Reaction     string
	FirstSeen    int64
	LastSeen     int64
}

// ChatMsg is one persisted chat message.
type ChatMsg struct {
	ID        int64
	Role      string // "user" | "assistant" | "system"
	Content   string
	Provider  string
	CreatedAt time.Time
}

// Store is the SQLite-backed persistence layer. It is safe for concurrent use.
type Store struct {
	db   *sql.DB
	now  func() time.Time
	path string

	roMu sync.Mutex
	ro   *sql.DB // read-only connection for consultar_db, opened on first use (sqlquery.go)
}

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

// Open opens (creating if needed) the database at path, enables WAL mode and
// applies pending schema migrations.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("store: empty database path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database dir: %w", err)
	}
	// cache_size keeps the page cache small (512 KiB): the store is a few MB and the app is
	// mostly idle. ShrinkMemory hands back what the cache still holds when the app goes idle.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_pragma=cache_size(-512)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	// One connection: SQLite serializes writers anyway, and it avoids SQLITE_BUSY
	// between transactions and pooled readers.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate database %s: %w", path, err)
	}
	return &Store{db: db, now: time.Now, path: path}, nil
}

// ShrinkMemory asks SQLite to release the memory its page cache holds beyond what the
// connection needs at rest. It is cheap and safe to call at any time; the app calls it when it
// goes idle.
func (s *Store) ShrinkMemory() error {
	if s == nil || s.db == nil {
		return nil
	}
	if _, err := s.db.Exec(`PRAGMA shrink_memory`); err != nil {
		return fmt.Errorf("shrink memory: %w", err)
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.roMu.Lock()
	if s.ro != nil {
		_ = s.ro.Close()
		s.ro = nil
	}
	s.roMu.Unlock()
	return s.db.Close()
}

// ---- Quests and NPCs -------------------------------------------------------

const questCols = `quest_id, title, objectives_text, description_text, level,
	giver_npc_id, giver_name, giver_map_id, giver_x, giver_y,
	turnin_npc_id, turnin_name, turnin_map_id, turnin_x, turnin_y,
	rewards_json, first_seen, last_seen`

const npcCols = `npc_id, name, map_id, x, y, is_quest_giver, is_vendor, is_trainer,
	trainer_type, reaction, first_seen, last_seen`

// UpsertQuest inserts or merges a quest. Non-empty incoming fields overwrite the
// stored ones, empty fields keep them, FirstSeen becomes the minimum and LastSeen the maximum.
func (s *Store) UpsertQuest(q KQuest) error {
	return s.inTx(func(tx *sql.Tx) error { return upsertQuest(tx, q) })
}

// UpsertNpc inserts or merges an NPC with the same merge rules as UpsertQuest.
func (s *Store) UpsertNpc(n KNpc) error {
	return s.inTx(func(tx *sql.Tx) error { return upsertNpc(tx, n) })
}

// GetQuest returns the quest with the given id, or nil if unknown.
func (s *Store) GetQuest(id int64) (*KQuest, error) {
	return getQuest(s.db, id)
}

// SearchQuests returns quests whose title or objectives contain text (case-insensitive for ASCII).
func (s *Store) SearchQuests(text string, limit int) ([]KQuest, error) {
	pattern := "%" + escapeLike(text) + "%"
	rows, err := s.db.Query(
		`SELECT `+questCols+` FROM quests
		 WHERE title LIKE ? ESCAPE '\' OR objectives_text LIKE ? ESCAPE '\'
		 ORDER BY last_seen DESC, quest_id
		 LIMIT ?`,
		pattern, pattern, clampLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("search quests: %w", err)
	}
	defer rows.Close()
	var out []KQuest
	for rows.Next() {
		q, err := scanQuest(rows)
		if err != nil {
			return nil, fmt.Errorf("scan quest: %w", err)
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate quests: %w", err)
	}
	return out, nil
}

// GetNpc returns the NPC with the given id, or nil if unknown.
func (s *Store) GetNpc(id int64) (*KNpc, error) {
	return getNpc(s.db, id)
}

// SearchNpcs returns NPCs whose name contains text.
func (s *Store) SearchNpcs(text string, limit int) ([]KNpc, error) {
	rows, err := s.db.Query(
		`SELECT `+npcCols+` FROM npcs
		 WHERE name LIKE ? ESCAPE '\'
		 ORDER BY last_seen DESC, npc_id
		 LIMIT ?`,
		"%"+escapeLike(text)+"%", clampLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("search npcs: %w", err)
	}
	return collectNpcs(rows)
}

// NpcsOnMap returns up to limit NPCs on the given map, ordered by name.
func (s *Store) NpcsOnMap(mapID int, limit int) ([]KNpc, error) {
	rows, err := s.db.Query(
		`SELECT `+npcCols+` FROM npcs WHERE map_id = ? ORDER BY name, npc_id LIMIT ?`,
		mapID, clampLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list npcs on map %d: %w", mapID, err)
	}
	return collectNpcs(rows)
}

// MapName returns the stored zone name for mapID, or "" if unknown.
func (s *Store) MapName(mapID int) (string, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM maps WHERE map_id = ?`, mapID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read map %d: %w", mapID, err)
	}
	return name, nil
}

// SetMapName stores the zone name for mapID.
func (s *Store) SetMapName(mapID int, name string) error {
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO maps (map_id, name) VALUES (?, ?)`, mapID, name); err != nil {
		return fmt.Errorf("store map %d: %w", mapID, err)
	}
	return nil
}

// ImportKnowledge merges sv.Raw["knowledge"] (quests and npcs) and sv.Raw["maps"]
// into the store, in one transaction. It returns the number of quests and NPCs imported.
func (s *Store) ImportKnowledge(sv *bridge.SavedVars) (quests, npcs int, err error) {
	if sv == nil {
		return 0, 0, errors.New("import knowledge: nil saved variables")
	}
	err = s.inTx(func(tx *sql.Tx) error {
		for _, e := range rawEntries(sv.Raw["maps"]) {
			id, convErr := strconv.Atoi(e.key)
			if convErr != nil {
				continue
			}
			name, ok := e.value.(string)
			if !ok || name == "" {
				continue
			}
			if _, execErr := tx.Exec(`INSERT OR REPLACE INTO maps (map_id, name) VALUES (?, ?)`, id, name); execErr != nil {
				return fmt.Errorf("import map %d: %w", id, execErr)
			}
		}

		knowledge, _ := sv.Raw["knowledge"].(map[string]any)
		quests, npcs = 0, 0
		for _, e := range rawEntries(knowledge["quests"]) {
			m, ok := e.value.(map[string]any)
			if !ok {
				continue
			}
			q := questFromRaw(m, e.key)
			if q.QuestID == 0 {
				continue
			}
			if err := upsertQuest(tx, q); err != nil {
				return fmt.Errorf("import quest %d: %w", q.QuestID, err)
			}
			quests++
		}
		for _, e := range rawEntries(knowledge["npcs"]) {
			m, ok := e.value.(map[string]any)
			if !ok {
				continue
			}
			n := npcFromRaw(m, e.key)
			if n.NpcID == 0 {
				continue
			}
			if err := upsertNpc(tx, n); err != nil {
				return fmt.Errorf("import npc %d: %w", n.NpcID, err)
			}
			npcs++
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return quests, npcs, nil
}

// ---- Web cache -------------------------------------------------------------

// CacheGet returns the cached value for key if it was stored less than maxAge ago.
// maxAge <= 0 disables the expiry check.
func (s *Store) CacheGet(key string, maxAge time.Duration) (string, bool, error) {
	var value string
	var fetchedMs int64
	err := s.db.QueryRow(`SELECT value, fetched_at FROM cache WHERE key = ?`, key).Scan(&value, &fetchedMs)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read cache %q: %w", key, err)
	}
	if maxAge > 0 && s.now().Sub(time.UnixMilli(fetchedMs)) > maxAge {
		return "", false, nil
	}
	return value, true, nil
}

// CachePut stores value for key, replacing any previous entry.
func (s *Store) CachePut(key, value string) error {
	if _, err := s.db.Exec(
		`INSERT OR REPLACE INTO cache (key, value, fetched_at) VALUES (?, ?, ?)`,
		key, value, s.now().UnixMilli()); err != nil {
		return fmt.Errorf("write cache %q: %w", key, err)
	}
	return nil
}

// ---- Chat history ----------------------------------------------------------

// AddMessage appends a message and returns its id. CreatedAt defaults to now.
func (s *Store) AddMessage(m ChatMsg) (int64, error) {
	switch m.Role {
	case "user", "assistant", "system":
	default:
		return 0, fmt.Errorf("add message: invalid role %q", m.Role)
	}
	created := m.CreatedAt
	if created.IsZero() {
		created = s.now()
	}
	res, err := s.db.Exec(
		`INSERT INTO messages (role, content, provider, created_at) VALUES (?, ?, ?, ?)`,
		m.Role, m.Content, m.Provider, created.UnixNano())
	if err != nil {
		return 0, fmt.Errorf("add message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("add message id: %w", err)
	}
	return id, nil
}

// RecentMessages returns the last n messages ordered oldest to newest.
func (s *Store) RecentMessages(n int) ([]ChatMsg, error) {
	if n <= 0 {
		return []ChatMsg{}, nil
	}
	rows, err := s.db.Query(
		`SELECT id, role, content, provider, created_at FROM messages ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return nil, fmt.Errorf("read messages: %w", err)
	}
	defer rows.Close()
	var out []ChatMsg
	for rows.Next() {
		var m ChatMsg
		var createdNs int64
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.Provider, &createdNs); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		m.CreatedAt = time.Unix(0, createdNs)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if out == nil {
		out = []ChatMsg{}
	}
	return out, nil
}

// ClearHistory deletes all chat messages.
func (s *Store) ClearHistory() error {
	if _, err := s.db.Exec(`DELETE FROM messages`); err != nil {
		return fmt.Errorf("clear history: %w", err)
	}
	return nil
}

// ---- helpers ---------------------------------------------------------------

func (s *Store) inTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func upsertQuest(ex querier, q KQuest) error {
	old, err := getQuest(ex, q.QuestID)
	if err != nil {
		return err
	}
	if old != nil {
		q = mergeQuest(*old, q)
	}
	_, err = ex.Exec(
		`INSERT OR REPLACE INTO quests (`+questCols+`) VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?, ?,?,?)`,
		q.QuestID, q.Title, q.ObjectivesText, q.DescriptionText, q.Level,
		q.GiverNpcID, q.GiverName, q.GiverMapID, q.GiverX, q.GiverY,
		q.TurnInNpcID, q.TurnInName, q.TurnInMapID, q.TurnInX, q.TurnInY,
		q.RewardsJSON, q.FirstSeen, q.LastSeen)
	if err != nil {
		return fmt.Errorf("write quest %d: %w", q.QuestID, err)
	}
	return nil
}

func upsertNpc(ex querier, n KNpc) error {
	old, err := getNpc(ex, n.NpcID)
	if err != nil {
		return err
	}
	if old != nil {
		n = mergeNpc(*old, n)
	}
	_, err = ex.Exec(
		`INSERT OR REPLACE INTO npcs (`+npcCols+`) VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?)`,
		n.NpcID, n.Name, n.MapID, n.X, n.Y,
		boolInt(n.IsQuestGiver), boolInt(n.IsVendor), boolInt(n.IsTrainer),
		n.TrainerType, n.Reaction, n.FirstSeen, n.LastSeen)
	if err != nil {
		return fmt.Errorf("write npc %d: %w", n.NpcID, err)
	}
	return nil
}

func getQuest(ex querier, id int64) (*KQuest, error) {
	row := ex.QueryRow(`SELECT `+questCols+` FROM quests WHERE quest_id = ?`, id)
	q, err := scanQuest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read quest %d: %w", id, err)
	}
	return &q, nil
}

func getNpc(ex querier, id int64) (*KNpc, error) {
	row := ex.QueryRow(`SELECT `+npcCols+` FROM npcs WHERE npc_id = ?`, id)
	n, err := scanNpc(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read npc %d: %w", id, err)
	}
	return &n, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanQuest(sc scanner) (KQuest, error) {
	var q KQuest
	var level, giverMap, turnMap int64
	err := sc.Scan(&q.QuestID, &q.Title, &q.ObjectivesText, &q.DescriptionText, &level,
		&q.GiverNpcID, &q.GiverName, &giverMap, &q.GiverX, &q.GiverY,
		&q.TurnInNpcID, &q.TurnInName, &turnMap, &q.TurnInX, &q.TurnInY,
		&q.RewardsJSON, &q.FirstSeen, &q.LastSeen)
	if err != nil {
		return KQuest{}, err
	}
	q.Level = int(level)
	q.GiverMapID = int(giverMap)
	q.TurnInMapID = int(turnMap)
	return q, nil
}

func scanNpc(sc scanner) (KNpc, error) {
	var n KNpc
	var mapID int64
	var quest, vendor, trainer int
	err := sc.Scan(&n.NpcID, &n.Name, &mapID, &n.X, &n.Y, &quest, &vendor, &trainer,
		&n.TrainerType, &n.Reaction, &n.FirstSeen, &n.LastSeen)
	if err != nil {
		return KNpc{}, err
	}
	n.MapID = int(mapID)
	n.IsQuestGiver = quest != 0
	n.IsVendor = vendor != 0
	n.IsTrainer = trainer != 0
	return n, nil
}

func collectNpcs(rows *sql.Rows) ([]KNpc, error) {
	defer rows.Close()
	var out []KNpc
	for rows.Next() {
		n, err := scanNpc(rows)
		if err != nil {
			return nil, fmt.Errorf("scan npc: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate npcs: %w", err)
	}
	return out, nil
}

// mergeQuest applies the merge rules: non-empty incoming wins, FirstSeen min, LastSeen max.
func mergeQuest(o, n KQuest) KQuest {
	m := o
	m.Title = pick(o.Title, n.Title, "")
	m.ObjectivesText = pick(o.ObjectivesText, n.ObjectivesText, "")
	m.DescriptionText = pick(o.DescriptionText, n.DescriptionText, "")
	m.Level = pick(o.Level, n.Level, 0)
	m.GiverNpcID = pick(o.GiverNpcID, n.GiverNpcID, 0)
	m.GiverName = pick(o.GiverName, n.GiverName, "")
	m.GiverMapID = pick(o.GiverMapID, n.GiverMapID, 0)
	m.GiverX = pick(o.GiverX, n.GiverX, 0)
	m.GiverY = pick(o.GiverY, n.GiverY, 0)
	m.TurnInNpcID = pick(o.TurnInNpcID, n.TurnInNpcID, 0)
	m.TurnInName = pick(o.TurnInName, n.TurnInName, "")
	m.TurnInMapID = pick(o.TurnInMapID, n.TurnInMapID, 0)
	m.TurnInX = pick(o.TurnInX, n.TurnInX, 0)
	m.TurnInY = pick(o.TurnInY, n.TurnInY, 0)
	m.RewardsJSON = pick(o.RewardsJSON, n.RewardsJSON, "")
	m.FirstSeen = minNonZero(o.FirstSeen, n.FirstSeen)
	m.LastSeen = max(o.LastSeen, n.LastSeen)
	return m
}

// mergeNpc applies the same rules as mergeQuest. A true flag is never cleared by a false one.
func mergeNpc(o, n KNpc) KNpc {
	m := o
	m.Name = pick(o.Name, n.Name, "")
	m.MapID = pick(o.MapID, n.MapID, 0)
	m.X = pick(o.X, n.X, 0)
	m.Y = pick(o.Y, n.Y, 0)
	m.IsQuestGiver = o.IsQuestGiver || n.IsQuestGiver
	m.IsVendor = o.IsVendor || n.IsVendor
	m.IsTrainer = o.IsTrainer || n.IsTrainer
	m.TrainerType = pick(o.TrainerType, n.TrainerType, "")
	m.Reaction = pick(o.Reaction, n.Reaction, "")
	m.FirstSeen = minNonZero(o.FirstSeen, n.FirstSeen)
	m.LastSeen = max(o.LastSeen, n.LastSeen)
	return m
}

func pick[T comparable](old, incoming, empty T) T {
	if incoming != empty {
		return incoming
	}
	return old
}

func minNonZero(a, b int64) int64 {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func clampLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultSearchLimit
	case limit > maxSearchLimit:
		return maxSearchLimit
	default:
		return limit
	}
}

// escapeLike escapes LIKE wildcards so user text is matched literally (ESCAPE '\').
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// rawEntry is one key/value of a converted SavedVariables table.
type rawEntry struct {
	key   string
	value any
}

// rawEntries lists the entries of a converted table. A []any yields keys "1".."n".
func rawEntries(v any) []rawEntry {
	switch t := v.(type) {
	case map[string]any:
		out := make([]rawEntry, 0, len(t))
		for k, val := range t {
			out = append(out, rawEntry{key: k, value: val})
		}
		return out
	case []any:
		out := make([]rawEntry, 0, len(t))
		for i, val := range t {
			out = append(out, rawEntry{key: strconv.Itoa(i + 1), value: val})
		}
		return out
	}
	return nil
}

func rawNum(m map[string]any, k string) float64 {
	switch v := m[k].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	}
	return 0
}

func rawStr(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func rawBool(m map[string]any, k string) bool {
	b, _ := m[k].(bool)
	return b
}

func questFromRaw(m map[string]any, key string) KQuest {
	id := int64(rawNum(m, "questID"))
	if id == 0 {
		id, _ = strconv.ParseInt(key, 10, 64)
	}
	q := KQuest{
		QuestID:         id,
		Title:           rawStr(m, "title"),
		ObjectivesText:  rawStr(m, "objectivesText"),
		DescriptionText: rawStr(m, "descriptionText"),
		Level:           int(rawNum(m, "level")),
		GiverNpcID:      int64(rawNum(m, "giverNpcID")),
		GiverName:       rawStr(m, "giverName"),
		GiverMapID:      int(rawNum(m, "giverMapID")),
		GiverX:          rawNum(m, "giverX"),
		GiverY:          rawNum(m, "giverY"),
		TurnInNpcID:     int64(rawNum(m, "turnInNpcID")),
		TurnInName:      rawStr(m, "turnInName"),
		TurnInMapID:     int(rawNum(m, "turnInMapID")),
		TurnInX:         rawNum(m, "turnInX"),
		TurnInY:         rawNum(m, "turnInY"),
		FirstSeen:       int64(rawNum(m, "firstSeen")),
		LastSeen:        int64(rawNum(m, "lastSeen")),
	}
	if r, ok := m["rewards"]; ok && r != nil {
		if b, err := json.Marshal(r); err == nil {
			q.RewardsJSON = string(b)
		}
	}
	return q
}

func npcFromRaw(m map[string]any, key string) KNpc {
	id := int64(rawNum(m, "npcID"))
	if id == 0 {
		id, _ = strconv.ParseInt(key, 10, 64)
	}
	return KNpc{
		NpcID:        id,
		Name:         rawStr(m, "name"),
		MapID:        int(rawNum(m, "mapID")),
		X:            rawNum(m, "x"),
		Y:            rawNum(m, "y"),
		IsQuestGiver: rawBool(m, "isQuestGiver"),
		IsVendor:     rawBool(m, "isVendor"),
		IsTrainer:    rawBool(m, "isTrainer"),
		TrainerType:  rawStr(m, "trainerType"),
		Reaction:     rawStr(m, "reaction"),
		FirstSeen:    int64(rawNum(m, "firstSeen")),
		LastSeen:     int64(rawNum(m, "lastSeen")),
	}
}
