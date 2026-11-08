package gamedata

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// The Questie export is written by the addon into each character entry under "questie"
// (see the contract in the project notes). Coordinates are Blizzard uiMapID plus 0..1
// floats; this file reads them and never invents any. Every piece is optional.

const (
	maxSpawnsShown  = 3  // spawns listed per source in the answers
	maxZoneQuests   = 40 // hard cap when reading zoneQuests
	maxQuestieNPCs  = 40 // hard cap when reading the zone NPCs
	markLabelRunes  = 60 // longest label kept in a /compa marca command
	nearMaxRankLeft = 15 // a profession within this many points of its cap is "train now"
)

// spawn is one place where a quest entity can be found.
type spawn struct {
	MapID int
	X, Y  float64
}

// questieSource is a quest starter or finisher (npc, object or item).
type questieSource struct {
	Kind   string
	ID     int64
	Name   string
	Spawns []spawn
}

// questieObjective is one objective of a quest with its spawns.
type questieObjective struct {
	Index    int
	Kind     string
	ID       int64
	Name     string
	Required int
	Spawns   []spawn
}

// questieQuest is the Questie data of one quest in the player's log.
type questieQuest struct {
	ID         int64
	Name       string
	Level      int
	Starters   []questieSource
	Finishers  []questieSource
	Objectives []questieObjective
	PreQuests  []int64
	NextQuest  int64
}

// questieNPC is a trainer, vendor or other service NPC of the zone.
type questieNPC struct {
	ID       int64
	Name     string
	Role     string
	Subtitle string
	MapID    int
	X, Y     float64
}

// questieZoneQuest is a quest the player could pick up in the current zone.
type questieZoneQuest struct {
	ID            int64
	Name          string
	Level         int
	RequiredLevel int
	Starter       questieSource
	HasStarter    bool
}

// questieExport is the decoded "questie" block of the active character.
type questieExport struct {
	Available  bool
	Version    string
	ExportedAt time.Time
	quests     map[int64]map[string]any
	ZoneMapID  int
	ZoneQuests []questieZoneQuest
	NPCs       []questieNPC
}

// readQuestie decodes the questie block of a character entry. Missing or unavailable
// exports yield a zero value with Available false.
func readQuestie(data map[string]any) questieExport {
	raw := asMap(data["questie"])
	out := questieExport{quests: map[int64]map[string]any{}}
	if raw == nil || !truthy(raw["available"]) {
		return out
	}
	out.Available = true
	out.Version, _ = raw["version"].(string)
	if ts := numberOf(raw["exportedAt"]); ts > 0 {
		out.ExportedAt = unixTime(ts)
	}
	for _, e := range entriesOf(raw["quests"]) {
		qm := asMap(e.value)
		if qm == nil {
			continue
		}
		if id := questIDOf(qm, e.key); id != 0 {
			out.quests[id] = qm
		}
	}
	if zq := asMap(raw["zoneQuests"]); zq != nil {
		out.ZoneMapID = int(numberOf(zq["mapID"]))
		for _, item := range listOf(zq["list"]) {
			if len(out.ZoneQuests) >= maxZoneQuests {
				break
			}
			m := asMap(item)
			if m == nil {
				continue
			}
			zqEntry := questieZoneQuest{
				ID:            int64(numberOf(m["questID"])),
				Name:          str(m["name"]),
				Level:         int(numberOf(m["level"])),
				RequiredLevel: int(numberOf(m["requiredLevel"])),
			}
			if st := asMap(m["starter"]); st != nil {
				zqEntry.Starter, zqEntry.HasStarter = readSource(st), true
			}
			out.ZoneQuests = append(out.ZoneQuests, zqEntry)
		}
	}
	for _, item := range listOf(raw["npcs"]) {
		if len(out.NPCs) >= maxQuestieNPCs {
			break
		}
		m := asMap(item)
		if m == nil {
			continue
		}
		mapID, x, y, ok := readPoint(m)
		if !ok {
			continue
		}
		out.NPCs = append(out.NPCs, questieNPC{
			ID:       int64(numberOf(m["id"])),
			Name:     str(m["name"]),
			Role:     str(m["role"]),
			Subtitle: str(m["subtitle"]),
			MapID:    mapID,
			X:        x,
			Y:        y,
		})
	}
	return out
}

