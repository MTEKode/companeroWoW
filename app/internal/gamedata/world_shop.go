package gamedata

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// donde_comprar and coste_entrenamiento: the vendors and trainers the account has seen, with their
// prices, services and the time to reach them from the player.

const (
	worldMaxBuyRows   = 12
	worldMaxTrainRows = 20
	worldMaxKnownNPC  = 5
)

// BuyOptions answers donde_comprar: the vendors that sell the item (by item id or name), cheapest
// first with the time to reach each one. item_id wins over the name when both are given.
func (g *Service) BuyOptions(nombre string, itemID int64) (any, error) {
	w, ok := g.loadWorld()
	if !ok {
		return unavailable(worldHintWorld), nil
	}
	pos := g.playerPosition()
	needle := fold(strings.TrimSpace(nombre))
	type row struct {
		vendor worldVendor
		item   worldItem
	}
	var rows []row
	for _, id := range sortedVendorIDs(w.Vendors) {
		v := w.Vendors[id]
		for _, it := range v.Items {
			match := itemID > 0 && it.ItemID == itemID
			if !match && itemID == 0 && needle != "" && strings.Contains(fold(it.Name), needle) {
				match = true
			}
			if match {
				rows = append(rows, row{vendor: v, item: it})
			}
		}
	}
	if len(rows) == 0 {
		out := map[string]any{"encontrado": false, "compras": []map[string]any{}}
		if itemID == 0 && needle == "" {
			return nil, fmt.Errorf("falta el nombre o el item_id del objeto")
		}
		out["nota"] = "Ningún vendedor que el addon haya visto vende eso. Abre el comerciante en el juego (el addon lo registra al abrirlo) o busca en la web."
		return available(sourceSV, w.At, out), nil
	}

	// The time to reach each vendor from the player, by the best route the flight data gives.
	var fr flightRouter
	var start questStop
	hasPos := pos.OK
	if hasPos {
		fr = newFlightRouter(w, false)
		start = questStop{MapID: pos.MapID, X: pos.X, Y: pos.Y}
	}
	list := make([]map[string]any, 0, len(rows))
	type rank struct {
		copper int64
		secs   float64
		idx    int
	}
	var ranks []rank
	for i, r := range rows {
		m := map[string]any{
			"objeto":      r.item.Name,
			"itemID":      r.item.ItemID,
			"precio":      r.item.Price,
			"precioTexto": moneyText(r.item.Price),
			"stack":       r.item.Stack,
			"vendedor":    r.vendor.Name,
			"npcID":       r.vendor.NpcID,
			"mapId":       r.vendor.MapID,
		}
		if r.item.Limited {
			m["limitado"] = true
		}
		if r.item.RequiresRep != "" {
			m["requiereReputacion"] = r.item.RequiresRep
		}
		secs := 0.0
		if r.vendor.HasPos {
			m["x"], m["y"] = round1(r.vendor.X*100), round1(r.vendor.Y*100)
			m["marca"] = marcaCommand(spawn{MapID: r.vendor.MapID, X: r.vendor.X, Y: r.vendor.Y}, r.vendor.Name)
			if hasPos {
				dest := questStop{MapID: r.vendor.MapID, X: r.vendor.X, Y: r.vendor.Y}
				if opts := fr.options(start, dest, r.vendor.Name, 0); len(opts) > 0 {
					secs = opts[0].Seconds
					m["tiempo"] = durText(secs)
					m["tipoRuta"] = opts[0].Kind
					if opts[0].Kind == "vuelo" {
						m["ruta"] = legTexts(opts[0].Legs)
					}
				}
			}
		}
		list = append(list, m)
		ranks = append(ranks, rank{copper: r.item.Price, secs: secs, idx: i})
	}
	// Cheapest first; on a tie, the nearest vendor.
	sort.SliceStable(ranks, func(i, j int) bool {
		if ranks[i].copper != ranks[j].copper {
			return ranks[i].copper < ranks[j].copper
		}
		return ranks[i].secs < ranks[j].secs
	})
	sorted := make([]map[string]any, 0, len(list))
	for _, rk := range ranks {
		sorted = append(sorted, list[rk.idx])
		if len(sorted) == worldMaxBuyRows {
			break
		}
	}
	out := map[string]any{
		"encontrado": true,
		"compras":    sorted,
		"total":      len(rows),
		"supuestos":  []string{"El tiempo es el de la mejor ruta (a pie o vuelos medidos); a pie entre mapas es estimado."},
	}
	if !hasPos {
		out["nota"] = "No sé dónde estás: los tiempos no se calculan."
	}
	if near := g.knownVendorsNear(pos); len(near) > 0 {
		out["vendedoresConocidos"] = near
		out["notaVendedores"] = "Son vendedores del mapa que el addon o Questie conocen; no sé qué venden."
	}
	return available(sourceSV, w.At, out), nil
}

