package gamedata

import (
	"fmt"
	"sort"
	"strings"
)

// info_mazmorra: a dungeon of the level range, or the suggestions for the level. The bosses
// list the loot that is an upgrade for the character; loot without exported data is counted,
// not guessed.

// DungeonInfo answers info_mazmorra. An empty nombre gives the suggestions for the level.
func (g *Service) DungeonInfo(nombre string) (any, error) {
	gd, ok := g.loadGearData()
	if !ok {
		return unavailable(HintSync), nil
	}
	if key, hint := gd.missingBlock("dungeons"); key != "" {
		return gd.unavailableFor(key, hint), nil
	}
	pos := g.playerPosition()
	needle := fold(nombre)
	if needle == "" {
		return gd.dungeonSuggestions(pos), nil
	}
	for _, d := range gd.Dungeons {
		name := fold(d.Name)
		if strings.Contains(name, needle) || fold(d.Key) == needle || (name != "" && strings.Contains(needle, name)) {
			return available(sourceSV, gd.At, gd.dungeonDetail(d, pos)), nil
		}
	}
	names := make([]string, 0, len(gd.Dungeons))
	for _, d := range gd.Dungeons {
		names = append(names, d.Name)
	}
	fields := map[string]any{
		"encontrada":  false,
		"disponibles": names,
		"nota":        fmt.Sprintf("No tengo la mazmorra «%s» en tu rango de nivel. Pregunta por una de las disponibles.", nombre),
	}
	return available(sourceSV, gd.At, fields), nil
}

// dungeonWindow says whether the character can enter the dungeon now, soon, or not yet.
func (gd gearData) dungeonWindow(d dungeonInfo) string {
	switch {
	case gd.Level >= d.Min:
		return winNow
	case d.Min <= gd.Level+gearSoonLevels:
		return winSoon
	}
	return "fuera de tu nivel"
}

// dungeonSuggestions lists the dungeons that fit the level, nearest first on the same map.
func (gd gearData) dungeonSuggestions(pos position) map[string]any {
	var fit []dungeonInfo
	for _, d := range gd.Dungeons {
		if d.Min <= gd.Level+gearSoonLevels && d.Max >= gd.Level-3 {
			fit = append(fit, d)
		}
	}
	if len(fit) == 0 {
		fit = append(fit, gd.Dungeons...)
	}
	type entry struct {
		d       dungeonInfo
		sameMap bool
		dist    float64
	}
	var list []entry
	for _, d := range fit {
		e := entry{d: d}
		if d.Entrance.OK {
			if n, ok := nearest([]spawn{{MapID: d.Entrance.MapID, X: d.Entrance.X, Y: d.Entrance.Y}}, pos); ok && n.SameMap {
				e.sameMap, e.dist = true, n.Distance
			}
		}
		list = append(list, e)
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.sameMap != b.sameMap {
			return a.sameMap
		}
		if a.sameMap && a.dist != b.dist {
			return a.dist < b.dist
		}
		return a.d.Min < b.d.Min
	})
	out := []map[string]any{}
	for _, e := range list {
		m := map[string]any{
			"nombre": e.d.Name,
			"nivel":  fmt.Sprintf("%d-%d", e.d.Min, e.d.Max),
			"estado": gd.dungeonWindow(e.d),
		}
		if e.d.Zone != "" {
			m["zona"] = e.d.Zone
		}
		mergePlace(m, e.d.Entrance, "entrada", e.d.Name)
		if e.sameMap {
			m["mismoMapa"] = true
			m["distancia"] = e.dist
		}
		out = append(out, m)
	}
	fields := map[string]any{"nivel": gd.Level, "sugerencias": out}
	if len(out) == 0 {
		fields["nota"] = "No tengo mazmorras para tu nivel con estos datos. Escribe /compa sync en el juego."
	}
	return available(sourceSV, gd.At, fields)
}

// dungeonDetail is one dungeon: its place, its quests and what each boss drops that is an upgrade.
func (gd gearData) dungeonDetail(d dungeonInfo, pos position) map[string]any {
	m := map[string]any{
		"encontrada": true,
		"nombre":     d.Name,
		"nivel":      fmt.Sprintf("%d-%d", d.Min, d.Max),
		"estado":     gd.dungeonWindow(d),
	}
	if d.Zone != "" {
		m["zona"] = d.Zone
	}
	entrance := map[string]any{}
	mergePlace(entrance, d.Entrance, "", d.Name)
	if d.Entrance.OK {
		if n, ok := nearest([]spawn{{MapID: d.Entrance.MapID, X: d.Entrance.X, Y: d.Entrance.Y}}, pos); ok && n.SameMap {
			entrance["mismoMapa"] = true
			entrance["distancia"] = n.Distance
		}
	}
	m["entrada"] = entrance

	quests := []map[string]any{}
	for _, id := range d.Quests {
		item := map[string]any{"questID": id}
		q, known := gd.Quests[id]
		switch {
		case gd.Completed[id]:
			item["estado"] = "ya completada"
		case known && q.Accessible:
			item["estado"] = winNow
		case known:
			item["estado"] = winSoon
		default:
			item["estado"] = "fuera de tu lista de misiones"
		}
		if known {
			item["nombre"] = q.Name
			item["requiereNivel"] = q.RequiredLevel
		} else {
			item["nombre"] = fmt.Sprintf("misión %d", id)
		}
		quests = append(quests, item)
	}
	m["misiones"] = quests

	bosses := []map[string]any{}
	for _, b := range d.Bosses {
		upgrades := []map[string]any{}
		unknown := 0
		for _, id := range b.Loot {
			c, ok := gd.Candidates[id]
			if !ok {
				unknown++
				continue
			}
			grp, ok := groupOf(c.item)
			if !ok {
				continue
			}
			if usable, _ := gd.usable(c.item); !usable || c.item.RequiredLevel > gd.Level+gearSoonLevels {
				continue
			}
			up, keep := gd.evaluate(c.item, grp)
			if !keep {
				continue
			}
			upgrades = append(upgrades, map[string]any{
				"objeto":    c.item.Name,
				"itemID":    c.item.ItemID,
				"ranura":    grp.Name,
				"ilvlDelta": signed(float64(up.Delta)),
				"calidad":   qualityName(c.item.Quality),
				"cambios":   changesText(up.Changes),
			})
		}
		bosses = append(bosses, map[string]any{
			"nombre":        b.Name,
			"mejorasParaTi": upgrades,
			"botinSinDatos": unknown,
		})
	}
	m["jefes"] = bosses
	if len(gd.Notes) > 0 {
		m["avisos"] = gd.Notes
	}
	return m
}