// quest returns the export of one quest, if the export has it.
func (q questieExport) quest(id int64) (questieQuest, bool) {
	m, ok := q.quests[id]
	if !ok {
		return questieQuest{}, false
	}
	out := questieQuest{
		ID:        id,
		Name:      str(m["name"]),
		Level:     int(numberOf(m["level"])),
		NextQuest: int64(numberOf(m["nextQuest"])),
	}
	out.Starters = readSources(m["starters"])
	out.Finishers = readSources(m["finishers"])
	for _, item := range listOf(m["objectives"]) {
		om := asMap(item)
		if om == nil {
			continue
		}
		out.Objectives = append(out.Objectives, questieObjective{
			Index:    int(numberOf(om["index"])),
			Kind:     str(om["kind"]),
			ID:       int64(numberOf(om["id"])),
			Name:     str(om["name"]),
			Required: int(numberOf(om["required"])),
			Spawns:   readSpawns(om["spawns"], 10),
		})
	}
	sort.SliceStable(out.Objectives, func(i, j int) bool { return out.Objectives[i].Index < out.Objectives[j].Index })
	for _, p := range listOf(m["preQuests"]) {
		out.PreQuests = append(out.PreQuests, int64(numberOf(p)))
	}
	return out, true
}

func readSources(v any) []questieSource {
	var out []questieSource
	for _, item := range listOf(v) {
		if m := asMap(item); m != nil {
			out = append(out, readSource(m))
		}
	}
	return out
}

func readSource(m map[string]any) questieSource {
	return questieSource{
		Kind:   str(m["kind"]),
		ID:     int64(numberOf(m["id"])),
		Name:   str(m["name"]),
		Spawns: readSpawns(m["spawns"], maxSpawnsShown*3),
	}
}

// readSpawns decodes up to limit spawns, dropping any without a map and coordinates.
func readSpawns(v any, limit int) []spawn {
	var out []spawn
	for _, item := range listOf(v) {
		if len(out) >= limit {
			break
		}
		m := asMap(item)
		if m == nil {
			continue
		}
		mapID, x, y, ok := readPoint(m)
		if ok {
			out = append(out, spawn{MapID: mapID, X: x, Y: y})
		}
	}
	return out
}

// readPoint reads mapID, x and y, requiring a map and coordinates inside 0..1.
func readPoint(m map[string]any) (int, float64, float64, bool) {
	mapID := int(numberOf(m["mapID"]))
	if mapID <= 0 {
		mapID = int(numberOf(m["mapId"]))
	}
	x, okX := numOK(m["x"])
	y, okY := numOK(m["y"])
	if mapID <= 0 || !okX || !okY || x < 0 || x > 1 || y < 0 || y > 1 {
		return 0, 0, 0, false
	}
	return mapID, x, y, true
}

// position is where the player is: a map and 0..1 coordinates.
type position struct {
	MapID int
	X, Y  float64
	OK    bool
	Live  bool
}

// playerPosition prefers the live pixel bridge and falls back to SavedVariables.
func (g *Service) playerPosition() position {
	if live, ok := g.Live(); ok && live.MapID != 0 {
		return position{MapID: int(live.MapID), X: live.X, Y: live.Y, OK: true, Live: true}
	}
	ch, _, ok := g.activeCharacter()
	if !ok {
		return position{}
	}
	loc := asMap(ch.data["location"])
	mapID := int(numberOf(loc["mapID"]))
	x, okX := numOK(loc["x"])
	y, okY := numOK(loc["y"])
	if mapID == 0 || !okX || !okY {
		return position{}
	}
	return position{MapID: mapID, X: x, Y: y, OK: true}
}

