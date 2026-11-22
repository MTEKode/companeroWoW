package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// Ingestion writes what the addon sends into the normalized tables. Three sources feed it:
//
//   - channel messages (IngestMessage): one complete message of a msgType, as the bridge decodes it;
//   - SavedVariables schema 1 (the blocks of CompaneroWoWDB: knowledge, maps, world and each
//     character's blocks), which IngestSavedVars reads;
//   - the SavedVariables outbox of schema 2 (IngestSavedVars too): items { seq, msgType, at,
//     payload } in seq order, where payload is the message body of that msgType with "oseq".
//
// Every write is an upsert by natural key, so applying the same data twice changes nothing. An
// item that carries an oseq is also logged in ingest_log by (msgType, oseq, body digest): the
// outbox resends an item in several share windows, and the log lets the app skip it after the first
// time without reading the body again. Without an oseq the natural keys alone make it idempotent.

// IngestStats counts what one ingestion call did.
type IngestStats struct {
	Applied int // messages written
	Skipped int // outbox items already in ingest_log
	Failed  int // items the ingestion refused (the rest still applies)
}

// IngestMessage stores one complete channel message. body is its JSON; charKey is the addon's
// character key the message belongs to ("" when unknown). at is when it arrived.
func (s *Store) IngestMessage(msgType uint8, body []byte, charKey string, at time.Time) (IngestStats, error) {
	var stats IngestStats
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return stats, fmt.Errorf("ingest message %d: decode: %w", msgType, err)
	}
	m := asObj(v)
	if list, ok := v.([]any); ok {
		m = map[string]any{"list": list} // a list body: completed quest ids
	}
	if m == nil {
		return stats, fmt.Errorf("ingest message %d: body is not an object", msgType)
	}
	err := s.inTx(func(tx *sql.Tx) error {
		in := newIngester(tx, at)
		skipped, err := in.applyItem(msgType, m, v, charKey)
		if err != nil {
			return fmt.Errorf("ingest message %d: %w", msgType, err)
		}
		if skipped {
			stats.Skipped++
		} else {
			stats.Applied++
		}
		return nil
	})
	return stats, err
}

// IngestSavedVars stores what the SavedVariables file holds that the app does not have yet: the
// outbox (schema 2), the knowledge and maps blocks, the world block and each character's blocks
// (schema 1, the current-state blocks of schema 2 and the state of schema 2.1: characters[k].state). Everything is an upsert, so reading the
// same file again changes nothing.
func (s *Store) IngestSavedVars(sv *bridge.SavedVars, at time.Time) (IngestStats, error) {
	var stats IngestStats
	if sv == nil {
		return stats, errors.New("ingest saved variables: nil data")
	}
	err := s.inTx(func(tx *sql.Tx) error {
		in := newIngester(tx, at)
		// The knowledge and the world come first: a character's records may refer to them.
		if err := in.apply(bridge.MsgKnowledge, asObjOrEmpty(sv.Raw["knowledge"]), ""); err != nil {
			return fmt.Errorf("ingest knowledge: %w", err)
		}
		if sv.Raw["knowledge"] != nil {
			stats.Applied++
		}
		for _, e := range rawEntries(sv.Raw["maps"]) {
			id, convErr := strconv.Atoi(e.key)
			name, ok := e.value.(string)
			if convErr != nil || !ok || name == "" {
				continue
			}
			if _, err := tx.Exec(`INSERT OR REPLACE INTO maps (map_id, name) VALUES (?, ?)`, id, name); err != nil {
				return fmt.Errorf("ingest map %d: %w", id, err)
			}
		}
		if w := asObj(sv.Raw["world"]); w != nil {
			if err := in.apply(bridge.MsgWorld, w, ""); err != nil {
				return fmt.Errorf("ingest world: %w", err)
			}
			stats.Applied++
		}
		for _, e := range rawEntries(sv.Raw["characters"]) {
			ch := asObj(e.value)
			if ch == nil {
				continue
			}
			if err := in.characterBlocks(e.key, ch); err != nil {
				return fmt.Errorf("ingest character %s: %w", e.key, err)
			}
			stats.Applied++
		}
		// Schema 2: the outbox, in seq order, each item once. Items without a charKey belong to the active character.
		active := activeCharKey(sv.Raw["characters"])
		outbox := asObj(sv.Raw["outbox"])
		items := rawEntries(outbox["items"])
		sort.SliceStable(items, func(i, j int) bool {
			return itemSeq(items[i].value) < itemSeq(items[j].value)
		})
		for _, e := range items {
			it := asObj(e.value)
			if it == nil {
				continue
			}
			msgType := uint8(rawNum(it, "msgType"))
			payload := asObj(it["payload"])
			seq := int64(rawNum(it, "seq"))
			if msgType == 0 || payload == nil {
				stats.Failed++
				continue
			}
			if seq == 0 {
				seq = int64(rawNum(payload, "oseq"))
			}
			charKey, _ := payload["charKey"].(string)
			if charKey == "" {
				charKey = active
			}
			// The item is stamped with the time the addon learned it, so the character's state keeps the newest.
			item := &ingester{tx: in.tx, at: in.at, now: in.now, n: in.n}
			if learned := int64(rawNum(it, "at")); learned > 0 {
				item.at = time.Unix(learned, 0)
				item.now = learned
			}
			skipped, err := item.applyItem(msgType, payload, payload, charKey)
			if err != nil {
				stats.Failed++ // not recorded as applied: the next read tries it again
				continue
			}
			if skipped {
				stats.Skipped++
			} else {
				stats.Applied++
			}
		}
		return nil
	})
	if err != nil {
		return IngestStats{}, err
	}
	return stats, nil
}

// characterBlocks stores one character's blocks of SavedVariables (schema 1, and the current
// state of schema 2) under the same rules as the channel messages.
func (in *ingester) characterBlocks(key string, ch map[string]any) error {
	parts := []struct {
		msgType uint8
		body    map[string]any
	}{
		{bridge.MsgCharacter, pick2(ch, "character", "updatedAt")},
		{bridge.MsgQuests, pick2(ch, "quests", "location")},
		{bridge.MsgEquipment, pick2(ch, "equipment", "gear")},
		{bridge.MsgCompletedQuests, completedBlock(ch)},
		{bridge.MsgCombat, map[string]any{"combats": ch["combats"]}},
		{bridge.MsgActionBars, pick2(ch, "actionBars", "")},
		{bridge.MsgHeavy, pick2(ch, "questWindow", "gearCandidates")},
		{bridge.MsgSheet, asObjOrEmpty(ch["sheet"])},
		{bridge.MsgMacros, asObjOrEmpty(ch["macros"])},
		{bridge.MsgSession, asObjOrEmpty(ch["session"])},
	}
	for _, p := range parts {
		if len(p.body) == 0 {
			continue
		}
		if err := in.apply(p.msgType, p.body, key); err != nil {
			return fmt.Errorf("block %d: %w", p.msgType, err)
		}
	}
	// The dungeons live in the heavy block of the addon and in the character table of schema 1.
	if d := asObj(ch["dungeons"]); d != nil {
		if err := in.dungeons(d); err != nil {
			return err
		}
	}
	// Schema 2.1: the current state of the character (characters[k].state, Storage.lua SaveState).
	if st := asObj(ch["state"]); st != nil {
		if err := in.characterState(key, st); err != nil {
			return fmt.Errorf("state: %w", err)
		}
	}
	return nil
}

