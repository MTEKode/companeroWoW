package gamedata

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// Readers of the gear, quest-window and dungeon blocks that the addon writes into each
// character entry (the contract is in the addon's Integrations/Gear code and in the notes of
// the project). Every block is optional. A missing block, or one that says available=false,
// is recorded as a Spanish hint in the notes of gearData; the answers that need it say so
// instead of failing. Nothing here invents an item, quest, coordinate or dungeon.

const (
	hintCompleted   = "No tengo las misiones completadas del personaje: escribe /compa sync en el juego"
	hintProficiency = "No tengo las armas y armaduras que puede usar: escribe /compa sync en el juego"
	hintGear        = "No tengo el equipo puesto: escribe /compa sync en el juego"
	hintQuestWindow = "No tengo las misiones accesibles para tu nivel: escribe /compa sync en el juego (Questie es opcional y mejora el detalle)"
	hintCandidates  = "No tengo objetos candidatos para el equipo: escribe /compa sync en el juego (hacen falta las recompensas de tus misiones)"
	hintDungeons    = "No tengo datos de mazmorras: escribe /compa sync en el juego (ForeverDungeonJournal es opcional; el diario del juego también sirve)"

	gearMaxSlot      = 19 // inventory slot ids 1..19
	gearMaxQuests    = 400
	gearMaxItems     = 300
	gearMaxDungeons  = 6
	gearSoonLevels   = 3 // "pronto" means up to this many levels above the player
	gearNotableStat  = 5 // a stat that gains at least this much makes a same-level item worth listing
	gearMaxPerGroup  = 3 // items listed per slot and window
	gearTopUpgrades  = 5 // best upgrades listed at the top of the analysis
	gearMaxQuestRows = 20
)

// gearItem is an equipped item (SlotID > 0) or a candidate (SlotID 0).
type gearItem struct {
	SlotID        int
	ItemID        int64
	Name          string
	Link          string
	ItemLevel     int
	RequiredLevel int
	Quality       int
	EquipLoc      string
	Class         string
	Subclass      string
	Stats         map[string]float64
	Weapon        *theory.Weapon // damage and speed of a weapon; nil for the other items
}

// npcPoint is a quest giver, quest taker, dungeon entrance or other place with coordinates.
type npcPoint struct {
	Kind   string
	ID     int64
	Name   string
	MapID  int
	X, Y   float64
	OK     bool
	Source string // srcJuego, srcQuestie or srcAprendido ("" when unknown)
}

// windowQuest is one quest accessible to the character (questWindow).
type windowQuest struct {
	ID            int64
	Name          string
	Level         int
	RequiredLevel int
	Zone          string
	Accessible    bool
	Missing       []int64
	Starter       npcPoint
	Finisher      npcPoint
	RewardChoice  []int64
	RewardFixed   []int64
	Source        string // where the quest comes from (srcJuego, srcQuestie or srcAprendido)
	InLog         bool   // the character has it in the log now
}

// itemSource says where a candidate comes from: a quest reward or a dungeon boss.
type itemSource struct {
	Kind       string // "quest" or "dungeon"
	QuestID    int64
	Choice     bool // the player picks it among the rewards
	DungeonKey string
	Boss       string
	Source     string // srcJuego, srcQuestie, srcFDJ or srcAprendido
}

// gearCandidate is an equippable item that the addon found as a reward or as loot.
type gearCandidate struct {
	item    gearItem
	Sources []itemSource
}

type dungeonBoss struct {
	Name string
	Loot []int64
}

// dungeonInfo is one dungeon of the level range, from ForeverDungeonJournal.
type dungeonInfo struct {
	Key         string
	Name        string
	Zone        string
	Min, Max    int
	Entrance    npcPoint
	Quests      []int64
	Bosses      []dungeonBoss
	Source      string // srcFDJ, or srcJuego for the Encounter Journal (no level range)
	LevelsKnown bool   // false: the source gives no level range (Min and Max are 0)
}