// nearest picks the spawn closest to the player. Spawns on the player's map win; without
// a position, or when none is on the map, the first listed spawn is used and sameMap is
// false. distance is in map units (0..100 across the map) and only set when sameMap.
type nearestSpawn struct {
	Spawn    spawn
	Distance float64
	SameMap  bool
}

func nearest(spawns []spawn, pos position) (nearestSpawn, bool) {
	if len(spawns) == 0 {
		return nearestSpawn{}, false
	}
	if pos.OK {
		best, bestD, found := spawn{}, math.MaxFloat64, false
		for _, s := range spawns {
			if s.MapID != pos.MapID {
				continue
			}
			d := math.Hypot(s.X-pos.X, s.Y-pos.Y) * 100
			if d < bestD {
				best, bestD, found = s, d, true
			}
		}
		if found {
			return nearestSpawn{Spawn: best, Distance: round1(bestD), SameMap: true}, true
		}
	}
	return nearestSpawn{Spawn: spawns[0]}, true
}

// marcaCommand is the exact chat command that marks a spawn with the explicit map id,
// so it works from any map. Coordinates use a dot and one decimal, as the addon parses them.
func marcaCommand(s spawn, label string) string {
	cmd := fmt.Sprintf("/compa marca %d %.1f %.1f", s.MapID, s.X*100, s.Y*100)
	if l := cleanLabel(label); l != "" {
		cmd += " " + l
	}
	return cmd
}

// cleanLabel makes a label safe for one chat line.
func cleanLabel(s string) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "|", "")), " ")
	r := []rune(s)
	if len(r) > markLabelRunes {
		s = strings.TrimSpace(string(r[:markLabelRunes]))
	}
	return s
}

func spawnView(n nearestSpawn, label string) map[string]any {
	m := map[string]any{
		"mapId":   n.Spawn.MapID,
		"x":       round1(n.Spawn.X * 100),
		"y":       round1(n.Spawn.Y * 100),
		"sameMap": n.SameMap,
		"marca":   marcaCommand(n.Spawn, label),
	}
	if n.SameMap {
		m["distancia"] = n.Distance
	}
	return m
}

// sourcesView lists starters or finishers with the nearest spawn of each.
func sourcesView(list []questieSource, pos position) []any {
	out := make([]any, 0, len(list))
	for _, s := range list {
		m := map[string]any{"kind": s.Kind, "id": s.ID, "name": s.Name}
		if n, ok := nearest(s.Spawns, pos); ok {
			m["nearest"] = spawnView(n, s.Name)
		}
		out = append(out, m)
	}
	return out
}

// objectivesView lists the Questie objectives with their nearest spawn.
func objectivesView(list []questieObjective, pos position) []any {
	out := make([]any, 0, len(list))
	for _, o := range list {
		m := map[string]any{"index": o.Index, "kind": o.Kind, "name": o.Name, "required": o.Required}
		if n, ok := nearest(o.Spawns, pos); ok {
			m["nearest"] = spawnView(n, o.Name)
		}
		out = append(out, m)
	}
	return out
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

// coordsText is the "(x, y)" form used in answers, 0..100 with one decimal.
func coordsText(x, y float64) string {
	return fmt.Sprintf("(%.1f, %.1f)", x*100, y*100)
}

// ageSeconds is how old t is in whole seconds, or -1 when t is unknown.
func ageSeconds(t time.Time) int64 {
	if t.IsZero() {
		return -1
	}
	return int64(time.Since(t).Seconds())
}

// truthy reports whether v is the boolean true.
func truthy(v any) bool {
	b, _ := v.(bool)
	return b
}

// str returns v as a string, or "" when it is not one.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// numOK returns a Lua number as float64 and whether v was a number at all.
func numOK(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	}
	return 0, false
}
