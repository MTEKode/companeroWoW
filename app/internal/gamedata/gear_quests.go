package gamedata

import (
	"fmt"
	"sort"
	"strings"
)

// misiones_disponibles: the quests the character can take now or soon, grouped by zone and
// ordered by distance when the player stands on the same map as the quest giver.

// questRow is one quest of the answer with what is needed to order it.
type questRow struct {
	q       windowQuest
	window  string
	sameMap bool
	dist    float64
}

// AvailableQuests answers misiones_disponibles. ventana is "ahora", "pronto" or "ambas";
// zona optionally keeps the quests of one zone.
func (g *Service) AvailableQuests(ventana, zona string) (any, error) {
	wins, err := windowsOf(ventana)
	if err != nil {
		return nil, err
	}
	gd, ok := g.loadGearData()
	if !ok {
		return unavailable(HintSync), nil
	}
	if key, hint := gd.missingBlock("questWindow"); key != "" {
		return gd.unavailableFor(key, hint), nil
	}
	pos := g.playerPosition()
	upgrades := map[int64]bool{}
	for _, r := range gd.upgradeRows(nil) {
		upgrades[r.cand.item.ItemID] = true
	}

	zoneNeedle := fold(zona)
	var rows []questRow
	for _, id := range sortedQuestIDs(gd.Quests) {
		q := gd.Quests[id]
		if gd.Completed[id] {
			continue
		}
		win := winSoon
		if q.Accessible {
			win = winNow
		}
		if !wins[win] {
			continue
		}
		if zoneNeedle != "" && !strings.Contains(fold(q.Zone), zoneNeedle) {
			continue
		}
		r := questRow{q: q, window: win}
		if q.Starter.OK {
			if n, found := nearest([]spawn{{MapID: q.Starter.MapID, X: q.Starter.X, Y: q.Starter.Y}}, pos); found && n.SameMap {
				r.sameMap, r.dist = true, n.Distance
			}
		}
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.sameMap != b.sameMap {
			return a.sameMap
		}
		if a.sameMap && a.dist != b.dist {
			return a.dist < b.dist
		}
		if a.q.RequiredLevel != b.q.RequiredLevel {
			return a.q.RequiredLevel < b.q.RequiredLevel
		}
		return a.q.ID < b.q.ID
	})

	total := len(rows)
	if len(rows) > gearMaxQuestRows {
		rows = rows[:gearMaxQuestRows]
	}
	type zoneGroup struct {
		zone  string
		items []map[string]any
	}
	var groups []*zoneGroup
	index := map[string]*zoneGroup{}
	for _, r := range rows {
		zone := r.q.Zone
		if zone == "" {
			zone = "Sin zona"
		}
		grp, ok := index[zone]
		if !ok {
			grp = &zoneGroup{zone: zone}
			index[zone] = grp
			groups = append(groups, grp)
		}
		grp.items = append(grp.items, gd.questView(r, upgrades))
	}
	zones := []map[string]any{}
	for _, grp := range groups {
		zones = append(zones, map[string]any{"zona": grp.zone, "misiones": grp.items})
	}

	fields := map[string]any{
		"ventana": normalizedWindow(ventana),
		"nivel":   gd.Level,
		"total":   total,
		"zonas":   zones,
	}
	switch {
	case total == 0:
		fields["nota"] = "No tengo misiones accesibles para ese filtro con estos datos."
	case total > len(rows):
		fields["nota"] = fmt.Sprintf("Muestro %d de %d misiones.", len(rows), total)
	}
	if len(gd.Notes) > 0 {
		fields["avisos"] = gd.Notes
	}
	return available(sourceSV, gd.At, fields), nil
}

// questView is one quest of the answer: where to take it, how far, its rewards and which
// rewards are upgrades for the character.
func (gd gearData) questView(r questRow, upgrades map[int64]bool) map[string]any {
	q := r.q
	m := map[string]any{
		"questID":       q.ID,
		"nombre":        q.Name,
		"nivel":         q.Level,
		"requiereNivel": q.RequiredLevel,
		"ventana":       r.window,
	}
	mergePlace(m, q.Starter, "dondeAceptar", q.Starter.Name)
	if r.sameMap {
		m["mismoMapa"] = true
		m["distancia"] = r.dist
	}
	if len(q.Missing) > 0 {
		m["faltan"] = gd.missingNames(q.Missing)
	}
	if rw := gd.rewardsView(q); len(rw) > 0 {
		m["recompensas"] = rw
	}
	var mejoras []string
	for _, id := range append(append([]int64{}, q.RewardChoice...), q.RewardFixed...) {
		if upgrades[id] {
			mejoras = append(mejoras, gd.rewardName(id))
		}
	}
	if len(mejoras) > 0 {
		m["mejoraEquipo"] = mejoras
	}
	return m
}

// rewardsView names the rewards of a quest: the ones to choose from and the fixed ones.
func (gd gearData) rewardsView(q windowQuest) map[string][]string {
	out := map[string][]string{}
	if len(q.RewardChoice) > 0 {
		out["elegir"] = gd.rewardNames(q.RewardChoice)
	}
	if len(q.RewardFixed) > 0 {
		out["fijas"] = gd.rewardNames(q.RewardFixed)
	}
	return out
}

func (gd gearData) rewardNames(ids []int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, gd.rewardName(id))
	}
	return out
}

// rewardName is the name of a reward when the addon exported it as a candidate, else its id.
func (gd gearData) rewardName(id int64) string {
	if c, ok := gd.Candidates[id]; ok && c.item.Name != "" {
		return c.item.Name
	}
	return fmt.Sprintf("objeto %d", id)
}

func sortedQuestIDs(m map[int64]windowQuest) []int64 {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