// characterState stores the current state of a character (schema 2.1, characters[k].state): each section
// is the newest copy of its block, stamped with the time the addon saved it (state.at, or the time of the
// import when there is none). Each section replaces the stored one only when it is newer (putSection), so
// a message the channel delivered after the save still wins. The names are the readers' names (sheet,
// equipment, gear, quests, location, training, profPlan, ...), as the channel's state writes them.
func (in *ingester) characterState(charKey string, st map[string]any) error {
	if charKey == "" {
		return nil
	}
	at := in.at
	if saved := int64(rawNum(st, "at")); saved > 0 {
		at = time.Unix(saved, 0)
	}
	for name, v := range st {
		if name == "at" || v == nil {
			continue
		}
		if compactStateSections[name] {
			v = expandKeys(v) // the addon writes these with the short keys of Storage.KEYS (Storage.Compact)
		}
		if err := in.putSection(charKey, name, v, at, 0); err != nil {
			return err
		}
	}
	// The action bars and the macros of the state also fill their tables (v_barras, v_macros), so a
	// reload delivers them without the channel. The file is read when it changes, and its state is
	// the newest copy the addon had when it was written.
	if bars := asObj(st["actionBars"]); bars != nil {
		if err := in.actionBars(charKey, map[string]any{"actionBars": bars}); err != nil {
			return err
		}
	}
	if macros := asObj(st["macros"]); macros != nil {
		if err := in.macros(charKey, macros); err != nil {
			return err
		}
	}
	return nil
}

// compactStateSections are the state sections the addon writes in the compact encoding (short keys, see
// Storage.KEYS): the store keeps them under the long names the readers use.
var compactStateSections = map[string]bool{"questWindow": true, "gearCandidates": true}

// pick2 returns a map with the given keys of m (the keys that are absent are left out).
func pick2(m map[string]any, a, b string) map[string]any {
	out := map[string]any{}
	if v, ok := m[a]; ok && v != nil {
		out[a] = v
	}
	if b != "" {
		if v, ok := m[b]; ok && v != nil {
			out[b] = v
		}
	}
	return out
}

// ingester applies messages inside one transaction.
type ingester struct {
	tx  *sql.Tx
	at  time.Time
	now int64
	n   int
}

func newIngester(tx *sql.Tx, at time.Time) *ingester {
	if at.IsZero() {
		at = time.Now()
	}
	return &ingester{tx: tx, at: at, now: at.Unix()}
}

// apply stores one message body of msgType for charKey.
func (in *ingester) apply(msgType uint8, m map[string]any, charKey string) error {
	day := ""
	if body, d, ok := outboxBody(msgType, m); ok {
		m, day = body, d
	}
	switch msgType {
	case bridge.MsgCharacter:
		return in.character(charKey, m)
	case bridge.MsgQuests:
		return in.questLog(charKey, m)
	case bridge.MsgEquipment:
		return in.equipment(charKey, m)
	case bridge.MsgCompletedQuests:
		return in.completed(charKey, m)
	case bridge.MsgCombat:
		return in.combats(charKey, m)
	case bridge.MsgActionBars:
		return in.actionBars(charKey, m)
	case bridge.MsgKnowledge:
		return in.knowledge(m)
	case bridge.MsgHeavy:
		return in.heavy(charKey, m)
	case bridge.MsgSheet:
		return in.sheet(charKey, day, m)
	case bridge.MsgMacros:
		return in.macros(charKey, m)
	case bridge.MsgSession:
		return in.session(charKey, m)
	case bridge.MsgWorld:
		return in.world(m)
	}
	return nil // messages without a table (bags, professions, meta, questie) are not stored
}

// ---- helpers ----------------------------------------------------------------

// asObj returns v as a map, or nil.
func asObj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asObjOrEmpty(v any) map[string]any {
	if m := asObj(v); m != nil {
		return m
	}
	return map[string]any{}
}

// itemSeq is the seq of an outbox item (0 when absent).
func itemSeq(v any) int64 {
	if m := asObj(v); m != nil {
		return int64(rawNum(m, "seq"))
	}
	return 0
}

// listValues returns the values of a list or of a table keyed by id, in key order.
func listValues(v any) []rawEntry {
	es := rawEntries(v)
	sort.SliceStable(es, func(i, j int) bool {
		a, errA := strconv.ParseInt(es[i].key, 10, 64)
		b, errB := strconv.ParseInt(es[j].key, 10, 64)
		if errA == nil && errB == nil {
			return a < b
		}
		return es[i].key < es[j].key
	})
	return es
}

// idOf reads an id field of m, falling back to the table key.
func idOf(m map[string]any, field, key string) int64 {
	if id := int64(rawNum(m, field)); id > 0 {
		return id
	}
	id, _ := strconv.ParseInt(key, 10, 64)
	return id
}

func truthyObj(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	}
	return false
}

var linkNameRe = regexp.MustCompile(`\|h\[([^\]]+)\]\|h`)

// nameFromLink returns the item name inside a chat link, or "".
func nameFromLink(link string) string {
	if m := linkNameRe.FindStringSubmatch(link); m != nil {
		return m[1]
	}
	return ""
}

// dayOf is the local calendar day of the ingestion, the key of a daily snapshot.
func (in *ingester) dayOf() string { return in.at.Local().Format("2006-01-02") }

// ---- characters ----------------------------------------------------------------

func charKeyOf(charKey string, c map[string]any) string {
	if charKey != "" {
		return charKey
	}
	name, realm := rawStr(c, "name"), rawStr(c, "realm")
	if name == "" {
		return ""
	}
	return strings.ReplaceAll(name+"-"+realm, " ", "")
}

// character stores the core fields of a character and its daily snapshot (msgType 1).
func (in *ingester) character(charKey string, m map[string]any) error {
	c := asObj(m["character"])
	if c == nil {
		return nil
	}
	key := charKeyOf(charKey, c)
	if key == "" {
		return nil
	}
	updated := int64(rawNum(m, "updatedAt"))
	if updated == 0 {
		updated = in.now
	}
	level, xp, xpMax, money := int64(rawNum(c, "level")), int64(rawNum(c, "xp")), int64(rawNum(c, "xpMax")), int64(rawNum(c, "money"))
	if _, err := in.tx.Exec(`
		INSERT INTO characters (char_key, name, realm, race, class, level, xp, xp_max, money, updated_at, seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(char_key) DO UPDATE SET
			name = COALESCE(NULLIF(excluded.name, ''), characters.name),
			realm = COALESCE(NULLIF(excluded.realm, ''), characters.realm),
			race = COALESCE(NULLIF(excluded.race, ''), characters.race),
			class = COALESCE(NULLIF(excluded.class, ''), characters.class),
			level = excluded.level, xp = excluded.xp, xp_max = excluded.xp_max,
			money = excluded.money, updated_at = excluded.updated_at, seen_at = excluded.seen_at`,
		key, rawStr(c, "name"), rawStr(c, "realm"), rawStr(c, "race"), rawStr(c, "class"),
		level, xp, xpMax, money, updated, in.now); err != nil {
		return fmt.Errorf("write character %s: %w", key, err)
	}
	if _, err := in.tx.Exec(`
		INSERT INTO character_snapshots (char_key, day, level, xp, money, taken_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(char_key, day) DO UPDATE SET
			level = excluded.level, xp = excluded.xp, money = excluded.money, taken_at = excluded.taken_at`,
		key, in.dayOf(), level, xp, money, in.now); err != nil {
		return fmt.Errorf("write snapshot %s: %w", key, err)
	}
	return nil
}

// sheet stores the character sheet of the day as JSON (msgType 12).
func (in *ingester) sheet(charKey, day string, m map[string]any) error {
	if len(m) == 0 {
		return nil
	}
	if day == "" {
		day = in.dayOf()
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode sheet: %w", err)
	}
	if _, err := in.tx.Exec(`
		INSERT INTO character_snapshots (char_key, day, sheet_json, taken_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(char_key, day) DO UPDATE SET sheet_json = excluded.sheet_json, taken_at = excluded.taken_at`,
		charKey, day, string(raw), in.now); err != nil {
		return fmt.Errorf("write sheet %s: %w", charKey, err)
	}
	return nil
}

