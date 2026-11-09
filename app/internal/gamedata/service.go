// Package gamedata is the single facade over live pixel data, SavedVariables and the
// knowledge store. MCP tools, the assistant and the UI all read game state through it.
//
// Every getter returns a JSON-serializable value. On success it is a map with
// "available": true, "source" ("live" or "savedvariables") and, when known, an
// "updatedAt" RFC 3339 timestamp. When data is missing it is a map with
// "available": false and a Spanish "hint" for the user.
package gamedata

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/session"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// Hints shown to the user when data is missing.
const (
	HintSync = "Escribe /compa sync en el juego para actualizar los datos"
	HintLive = "Abre World of Warcraft en modo ventana o sin bordes para leer datos en vivo"
)

const (
	sourceLive = "live"
	sourceSV   = "savedvariables"
)

// Service holds the data sources. Any of them may be nil; the getters then report
// the data as unavailable instead of failing.
type Service struct {
	reader   *bridge.Reader
	watcher  *bridge.SVWatcher
	store    *store.Store
	sessions *session.Tracker // optional: the running session, see SetSessions

	startOnce sync.Once
}

// NewService creates the facade over the given sources.
func NewService(r *bridge.Reader, w *bridge.SVWatcher, s *store.Store) *Service {
	return &Service{reader: r, watcher: w, store: s}
}

// Start starts the reader and the SavedVariables watcher (both are idempotent) and
// imports knowledge into the store on start and on each SavedVariables change.
// It returns immediately; the work runs until ctx is cancelled.
func (g *Service) Start(ctx context.Context) {
	g.startOnce.Do(func() {
		if g.reader != nil {
			g.reader.Start(ctx)
		}
		if g.watcher == nil {
			return
		}
		g.watcher.Start(ctx)
		go g.importLoop(ctx)
	})
}

func (g *Service) importLoop(ctx context.Context) {
	if sv := g.watcher.Current(); sv != nil {
		g.importKnowledge(sv)
	}
	changes := g.watcher.Changes()
	for {
		select {
		case <-ctx.Done():
			return
		case sv := <-changes:
			g.importKnowledge(sv)
		}
	}
}

func (g *Service) importKnowledge(sv *bridge.SavedVars) {
	if g.store == nil {
		return
	}
	if _, _, err := g.store.ImportKnowledge(sv); err != nil {
		log.Printf("gamedata: import knowledge: %v", err)
	}
}

// Store returns the knowledge store (may be nil).
func (g *Service) Store() *store.Store { return g.store }

// Live returns the latest fresh pixel-bridge state.
func (g *Service) Live() (bridge.LiveState, bool) {
	if g.reader == nil {
		return bridge.LiveState{}, false
	}
	return g.reader.Latest()
}

// Location returns the current map, coordinates and zone names.
func (g *Service) Location() (any, error) {
	live, liveOK := g.Live()
	ch, sv, chOK := g.activeCharacter()
	if !liveOK && !chOK {
		return unavailable(HintSync), nil
	}

	fields := map[string]any{}
	source := sourceSV
	var at time.Time
	var mapID int
	var loc map[string]any
	if chOK {
		loc, _ = ch.data["location"].(map[string]any)
		at = ch.updatedAt
	}
	if liveOK {
		source = sourceLive
		at = live.ReadAt
		mapID = int(live.MapID)
		fields["x"] = live.X
		fields["y"] = live.Y
		fields["inInstance"] = live.InInstance
	} else if loc != nil {
		mapID = int(numberOf(loc["mapID"]))
		copyIfPresent(fields, loc, "x")
		copyIfPresent(fields, loc, "y")
	}

	zone, err := g.zoneName(mapID, sv)
	if err != nil {
		return nil, err
	}
	if zone == "" && loc != nil {
		zone, _ = loc["zone"].(string)
	}
	fields["mapId"] = mapID
	fields["zone"] = zone
	if loc != nil {
		copyIfPresent(fields, loc, "subZone")
		copyIfPresent(fields, loc, "instanceName")
		copyIfPresent(fields, loc, "instanceType")
	}
	return available(source, at, fields), nil
}