// gearData is what the gear answers read from the active character, with the flags that say
// which blocks were present.
type gearData struct {
	At        time.Time
	Level     int
	Class     string
	Spec      string
	Name      string
	Armor     []string
	Weapons   []string
	DualWield bool

	ProfKnown       bool
	GearKnown       bool
	CompletedKnown  bool
	QuestsKnown     bool
	CandidatesKnown bool
	DungeonsKnown   bool

	Equipped   map[int]gearItem
	Completed  map[int64]bool
	Quests     map[int64]windowQuest
	Candidates map[int64]gearCandidate
	Dungeons   []dungeonInfo

	// Notes are the hints for the blocks that were missing, with the addon's reason when it
	// gave one. reasons holds that reason by block key ("gear", "questWindow", ...).
	Notes   []string
	reasons map[string]string

	// Native data and the source summaries of the blocks (see native.go).
	Native           nativeExport
	QuestSources     sourceSet
	CandidateSources sourceSet
	DungeonSources   sourceSet
}

// loadGearData reads the active character's gear blocks. ok is false when there is no
// character at all. The level is the live one when the bridge is connected.
func (g *Service) loadGearData() (gearData, bool) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return gearData{}, false
	}
	gd := gearData{
		At:         ch.updatedAt,
		Equipped:   map[int]gearItem{},
		Completed:  map[int64]bool{},
		Quests:     map[int64]windowQuest{},
		Candidates: map[int64]gearCandidate{},
	}
	char := asMap(ch.data["character"])
	gd.Name = str(char["name"])
	gd.Class = str(char["class"])
	gd.Spec = str(char["specOrTalentSummary"])
	gd.Level = int(numberOf(char["level"]))
	if c, err := g.Character(); err == nil {
		if m := asMap(c); truthy(m["available"]) {
			if lv := int(numberOf(m["level"])); lv > 0 {
				gd.Level = lv
			}
		}
	}

	gd.Native = readNative(ch.data)
	gd.readCompleted(ch.data)
	gd.readProficiencies(ch.data)
	gd.readGear(ch.data)
	gd.readQuestWindow(ch.data)
	gd.readCandidates(ch.data)
	gd.readDungeons(ch.data)
	return gd, true
}

// note records a missing block: its hint for the notes, and the addon's reason when it gave one.
func (gd *gearData) note(key, hint string, block map[string]any) {
	reason := str(block["reason"])
	if gd.reasons == nil {
		gd.reasons = map[string]string{}
	}
	gd.reasons[key] = reason
	if reason != "" {
		hint += " Motivo: " + reason
	}
	gd.Notes = append(gd.Notes, hint)
}

// missingBlock names the first required block that is missing, with its hint, or "".
func (gd gearData) missingBlock(required ...string) (key, hint string) {
	for _, k := range required {
		var missing bool
		switch k {
		case "gear":
			missing, hint = !gd.GearKnown, hintGear
		case "questWindow":
			missing, hint = !gd.QuestsKnown, hintQuestWindow
		case "gearCandidates":
			missing, hint = !gd.CandidatesKnown, hintCandidates
		case "dungeons":
			missing, hint = !gd.DungeonsKnown, hintDungeons
		}
		if missing {
			return k, hint
		}
	}
	return "", ""
}

// unavailableFor is the answer for a missing block: the hint, plus the addon's reason when it
// gave one.
func (gd gearData) unavailableFor(key, hint string) map[string]any {
	u := unavailable(hint)
	if r := gd.reasons[key]; r != "" {
		u["motivo"] = r
	}
	return u
}

// readCompleted reads completedQuests, the comma-joined ids of the quests done.
func (gd *gearData) readCompleted(data map[string]any) {
	s, ok := data["completedQuests"].(string)
	if !ok {
		gd.note("completed", hintCompleted, nil)
		return
	}
	gd.CompletedKnown = true
	for _, part := range strings.Split(s, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && id > 0 {
			gd.Completed[id] = true
		}
	}
}

// readProficiencies reads the armour and weapon subclasses the class can equip.
func (gd *gearData) readProficiencies(data map[string]any) {
	pm := asMap(data["proficiencies"])
	if pm == nil || (pm["available"] != nil && !truthy(pm["available"])) {
		gd.note("proficiencies", hintProficiency, pm)
		return
	}
	gd.ProfKnown = true
	gd.Armor = strList(pm["armor"])
	gd.Weapons = strList(pm["weapons"])
	gd.DualWield = truthy(pm["dualWield"])
}