// ---- quests ----------------------------------------------------------------------

// questLog replaces the quest log of the character with the message's quests (msgType 2).
func (in *ingester) questLog(charKey string, m map[string]any) error {
	if m["quests"] == nil {
		return nil
	}
	if _, err := in.tx.Exec(`DELETE FROM quest_log WHERE char_key = ?`, charKey); err != nil {
		return fmt.Errorf("clear quest log: %w", err)
	}
	if _, err := in.tx.Exec(`DELETE FROM quest_objectives WHERE char_key = ?`, charKey); err != nil {
		return fmt.Errorf("clear objectives: %w", err)
	}
	for _, e := range listValues(m["quests"]) {
		q := asObj(e.value)
		if q == nil {
			continue
		}
		id := idOf(q, "questID", e.key)
		if id <= 0 {
			continue
		}
		complete := truthyObj(q["complete"]) || truthyObj(q["isComplete"]) || truthyObj(q["ready"])
		if _, err := in.tx.Exec(`
			INSERT OR REPLACE INTO quest_log (char_key, quest_id, title, level, complete, seen_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			charKey, id, rawStr(q, "title"), int64(rawNum(q, "level")), boolInt(complete), in.now); err != nil {
			return fmt.Errorf("write quest log %d: %w", id, err)
		}
		for i, oe := range rawEntries(q["objectives"]) {
			o := asObj(oe.value)
			if o == nil {
				continue
			}
			if _, err := in.tx.Exec(`
				INSERT OR REPLACE INTO quest_objectives (char_key, quest_id, idx, text, fulfilled, required)
				VALUES (?, ?, ?, ?, ?, ?)`,
				charKey, id, i+1, rawStr(o, "text"), int64(rawNum(o, "numFulfilled")), int64(rawNum(o, "numRequired"))); err != nil {
				return fmt.Errorf("write objective %d: %w", id, err)
			}
		}
	}
	return nil
}

// completed records the completed quests: a full list replaces the stored ones, {"add": [...]} adds (msgType 6).
func (in *ingester) completed(charKey string, m map[string]any) error {
	// no guard here: a record of an unknown character is kept under the empty key
	var ids []any
	full := true
	switch {
	case m["add"] != nil:
		ids, full = asList(m["add"]), false
	case m["list"] != nil:
		ids = asList(m["list"])
	default:
		ids = listOfValues(m)
	}
	if full {
		if _, err := in.tx.Exec(`DELETE FROM completed_quests WHERE char_key = ?`, charKey); err != nil {
			return fmt.Errorf("clear completed quests: %w", err)
		}
	}
	for _, v := range ids {
		id := int64(valueNum(v))
		if id <= 0 {
			continue
		}
		if _, err := in.tx.Exec(
			`INSERT OR REPLACE INTO completed_quests (char_key, quest_id, seen_at) VALUES (?, ?, ?)`,
			charKey, id, in.now); err != nil {
			return fmt.Errorf("write completed quest %d: %w", id, err)
		}
	}
	return nil
}

// asList returns v as a list of values (a table keyed by index yields its values in order).
func asList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

// listOfValues returns the values of m when m is itself a list encoded as the whole body.
func listOfValues(m map[string]any) []any {
	var out []any
	for _, e := range rawEntries(m) {
		out = append(out, e.value)
	}
	return out
}

func valueNum(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int64:
		return float64(t)
	case int:
		return float64(t)
	}
	return 0
}

// ---- equipment and items ---------------------------------------------------------

// equipment stores the items worn (msgType 4): the item records, their stats and the history of each slot.
func (in *ingester) equipment(charKey string, m map[string]any) error {
	// no guard here: a record of an unknown character is kept under the empty key
	for _, e := range listValues(m["gear"]) {
		slot, err := strconv.Atoi(e.key)
		if err != nil || slot < 1 {
			continue
		}
		it := asObj(e.value)
		if it == nil {
			continue
		}
		if err := in.item(it); err != nil {
			return err
		}
		if err := in.wear(charKey, slot, int64(rawNum(it, "itemID"))); err != nil {
			return err
		}
	}
	for _, e := range rawEntries(m["equipment"]) {
		it := asObj(e.value)
		if it == nil {
			continue
		}
		slot := int(rawNum(it, "slot"))
		id := int64(rawNum(it, "itemID"))
		if slot < 1 || id <= 0 {
			continue
		}
		if err := in.item(it); err != nil {
			return err
		}
		if err := in.wear(charKey, slot, id); err != nil {
			return err
		}
	}
	return nil
}

// wear appends a history row when the item in the slot differs from the last one recorded.
func (in *ingester) wear(charKey string, slot int, itemID int64) error {
	if itemID <= 0 {
		return nil
	}
	var last sql.NullInt64
	err := in.tx.QueryRow(`SELECT item_id FROM equipment_history
		WHERE char_key = ? AND slot = ? ORDER BY id DESC LIMIT 1`, charKey, slot).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read equipment slot %d: %w", slot, err)
	}
	if last.Valid && last.Int64 == itemID {
		return nil
	}
	if _, err := in.tx.Exec(`INSERT INTO equipment_history (char_key, slot, item_id, changed_at) VALUES (?, ?, ?, ?)`,
		charKey, slot, itemID, in.now); err != nil {
		return fmt.Errorf("write equipment slot %d: %w", slot, err)
	}
	return nil
}

// item upserts an item record from a message table (itemID, name, link, stats, weapon...). Fields the
// message does not carry keep their stored value.
func (in *ingester) item(m map[string]any) error {
	id := int64(rawNum(m, "itemID"))
	if id <= 0 {
		return nil
	}
	name := rawStr(m, "name")
	link := rawStr(m, "link")
	if name == "" {
		name = nameFromLink(link)
	}
	var wmin, wmax, wspeed float64
	if w := asObj(m["weapon"]); w != nil {
		wmin, wmax, wspeed = rawNum(w, "min"), rawNum(w, "max"), rawNum(w, "speed")
	}
	if _, err := in.tx.Exec(`
		INSERT INTO items (item_id, name, link, item_level, required_level, quality, equip_loc, class, subclass,
			weapon_min, weapon_max, weapon_speed, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET
			name = COALESCE(NULLIF(excluded.name, ''), items.name),
			link = COALESCE(NULLIF(excluded.link, ''), items.link),
			item_level = CASE WHEN excluded.item_level > 0 THEN excluded.item_level ELSE items.item_level END,
			required_level = CASE WHEN excluded.required_level > 0 THEN excluded.required_level ELSE items.required_level END,
			quality = CASE WHEN excluded.quality > 0 THEN excluded.quality ELSE items.quality END,
			equip_loc = COALESCE(NULLIF(excluded.equip_loc, ''), items.equip_loc),
			class = COALESCE(NULLIF(excluded.class, ''), items.class),
			subclass = COALESCE(NULLIF(excluded.subclass, ''), items.subclass),
			weapon_min = CASE WHEN excluded.weapon_max > 0 THEN excluded.weapon_min ELSE items.weapon_min END,
			weapon_max = CASE WHEN excluded.weapon_max > 0 THEN excluded.weapon_max ELSE items.weapon_max END,
			weapon_speed = CASE WHEN excluded.weapon_max > 0 THEN excluded.weapon_speed ELSE items.weapon_speed END,
			updated_at = excluded.updated_at`,
		id, name, link, int64(rawNum(m, "itemLevel")), int64(rawNum(m, "requiredLevel")), int64(rawNum(m, "quality")),
		rawStr(m, "equipLoc"), rawStr(m, "class"), rawStr(m, "subclass"), wmin, wmax, wspeed, in.now); err != nil {
		return fmt.Errorf("write item %d: %w", id, err)
	}
	if st := asObj(m["stats"]); st != nil {
		if _, err := in.tx.Exec(`DELETE FROM item_stats WHERE item_id = ?`, id); err != nil {
			return fmt.Errorf("clear stats %d: %w", id, err)
		}
		for k, v := range st {
			n := valueNum(v)
			if n == 0 {
				continue
			}
			if _, err := in.tx.Exec(`INSERT OR REPLACE INTO item_stats (item_id, stat, value) VALUES (?, ?, ?)`, id, k, n); err != nil {
				return fmt.Errorf("write stat %d %s: %w", id, k, err)
			}
		}
	}
	return nil
}

// itemRef upserts an item known only by id and name (a reward, a vendor's item).
func (in *ingester) itemRef(id int64, name, link string) error {
	if id <= 0 {
		return nil
	}
	if name == "" {
		name = nameFromLink(link)
	}
	_, err := in.tx.Exec(`
		INSERT INTO items (item_id, name, link, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET
			name = COALESCE(NULLIF(excluded.name, ''), items.name),
			link = COALESCE(NULLIF(excluded.link, ''), items.link)`,
		id, name, link, in.now)
	if err != nil {
		return fmt.Errorf("write item %d: %w", id, err)
	}
	return nil
}

// ---- combats --------------------------------------------------------------------

// combats stores one or more finished encounters (msgType 7: combat and combats; the SavedVariables
// block has combats only). Each record replaces its children (spells, taken, death).
func (in *ingester) combats(charKey string, m map[string]any) error {
	// no guard here: a record of an unknown character is kept under the empty key
	var recs []map[string]any
	if c := asObj(m["combat"]); c != nil {
		recs = append(recs, c)
	}
	for _, e := range listValues(m["combats"]) {
		if c := asObj(e.value); c != nil {
			recs = append(recs, c)
		}
	}
	seen := map[int64]bool{}
	for _, c := range recs {
		id := int64(rawNum(c, "id"))
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		if err := in.combat(charKey, id, c); err != nil {
			return err
		}
	}
	return nil
}

func (in *ingester) combat(charKey string, id int64, c map[string]any) error {
	started := int64(rawNum(c, "startedAt"))
	duration := rawNum(c, "duration")
	dd := asObjOrEmpty(c["damageDone"])
	hd := asObjOrEmpty(c["healingDone"])
	dt := asObjOrEmpty(c["damageTaken"])
	damage, dps := rawNum(dd, "total"), rawNum(dd, "dps")
	if dps == 0 && duration > 0 {
		dps = damage / duration
	}
	health := -1
	if r := asObj(c["resourcesAtEnd"]); r != nil {
		if _, has := r["healthPct"]; has {
			health = int(rawNum(r, "healthPct"))
		}
	}
	targets := "[]"
	var names []string
	for _, t := range rawEntries(c["targets"]) {
		if tm := asObj(t.value); tm != nil && rawStr(tm, "name") != "" {
			names = append(names, rawStr(tm, "name"))
		}
	}
	if len(names) > 0 {
		if raw, err := json.Marshal(names); err == nil {
			targets = string(raw)
		}
	}
	died := truthyObj(c["died"]) || asObj(c["death"]) != nil
	if _, err := in.tx.Exec(`
		INSERT OR REPLACE INTO combats (char_key, combat_id, started_at, duration, zone, damage, dps, healing,
			taken, died, health_pct, targets, seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		charKey, id, started, duration, rawStr(c, "zone"), damage, dps, rawNum(hd, "total"),
		rawNum(dt, "total"), boolInt(died), health, targets, in.now); err != nil {
		return fmt.Errorf("write combat %d: %w", id, err)
	}
	for _, table := range []string{"combat_spells", "combat_taken", "deaths"} {
		if _, err := in.tx.Exec(`DELETE FROM `+table+` WHERE char_key = ? AND combat_id = ?`, charKey, id); err != nil {
			return fmt.Errorf("clear %s %d: %w", table, id, err)
		}
	}
	for _, kv := range []struct {
		kind  string
		spell map[string]any
	}{{"dmg", dd}, {"heal", hd}} {
		for _, s := range rawEntries(kv.spell["spells"]) {
			sm := asObj(s.value)
			if sm == nil {
				continue
			}
			if _, err := in.tx.Exec(`
				INSERT OR REPLACE INTO combat_spells (char_key, combat_id, kind, spell_id, name, amount, hits)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				charKey, id, kv.kind, int64(rawNum(sm, "spellID")), rawStr(sm, "name"),
				rawNum(sm, "amount"), int64(rawNum(sm, "hits"))); err != nil {
				return fmt.Errorf("write spell of combat %d: %w", id, err)
			}
		}
	}
	for _, s := range rawEntries(dt["sources"]) {
		sm := asObj(s.value)
		if sm == nil || rawStr(sm, "name") == "" {
			continue
		}
		if _, err := in.tx.Exec(`INSERT OR REPLACE INTO combat_taken (char_key, combat_id, source, amount) VALUES (?, ?, ?, ?)`,
			charKey, id, rawStr(sm, "name"), rawNum(sm, "amount")); err != nil {
			return fmt.Errorf("write taken of combat %d: %w", id, err)
		}
	}
	if d := asObj(c["death"]); d != nil || died {
		d = asObjOrEmpty(d)
		var hits []map[string]any
		for _, h := range rawEntries(d["lastHits"]) {
			if hm := asObj(h.value); hm != nil {
				hits = append(hits, map[string]any{
					"fuente": rawStr(hm, "source"), "hechizo": rawStr(hm, "spellName"), "cantidad": rawNum(hm, "amount"),
				})
			}
		}
		raw, err := json.Marshal(hits)
		if err != nil {
			return fmt.Errorf("encode last hits of combat %d: %w", id, err)
		}
		if _, err := in.tx.Exec(`INSERT OR REPLACE INTO deaths (char_key, combat_id, died_at, zone, killer, last_hits_json)
			VALUES (?, ?, ?, ?, ?, ?)`,
			charKey, id, started+int64(duration), rawStr(c, "zone"), rawStr(d, "killer"), string(raw)); err != nil {
			return fmt.Errorf("write death of combat %d: %w", id, err)
		}
	}
	return nil
}

// ---- action bars and macros ---------------------------------------------------------

// actionBars replaces the character's action bar slots (msgType 8).
func (in *ingester) actionBars(charKey string, m map[string]any) error {
	// no guard here: a record of an unknown character is kept under the empty key
	bars := asObj(m["actionBars"])
	if bars == nil {
		bars = m
	}
	if _, err := in.tx.Exec(`DELETE FROM action_bars WHERE char_key = ?`, charKey); err != nil {
		return fmt.Errorf("clear action bars: %w", err)
	}
	for _, e := range rawEntries(bars["slots"]) {
		s := asObj(e.value)
		if s == nil {
			continue
		}
		slot := int64(rawNum(s, "slot"))
		if slot <= 0 {
			continue
		}
		if _, err := in.tx.Exec(`INSERT OR REPLACE INTO action_bars (char_key, slot, kind, name, item_id, key)
			VALUES (?, ?, ?, ?, ?, ?)`,
			charKey, slot, rawStr(s, "kind"), rawStr(s, "name"), int64(rawNum(s, "id")), rawStr(s, "key")); err != nil {
			return fmt.Errorf("write action bar slot %d: %w", slot, err)
		}
	}
	return nil
}

// macros replaces the account and the character macros (msgType 13).
func (in *ingester) macros(charKey string, m map[string]any) error {
	for _, scope := range []string{"account", "character"} {
		key := ""
		if scope == "character" {
			key = charKey
		} else {
			key = ""
		}
		block := asObj(m[scope])
		if block == nil {
			continue
		}
		if _, err := in.tx.Exec(`DELETE FROM macros WHERE scope = ? AND char_key = ?`, scope, key); err != nil {
			return fmt.Errorf("clear %s macros: %w", scope, err)
		}
		for i, e := range listValues(block["list"]) {
			mm := asObj(e.value)
			if mm == nil {
				continue
			}
			idx := int64(rawNum(mm, "index"))
			if idx <= 0 {
				idx = int64(i + 1)
			}
			if _, err := in.tx.Exec(`INSERT OR REPLACE INTO macros (scope, char_key, idx, name, body) VALUES (?, ?, ?, ?, ?)`,
				scope, key, idx, rawStr(mm, "name"), rawStr(mm, "body")); err != nil {
				return fmt.Errorf("write %s macro %d: %w", scope, idx, err)
			}
		}
	}
	return nil
}

// ---- sessions --------------------------------------------------------------------

// session stores the addon's current session and its previous ones (msgType 14). A session is
// identified by its character and start time, so a session sent again updates its row.
func (in *ingester) session(charKey string, m map[string]any) error {
	if len(m) == 0 {
		return nil
	}
	xpMax := in.xpMax(charKey)
	if start := int64(rawNum(m, "startedAt")); start > 0 {
		levels, money := asObjOrEmpty(m["level"]), asObjOrEmpty(m["money"])
		updated := int64(rawNum(m, "updatedAt"))
		if updated == 0 {
			updated = in.now
		}
		if err := in.sessionRow(charKey, start, sessionFields{
			lastData: updated, open: true,
			startLevel: int(rawNum(levels, "start")), endLevel: int(rawNum(levels, "now")),
			xpPct:      pctOf(rawNum(m, "xpGained"), xpMax),
			moneyStart: int64(rawNum(money, "start")), moneyEnd: int64(rawNum(money, "now")),
			completed: ids(m["questsCompleted"]), zones: ids(m["zones"]), deaths: int(rawNum(m, "deaths")),
		}); err != nil {
			return err
		}
	}
	for _, e := range rawEntries(m["previous"]) {
		p := asObj(e.value)
		if p == nil {
			continue
		}
		start := int64(rawNum(p, "startedAt"))
		if start == 0 {
			continue
		}
		if err := in.sessionRow(charKey, start, sessionFields{
			ended: int64(rawNum(p, "endedAt")), startLevel: int(rawNum(p, "levelStart")),
			endLevel: int(rawNum(p, "levelEnd")), xpPct: pctOf(rawNum(p, "xpGained"), xpMax),
			moneyEnd: int64(rawNum(p, "moneyDelta")), deaths: int(rawNum(p, "deaths")),
		}); err != nil {
			return err
		}
	}
	return nil
}

type sessionFields struct {
	lastData, ended      int64
	open                 bool
	startLevel, endLevel int
	xpPct                float64
	moneyStart, moneyEnd int64
	completed, zones     []int64
	deaths               int
}

// sessionRow inserts or updates the addon session of charKey that started at start.
func (in *ingester) sessionRow(charKey string, start int64, f sessionFields) error {
	completed, _ := json.Marshal(nonNilIDs(f.completed))
	zones, _ := json.Marshal(nonNilIDs(f.zones))
	gained := f.endLevel - f.startLevel
	if gained < 0 {
		gained = 0
	}
	var id int64
	err := in.tx.QueryRow(`SELECT id FROM sessions WHERE char_key = ? AND started_at = ?`, charKey, start).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = in.tx.Exec(`INSERT INTO sessions (char_key, started_at, last_data_at, ended_at, open, start_level, end_level,
			levels_gained, xp_gained_pct, money_start, money_end, quests_completed, quests_turned_in, zones, deaths)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '[]', ?, ?)`,
			charKey, start, f.lastData, f.ended, boolInt(f.open), f.startLevel, f.endLevel, gained, f.xpPct,
			f.moneyStart, f.moneyEnd, string(completed), string(zones), f.deaths)
	case err == nil:
		_, err = in.tx.Exec(`UPDATE sessions SET last_data_at = ?, ended_at = ?, open = ?, start_level = ?, end_level = ?,
			levels_gained = ?, xp_gained_pct = ?, money_start = ?, money_end = ?, quests_completed = ?, zones = ?, deaths = ?
			WHERE id = ?`,
			f.lastData, f.ended, boolInt(f.open), f.startLevel, f.endLevel, gained, f.xpPct,
			f.moneyStart, f.moneyEnd, string(completed), string(zones), f.deaths, id)
	}
	if err != nil {
		return fmt.Errorf("write session %s %d: %w", charKey, start, err)
	}
	return nil
}

// xpMax is the XP needed for the character's current level (0 when unknown).
func (in *ingester) xpMax(charKey string) float64 {
	var v float64
	_ = in.tx.QueryRow(`SELECT xp_max FROM characters WHERE char_key = ?`, charKey).Scan(&v)
	return v
}

func pctOf(xp, max float64) float64 {
	if max <= 0 {
		return 0
	}
	return xp / max * 100
}

func ids(v any) []int64 {
	var out []int64
	for _, e := range rawEntries(v) {
		if id := int64(valueNum(e.value)); id > 0 {
			out = append(out, id)
		}
	}
	return out
}

// ---- heavy: quest window, gear candidates, dungeons --------------------------------------

// heavy stores the quest window (what the character can take now), the gear candidates and the
// dungeons (msgType 10).
func (in *ingester) heavy(charKey string, m map[string]any) error {
	if err := in.questWindow(charKey, asObjOrEmpty(m["questWindow"])); err != nil {
		return err
	}
	if err := in.gearCandidates(charKey, asObjOrEmpty(m["gearCandidates"])); err != nil {
		return err
	}
	if d := asObj(m["dungeons"]); d != nil {
		return in.dungeons(d)
	}
	return nil
}

func (in *ingester) questWindow(charKey string, qw map[string]any) error {
	if !truthyObj(qw["available"]) {
		return nil
	}
	if _, err := in.tx.Exec(`DELETE FROM quest_window WHERE char_key = ?`, charKey); err != nil {
		return fmt.Errorf("clear quest window: %w", err)
	}
	for _, e := range listValues(qw["quests"]) {
		q := asObj(e.value)
		if q == nil {
			continue
		}
		id := idOf(q, "questID", e.key)
		if id <= 0 {
			continue
		}
		missing, _ := json.Marshal(ids(q["missingPrereqs"]))
		starter, _ := json.Marshal(asObjOrEmpty(q["starter"]))
		finisher, _ := json.Marshal(asObjOrEmpty(q["finisher"]))
		rewards, _ := json.Marshal(asObjOrEmpty(q["rewards"]))
		if _, err := in.tx.Exec(`
			INSERT OR REPLACE INTO quest_window (char_key, quest_id, name, level, required_level, zone, accessible,
				source, in_log, missing_json, starter_json, finisher_json, rewards_json, seen_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			charKey, id, rawStr(q, "name"), int64(rawNum(q, "level")), int64(rawNum(q, "requiredLevel")),
			rawStr(q, "zone"), boolInt(truthyObj(q["accessibleNow"])), rawStr(q, "source"),
			boolInt(truthyObj(q["inLog"])), string(missing), string(starter), string(finisher), string(rewards),
			in.now); err != nil {
			return fmt.Errorf("write quest window %d: %w", id, err)
		}
	}
	return nil
}

func (in *ingester) gearCandidates(charKey string, cw map[string]any) error {
	if !truthyObj(cw["available"]) {
		return nil
	}
	if _, err := in.tx.Exec(`DELETE FROM gear_candidates WHERE char_key = ?`, charKey); err != nil {
		return fmt.Errorf("clear gear candidates: %w", err)
	}
	for _, e := range listValues(cw["items"]) {
		it := asObj(e.value)
		if it == nil {
			continue
		}
		id := idOf(it, "itemID", e.key)
		if id <= 0 {
			continue
		}
		it["itemID"] = float64(id)
		if err := in.item(it); err != nil {
			return err
		}
		for _, s := range rawEntries(it["sources"]) {
			sm := asObj(s.value)
			if sm == nil {
				continue
			}
			if _, err := in.tx.Exec(`
				INSERT OR REPLACE INTO gear_candidates (char_key, item_id, source_kind, quest_id, dungeon_key, boss, source, choice)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				charKey, id, rawStr(sm, "kind"), int64(rawNum(sm, "questID")), rawStr(sm, "dungeonKey"),
				rawStr(sm, "boss"), rawStr(sm, "source"), boolInt(truthyObj(sm["choice"]))); err != nil {
				return fmt.Errorf("write candidate %d: %w", id, err)
			}
		}
	}
	return nil
}

// dungeons replaces the bosses and loot of each dungeon in the list (account-wide data).
func (in *ingester) dungeons(d map[string]any) error {
	if !truthyObj(d["available"]) {
		return nil
	}
	for _, e := range listValues(d["list"]) {
		m := asObj(e.value)
		if m == nil {
			continue
		}
		key := rawStr(m, "key")
		if key == "" {
			key = rawStr(m, "name")
		}
		if key == "" {
			continue
		}
		entrance := asObjOrEmpty(m["entrance"])
		quests, _ := json.Marshal(ids(m["quests"]))
		if _, err := in.tx.Exec(`
			INSERT INTO dungeons (dungeon_key, name, zone, min_level, max_level, source, map_id, x, y, quests_json, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(dungeon_key) DO UPDATE SET
				name = excluded.name, zone = excluded.zone, min_level = excluded.min_level,
				max_level = excluded.max_level, source = excluded.source, map_id = excluded.map_id,
				x = excluded.x, y = excluded.y, quests_json = excluded.quests_json, updated_at = excluded.updated_at`,
			key, rawStr(m, "name"), rawStr(m, "zone"), int64(rawNum(m, "minLevel")), int64(rawNum(m, "maxLevel")),
			rawStr(m, "source"), int64(rawNum(entrance, "mapID")), rawNum(entrance, "x"), rawNum(entrance, "y"),
			string(quests), in.now); err != nil {
			return fmt.Errorf("write dungeon %s: %w", key, err)
		}
		if _, err := in.tx.Exec(`DELETE FROM dungeon_bosses WHERE dungeon_key = ?`, key); err != nil {
			return fmt.Errorf("clear bosses of %s: %w", key, err)
		}
		if _, err := in.tx.Exec(`DELETE FROM dungeon_loot WHERE dungeon_key = ?`, key); err != nil {
			return fmt.Errorf("clear loot of %s: %w", key, err)
		}
		for _, b := range rawEntries(m["bosses"]) {
			bm := asObj(b.value)
			if bm == nil || rawStr(bm, "name") == "" {
				continue
			}
			boss := rawStr(bm, "name")
			if _, err := in.tx.Exec(`INSERT OR REPLACE INTO dungeon_bosses (dungeon_key, boss) VALUES (?, ?)`, key, boss); err != nil {
				return fmt.Errorf("write boss %s: %w", boss, err)
			}
			for _, item := range ids(bm["loot"]) {
				if _, err := in.tx.Exec(`INSERT OR REPLACE INTO dungeon_loot (dungeon_key, boss, item_id) VALUES (?, ?, ?)`,
					key, boss, item); err != nil {
					return fmt.Errorf("write loot of %s: %w", boss, err)
				}
			}
		}
	}
	return nil
}

// ---- knowledge (quests and NPCs learned in play) -----------------------------------------

// knowledge merges the learned quests and NPCs (msgType 9: {npcs, quests}; SavedVariables knowledge).
func (in *ingester) knowledge(m map[string]any) error {
	for _, e := range listValues(m["npcs"]) {
		nm := asObj(e.value)
		if nm == nil {
			continue
		}
		n := npcFromRaw(nm, e.key)
		if n.LastSeen == 0 {
			n.LastSeen = in.now // the addon does not keep lastSeen (volatile); the ingestion time stands in
		}
		if n.NpcID == 0 {
			continue
		}
		if err := upsertNpc(in.tx, n); err != nil {
			return err
		}
		if err := in.npcRoles(n); err != nil {
			return err
		}
	}
	for _, e := range listValues(m["quests"]) {
		qm := asObj(e.value)
		if qm == nil {
			continue
		}
		q := questFromRaw(qm, e.key)
		if q.LastSeen == 0 {
			q.LastSeen = in.now
		}
		if q.QuestID == 0 {
			continue
		}
		if err := upsertQuest(in.tx, q); err != nil {
			return err
		}
		if err := in.questRewards(q.QuestID, qm["rewards"]); err != nil {
			return err
		}
		if q.TurnInNpcID != 0 {
			if _, err := in.tx.Exec(`INSERT OR REPLACE INTO npc_roles (npc_id, role, map_id, x, y) VALUES (?, 'turnin', ?, ?, ?)`,
				q.TurnInNpcID, q.TurnInMapID, q.TurnInX, q.TurnInY); err != nil {
				return fmt.Errorf("write turn-in role of %d: %w", q.TurnInNpcID, err)
			}
		}
	}
	return nil
}

// npcRoles writes the role rows that follow from the NPC's flags.
func (in *ingester) npcRoles(n KNpc) error {
	roles := []string{}
	if n.IsQuestGiver {
		roles = append(roles, "quest_giver")
	}
	if n.IsVendor {
		roles = append(roles, "vendor")
	}
	if n.IsTrainer {
		roles = append(roles, "trainer")
	}
	for _, r := range roles {
		if _, err := in.tx.Exec(`INSERT OR REPLACE INTO npc_roles (npc_id, role, map_id, x, y) VALUES (?, ?, ?, ?, ?)`,
			n.NpcID, r, n.MapID, n.X, n.Y); err != nil {
			return fmt.Errorf("write role %s of %d: %w", r, n.NpcID, err)
		}
	}
	return nil
}

// questRewards replaces the rewards of a quest: {money, xp, items: [{itemID, link, count, choice}]}.
func (in *ingester) questRewards(questID int64, v any) error {
	r := asObj(v)
	if r == nil {
		return nil
	}
	if _, err := in.tx.Exec(`DELETE FROM quest_rewards WHERE quest_id = ?`, questID); err != nil {
		return fmt.Errorf("clear rewards of %d: %w", questID, err)
	}
	if money := int64(rawNum(r, "money")); money > 0 {
		if _, err := in.tx.Exec(`INSERT OR REPLACE INTO quest_rewards (quest_id, kind, item_id, amount) VALUES (?, 'money', 0, ?)`, questID, money); err != nil {
			return fmt.Errorf("write money reward of %d: %w", questID, err)
		}
	}
	if xp := int64(rawNum(r, "xp")); xp > 0 {
		if _, err := in.tx.Exec(`INSERT OR REPLACE INTO quest_rewards (quest_id, kind, item_id, amount) VALUES (?, 'xp', 0, ?)`, questID, xp); err != nil {
			return fmt.Errorf("write xp reward of %d: %w", questID, err)
		}
	}
	for _, e := range rawEntries(r["items"]) {
		it := asObj(e.value)
		if it == nil {
			continue
		}
		id := int64(rawNum(it, "itemID"))
		if id <= 0 {
			continue
		}
		kind := "reward"
		if truthyObj(it["choice"]) {
			kind = "choice"
		}
		if err := in.itemRef(id, "", rawStr(it, "link")); err != nil {
			return err
		}
		if _, err := in.tx.Exec(`INSERT OR REPLACE INTO quest_rewards (quest_id, kind, item_id, amount) VALUES (?, ?, ?, ?)`,
			questID, kind, id, int64(rawNum(it, "count"))); err != nil {
			return fmt.Errorf("write reward item %d of %d: %w", id, questID, err)
		}
	}
	return nil
}

// ---- world (flights, vendors, trainers, quest chains, consumables) -------------------------

// world stores the world block (msgType 16: changed parts only; the SavedVariables block is whole).
func (in *ingester) world(m map[string]any) error {
	if err := in.flights(asObjOrEmpty(m["flight"])); err != nil {
		return err
	}
	for _, e := range listValues(m["vendors"]) {
		v := asObj(e.value)
		if v == nil {
			continue
		}
		if err := in.vendor(idOf(v, "npcID", e.key), v); err != nil {
			return err
		}
	}
	for _, e := range listValues(m["trainers"]) {
		t := asObj(e.value)
		if t == nil {
			continue
		}
		if err := in.trainer(idOf(t, "npcID", e.key), t); err != nil {
			return err
		}
	}
	for _, e := range listValues(m["questChains"]) {
		c := asObj(e.value)
		id, err := strconv.ParseInt(e.key, 10, 64)
		if c == nil || err != nil || id <= 0 {
			continue
		}
		pre, _ := json.Marshal(ids(c["pre"]))
		if _, err := in.tx.Exec(`INSERT OR REPLACE INTO quest_chains (quest_id, next_id, chain_name, name, pre_json)
			VALUES (?, ?, ?, ?, ?)`,
			id, int64(rawNum(c, "next")), rawStr(c, "chainName"), rawStr(c, "name"), string(pre)); err != nil {
			return fmt.Errorf("write chain %d: %w", id, err)
		}
	}
	for _, e := range listValues(m["consumables"]) {
		c := asObj(e.value)
		if c == nil {
			continue
		}
		id := idOf(c, "itemID", e.key)
		if id <= 0 {
			continue
		}
		var health, mana, seconds int64
		if r := asObj(c["restores"]); r != nil {
			health, mana, seconds = int64(rawNum(r, "health")), int64(rawNum(r, "mana")), int64(rawNum(r, "seconds"))
		}
		skill, rank := "", int64(0)
		if sk := asList(c["skill"]); len(sk) == 2 {
			skill, rank = rawStrV(sk[0]), int64(valueNum(sk[1]))
		}
		if _, err := in.tx.Exec(`
			INSERT INTO consumables (item_id, name, kind, required_level, health, mana, seconds, skill_name, skill_rank, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(item_id) DO UPDATE SET
				name = excluded.name, kind = excluded.kind, required_level = excluded.required_level,
				health = excluded.health, mana = excluded.mana, seconds = excluded.seconds,
				skill_name = excluded.skill_name, skill_rank = excluded.skill_rank, updated_at = excluded.updated_at`,
			id, rawStr(c, "name"), rawStr(c, "kind"), int64(rawNum(c, "requiredLevel")), health, mana, seconds,
			skill, rank, in.now); err != nil {
			return fmt.Errorf("write consumable %d: %w", id, err)
		}
		if err := in.itemRef(id, rawStr(c, "name"), ""); err != nil {
			return err
		}
	}
	return nil
}

func rawStrV(v any) string {
	s, _ := v.(string)
	return s
}

// flights stores the flight nodes and routes of each faction, and the nodes each character knows.
func (in *ingester) flights(f map[string]any) error {
	for _, e := range rawEntries(f) {
		if e.key == "known" {
			continue
		}
		block := asObj(e.value)
		if block == nil {
			continue
		}
		for _, ne := range listValues(block["nodes"]) {
			nm := asObj(ne.value)
			if nm == nil {
				continue
			}
			id := idOf(nm, "nodeID", ne.key)
			mapID := int64(rawNum(nm, "mapID"))
			if id <= 0 || mapID <= 0 {
				continue
			}
			x, y, hasPos := point(nm)
			if _, err := in.tx.Exec(`
				INSERT INTO flight_nodes (node_id, name, map_id, faction, x, y, has_pos) VALUES (?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(node_id) DO UPDATE SET
					name = COALESCE(NULLIF(excluded.name, ''), flight_nodes.name),
					map_id = excluded.map_id, faction = excluded.faction,
					x = CASE WHEN excluded.has_pos = 1 THEN excluded.x ELSE flight_nodes.x END,
					y = CASE WHEN excluded.has_pos = 1 THEN excluded.y ELSE flight_nodes.y END,
					has_pos = MAX(excluded.has_pos, flight_nodes.has_pos)`,
				id, rawStr(nm, "name"), mapID, e.key, x, y, boolInt(hasPos)); err != nil {
				return fmt.Errorf("write flight node %d: %w", id, err)
			}
		}
		for _, ce := range listValues(block["costs"]) {
			from, to, ok := parseEdge(ce.key)
			cm := asObj(ce.value)
			if !ok || cm == nil || rawNum(cm, "seconds") <= 0 {
				continue
			}
			if _, err := in.tx.Exec(`
				INSERT INTO flight_routes (from_id, to_id, copper, hops, seconds) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(from_id, to_id) DO UPDATE SET
					copper = excluded.copper, hops = excluded.hops, seconds = excluded.seconds`,
				from, to, int64(rawNum(cm, "copper")), int64(rawNum(cm, "hops")), rawNum(cm, "seconds")); err != nil {
				return fmt.Errorf("write flight route %d>%d: %w", from, to, err)
			}
		}
	}
	known := asObj(f["known"])
	for _, e := range rawEntries(known) {
		list := ids(e.value)
		if _, err := in.tx.Exec(`DELETE FROM flight_known WHERE char_key = ?`, e.key); err != nil {
			return fmt.Errorf("clear known flights of %s: %w", e.key, err)
		}
		for _, id := range list {
			if _, err := in.tx.Exec(`INSERT OR REPLACE INTO flight_known (char_key, node_id) VALUES (?, ?)`, e.key, id); err != nil {
				return fmt.Errorf("write known flight %d: %w", id, err)
			}
		}
	}
	return nil
}

// vendor stores a vendor NPC and replaces the list of items it sells.
func (in *ingester) vendor(npcID int64, v map[string]any) error {
	if npcID <= 0 {
		return nil
	}
	x, y, _ := point(v)
	mapID := int64(rawNum(v, "mapID"))
	if _, err := in.tx.Exec(`
		INSERT INTO vendors (npc_id, name, map_id, x, y, faction, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(npc_id) DO UPDATE SET
			name = COALESCE(NULLIF(excluded.name, ''), vendors.name), map_id = excluded.map_id,
			x = excluded.x, y = excluded.y, faction = excluded.faction, updated_at = excluded.updated_at`,
		npcID, rawStr(v, "name"), mapID, x, y, rawStr(v, "faction"), in.now); err != nil {
		return fmt.Errorf("write vendor %d: %w", npcID, err)
	}
	if err := upsertNpc(in.tx, KNpc{NpcID: npcID, Name: rawStr(v, "name"), MapID: int(mapID), X: x, Y: y,
		IsVendor: true, LastSeen: in.now}); err != nil {
		return err
	}
	if err := in.npcRoles(KNpc{NpcID: npcID, MapID: int(mapID), X: x, Y: y, IsVendor: true}); err != nil {
		return err
	}
	if _, err := in.tx.Exec(`DELETE FROM vendor_items WHERE npc_id = ?`, npcID); err != nil {
		return fmt.Errorf("clear items of vendor %d: %w", npcID, err)
	}
	for _, e := range rawEntries(v["items"]) {
		it := asObj(e.value)
		if it == nil {
			continue
		}
		id := int64(rawNum(it, "itemID"))
		if id <= 0 {
			continue
		}
		if err := in.itemRef(id, rawStr(it, "name"), ""); err != nil {
			return err
		}
		if _, err := in.tx.Exec(`INSERT OR REPLACE INTO vendor_items (npc_id, item_id, name, price, stack, limited, requires_rep)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			npcID, id, rawStr(it, "name"), int64(rawNum(it, "price")), int64(rawNum(it, "stack")),
			boolInt(truthyObj(it["limited"])), rawStr(it, "requiresRep")); err != nil {
			return fmt.Errorf("write vendor item %d: %w", id, err)
		}
	}
	return nil
}

// trainer stores a trainer NPC and replaces its services.
func (in *ingester) trainer(npcID int64, t map[string]any) error {
	if npcID <= 0 {
		return nil
	}
	x, y, _ := point(t)
	mapID := int64(rawNum(t, "mapID"))
	if _, err := in.tx.Exec(`
		INSERT INTO trainers (npc_id, name, map_id, x, y, kind, profession, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(npc_id) DO UPDATE SET
			name = COALESCE(NULLIF(excluded.name, ''), trainers.name), map_id = excluded.map_id,
			x = excluded.x, y = excluded.y, kind = excluded.kind, profession = excluded.profession,
			updated_at = excluded.updated_at`,
		npcID, rawStr(t, "name"), mapID, x, y, rawStr(t, "kind"), rawStr(t, "profession"), in.now); err != nil {
		return fmt.Errorf("write trainer %d: %w", npcID, err)
	}
	if err := upsertNpc(in.tx, KNpc{NpcID: npcID, Name: rawStr(t, "name"), MapID: int(mapID), X: x, Y: y,
		IsTrainer: true, TrainerType: rawStr(t, "kind"), LastSeen: in.now}); err != nil {
		return err
	}
	if err := in.npcRoles(KNpc{NpcID: npcID, MapID: int(mapID), X: x, Y: y, IsTrainer: true}); err != nil {
		return err
	}
	if _, err := in.tx.Exec(`DELETE FROM trainer_services WHERE npc_id = ?`, npcID); err != nil {
		return fmt.Errorf("clear services of trainer %d: %w", npcID, err)
	}
	for _, e := range rawEntries(t["services"]) {
		s := asObj(e.value)
		if s == nil || rawStr(s, "name") == "" {
			continue
		}
		skill, rank := "", int64(0)
		if sk := asObj(s["skill"]); sk != nil {
			skill, rank = rawStr(sk, "name"), int64(rawNum(sk, "rank"))
		}
		if _, err := in.tx.Exec(`INSERT OR REPLACE INTO trainer_services (npc_id, name, rank, cost, level, skill_name, skill_rank, available)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			npcID, rawStr(s, "name"), rawStr(s, "rank"), int64(rawNum(s, "cost")), int64(rawNum(s, "level")),
			skill, rank, rawStr(s, "available")); err != nil {
			return fmt.Errorf("write service of trainer %d: %w", npcID, err)
		}
	}
	return nil
}

// point reads x and y of a table: the addon writes 0..1, a value above 1 is taken as 0..100.
func point(m map[string]any) (x, y float64, ok bool) {
	xv, okX := m["x"].(float64)
	yv, okY := m["y"].(float64)
	if !okX || !okY || xv < 0 || yv < 0 {
		return 0, 0, false
	}
	if xv > 1 || yv > 1 {
		xv, yv = xv/100, yv/100
	}
	if xv > 1 || yv > 1 {
		return 0, 0, false
	}
	return xv, yv, true
}

// parseEdge reads "fromID>toID".
func parseEdge(key string) (int64, int64, bool) {
	parts := strings.Split(key, ">")
	if len(parts) != 2 {
		return 0, 0, false
	}
	from, e1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	to, e2 := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if e1 != nil || e2 != nil || from <= 0 || to <= 0 {
		return 0, 0, false
	}
	return from, to, true
}

// completedBlock is the completed-quests part of a character: present only when the block is.
func completedBlock(ch map[string]any) map[string]any {
	if l, ok := ch["completedQuests"].([]any); ok {
		return map[string]any{"list": l}
	}
	return nil
}

// activeCharKey is the character the SavedVariables data is about: the one the player was on last, as
// gamedata picks it (ActiveRank). It names the owner of outbox items that carry no charKey.
func activeCharKey(chars any) string {
	best, bestTS := "", -1.0
	for _, e := range rawEntries(chars) {
		ch := asObj(e.value)
		if ch == nil {
			continue
		}
		ts := ActiveRank(ch)
		if ts > bestTS || (ts == bestTS && e.key < best) {
			best, bestTS = e.key, ts
		}
	}
	return best
}

// ---- the lean outbox record (schema 2, Storage.lua) ----------------------------------------
//
// The addon sends the records it learned (combats 7, knowledge 9, sheet 12, world 16) as outbox
// payloads { oseq, p, v }: p is the path of the record (for example {"quests", 127}, {"combats", 3},
// {"sheet", <day>} or {"flight", "Alliance", "nodes", 10}) and v is the record with short keys
// (Storage.KEYS). outboxBody turns such a payload back into the body of its message type, the
// shape the ingesters read, so the old and the lean forms share one path.

// outboxKeys maps the short keys of a compact record to their long names (Storage.KEYS, inverted).
var outboxKeys = map[string]string{
	"n": "name", "m": "mapID", "x": "x", "y": "y", "r": "reaction",
	"qg": "isQuestGiver", "vd": "isVendor", "tr": "isTrainer", "ac": "availableCount", "sv": "services",
	"fs": "firstSeen", "pn": "npcID", "qd": "questID", "ti": "title", "lv": "level",
	"ot": "objectivesText", "dt": "descriptionText", "rw": "rewards", "mo": "money", "xp": "xp",
	"it": "items", "i": "itemID", "l": "link", "c": "count", "ch": "choice",
	"gn": "giverNpcID", "gna": "giverName", "gm": "giverMapID", "gx": "giverX", "gy": "giverY",
	"tn": "turnInNpcID", "tna": "turnInName", "tm": "turnInMapID", "tx": "turnInX", "ty": "turnInY",
	"oa": "offeredAt", "os": "offeredSource", "nd": "nodeID", "kn": "known",
	"cu": "copper", "ho": "hops", "se": "seconds", "st": "status", "sl": "slot",
}

// expandKeys returns v with the short keys of every nested table replaced by the long names.
func expandKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if long, ok := outboxKeys[k]; ok {
				k = long
			}
			out[k] = expandKeys(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = expandKeys(val)
		}
		return out
	}
	return v
}

// outboxBody decodes an outbox payload of a record message type into the body the ingesters read.
// day is set for the sheet, whose path carries the day (days since the Unix epoch, as the addon
// writes it). ok is false when m is not in the { oseq, p, v } shape, so the old bodies pass as they are.
func outboxBody(msgType uint8, m map[string]any) (body map[string]any, day string, ok bool) {
	switch msgType {
	case bridge.MsgCombat, bridge.MsgKnowledge, bridge.MsgSheet, bridge.MsgWorld:
	default:
		return nil, "", false
	}
	p, isPath := m["p"].([]any)
	v, hasValue := m["v"]
	_, hasSeq := m["oseq"]
	if !isPath || !hasValue || !hasSeq || len(p) == 0 {
		return nil, "", false
	}
	rec := expandKeys(v)
	if msgType == bridge.MsgSheet {
		if len(p) >= 2 {
			if d, isNum := p[1].(float64); isNum {
				day = time.Unix(int64(d)*86400, 0).UTC().Format("2006-01-02")
			}
		}
		return asObjOrEmpty(rec), day, true
	}
	// Nest the record under its path: {"quests", 127} -> {"quests": {"127": rec}}.
	var cur any = rec
	for i := len(p) - 1; i >= 0; i-- {
		cur = map[string]any{pathKey(p[i]): cur}
	}
	return cur.(map[string]any), "", true
}

// pathKey is a path element as a table key: numbers are written as integers.
func pathKey(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	}
	return ""
}