// Character returns the active character merged with live level, xp, money and health.
func (g *Service) Character() (any, error) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return unavailable(HintSync), nil
	}
	char, _ := ch.data["character"].(map[string]any)
	if char == nil {
		return unavailable(HintSync), nil
	}
	fields := copyMap(char)
	fields["key"] = ch.key
	source := sourceSV
	at := ch.updatedAt
	if live, ok := g.Live(); ok {
		source = sourceLive
		at = live.ReadAt
		if live.Level > 0 {
			fields["level"] = float64(live.Level)
		}
		fields["xpPct"] = float64(live.XPPct)
		fields["money"] = float64(live.Money)
		if live.HealthPct != 255 {
			fields["healthPct"] = float64(live.HealthPct)
		}
		if live.PowerPct != 255 {
			fields["powerPct"] = float64(live.PowerPct)
		}
		fields["inCombat"] = live.InCombat
		fields["dead"] = live.Dead
		fields["resting"] = live.Resting
		fields["mounted"] = live.Mounted
	}
	return available(source, at, fields), nil
}

// Quests returns the quest log of the active character, enriched with knowledge
// (giver and turn-in NPCs, rewards) and the live tracked quest id.
func (g *Service) Quests() (any, error) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return unavailable(HintSync), nil
	}
	var trackedFromSV int64
	list := make([]map[string]any, 0)
	qx := readQuestie(ch.data)
	pos := g.playerPosition()
	for _, e := range entriesOf(ch.data["quests"]) {
		qm, ok := e.value.(map[string]any)
		if !ok {
			continue
		}
		id := questIDOf(qm, e.key)
		if b, _ := qm["isSuperTracked"].(bool); b && trackedFromSV == 0 {
			trackedFromSV = id
		}
		q := copyMap(qm)
		if _, has := q["questID"]; !has {
			q["questID"] = id
		}
		if err := g.enrichQuest(q, id); err != nil {
			return nil, err
		}
		if qq, ok := qx.quest(id); ok {
			q["questie"] = map[string]any{
				"starters":   sourcesView(qq.Starters, pos),
				"finishers":  sourcesView(qq.Finishers, pos),
				"objectives": objectivesView(qq.Objectives, pos),
			}
		}
		list = append(list, q)
	}
	tracked := trackedFromSV
	if live, ok := g.Live(); ok && live.TrackedQuestID != 0 {
		tracked = int64(live.TrackedQuestID)
	}
	return available(sourceSV, ch.updatedAt, map[string]any{
		"quests":         list,
		"count":          len(list),
		"trackedQuestId": tracked,
	}), nil
}

// Quest returns one quest: its log entry if present, plus knowledge.
func (g *Service) Quest(id int64) (any, error) {
	var logEntry map[string]any
	var at time.Time
	if ch, _, ok := g.activeCharacter(); ok {
		for _, e := range entriesOf(ch.data["quests"]) {
			qm, ok := e.value.(map[string]any)
			if ok && questIDOf(qm, e.key) == id {
				logEntry = copyMap(qm)
				at = ch.updatedAt
				break
			}
		}
	}
	var k *store.KQuest
	if g.store != nil {
		var err error
		k, err = g.store.GetQuest(id)
		if err != nil {
			return nil, fmt.Errorf("read knowledge for quest %d: %w", id, err)
		}
	}
	if logEntry == nil && k == nil {
		return unavailable(fmt.Sprintf("No tengo datos de la misión %d. %s", id, HintSync)), nil
	}

	fields := logEntry
	inLog := logEntry != nil
	if fields == nil {
		fields = map[string]any{"questID": id}
	}
	fields["inLog"] = inLog
	if k != nil {
		mergeMissing(fields, knowledgeFields(*k))
		if at.IsZero() {
			at = unixTime(float64(k.LastSeen))
		}
	}
	return available(sourceSV, at, fields), nil
}