// knownVendorsNear lists the vendor NPCs of the player's map that the knowledge store has, without
// their stock (the store has no items). Empty without a position.
func (g *Service) knownVendorsNear(pos position) []map[string]any {
	if !pos.OK || g.store == nil {
		return nil
	}
	npcs, err := g.store.NpcsOnMap(pos.MapID, 200)
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, n := range npcs {
		if !n.IsVendor || (n.X == 0 && n.Y == 0) {
			continue
		}
		out = append(out, map[string]any{"nombre": n.Name, "mapId": n.MapID, "x": round1(n.X * 100), "y": round1(n.Y * 100)})
		if len(out) == worldMaxKnownNPC {
			break
		}
	}
	return out
}

// trainSvc is a trainer service with its trainer.
type trainSvc struct {
	trainer worldTrainer
	s       worldService
}

// TrainingCost answers coste_entrenamiento: what the services of a profession (profesion) or of
// the class trainers (no argument) cost now and at the next step, with the trainer and where it
// is. nombre narrows to the services with that name. It cross-checks the addon's que_entrenar.
func (g *Service) TrainingCost(profesion, nombre string) (any, error) {
	w, ok := g.loadWorld()
	if !ok {
		return unavailable(worldHintWorld), nil
	}
	gd, okGear := g.loadGearData()
	if !okGear {
		return unavailable(HintSync), nil
	}
	pos := g.playerPosition()
	profNeedle := fold(strings.TrimSpace(profesion))
	nameNeedle := fold(strings.TrimSpace(nombre))
	profRank := g.professionRanks()
	// rankOf is the player's rank in the service's profession (0 when not learned).
	rankOf := func(t worldTrainer) int {
		if r, ok := profRank[fold(t.Profession)]; ok {
			return r
		}
		best := 0
		for name, r := range profRank {
			if profNeedle != "" && strings.Contains(name, profNeedle) {
				best = r
			}
		}
		return best
	}

	var all []trainSvc
	for _, id := range sortedTrainerIDs(w.Trainers) {
		t := w.Trainers[id]
		for _, s := range t.Services {
			if s.Available == "used" {
				continue
			}
			switch {
			case profNeedle != "":
				if t.Kind != "profession" || !matchesProfession(t, s, profNeedle) {
					continue
				}
			case nameNeedle != "":
				if !strings.Contains(fold(s.Name), nameNeedle) {
					continue
				}
			default:
				if t.Kind != "class" {
					continue
				}
			}
			all = append(all, trainSvc{trainer: t, s: s})
		}
	}
	if len(all) == 0 {
		out := map[string]any{"encontrado": false,
			"nota": "No tengo entrenadores guardados para eso. Abre el entrenador en el juego (el addon lo registra) o escribe /compa sync cerca de él."}
		return available(sourceSV, w.At, out), nil
	}

	// Now: available and within reach (the profession rank, or the character level). Next: the
	// services above that, of the lowest tier only.
	var now, next []trainSvc
	nextTier := 0
	for _, x := range all {
		byProf := profNeedle != ""
		reach := x.s.Level <= gd.Level
		key := x.s.Level
		if byProf {
			r := rankOf(x.trainer)
			reach = x.s.SkillRank <= r
			key = x.s.SkillRank
		}
		if reach {
			if x.s.Available == "available" {
				now = append(now, x)
			}
			continue
		}
		next = append(next, x)
		if nextTier == 0 || key < nextTier {
			nextTier = key
		}
	}
	var nextList []trainSvc
	for _, x := range next {
		key := x.s.Level
		if profNeedle != "" {
			key = x.s.SkillRank
		}
		if key == nextTier {
			nextList = append(nextList, x)
		}
	}

	out := map[string]any{
		"encontrado":        true,
		"nivel":             gd.Level,
		"ahora":             trainRows(now, pos),
		"costeAhora":        sumTrainCost(now),
		"costeAhoraTexto":   moneyText(sumTrainCost(now)),
		"proximo":           trainRows(nextList, pos),
		"costeProximo":      sumTrainCost(nextList),
		"costeProximoTexto": moneyText(sumTrainCost(nextList)),
	}
	if profNeedle != "" {
		out["profesion"] = profesion
		out["rangoActual"] = rankOf(all[0].trainer)
		out["habilidadProxima"] = nextTier
	} else if nextTier > 0 {
		out["nivelProximo"] = nextTier
	}
	if mo, err := g.Money(); err == nil {
		if m := asMap(mo); truthy(m["available"]) {
			copper := int64(numberOf(m["copper"]))
			out["dinero"] = moneyText(copper)
			out["puedesPagarAhora"] = copper >= sumTrainCost(now)
		}
	}
	// que_entrenar lists the class spells the addon has; when both are class lists, the totals should agree.
	if profNeedle == "" && nameNeedle == "" {
		if plan, err := g.TrainingPlan(); err == nil {
			if m := asMap(plan); truthy(m["available"]) {
				if c, ok := numOK(m["costeAhora"]); ok {
					out["queEntrenarCosteAhora"] = int64(c)
					out["coincideConQueEntrenar"] = int64(c) == sumTrainCost(now)
				}
			}
		}
	}
	out["supuestos"] = []string{"Los costes son los que el entrenador mostró cuando lo abriste; el addon los guarda tal cual."}
	return available(sourceSV, w.At, out), nil
}

