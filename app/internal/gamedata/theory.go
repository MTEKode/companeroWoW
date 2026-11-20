package gamedata

// What the theorycraft engine reads from the active character: the character sheet (the addon's
// "sheet" block, contract CHARACTER SHEET + SPELL DATA) and the gear candidates as items the engine
// can score. Nothing here computes a number; the engine does that.

import (
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// maxBagCandidates caps the bag items the engine scores (the addon exports up to 60 equippable ones).
const maxBagCandidates = 60

// SheetJSON returns the active character's sheet as JSON, with the time it was exported. ok is false
// when the character has no sheet (the addon is older, or it has not exported one yet).
func (g *Service) SheetJSON() ([]byte, time.Time, bool) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return nil, time.Time{}, false
	}
	m := asMap(ch.data["sheet"])
	if m == nil {
		return nil, time.Time{}, false
	}
	m = sheetWithCharacter(m, ch.data)
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, time.Time{}, false
	}
	at := unixTime(numberOf(m["exportedAt"]))
	if at.IsZero() {
		at = ch.updatedAt
	}
	return raw, at, true
}

// sheetWithCharacter returns the sheet with the class and the level of the character when the sheet
// lacks them (a sheet of the outbox carries its level as "lv"; the character table has both). The
// engine reads the class to pick its model and the level for the talent points.
func sheetWithCharacter(sheet map[string]any, data map[string]any) map[string]any {
	char := asMap(data["character"])
	if char == nil {
		return sheet
	}
	out := make(map[string]any, len(sheet)+2)
	for k, v := range sheet {
		out[k] = v
	}
	if str(out["class"]) == "" {
		if cf := str(char["classFile"]); cf != "" {
			out["class"] = cf
		}
	}
	if numberOf(out["level"]) <= 0 {
		if lv := numberOf(char["level"]); lv > 0 {
			out["level"] = lv
		}
	}
	return out
}

// TheoryCandidates returns the gear the engine can score: the candidates the character can use (its class,
// its level), and the equippable items in its bags (with their stats and, for weapons, their damage and
// speed). Each one carries the item it would replace: the weakest item of its slot group, as the gear
// analysis picks it. The second result names the candidates that were left out and why.
func (g *Service) TheoryCandidates() ([]theory.Item, []string, bool) {
	gd, ok := g.loadGearData()
	if !ok || !gd.CandidatesKnown {
		return nil, nil, false
	}
	var out []theory.Item
	var left []string
	seen := map[int64]bool{}
	for _, id := range gd.sortedCandidateIDs() {
		c := gd.Candidates[id]
		seen[c.item.ItemID] = true
		grp, ok := groupOf(c.item)
		if !ok {
			left = append(left, c.item.Name+": no es una ranura de equipo")
			continue
		}
		if usable, why := gd.usable(c.item); !usable {
			left = append(left, c.item.Name+": "+why)
			continue
		}
		win, _ := gd.itemWindow(c)
		if win == "" {
			continue
		}
		out = append(out, gd.theoryItem(c.item, grp, win))
	}
	out = append(out, g.bagTheoryItems(gd, seen)...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, left, true
}

// theoryItem turns a gear item into the engine's candidate: its stats, its weapon, and the item of its slot
// group that it would replace.
func (gd gearData) theoryItem(gi gearItem, grp gearGroup, source string) theory.Item {
	it := theory.Item{
		Name:      gi.Name,
		ItemID:    gi.ItemID,
		EquipLoc:  gi.EquipLoc,
		Subclass:  gi.Subclass,
		ItemLevel: gi.ItemLevel,
		Slot:      grp.Name,
		Stats:     gi.Stats,
		Source:    source,
		Weapon:    gi.Weapon,
	}
	if target := gd.weakest(grp); target != nil {
		it.Replaces = target.Stats
		it.Slot = grp.Name + " (cambia: " + target.Name + ")"
	}
	return it
}

// bagTheoryItems returns the equippable bag items the engine can score, skipping the ids already listed.
// Only items the character can use are scored; the addon exports at most maxBagCandidates of them.
func (g *Service) bagTheoryItems(gd gearData, seen map[int64]bool) []theory.Item {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return nil
	}
	var out []theory.Item
	for _, e := range listOf(asMap(ch.data["inventory"])["items"]) {
		if len(out) >= maxBagCandidates {
			break
		}
		gi, ok := parseGearItem(asMap(e), 0)
		if !ok || gi.ItemID == 0 || seen[gi.ItemID] || gi.EquipLoc == "" {
			continue
		}
		if len(gi.Stats) == 0 && gi.Weapon == nil {
			continue
		}
		seen[gi.ItemID] = true
		if usable, _ := gd.usable(gi); !usable {
			continue
		}
		grp, ok := groupOf(gi)
		if !ok {
			continue
		}
		it := gd.theoryItem(gi, grp, "bolsa")
		it.Slot = grp.Name + " (en la bolsa)"
		it.Bag = true
		out = append(out, it)
	}
	return out
}

// CalibrationKey names the stored calibration of the active character's sheet: the character and the time
// the sheet was exported. ok is false when there is no sheet.
func (g *Service) CalibrationKey() (string, bool) {
	ch, _, ok := g.activeCharacter()
	if !ok || asMap(ch.data["sheet"]) == nil {
		return "", false
	}
	at := unixTime(numberOf(asMap(ch.data["sheet"])["exportedAt"]))
	if at.IsZero() {
		at = ch.updatedAt
	}
	return "teoria.calibracion:" + ch.key + ":" + strconv.FormatInt(at.Unix(), 10), true
}

// StoredCalibration returns the calibration line stored for a key (see CalibrationKey).
func (g *Service) StoredCalibration(key string) (string, bool) {
	if g.store == nil {
		return "", false
	}
	v, ok, err := g.store.CacheGet(key, 0)
	return v, ok && err == nil
}

// StoreCalibration stores the calibration line of a key, so the check runs once per exported sheet.
func (g *Service) StoreCalibration(key, line string) {
	if g.store == nil {
		return
	}
	_ = g.store.CachePut(key, line)
}

// RecentFights returns up to n recorded fights of the active character, newest first, as the theory
// engine reads them: the duration, the damage dealt, whether the character died and the spells that
// dealt damage (by name, as the addon records them).
func (g *Service) RecentFights(n int) []theory.FightRecord {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return nil
	}
	if n <= 0 || n > maxReviewCombats {
		n = maxReviewCombats
	}
	var out []theory.FightRecord
	for _, c := range parseCombats(ch.data["combats"]) {
		if len(out) == n {
			break
		}
		f := theory.FightRecord{Segundos: c.duration, Muerte: c.died, DanoHecho: c.damage}
		for _, row := range c.dmg {
			if row.amount <= 0 {
				continue
			}
			if row.id > 0 {
				f.Hechizos = append(f.Hechizos, int(row.id))
			}
			if row.name != "" {
				f.Habilidades = append(f.Habilidades, row.name)
			}
		}
		out = append(out, f)
	}
	return out
}
