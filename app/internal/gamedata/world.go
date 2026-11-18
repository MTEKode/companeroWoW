package gamedata

import (
	"strconv"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// World data (the WORLD DATA CONTRACT shared with the addon): the flight network with measured
// flight times, the vendors and trainers the account has seen, quest chains and consumables.
// It is account-wide: SavedVariables holds CompaneroWoWDB.world, and the live channel
// (msgType 16) streams the changed parts, which are laid over the saved block as a deep merge.
// Everything here is read-only and tolerant: a missing table is an empty table.

const (
	worldMaxVendors  = 400
	worldMaxItems    = 80
	worldMaxServices = 120
	worldMaxConsume  = 2000
	worldMountYPS    = 11.0 // mounted speed in yards per second (estimate; walking is questSpeedYPS)
)

// worldNode is a flight master (taxi node).
type worldNode struct {
	ID      int64
	Name    string
	MapID   int
	X, Y    float64
	HasPos  bool
	Faction string
	Flag    bool // the node's own known flag (used when the character has no learned list)
}

// worldEdge is one measured flight from one node to another.
type worldEdge struct {
	From, To int64
	Copper   int64
	Hops     int
	Seconds  float64 // measured: taxi start to landing
}

// worldItem is one item a vendor sells.
type worldItem struct {
	ItemID      int64
	Name        string
	Price       int64 // copper
	Stack       int
	Limited     bool
	RequiresRep string
}

// worldVendor is an NPC that sells items.
type worldVendor struct {
	NpcID   int64
	Name    string
	MapID   int
	X, Y    float64
	HasPos  bool
	Faction string
	Items   []worldItem
}

// worldService is one service of a trainer.
type worldService struct {
	Name      string
	Rank      string
	Cost      int64
	Level     int
	SkillName string
	SkillRank int
	Available string // available, unavailable or used
}

// worldTrainer is an NPC that teaches services (class, profession, riding, weapon or other).
type worldTrainer struct {
	NpcID      int64
	Name       string
	MapID      int
	X, Y       float64
	HasPos     bool
	Kind       string
	Profession string
	Services   []worldService
}

// worldChain is the place of a quest in its chain.
type worldChain struct {
	QuestID int64
	Pre     []int64
	Next    int64
	Name    string // the chain's name
	Quest   string // the quest's own name, when the addon has it
}

// worldConsumable is a food, drink, potion, bandage or elixir with what it restores.
type worldConsumable struct {
	ItemID        int64
	Name          string
	Kind          string
	Health        int64
	Mana          int64
	Seconds       int64
	RequiredLevel int
	SkillName     string
	SkillRank     int
}

// worldData is the decoded world block, with the flight graph of the active character.
type worldData struct {
	Known       bool // the world block exists at all
	At          time.Time
	CharKey     string
	Faction     string // the character's faction when the race is known
	Nodes       map[int64]worldNode
	Edges       map[[2]int64]worldEdge
	KnownNodes  map[int64]bool // the flight nodes the active character has learned
	Vendors     map[int64]worldVendor
	Trainers    map[int64]worldTrainer
	Chains      map[int64]worldChain
	Consumables map[int64]worldConsumable
}

// worldRaw returns the world block: the saved one laid under the newest live message, deep merged.
// ok is false when there is no world block at all.
func (g *Service) worldRaw() (map[string]any, time.Time, bool) {
	var base map[string]any
	var at time.Time
	if sv := g.savedVars(); sv != nil {
		base = asMap(sv.Raw["world"])
		at = sv.ModTime
	}
	if live := g.LiveChannel(); live != nil && live.Data != nil {
		if it, ok := live.Data.Get(bridge.MsgWorld); ok {
			if m := asMap(it.Value); m != nil {
				base = mergeWorld(base, m)
				if it.At.After(at) {
					at = it.At
				}
			}
		}
	}
	return base, at, base != nil
}

// mergeWorld returns base with over laid on top: nested tables are merged key by key, and every
// other value (lists, numbers, text) is replaced. Neither input is modified.
func mergeWorld(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		ov, isMap := v.(map[string]any)
		bv, baseMap := out[k].(map[string]any)
		if isMap && baseMap {
			out[k] = mergeWorld(bv, ov)
			continue
		}
		out[k] = v
	}
	return out
}

