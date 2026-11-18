package gamedata

import (
	"math"
	"sort"
)

// consumibles: what the character carries, what restores most per copper and per second, where to
// buy it, the bandage and food the First Aid and Cooking skills allow, and how many to carry for N
// pulls. The damage per pull is the damage taken in the fights the addon recorded and won, which is
// the same record the theory's downtime model reads.

const (
	worldDefaultPulls = 5
	worldMaxPulls     = 50
	worldMaxBuyList   = 8
	worldMaxCarry     = 4
)

// firstAidNames and cookingNames are the profession names of both clients, folded.
var (
	firstAidNames = []string{"primeros auxilios", "first aid"}
	cookingNames  = []string{"cocina", "cooking"}
)

// ConsumablesPlan answers consumibles for the given number of pulls (0 means the default).
func (g *Service) ConsumablesPlan(pulls int) (any, error) {
	w, ok := g.loadWorld()
	if !ok {
		return unavailable(worldHintWorld), nil
	}
	if pulls <= 0 {
		pulls = worldDefaultPulls
	}
	if pulls > worldMaxPulls {
		pulls = worldMaxPulls
	}
	ch, _, hasChar := g.activeCharacter()
	if !hasChar {
		return unavailable(HintSync), nil
	}
	bags := bagCounts(g)
	cheapest := cheapestVendors(w)
	ranks := g.professionRanks()

	// What the character carries, with its value per copper and per second.
	var tienes []map[string]any
	for _, id := range sortedConsumableIDs(w.Consumables) {
		c := w.Consumables[id]
		n := bags[id]
		if n == 0 {
			continue
		}
		var vp *vendorPick
		if v, ok := cheapest[id]; ok {
			vp = &v
		}
		m := consumableView(c, vp)
		m["cantidad"] = n
		tienes = append(tienes, m)
	}

	// The best buys: the cheapest vendor of each consumable that restores something, by health per copper.
	var compra []map[string]any
	for _, id := range sortedConsumableIDs(w.Consumables) {
		c := w.Consumables[id]
		v, sold := cheapest[id]
		if !sold || (c.Health == 0 && c.Mana == 0) {
			continue
		}
		m := consumableView(c, &v)
		m["vendedor"] = v.vendor
		m["precio"] = v.price
		m["precioTexto"] = moneyText(v.price)
		if v.hasPos {
			m["marca"] = marcaCommand(spawn{MapID: v.mapID, X: v.x, Y: v.y}, v.vendor)
		}
		compra = append(compra, m)
	}
	sort.SliceStable(compra, func(i, j int) bool {
		return floatOf(compra[i]["saludPorCobre"]) > floatOf(compra[j]["saludPorCobre"])
	})
	if len(compra) > worldMaxBuyList {
		compra = compra[:worldMaxBuyList]
	}

	// The fights: the damage taken per won fight, from the recorded combats.
	dmg := wonFightDamage(ch.data)
	pelea := map[string]any{"peleasMedidas": len(dmg), "peleas": pulls}
	need := 0.0
	if len(dmg) > 0 {
		mean, max := meanMax(dmg)
		need = mean * float64(pulls)
		pelea["danoMedio"] = round1(mean)
		pelea["danoMaximo"] = round1(max)
		pelea["vidaNecesaria"] = round1(need)
	} else {
		pelea["nota"] = "No tengo peleas ganadas grabadas: calculo sin daño medido. Juega unas peleas para medirlo."
	}

	// The plan: the candidates (owned or buyable), by health per copper, and how many of each to carry.
	carry := carryPlan(w, bags, cheapest, ranks, need)

	out := map[string]any{
		"tienes":        tienes,
		"mejorPorCobre": compra,
		"habilidades":   skillNotes(w, ranks),
		"pelea":         pelea,
		"llevar":        carry,
		"supuestos": []string{
			"El daño por pelea es la media de las peleas ganadas grabadas (estimado); no cuenta la regeneración.",
			"Cada consumible tarda en usarse lo que dice «segundos» (descanso fuera de combate); eso es lo que cuesta cada uso.",
			"Los precios son los que anotó el vendedor cuando lo abriste.",
		},
	}
	if len(tienes) == 0 {
		out["nota"] = "No tienes consumibles que el addon conozca en las bolsas: los que compras aparecen en «mejorPorCobre»."
	}
	return available(sourceSV, w.At, out), nil
}

// vendorPick is the cheapest vendor of one consumable.
type vendorPick struct {
	price  int64
	vendor string
	mapID  int
	x, y   float64
	hasPos bool
}

// cheapestVendors is the cheapest price each known consumable has at a vendor.
func cheapestVendors(w worldData) map[int64]vendorPick {
	out := map[int64]vendorPick{}
	for _, id := range sortedVendorIDs(w.Vendors) {
		v := w.Vendors[id]
		for _, it := range v.Items {
			if _, known := w.Consumables[it.ItemID]; !known || it.Price <= 0 {
				continue
			}
			cur, ok := out[it.ItemID]
			if !ok || it.Price < cur.price {
				out[it.ItemID] = vendorPick{price: it.Price, vendor: v.Name, mapID: v.MapID, x: v.X, y: v.Y, hasPos: v.HasPos}
			}
		}
	}
	return out
}

