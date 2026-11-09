package gamedata

// Native data and source labels. The addon writes two kinds of data for each character:
//   - "native": what the game itself offers (quest lines on the map, next waypoints) plus the
//     NPCs the addon learned by talking to them (trainers, vendors). Source "juego" / "aprendido".
//   - the rest of the blocks carry a "source" per item and a "sources" summary, so an answer can
//     say where a piece of data came from. Questie and ForeverDungeonJournal are optional: when
//     they are missing the answers still work from "juego" and "aprendido".
// Nothing here invents a place, an item or a quest. Missing data stays missing.

const (
	srcJuego     = "juego"     // Blizzard APIs (quest lines, waypoints, rewards of the log, Encounter Journal)
	srcQuestie   = "questie"   // optional addon: Questie
	srcFDJ       = "fdj"       // optional addon: ForeverDungeonJournal
	srcAprendido = "aprendido" // learned by the addon while the player plays
	maxNativeNPC = 60          // learned NPCs read from the native block
)

// sourceSet is the "sources" summary of a block: which sources contributed to it.
type sourceSet struct {
	Juego, Questie, FDJ, Aprendido bool
}

// readSourceSet reads {juego, questie, fdj, aprendido}. A missing summary is all false.
func readSourceSet(v any) sourceSet {
	m := asMap(v)
	return sourceSet{
		Juego:     truthy(m["juego"]),
		Questie:   truthy(m["questie"]),
		FDJ:       truthy(m["fdj"]),
		Aprendido: truthy(m["aprendido"]),
	}
}

// view is the summary as an answer shows it.
func (s sourceSet) view() map[string]any {
	return map[string]any{"juego": s.Juego, "questie": s.Questie, "fdj": s.FDJ, "aprendido": s.Aprendido}
}

// sourceLabel names a source for the answers. Use it only where the source matters: when the data
// is partial or when the player asks where it comes from. "" for an unknown source.
func sourceLabel(src string) string {
	switch src {
	case srcJuego:
		return "según el juego"
	case srcQuestie:
		return "según Questie"
	case srcFDJ:
		return "según ForeverDungeonJournal"
	case srcAprendido:
		return "aprendido jugando"
	}
	return ""
}

// nativeOffer is a quest the game offers on the player's map (or its parents).
type nativeOffer struct {
	ID    int64
	Name  string
	MapID int
	X, Y  float64
	OK    bool
}

// nativeActive is the next waypoint the game gives for a quest in the log.
type nativeActive struct {
	ID    int64
	Name  string
	Spawn spawn
}

// nativeNPC is a trainer or vendor the addon has seen.
type nativeNPC struct {
	ID             int64
	Name           string
	Role           string
	MapID          int
	X, Y           float64
	Services       []string
	AvailableCount int
}

// nativeExport is the decoded "native" block of the active character.
type nativeExport struct {
	Available bool
	MapID     int
	Offered   []nativeOffer
	Active    map[int64]nativeActive
	NPCs      []nativeNPC
	Sources   sourceSet
}

// readNative decodes the native block. Missing or unavailable blocks give a zero value.
func readNative(data map[string]any) nativeExport {
	out := nativeExport{Active: map[int64]nativeActive{}}
	raw := asMap(data["native"])
	if raw == nil || !truthy(raw["available"]) {
		return out
	}
	out.Available = true
	out.Sources = readSourceSet(raw["sources"])
	out.MapID = int(numberOf(raw["mapID"]))
	if zq := asMap(raw["zoneQuests"]); zq != nil {
		for _, item := range listOf(zq["list"]) {
			if len(out.Offered) >= maxZoneQuests {
				break
			}
			m := asMap(item)
			id := int64(numberOf(asMapValue(m, "questID")))
			if m == nil || id <= 0 {
				continue
			}
			mapID, x, y, ok := readPoint(m)
			out.Offered = append(out.Offered, nativeOffer{ID: id, Name: str(m["name"]), MapID: mapID, X: x, Y: y, OK: ok})
		}
	}
	for _, item := range listOf(raw["active"]) {
		m := asMap(item)
		id := int64(numberOf(asMapValue(m, "questID")))
		if m == nil || id <= 0 {
			continue
		}
		mapID, x, y, ok := readPoint(asMap(m["waypoint"]))
		if !ok {
			continue
		}
		out.Active[id] = nativeActive{ID: id, Name: str(m["name"]), Spawn: spawn{MapID: mapID, X: x, Y: y}}
	}
	for _, item := range listOf(raw["npcs"]) {
		if len(out.NPCs) >= maxNativeNPC {
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
		out.NPCs = append(out.NPCs, nativeNPC{
			ID: int64(numberOf(m["id"])), Name: str(m["name"]), Role: str(m["role"]),
			MapID: mapID, X: x, Y: y, Services: strList(m["services"]),
			AvailableCount: int(numberOf(m["availableCount"])),
		})
	}
	return out
}

// asMapValue reads one key of a map that may be nil (nil-safe for the readers above).
func asMapValue(m map[string]any, key string) any {
	if m == nil {
		return nil
	}
	return m[key]
}

// offerFor returns where the game offers a quest now, if it does.
func (n nativeExport) offerFor(id int64) (nativeOffer, bool) {
	for _, o := range n.Offered {
		if o.ID == id && o.OK {
			return o, true
		}
	}
	return nativeOffer{}, false
}

// spotFor is the place the game gives for a quest: the next waypoint when the quest is in the log,
// else the offer position when the game offers it now. ok is false when the game gives no place.
func (n nativeExport) spotFor(id int64, inLog bool) (spawn, bool) {
	if inLog {
		if a, ok := n.Active[id]; ok {
			return a.Spawn, true
		}
	}
	if o, ok := n.offerFor(id); ok {
		return spawn{MapID: o.MapID, X: o.X, Y: o.Y}, true
	}
	return spawn{}, false
}