// readGear reads the equipped items, keyed by slot id 1..19.
func (gd *gearData) readGear(data map[string]any) {
	for _, e := range entriesOf(data["gear"]) {
		n, err := strconv.Atoi(e.key)
		if err != nil || n < 1 || n > gearMaxSlot {
			continue
		}
		if it, ok := parseGearItem(asMap(e.value), n); ok {
			gd.Equipped[n] = it
		}
	}
	if len(gd.Equipped) == 0 {
		gd.note("gear", hintGear, asMap(data["gear"]))
		return
	}
	gd.GearKnown = true
}

// readQuestWindow reads the quests accessible to the character (questWindow).
func (gd *gearData) readQuestWindow(data map[string]any) {
	qw := asMap(data["questWindow"])
	if !truthy(qw["available"]) {
		gd.note("questWindow", hintQuestWindow, qw)
		return
	}
	gd.QuestsKnown = true
	gd.QuestSources = readSourceSet(qw["sources"])
	for _, e := range entriesOf(qw["quests"]) {
		if len(gd.Quests) >= gearMaxQuests {
			break
		}
		m := asMap(e.value)
		if m == nil {
			continue
		}
		if id := questIDOf(m, e.key); id != 0 {
			gd.Quests[id] = parseWindowQuest(m, id)
		}
	}
}

// readCandidates reads the equippable rewards and loot (gearCandidates).
func (gd *gearData) readCandidates(data map[string]any) {
	cw := asMap(data["gearCandidates"])
	if !truthy(cw["available"]) {
		gd.note("gearCandidates", hintCandidates, cw)
		return
	}
	gd.CandidatesKnown = true
	gd.CandidateSources = readSourceSet(cw["sources"])
	for _, e := range entriesOf(cw["items"]) {
		if len(gd.Candidates) >= gearMaxItems {
			break
		}
		m := asMap(e.value)
		it, ok := parseGearItem(m, 0)
		if !ok {
			continue
		}
		if id := int64(numberOf(m["itemID"])); id != 0 {
			it.ItemID = id
		}
		c := gearCandidate{item: it}
		for _, s := range listOf(m["sources"]) {
			sm := asMap(s)
			if sm == nil {
				continue
			}
			c.Sources = append(c.Sources, itemSource{
				Kind:       str(sm["kind"]),
				QuestID:    int64(numberOf(sm["questID"])),
				Choice:     truthy(sm["choice"]),
				DungeonKey: str(sm["dungeonKey"]),
				Boss:       str(sm["boss"]),
				Source:     str(sm["source"]),
			})
		}
		gd.Candidates[it.ItemID] = c
	}
}

// readDungeons reads the dungeons of the level range (dungeons).
func (gd *gearData) readDungeons(data map[string]any) {
	dj := asMap(data["dungeons"])
	if !truthy(dj["available"]) {
		gd.note("dungeons", hintDungeons, dj)
		return
	}
	gd.DungeonsKnown = true
	gd.DungeonSources = readSourceSet(dj["sources"])
	for _, item := range listOf(dj["list"]) {
		if len(gd.Dungeons) >= gearMaxDungeons {
			break
		}
		m := asMap(item)
		if m == nil {
			continue
		}
		d := dungeonInfo{
			Key:         str(m["key"]),
			Name:        str(m["name"]),
			Zone:        str(m["zone"]),
			Min:         int(numberOf(m["minLevel"])),
			Max:         int(numberOf(m["maxLevel"])),
			Quests:      int64List(m["quests"]),
			Source:      str(m["source"]),
			LevelsKnown: m["levelsKnown"] != false,
		}
		if d.Key == "" {
			d.Key = d.Name
		}
		d.Entrance = readNPCPoint(m)
		d.Entrance.Name = d.Name
		d.Entrance.Kind = "dungeon"
		for _, b := range listOf(m["bosses"]) {
			bm := asMap(b)
			if bm == nil {
				continue
			}
			d.Bosses = append(d.Bosses, dungeonBoss{Name: str(bm["name"]), Loot: int64List(bm["loot"])})
		}
		gd.Dungeons = append(gd.Dungeons, d)
	}
}

