package gamedata

import (
	"fmt"
	"sort"
	"strings"
)

// analizar_equipo: for each slot, the equipped item and the candidates that are upgrades and
// that the character can reach now ("ahora") or soon ("pronto"). Everything the answer names
// comes from the exported blocks; the stat priorities are left to the model and the player.

// gearRow is one candidate that is an upgrade in one window.
type gearRow struct {
	grp     gearGroup
	cand    gearCandidate
	up      upgrade
	window  string
	sources []itemSource
}

// upgradeRows lists the candidates that are upgrades, best first: reachable now before soon,
// then the largest item level gain, then the quality. only, when set, keeps one slot group.
func (gd gearData) upgradeRows(only *gearGroup) []gearRow {
	var rows []gearRow
	for _, id := range gd.sortedCandidateIDs() {
		c := gd.Candidates[id]
		grp, ok := groupOf(c.item)
		if !ok || (only != nil && grp.Key != only.Key) {
			continue
		}
		if c.item.RequiredLevel > gd.Level+gearSoonLevels {
			continue
		}
		if usable, _ := gd.usable(c.item); !usable {
			continue
		}
		win, sources := gd.itemWindow(c)
		if win == "" {
			continue
		}
		up, keep := gd.evaluate(c.item, grp)
		if !keep {
			continue
		}
		rows = append(rows, gearRow{grp: grp, cand: c, up: up, window: win, sources: sources})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.window != b.window {
			return a.window == winNow
		}
		if a.up.Delta != b.up.Delta {
			return a.up.Delta > b.up.Delta
		}
		if a.cand.item.Quality != b.cand.item.Quality {
			return a.cand.item.Quality > b.cand.item.Quality
		}
		return a.cand.item.Name < b.cand.item.Name
	})
	return rows
}

// GearAnalysis answers analizar_equipo. ventana is "ahora", "pronto" or "ambas"; slot is an
// optional slot name or number.
func (g *Service) GearAnalysis(ventana, slot string) (any, error) {
	wins, err := windowsOf(ventana)
	if err != nil {
		return nil, err
	}
	gd, ok := g.loadGearData()
	if !ok {
		return unavailable(HintSync), nil
	}
	if key, hint := gd.missingBlock("gear", "questWindow", "gearCandidates"); key != "" {
		return gd.unavailableFor(key, hint), nil
	}
	var only *gearGroup
	if strings.TrimSpace(slot) != "" {
		grp, found := groupForSlotArg(slot)
		if !found {
			return unavailable("No conozco la ranura «" + slot + "». Usa cabeza, cuello, hombros, pecho, cintura, piernas, pies, muñecas, manos, anillo, abalorio, espalda, mano derecha, mano izquierda o a distancia."), nil
		}
		only = &grp
	}

	rows := gd.upgradeRows(only)
	var kept []gearRow
	for _, r := range rows {
		if wins[r.window] {
			kept = append(kept, r)
		}
	}

	slots := []map[string]any{}
	for _, grp := range gearGroups {
		if only != nil && grp.Key != only.Key {
			continue
		}
		ahora, pronto := []map[string]any{}, []map[string]any{}
		for _, r := range kept {
			if r.grp.Key != grp.Key {
				continue
			}
			switch {
			case r.window == winNow && len(ahora) < gearMaxPerGroup:
				ahora = append(ahora, gd.rowView(r))
			case r.window == winSoon && len(pronto) < gearMaxPerGroup:
				pronto = append(pronto, gd.rowView(r))
			}
		}
		if len(ahora) == 0 && len(pronto) == 0 && only == nil {
			continue
		}
		entry := map[string]any{"ranura": grp.Name, "actual": gd.currentView(grp)}
		if wins[winNow] {
			entry[winNow] = ahora
		}
		if wins[winSoon] {
			entry[winSoon] = pronto
		}
		slots = append(slots, entry)
	}

	fields := map[string]any{
		"personaje": map[string]any{
			"nombre":          gd.Name,
			"clase":           gd.Class,
			"especializacion": gd.Spec,
			"nivel":           gd.Level,
			"armaduras":       nonNilStrings(gd.Armor),
			"armas":           nonNilStrings(gd.Weapons),
			"dosArmas":        gd.DualWield,
		},
		"ventana": normalizedWindow(ventana),
		"total":   len(kept),
		"mejores": gd.topUpgrades(kept),
		"ranuras": slots,
	}
	if slotName := slotLabel(only); slotName != "" {
		fields["ranura"] = slotName
	}
	if len(kept) == 0 {
		fields["nota"] = "No tengo mejoras accesibles para tu nivel con estos datos."
		if only != nil {
			fields["nota"] = "No tengo mejoras accesibles para tu nivel en esta ranura con estos datos."
		}
	}
	if len(gd.Notes) > 0 {
		fields["avisos"] = gd.Notes
	}
	return available(sourceSV, gd.At, fields), nil
}

// topUpgrades is the short list of the best upgrades across slots, for the top of the answer.
func (gd gearData) topUpgrades(rows []gearRow) []map[string]any {
	// Reachable now first, then soon; by item level gain inside each.
	best := append([]gearRow(nil), rows...)
	sort.SliceStable(best, func(i, j int) bool {
		a, b := best[i], best[j]
		if a.window != b.window {
			return a.window == winNow
		}
		return a.up.Delta > b.up.Delta
	})
	out := []map[string]any{}
	for _, r := range best {
		if len(out) >= gearTopUpgrades {
			break
		}
		out = append(out, map[string]any{
			"ranura":    r.grp.Name,
			"objeto":    r.cand.item.Name,
			"ilvlDelta": signed(float64(r.up.Delta)),
			"ventana":   r.window,
			"fuente":    gd.primarySource(r),
		})
	}
	return out
}