// Target returns the current target from live data, with knowledge about its NPC.
func (g *Service) Target() (any, error) {
	live, ok := g.Live()
	if !ok {
		return unavailable(HintLive), nil
	}
	fields := map[string]any{"hasTarget": live.HasTarget}
	if live.HasTarget {
		fields["hostile"] = live.TargetHostile
		switch {
		case live.TargetLevel == 255:
			fields["level"] = -1 // UnitLevel -1 (boss/skull)
		case live.TargetLevel > 0:
			fields["level"] = int(live.TargetLevel)
		}
		if live.TargetNpcID != 0 {
			fields["npcId"] = int64(live.TargetNpcID)
			if g.store != nil {
				npc, err := g.store.GetNpc(int64(live.TargetNpcID))
				if err != nil {
					return nil, fmt.Errorf("read knowledge for npc %d: %w", live.TargetNpcID, err)
				}
				if npc != nil {
					fields["npc"] = npcInfo(*npc)
				}
			}
		}
	}
	return available(sourceLive, live.ReadAt, fields), nil
}

// Inventory returns bags. freeSlots comes from live data when available.
func (g *Service) Inventory() (any, error) {
	v, at, ok := g.section("inventory")
	if !ok {
		return unavailable(HintSync), nil
	}
	fields := copyMap(asMap(v))
	if fields["items"] == nil {
		fields["items"] = []any{}
	}
	if live, ok := g.Live(); ok {
		fields["freeSlots"] = float64(live.FreeBagSlots)
	}
	return available(sourceSV, at, fields), nil
}

// Equipment returns equipped items as a list ordered by slot.
func (g *Service) Equipment() (any, error) {
	v, at, ok := g.section("equipment")
	if !ok {
		return unavailable(HintSync), nil
	}
	items := make([]map[string]any, 0)
	for _, e := range entriesOf(v) {
		m, ok := e.value.(map[string]any)
		if !ok {
			continue
		}
		item := copyMap(m)
		if _, has := item["slotID"]; !has {
			if n, err := strconv.Atoi(e.key); err == nil {
				item["slotID"] = n
			}
		}
		items = append(items, item)
	}
	return available(sourceSV, at, map[string]any{"items": items, "count": len(items)}), nil
}

// Group returns party or raid membership.
func (g *Service) Group() (any, error) {
	v, at, ok := g.section("group")
	if !ok {
		return unavailable(HintSync), nil
	}
	fields := copyMap(asMap(v))
	if fields["members"] == nil {
		fields["members"] = []any{}
	}
	return available(sourceSV, at, fields), nil
}

// Spells returns the spellbook.
func (g *Service) Spells() (any, error) {
	v, at, ok := g.section("spells")
	if !ok {
		return unavailable(HintSync), nil
	}
	return available(sourceSV, at, map[string]any{"spells": listOf(v)}), nil
}

// Talents returns the best-effort talent data stored by the addon.
func (g *Service) Talents() (any, error) {
	v, at, ok := g.section("talents")
	if !ok {
		return unavailable(HintSync), nil
	}
	return available(sourceSV, at, map[string]any{"talents": v}), nil
}

// Professions returns learned professions.
func (g *Service) Professions() (any, error) {
	v, at, ok := g.section("professions")
	if !ok {
		return unavailable(HintSync), nil
	}
	return available(sourceSV, at, map[string]any{"professions": listOf(v)}), nil
}

// Reputations returns faction standings.
func (g *Service) Reputations() (any, error) {
	v, at, ok := g.section("reputations")
	if !ok {
		return unavailable(HintSync), nil
	}
	return available(sourceSV, at, map[string]any{"reputations": listOf(v)}), nil
}