// loadWorld decodes the world block for the active character. ok is false when there is no
// world block; the flight graph is then empty and the tools say so.
func (g *Service) loadWorld() (worldData, bool) {
	raw, at, ok := g.worldRaw()
	w := worldData{
		At:          at,
		Nodes:       map[int64]worldNode{},
		Edges:       map[[2]int64]worldEdge{},
		KnownNodes:  map[int64]bool{},
		Vendors:     map[int64]worldVendor{},
		Trainers:    map[int64]worldTrainer{},
		Chains:      map[int64]worldChain{},
		Consumables: map[int64]worldConsumable{},
	}
	if !ok {
		return w, false
	}
	w.Known = true
	race := ""
	if ch, _, has := g.activeCharacter(); has {
		w.CharKey = ch.key
		race = str(asMap(ch.data["character"])["race"])
	}
	w.Faction = factionOfRace(race)
	readFlights(&w, asMap(raw["flight"]), w.CharKey)
	readVendors(&w, asMap(raw["vendors"]))
	readTrainers(&w, asMap(raw["trainers"]))
	readChains(&w, asMap(raw["questChains"]))
	readConsumables(&w, asMap(raw["consumables"]))
	return w, true
}

// factionOfRace maps a race name (English or Spanish) to its faction, "" when unknown.
func factionOfRace(race string) string {
	switch fold(race) {
	case "human", "humano", "dwarf", "enano", "night elf", "elfo de la noche", "gnome", "gnomo",
		"draenei", "worgen", "huargen":
		return "Alliance"
	case "orc", "orco", "undead", "no-muerto", "tauren", "troll", "blood elf", "elfo de sangre", "goblin":
		return "Horde"
	}
	return "" // unknown: every faction's nodes are used
}

// readFlights decodes the flight block: nodes and measured costs of the factions the character
// can use, and the nodes the character has learned.
func readFlights(w *worldData, flight map[string]any, charKey string) {
	if flight == nil {
		return
	}
	knownTable := asMap(flight["known"])
	for _, e := range entriesOf(flight) {
		if e.key == "known" {
			continue
		}
		block := asMap(e.value)
		if block == nil {
			continue
		}
		if w.Faction != "" && e.key != w.Faction {
			continue
		}
		for _, ne := range entriesOf(block["nodes"]) {
			m := asMap(ne.value)
			if m == nil {
				continue
			}
			id := idOf(m, "nodeID", ne.key)
			mapID := int(numberOf(m["mapID"]))
			if id <= 0 || mapID <= 0 {
				continue
			}
			n := worldNode{ID: id, Name: str(m["name"]), MapID: mapID, Faction: e.key, Flag: truthy(m["known"])}
			n.X, n.Y, n.HasPos = worldPoint(m)
			if _, dup := w.Nodes[id]; dup {
				continue
			}
			w.Nodes[id] = n
		}
		for _, ce := range entriesOf(block["costs"]) {
			from, to, ok := parseEdgeKey(ce.key)
			m := asMap(ce.value)
			if !ok || m == nil {
				continue
			}
			sec := numberOf(m["seconds"])
			if sec <= 0 {
				continue
			}
			w.Edges[[2]int64{from, to}] = worldEdge{
				From: from, To: to, Copper: int64(numberOf(m["copper"])),
				Hops: int(numberOf(m["hops"])), Seconds: sec,
			}
		}
	}
	// The learned nodes: the character's own list when the addon kept one, else the nodes flagged known.
	if list, ok := knownList(knownTable, charKey); ok {
		for _, id := range list {
			w.KnownNodes[id] = true
		}
	} else {
		for id, n := range w.Nodes {
			if n.Flag {
				w.KnownNodes[id] = true
			}
		}
	}
}

// knownList reads the character's list of learned nodes from the known table.
func knownList(known map[string]any, charKey string) ([]int64, bool) {
	if known == nil || charKey == "" {
		return nil, false
	}
	v, ok := known[charKey]
	if !ok {
		return nil, false
	}
	var out []int64
	for _, item := range listOf(v) {
		if id := int64(numberOf(item)); id > 0 {
			out = append(out, id)
		}
	}
	return out, true
}