// primarySource names the first source of a row in a short form.
func (gd gearData) primarySource(r gearRow) string {
	if len(r.sources) == 0 {
		return ""
	}
	s := r.sources[0]
	if s.Kind == "quest" {
		if q, ok := gd.Quests[s.QuestID]; ok {
			return "Misión «" + q.Name + "»"
		}
		return fmt.Sprintf("Misión %d", s.QuestID)
	}
	if d, ok := gd.dungeon(s.DungeonKey); ok {
		return "Mazmorra " + d.Name
	}
	return "Mazmorra " + s.DungeonKey
}

// rowView is one upgrade as the answer shows it.
func (gd gearData) rowView(r gearRow) map[string]any {
	c := r.cand.item
	m := map[string]any{
		"objeto":        c.Name,
		"itemID":        c.ItemID,
		"ilvl":          c.ItemLevel,
		"ilvlDelta":     signed(float64(r.up.Delta)),
		"calidad":       qualityName(c.Quality),
		"requiereNivel": c.RequiredLevel,
		"cambios":       changesText(r.up.Changes),
		"ventana":       r.window,
	}
	if s := statsText(c.Stats); s != "" {
		m["stats"] = s
	}
	if len(r.up.Notes) > 0 {
		m["nota"] = strings.Join(r.up.Notes, ". ")
	}
	fuentes := []map[string]any{}
	for _, s := range r.sources {
		fuentes = append(fuentes, gd.sourceView(s, r.window))
	}
	m["fuentes"] = fuentes
	return m
}

// currentView is what the slot holds now: the weakest item of the group, or an empty slot.
func (gd gearData) currentView(grp gearGroup) any {
	cur := gd.weakest(grp)
	if cur == nil {
		return "ranura vacía"
	}
	m := map[string]any{
		"objeto":  cur.Name,
		"itemID":  cur.ItemID,
		"ilvl":    cur.ItemLevel,
		"calidad": qualityName(cur.Quality),
	}
	if s := statsText(cur.Stats); s != "" {
		m["stats"] = s
	}
	return m
}

// sourceView describes one source of an item or quest. state is the window or the reason
// the source is out of reach.
func (gd gearData) sourceView(s itemSource, state string) map[string]any {
	switch s.Kind {
	case "quest":
		q, ok := gd.Quests[s.QuestID]
		name := fmt.Sprintf("misión %d", s.QuestID)
		if ok && q.Name != "" {
			name = q.Name
		}
		m := map[string]any{"tipo": "mision", "mision": name, "questID": s.QuestID, "estado": state, "aElegir": s.Choice}
		if ok {
			m["nivel"] = q.Level
			m["requiereNivel"] = q.RequiredLevel
			if q.Zone != "" {
				m["zona"] = q.Zone
			}
			if len(q.Missing) > 0 {
				m["faltan"] = gd.missingNames(q.Missing)
			}
			mergePlace(m, q.Starter, "dondeAceptar", q.Starter.Name)
		}
		return m
	case "dungeon":
		d, ok := gd.dungeon(s.DungeonKey)
		name := s.DungeonKey
		if ok {
			name = d.Name
		}
		m := map[string]any{"tipo": "mazmorra", "mazmorra": name, "estado": state}
		if s.Boss != "" {
			m["jefe"] = s.Boss
		}
		if ok {
			m["nivel"] = fmt.Sprintf("%d-%d", d.Min, d.Max)
			if d.Zone != "" {
				m["zona"] = d.Zone
			}
			mergePlace(m, d.Entrance, "entrada", d.Name)
		}
		return m
	}
	return map[string]any{"tipo": s.Kind, "estado": state}
}

// mergePlace adds the place of an NPC or entrance: its coordinates and mark command. key
// holds the name, so that the answer can say where it is. Without a place it says so.
func mergePlace(m map[string]any, p npcPoint, key, name string) {
	if key != "" && name != "" {
		m[key] = name
	}
	if !p.OK {
		m["ubicacion"] = "desconocida: escribe /compa sync en el juego"
		return
	}
	m["coords"] = coordsText(p.X, p.Y)
	m["marca"] = marcaCommand(spawn{MapID: p.MapID, X: p.X, Y: p.Y}, name)
}

// missingNames names the quests still needed: «Name» or "misión N".
func (gd gearData) missingNames(ids []int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if q, ok := gd.Quests[id]; ok && q.Name != "" {
			out = append(out, "«"+q.Name+"»")
		} else {
			out = append(out, fmt.Sprintf("misión %d", id))
		}
	}
	return out
}

// slotLabel is the slot name of a filtered analysis, or "".
func slotLabel(only *gearGroup) string {
	if only == nil {
		return ""
	}
	return only.Name
}

// normalizedWindow is the window name the answer reports.
func normalizedWindow(v string) string {
	switch fold(v) {
	case "ahora", "ya":
		return winNow
	case "pronto":
		return winSoon
	}
	return "ambas"
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// gearSummary is the compact count for the player context: upgrades now and soon, and the
// quests accessible now. It is nil when the gear data is missing.
func gearSummary(g *Service) map[string]any {
	gd, ok := g.loadGearData()
	if !ok {
		return nil
	}
	if key, _ := gd.missingBlock("gear", "questWindow", "gearCandidates"); key != "" {
		return nil
	}
	now, soon := 0, 0
	for _, r := range gd.upgradeRows(nil) {
		if r.window == winNow {
			now++
		} else {
			soon++
		}
	}
	quests := 0
	for id, q := range gd.Quests {
		if q.Accessible && !gd.Completed[id] {
			quests++
		}
	}
	return map[string]any{"mejorasAhora": now, "mejorasPronto": soon, "misionesAhora": quests}
}