// Money returns the character's money in copper, gold, silver and copper remainder.
// The live value is used when available.
func (g *Service) Money() (any, error) {
	var copper float64
	var at time.Time
	source := sourceSV
	have := false
	if ch, _, ok := g.activeCharacter(); ok {
		char, _ := ch.data["character"].(map[string]any)
		if m, present := char["money"]; present {
			copper = numberOf(m)
			have = true
			at = ch.updatedAt
		}
	}
	if live, ok := g.Live(); ok {
		copper = float64(live.Money)
		have = true
		source = sourceLive
		at = live.ReadAt
	}
	if !have {
		return unavailable(HintSync), nil
	}
	c := int64(copper)
	return available(source, at, map[string]any{
		"copper":          c,
		"gold":            c / 10000,
		"silver":          (c % 10000) / 100,
		"copperRemainder": c % 100,
	}), nil
}

// Status reports the bridge connection, the SavedVariables age and the active character.
func (g *Service) Status() (any, error) {
	live, liveOK := g.Live()
	ch, sv, chOK := g.activeCharacter()
	fields := map[string]any{
		"bridgeConnected":      liveOK,
		"savedVariablesLoaded": sv != nil,
		"characterKey":         "",
	}
	if chOK {
		fields["characterKey"] = ch.key
	}
	if liveOK {
		fields["lastReadAgeMs"] = time.Since(live.ReadAt).Milliseconds()
	}
	if sv != nil {
		fields["savedVariablesAgeSec"] = int64(time.Since(sv.ModTime).Seconds())
		fields["savedVariablesModTime"] = sv.ModTime.Format(time.RFC3339)
	}
	if g.watcher != nil {
		if err := g.watcher.LastError(); err != nil {
			fields["savedVariablesError"] = err.Error()
		}
	}
	// The addon's own error counter, when it has exported one (for diagnosis only).
	if h, err := g.Health(); err == nil {
		if m := asMap(h); truthy(m["available"]) {
			fields["addonSalud"] = m
		}
	}
	source := sourceSV
	at := time.Now()
	if liveOK {
		source = sourceLive
	}
	return available(source, at, fields), nil
}

// ---- internals -------------------------------------------------------------

type charEntry struct {
	key       string
	data      map[string]any
	updatedAt time.Time
}

// activeCharacter picks the character with the newest updatedAt from SavedVariables.
func (g *Service) activeCharacter() (charEntry, *bridge.SavedVars, bool) {
	sv := g.savedVars()
	if sv == nil {
		return charEntry{}, nil, false
	}
	chars, _ := sv.Raw["characters"].(map[string]any)
	var best charEntry
	var bestTS float64
	found := false
	for k, v := range chars {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		ts := numberOf(m["updatedAt"])
		if !found || ts > bestTS || (ts == bestTS && k < best.key) {
			best = charEntry{key: k, data: m, updatedAt: unixTime(ts)}
			bestTS = ts
			found = true
		}
	}
	if found {
		best = g.overlayLive(best) // bloques del canal en vivo mas nuevos que SavedVariables
	}
	return best, sv, found
}

func (g *Service) savedVars() *bridge.SavedVars {
	if g.watcher == nil {
		return nil
	}
	return g.watcher.Current()
}

// section returns a top-level section of the active character.
func (g *Service) section(name string) (any, time.Time, bool) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return nil, time.Time{}, false
	}
	v, present := ch.data[name]
	if !present || v == nil {
		return nil, time.Time{}, false
	}
	return v, ch.updatedAt, true
}

// zoneName resolves a map id through the knowledge store, then SavedVariables maps.
func (g *Service) zoneName(mapID int, sv *bridge.SavedVars) (string, error) {
	if mapID == 0 {
		return "", nil
	}
	if g.store != nil {
		name, err := g.store.MapName(mapID)
		if err != nil {
			return "", fmt.Errorf("read map name %d: %w", mapID, err)
		}
		if name != "" {
			return name, nil
		}
	}
	if sv != nil {
		maps, _ := sv.Raw["maps"].(map[string]any)
		name, _ := maps[strconv.Itoa(mapID)].(string)
		return name, nil
	}
	return "", nil
}

// enrichQuest adds knowledge fields that the quest log entry does not already have.
func (g *Service) enrichQuest(q map[string]any, id int64) error {
	if g.store == nil {
		return nil
	}
	k, err := g.store.GetQuest(id)
	if err != nil {
		return fmt.Errorf("read knowledge for quest %d: %w", id, err)
	}
	if k != nil {
		mergeMissing(q, knowledgeFields(*k))
	}
	return nil
}