// parseEdgeKey reads "fromID>toID".
func parseEdgeKey(key string) (int64, int64, bool) {
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

// readVendors decodes the vendors table, capped at worldMaxVendors (by npc id).
func readVendors(w *worldData, vendors map[string]any) {
	count := 0
	for _, e := range entriesOf(vendors) {
		if count >= worldMaxVendors {
			break
		}
		m := asMap(e.value)
		if m == nil {
			continue
		}
		id := idOf(m, "npcID", e.key)
		mapID := int(numberOf(m["mapID"]))
		if id <= 0 {
			continue
		}
		v := worldVendor{NpcID: id, Name: str(m["name"]), MapID: mapID, Faction: str(m["faction"])}
		v.X, v.Y, v.HasPos = worldPoint(m)
		for _, it := range listOf(m["items"]) {
			if len(v.Items) >= worldMaxItems {
				break
			}
			im := asMap(it)
			if im == nil {
				continue
			}
			item := worldItem{
				ItemID: int64(numberOf(im["itemID"])), Name: str(im["name"]),
				Price: int64(numberOf(im["price"])), Stack: int(numberOf(im["stack"])),
				Limited: truthy(im["limited"]), RequiresRep: str(im["requiresRep"]),
			}
			if item.ItemID > 0 {
				v.Items = append(v.Items, item)
			}
		}
		count++
		w.Vendors[id] = v
	}
}

// readTrainers decodes the trainers table: services with cost, level and skill requirements.
func readTrainers(w *worldData, trainers map[string]any) {
	for _, e := range entriesOf(trainers) {
		m := asMap(e.value)
		if m == nil {
			continue
		}
		id := idOf(m, "npcID", e.key)
		if id <= 0 {
			continue
		}
		t := worldTrainer{
			NpcID: id, Name: str(m["name"]), MapID: int(numberOf(m["mapID"])),
			Kind: str(m["kind"]), Profession: str(m["profession"]),
		}
		t.X, t.Y, t.HasPos = worldPoint(m)
		for _, sv := range listOf(m["services"]) {
			if len(t.Services) >= worldMaxServices {
				break
			}
			sm := asMap(sv)
			if sm == nil {
				continue
			}
			s := worldService{
				Name: str(sm["name"]), Rank: str(sm["rank"]), Cost: int64(numberOf(sm["cost"])),
				Level: int(numberOf(sm["level"])), Available: str(sm["available"]),
			}
			if sk := asMap(sm["skill"]); sk != nil {
				s.SkillName = str(sk["name"])
				s.SkillRank = int(numberOf(sk["rank"]))
			}
			t.Services = append(t.Services, s)
		}
		w.Trainers[id] = t
	}
}

// readChains decodes the quest chains: the previous quests (pre) and the next one.
func readChains(w *worldData, chains map[string]any) {
	for _, e := range entriesOf(chains) {
		m := asMap(e.value)
		if m == nil {
			continue
		}
		id, err := strconv.ParseInt(e.key, 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		c := worldChain{QuestID: id, Next: int64(numberOf(m["next"])), Name: str(m["chainName"]), Quest: str(m["name"])}
		for _, p := range listOf(m["pre"]) {
			if pid := int64(numberOf(p)); pid > 0 {
				c.Pre = append(c.Pre, pid)
			}
		}
		w.Chains[id] = c
	}
}

// readConsumables decodes the consumables seen in bags, vendors or loot.
func readConsumables(w *worldData, items map[string]any) {
	count := 0
	for _, e := range entriesOf(items) {
		if count >= worldMaxConsume {
			break
		}
		m := asMap(e.value)
		if m == nil {
			continue
		}
		id := idOf(m, "itemID", e.key)
		if id <= 0 {
			continue
		}
		c := worldConsumable{
			ItemID: id, Name: str(m["name"]), Kind: str(m["kind"]),
			RequiredLevel: int(numberOf(m["requiredLevel"])),
		}
		if r := asMap(m["restores"]); r != nil {
			c.Health = int64(numberOf(r["health"]))
			c.Mana = int64(numberOf(r["mana"]))
			c.Seconds = int64(numberOf(r["seconds"]))
		}
		if sk := listOf(m["skill"]); len(sk) == 2 {
			c.SkillName = str(sk[0])
			c.SkillRank = int(numberOf(sk[1]))
		}
		w.Consumables[id] = c
		count++
	}
}

// idOf reads an id field, falling back to the table key.
func idOf(m map[string]any, field, key string) int64 {
	if id := int64(numberOf(m[field])); id > 0 {
		return id
	}
	id, _ := strconv.ParseInt(key, 10, 64)
	return id
}

// worldPoint reads x and y of an entry. The addon writes 0..1; a value above 1 is taken as 0..100.
func worldPoint(m map[string]any) (x, y float64, ok bool) {
	x, okX := numOK(m["x"])
	y, okY := numOK(m["y"])
	if !okX || !okY || x < 0 || y < 0 {
		return 0, 0, false
	}
	if x > 1 || y > 1 {
		x, y = x/100, y/100
	}
	if x > 1 || y > 1 {
		return 0, 0, false
	}
	return x, y, true
}