// parseGearItem reads one item table. ok is false when it has neither a name nor an id.
func parseGearItem(m map[string]any, slotID int) (gearItem, bool) {
	if m == nil {
		return gearItem{}, false
	}
	it := gearItem{
		SlotID:        slotID,
		ItemID:        int64(numberOf(m["itemID"])),
		Name:          str(m["name"]),
		Link:          str(m["link"]),
		ItemLevel:     int(numberOf(m["itemLevel"])),
		RequiredLevel: int(numberOf(m["requiredLevel"])),
		Quality:       int(numberOf(m["quality"])),
		EquipLoc:      str(m["equipLoc"]),
		Class:         str(m["class"]),
		Subclass:      str(m["subclass"]),
		Stats:         readStats(m["stats"]),
	}
	if w := asMap(m["weapon"]); w != nil {
		mn, okMin := numOK(w["min"])
		mx, okMax := numOK(w["max"])
		sp, okSp := numOK(w["speed"])
		if okMin && okMax && okSp && mx > 0 && sp > 0 {
			it.Weapon = &theory.Weapon{Min: mn, Max: mx, Speed: sp}
		}
	}
	if it.Name == "" && it.ItemID == 0 {
		return gearItem{}, false
	}
	if it.Name == "" {
		it.Name = "objeto " + strconv.FormatInt(it.ItemID, 10)
	}
	return it, true
}

// parseWindowQuest reads one questWindow entry.
func parseWindowQuest(m map[string]any, id int64) windowQuest {
	rewards := asMap(m["rewards"])
	return windowQuest{
		ID:            id,
		Name:          str(m["name"]),
		Level:         int(numberOf(m["level"])),
		RequiredLevel: int(numberOf(m["requiredLevel"])),
		Zone:          str(m["zone"]),
		Accessible:    truthy(m["accessibleNow"]),
		Source:        str(m["source"]),
		InLog:         truthy(m["inLog"]),
		Missing:       int64List(m["missingPrereqs"]),
		Starter:       readNPCPoint(asMap(m["starter"])),
		Finisher:      readNPCPoint(asMap(m["finisher"])),
		RewardChoice:  int64List(rewards["choice"]),
		RewardFixed:   int64List(rewards["fixed"]),
	}
}

// readNPCPoint reads kind, id, name and mapID/x/y. The point is OK only with a valid place.
func readNPCPoint(m map[string]any) npcPoint {
	if m == nil {
		return npcPoint{}
	}
	mapID, x, y, ok := readPoint(m)
	return npcPoint{
		Kind:   str(m["kind"]),
		ID:     int64(numberOf(m["id"])),
		Name:   str(m["name"]),
		MapID:  mapID,
		X:      x,
		Y:      y,
		OK:     ok,
		Source: str(m["source"]),
	}
}

// readStats reads GetItemStats keys (ITEM_MOD_*, RESISTANCE*_NAME) with their values.
func readStats(v any) map[string]float64 {
	out := map[string]float64{}
	m := asMap(v)
	for k, val := range m {
		if n, ok := numOK(val); ok && n != 0 {
			out[k] = n
		}
	}
	return out
}

// int64List reads a Lua list of ids, dropping anything that is not a positive number.
func int64List(v any) []int64 {
	var out []int64
	for _, item := range listOf(v) {
		if id := int64(numberOf(item)); id > 0 {
			out = append(out, id)
		}
	}
	return out
}

func strList(v any) []string {
	var out []string
	for _, item := range listOf(v) {
		if s := str(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// dungeon returns the dungeon with the given key.
func (gd gearData) dungeon(key string) (dungeonInfo, bool) {
	for _, d := range gd.Dungeons {
		if d.Key == key {
			return d, true
		}
	}
	return dungeonInfo{}, false
}

// sortedCandidateIDs lists the candidate ids in order, so that answers are stable.
func (gd gearData) sortedCandidateIDs() []int64 {
	ids := make([]int64, 0, len(gd.Candidates))
	for id := range gd.Candidates {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
