package gamedata

// What the theorycraft engine reads from the active character: the character sheet (the addon's
// "sheet" block, contract CHARACTER SHEET + SPELL DATA) and the gear candidates as items the engine
// can score. Nothing here computes a number; the engine does that.

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

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

// TheoryCandidates returns the gear candidates the character can use (its class, its level), as items
// the engine scores. Each one carries the stats of the item it would replace: the weakest item of its
// slot group, as the gear analysis picks it. Bag items are not included: the addon exports them without
// stats. The second result names the candidates that were left out and why.
func (g *Service) TheoryCandidates() ([]theory.Item, []string, bool) {
	gd, ok := g.loadGearData()
	if !ok || !gd.CandidatesKnown {
		return nil, nil, false
	}
	var out []theory.Item
	var left []string
	for _, id := range gd.sortedCandidateIDs() {
		c := gd.Candidates[id]
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
		it := theory.Item{
			Name:      c.item.Name,
			ItemID:    c.item.ItemID,
			EquipLoc:  c.item.EquipLoc,
			ItemLevel: c.item.ItemLevel,
			Slot:      grp.Name,
			Stats:     c.item.Stats,
			Source:    win,
		}
		if target := gd.weakest(grp); target != nil {
			it.Replaces = target.Stats
			it.Slot = grp.Name + " (cambia: " + target.Name + ")"
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, left, true
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
			if row.name != "" && row.amount > 0 {
				f.Habilidades = append(f.Habilidades, row.name)
			}
		}
		out = append(out, f)
	}
	return out
}