// matchesProfession is true when the service or its trainer belongs to the profession needle.
func matchesProfession(t worldTrainer, s worldService, needle string) bool {
	return strings.Contains(fold(t.Profession), needle) || strings.Contains(fold(s.SkillName), needle) ||
		strings.Contains(fold(s.Name), needle)
}

// professionRanks is the rank of each learned profession of the active character, by folded name.
func (g *Service) professionRanks() map[string]int {
	out := map[string]int{}
	v, err := g.Professions()
	if err != nil {
		return out
	}
	for _, item := range listOf(asMap(v)["professions"]) {
		m := asMap(item)
		if m == nil {
			continue
		}
		out[fold(str(m["name"]))] = int(numberOf(m["rank"]))
	}
	return out
}

// trainRows is the answer list of trainer services, nearest trainer first when the player is known.
func trainRows(list []trainSvc, pos position) []map[string]any {
	rows := make([]map[string]any, 0, len(list))
	for _, x := range list {
		m := map[string]any{
			"servicio":   x.s.Name,
			"coste":      x.s.Cost,
			"costeTexto": moneyText(x.s.Cost),
			"entrenador": x.trainer.Name,
			"npcID":      x.trainer.NpcID,
			"mapId":      x.trainer.MapID,
		}
		if x.s.Level > 0 {
			m["nivel"] = x.s.Level
		}
		if x.s.SkillName != "" {
			m["habilidad"] = map[string]any{"nombre": x.s.SkillName, "rango": x.s.SkillRank}
		}
		if x.trainer.HasPos {
			m["x"], m["y"] = round1(x.trainer.X*100), round1(x.trainer.Y*100)
			m["marca"] = marcaCommand(spawn{MapID: x.trainer.MapID, X: x.trainer.X, Y: x.trainer.Y}, x.trainer.Name)
		}
		rows = append(rows, m)
		if len(rows) == worldMaxTrainRows {
			break
		}
	}
	return rows
}

func sumTrainCost(list []trainSvc) int64 {
	var total int64
	seen := map[string]bool{}
	for _, x := range list {
		key := strconv.FormatInt(x.trainer.NpcID, 10) + "|" + x.s.Name + "|" + x.s.Rank
		if seen[key] {
			continue
		}
		seen[key] = true
		total += x.s.Cost
	}
	return total
}