// consumableView is one consumable with its restores and its value per copper and per second.
func consumableView(c worldConsumable, v *vendorPick) map[string]any {
	m := map[string]any{
		"objeto":   c.Name,
		"itemID":   c.ItemID,
		"tipo":     c.Kind,
		"salud":    c.Health,
		"mana":     c.Mana,
		"segundos": c.Seconds,
	}
	if c.RequiredLevel > 0 {
		m["nivelRequerido"] = c.RequiredLevel
	}
	if c.SkillName != "" {
		m["habilidad"] = map[string]any{"nombre": c.SkillName, "rango": c.SkillRank}
	}
	restore := float64(c.Health + c.Mana)
	if v != nil && v.price > 0 {
		m["saludPorCobre"] = round1(restore * 100 / float64(v.price))
	}
	if c.Seconds > 0 {
		m["saludPorSegundo"] = round1(restore / float64(c.Seconds))
	}
	return m
}

// carryPlan suggests how many of the usable healing items to carry for the damage of the pulls.
func carryPlan(w worldData, bags map[int64]int, cheapest map[int64]vendorPick, ranks map[string]int, need float64) []map[string]any {
	type cand struct {
		c     worldConsumable
		owned int
		value float64
	}
	var cands []cand
	for _, id := range sortedConsumableIDs(w.Consumables) {
		c := w.Consumables[id]
		heal := c.Health
		if heal <= 0 || !healingKind(c.Kind) || !skillOK(c, ranks) {
			continue
		}
		owned := bags[id]
		v, sold := cheapest[id]
		if owned == 0 && !sold {
			continue
		}
		value := float64(heal)
		if sold && v.price > 0 {
			value = float64(heal) * 100 / float64(v.price)
		}
		cands = append(cands, cand{c: c, owned: owned, value: value})
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].value > cands[j].value })
	var out []map[string]any
	for i, cd := range cands {
		if i == worldMaxCarry {
			break
		}
		per := float64(cd.c.Health)
		count := 0
		if need > 0 {
			count = int(math.Ceil(need / per))
		}
		m := map[string]any{
			"objeto":       cd.c.Name,
			"itemID":       cd.c.ItemID,
			"cantidad":     count,
			"tienes":       cd.owned,
			"faltan":       maxInt(0, count-cd.owned),
			"curaTotal":    cd.c.Health * int64(count),
			"tiempoUsoSeg": cd.c.Seconds * int64(count),
		}
		out = append(out, m)
	}
	return out
}

// healingKind is true for the kinds that restore health out of combat.
func healingKind(kind string) bool {
	switch kind {
	case "food", "bandage", "potion_health", "elixir":
		return true
	}
	return false
}

// skillOK is true when the character's profession rank allows the consumable (or it needs none).
func skillOK(c worldConsumable, ranks map[string]int) bool {
	if c.SkillName == "" {
		return true
	}
	rank, has := rankNamed(ranks, c.SkillName)
	return has && rank >= c.SkillRank
}

// rankNamed finds the rank of a profession by its name, in either client's language (First Aid
// and Primeros auxilios, Cooking and Cocina are the same profession).
func rankNamed(ranks map[string]int, name string) (int, bool) {
	f := fold(name)
	if r, ok := ranks[f]; ok {
		return r, true
	}
	for _, group := range [][]string{firstAidNames, cookingNames} {
		for _, n := range group {
			if n == f {
				for _, alt := range group {
					if r, ok := ranks[alt]; ok {
						return r, true
					}
				}
			}
		}
	}
	return 0, false
}

// skillNotes says, for First Aid and Cooking, the rank the character has and the consumables it allows.
func skillNotes(w worldData, ranks map[string]int) []map[string]any {
	var out []map[string]any
	for _, group := range []struct {
		label string
		names []string
		kind  string
	}{
		{"Primeros auxilios", firstAidNames, "bandage"},
		{"Cocina", cookingNames, "food"},
	} {
		rank, learned := 0, false
		for _, n := range group.names {
			if r, ok := rankNamed(ranks, n); ok {
				rank, learned = r, true
				break
			}
		}
		m := map[string]any{"profesion": group.label, "aprendida": learned}
		if learned {
			m["rango"] = rank
		}
		var usable []string
		for _, id := range sortedConsumableIDs(w.Consumables) {
			c := w.Consumables[id]
			if c.Kind != group.kind {
				continue
			}
			if learned && rank >= c.SkillRank {
				usable = append(usable, c.Name)
			}
		}
		if learned {
			m["usables"] = usable
		} else {
			m["nota"] = "No tienes esta profesión en los datos del addon."
		}
		out = append(out, m)
	}
	return out
}

// bagCounts is the number of each item the character carries, by item id.
func bagCounts(g *Service) map[int64]int {
	out := map[int64]int{}
	v, err := g.Inventory()
	if err != nil {
		return out
	}
	for _, item := range listOf(asMap(v)["items"]) {
		m := asMap(item)
		if m == nil {
			continue
		}
		id := int64(numberOf(m["itemID"]))
		n := int(numberOf(m["count"]))
		if n <= 0 {
			n = 1
		}
		out[id] += n
	}
	return out
}

// wonFightDamage is the damage taken in each fight the character won, from the recorded combats.
func wonFightDamage(data map[string]any) []float64 {
	var out []float64
	for _, c := range parseCombats(data["combats"]) {
		if !c.died && c.taken > 0 {
			out = append(out, c.taken)
		}
	}
	return out
}

func meanMax(v []float64) (mean, max float64) {
	for _, x := range v {
		mean += x
		if x > max {
			max = x
		}
	}
	return mean / float64(len(v)), max
}

func sortedConsumableIDs(m map[int64]worldConsumable) []int64 {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func floatOf(v any) float64 {
	f, _ := numOK(v)
	return f
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