func knowledgeFields(k store.KQuest) map[string]any {
	f := map[string]any{}
	if k.Title != "" {
		f["title"] = k.Title
	}
	if k.Level > 0 {
		f["level"] = k.Level
	}
	if k.ObjectivesText != "" {
		f["objectivesText"] = k.ObjectivesText
	}
	if k.DescriptionText != "" {
		f["descriptionText"] = k.DescriptionText
	}
	if k.GiverNpcID != 0 || k.GiverName != "" {
		f["giver"] = npcRef(k.GiverNpcID, k.GiverName, k.GiverMapID, k.GiverX, k.GiverY)
	}
	if k.TurnInNpcID != 0 || k.TurnInName != "" {
		f["turnIn"] = npcRef(k.TurnInNpcID, k.TurnInName, k.TurnInMapID, k.TurnInX, k.TurnInY)
	}
	if k.RewardsJSON != "" {
		var rewards any
		if err := json.Unmarshal([]byte(k.RewardsJSON), &rewards); err == nil {
			f["rewards"] = rewards
		}
	}
	return f
}

func npcRef(id int64, name string, mapID int, x, y float64) map[string]any {
	m := map[string]any{"npcId": id, "name": name, "mapId": mapID}
	if x != 0 || y != 0 {
		m["x"] = x
		m["y"] = y
	}
	return m
}

func npcInfo(n store.KNpc) map[string]any {
	return map[string]any{
		"npcId":        n.NpcID,
		"name":         n.Name,
		"mapId":        n.MapID,
		"x":            n.X,
		"y":            n.Y,
		"isQuestGiver": n.IsQuestGiver,
		"isVendor":     n.IsVendor,
		"isTrainer":    n.IsTrainer,
		"trainerType":  n.TrainerType,
		"reaction":     n.Reaction,
	}
}

func available(source string, at time.Time, fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields)+3)
	for k, v := range fields {
		out[k] = v
	}
	out["available"] = true
	out["source"] = source
	if !at.IsZero() {
		out["updatedAt"] = at.Format(time.RFC3339)
	}
	return out
}

func unavailable(hint string) map[string]any {
	return map[string]any{"available": false, "hint": hint}
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func copyIfPresent(dst, src map[string]any, key string) {
	if v, ok := src[key]; ok {
		dst[key] = v
	}
}

// mergeMissing copies keys from src that dst does not have yet.
func mergeMissing(dst, src map[string]any) {
	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

func numberOf(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func unixTime(ts float64) time.Time {
	if ts <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(ts), 0)
}

func questIDOf(m map[string]any, key string) int64 {
	if id := int64(numberOf(m["questID"])); id != 0 {
		return id
	}
	id, _ := strconv.ParseInt(key, 10, 64)
	return id
}

type entry struct {
	key   string
	value any
}

// entriesOf lists a converted table's entries in key order (numeric when possible).
// A []any yields the keys "1".."n".
func entriesOf(v any) []entry {
	switch t := v.(type) {
	case map[string]any:
		out := make([]entry, 0, len(t))
		for k, val := range t {
			out = append(out, entry{key: k, value: val})
		}
		sort.Slice(out, func(i, j int) bool { return keyLess(out[i].key, out[j].key) })
		return out
	case []any:
		out := make([]entry, 0, len(t))
		for i, val := range t {
			out = append(out, entry{key: strconv.Itoa(i + 1), value: val})
		}
		return out
	}
	return nil
}

func keyLess(a, b string) bool {
	ai, errA := strconv.ParseInt(a, 10, 64)
	bi, errB := strconv.ParseInt(b, 10, 64)
	if errA == nil && errB == nil {
		return ai < bi
	}
	return a < b
}

// listOf returns the values of a converted table in key order.
func listOf(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	entries := entriesOf(v)
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.value)
	}
	return out
}
