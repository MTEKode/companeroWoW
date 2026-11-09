package gamedata

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Action bars: the layout the addon exported (contract: ACTION BARS), advice on it, and the
// /compa barras aplicar commands that the player pastes in the game. The app never sends them.

const (
	maxBarSlots        = 120
	maxBarPairs        = 40
	maxBarCommandChars = 255
	barCommandPrefix   = "/compa barras aplicar"
	barSlotsPerBar     = 12
	maxBarAdvice       = 10
	maxBarProposal     = 6
)

// reachableKeys are the keys the heuristic counts as easy to press: 1 to 6, Q, E, R, F and
// SHIFT-1 to SHIFT-4.
var reachableKeys = map[string]bool{
	"1": true, "2": true, "3": true, "4": true, "5": true, "6": true,
	"Q": true, "E": true, "R": true, "F": true,
	"SHIFT-1": true, "SHIFT-2": true, "SHIFT-3": true, "SHIFT-4": true,
}

// barSlot is one occupied slot of the action bars.
type barSlot struct {
	slot int
	kind string
	name string
	id   int64
	key  string
}

// barsOf reads the action bars of the active character. When they are not there or the addon
// reported them unavailable, ok is false and unav is the answer to return.
func readBars(ch charEntry) (slots []barSlot, visible []int, unav map[string]any, ok bool) {
	bars := asMap(ch.data["actionBars"])
	if bars == nil {
		return nil, nil, unavailable(HintBars), false
	}
	if v, has := bars["available"]; has && !truthy(v) {
		u := unavailable(HintBars)
		if r := str(bars["reason"]); r != "" {
			u["motivo"] = r
		}
		return nil, nil, u, false
	}
	for _, e := range entriesOf(bars["slots"]) {
		m := asMap(e.value)
		if m == nil {
			continue
		}
		slot := int(numberOf(m["slot"]))
		if slot == 0 {
			slot, _ = strconv.Atoi(e.key)
		}
		if slot < 1 || slot > maxBarSlots {
			continue
		}
		slots = append(slots, barSlot{
			slot: slot,
			kind: str(m["kind"]),
			name: str(m["name"]),
			id:   int64(numberOf(m["id"])),
			key:  str(m["key"]),
		})
	}
	if len(slots) == 0 {
		return nil, nil, unavailable(HintBars), false
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].slot < slots[j].slot })
	for _, v := range listOf(bars["visibleBars"]) {
		if b := int(numberOf(v)); b > 0 {
			visible = append(visible, b)
		}
	}
	if len(visible) == 0 {
		visible = []int{1}
	}
	return slots, visible, nil, true
}

// BarsAdvice returns the layout and what is wrong with it: duplicates, keys that are hard to
// reach, important spells that are not on the bars and a proposal of free slots for them.
func (g *Service) BarsAdvice() (any, error) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return unavailable(HintSync), nil
	}
	slots, visible, unav, ok := readBars(ch)
	if !ok {
		return unav, nil
	}
	known := readKnownSpells(ch.data["spells"])
	combats := parseCombats(ch.data["combats"])

	layout := make([]map[string]any, 0, len(slots))
	placedIDs := map[int64]bool{}
	placedNames := map[string]bool{}
	occupied := map[int]bool{}
	byID := map[int64][]int{}
	var far, noKey []map[string]any
	var noKeyNames []string
	for _, s := range slots {
		occupied[s.slot] = true
		item := map[string]any{"ranura": s.slot, "tipo": s.kind, "nombre": s.name}
		if s.id > 0 {
			item["id"] = s.id
		}
		if s.key != "" {
			item["tecla"] = s.key
		} else {
			item["tecla"] = ""
		}
		layout = append(layout, item)
		if s.kind == "spell" {
			placedIDs[s.id] = true
			placedNames[strings.ToLower(s.name)] = true
			byID[s.id] = append(byID[s.id], s.slot)
			if s.key == "" {
				noKey = append(noKey, map[string]any{"ranura": s.slot, "nombre": s.name})
				noKeyNames = append(noKeyNames, s.name)
			}
		}
		if s.key != "" && !reachableKeys[strings.ToUpper(s.key)] {
			far = append(far, map[string]any{"ranura": s.slot, "tecla": s.key, "nombre": s.name})
		}
	}

	var duplicates []map[string]any
	dupIDs := make([]int64, 0)
	for id, list := range byID {
		if len(list) > 1 {
			dupIDs = append(dupIDs, id)
		}
	}
	sort.Slice(dupIDs, func(i, j int) bool { return dupIDs[i] < dupIDs[j] })
	for _, id := range dupIDs {
		name := ""
		for _, s := range slots {
			if s.id == id && s.kind == "spell" {
				name = s.name
				break
			}
		}
		duplicates = append(duplicates, map[string]any{"nombre": name, "ranuras": byID[id]})
	}

	missing := missingSpells(known, placedIDs, placedNames, combats)
	proposal := proposeSlots(missing, occupied, visible)
	proposalViews := make([]map[string]any, 0, len(proposal))
	for _, p := range proposal {
		item := map[string]any{"ranura": p.slot, "id": p.spell.id, "nombre": p.spell.name}
		if key := defaultKey(p.slot); key != "" {
			item["teclaPorDefecto"] = key
		}
		proposalViews = append(proposalViews, item)
	}

	fields := map[string]any{
		"disposicion":    layout,
		"barrasVisibles": visible,
		"duplicados":     duplicates,
		"teclasLejanas":  far,
		"sinTecla":       noKey,
		"noColocados":    missingViews(missing, maxBarAdvice),
		"propuesta":      proposalViews,
		"criterio": "Teclas fáciles: 1 a 6, Q, E, R, F y SHIFT-1 a SHIFT-4. " +
			"«noColocados» son hechizos que usaste en combate o que son defensivos, de curación o de control y no están en las barras.",
	}
	if len(proposal) > 0 {
		fields["comandos"] = barCommands(proposalPairs(proposal))
	}
	return available(sourceSV, ch.updatedAt, fields), nil
}

// missingSpell is a known spell that is not on the bars but is worth placing.
type missingSpell struct {
	spell  knownSpell
	reason string
}

// missingSpells lists the spells not on the bars that were used in combat first, then the
// defensive, healing and control ones. Damage spells that were not used are left out here; the
// unused hint of revisar_combates covers them.
func missingSpells(known []knownSpell, placedIDs map[int64]bool, placedNames map[string]bool, combats []combatRec) []missingSpell {
	used := map[string]bool{}
	for _, c := range combats {
		for _, h := range c.dmg {
			used[strings.ToLower(h.name)] = true
		}
		for _, h := range c.heal {
			used[strings.ToLower(h.name)] = true
		}
	}
	var usedList, roleList []missingSpell
	for _, sp := range known {
		if placedIDs[sp.id] || placedNames[strings.ToLower(sp.name)] {
			continue
		}
		if used[strings.ToLower(sp.name)] {
			usedList = append(usedList, missingSpell{sp, "usado en combate"})
			continue
		}
		for _, role := range []string{"defensivo", "curación", "control"} {
			if sp.hasRole(role) {
				roleList = append(roleList, missingSpell{sp, role})
				break
			}
		}
	}
	return append(usedList, roleList...)
}

func missingViews(list []missingSpell, limit int) []map[string]any {
	out := make([]map[string]any, 0, limit)
	for i, m := range list {
		if i >= limit {
			break
		}
		out = append(out, map[string]any{"nombre": m.spell.name, "id": m.spell.id, "motivo": m.reason})
	}
	return out
}

// proposedSlot is one free slot the proposal fills with a missing spell.
type proposedSlot struct {
	slot  int
	spell knownSpell
}

// proposeSlots places the missing spells in free slots, the easy keys first: slots 1 to 6 of bar 1
// and 13 to 16 of bar 2, then the other free slots of the visible bars.
func proposeSlots(list []missingSpell, occupied map[int]bool, visible []int) []proposedSlot {
	show := map[int]bool{}
	for _, b := range visible {
		show[b] = true
	}
	var candidates []int
	for slot := 1; slot <= 6; slot++ {
		candidates = append(candidates, slot)
	}
	for slot := 13; slot <= 16; slot++ {
		candidates = append(candidates, slot)
	}
	for slot := 7; slot <= 12; slot++ {
		candidates = append(candidates, slot)
	}
	for slot := 17; slot <= 24; slot++ {
		candidates = append(candidates, slot)
	}
	var out []proposedSlot
	for _, slot := range candidates {
		if len(out) >= len(list) || len(out) >= maxBarProposal {
			break
		}
		if occupied[slot] || !show[barOf(slot)] {
			continue
		}
		out = append(out, proposedSlot{slot: slot, spell: list[len(out)].spell})
	}
	return out
}

// barOf is the bar a slot belongs to (1 for slots 1 to 12).
func barOf(slot int) int {
	return (slot-1)/barSlotsPerBar + 1
}

// defaultKey is the key a slot has by default in WoW, for the slots the proposal uses.
func defaultKey(slot int) string {
	switch {
	case slot >= 1 && slot <= 6:
		return strconv.Itoa(slot)
	case slot >= 13 && slot <= 16:
		return fmt.Sprintf("SHIFT-%d", slot-12)
	}
	return ""
}

// proposalPairs turns the proposal into slot:id pairs.
func proposalPairs(proposal []proposedSlot) []string {
	out := make([]string, 0, len(proposal))
	for _, p := range proposal {
		out = append(out, fmt.Sprintf("%d:%d", p.slot, p.spell.id))
	}
	return out
}

// BarRequest is one slot the assistant wants to fill: the slot number and the spell, by spell id
// or by name.
type BarRequest struct {
	Slot    int64
	Spell   string
	SpellID int64
}

// BarCommands validates a proposed layout against the spellbook and returns the
// /compa barras aplicar commands. Each command fits in maxBarCommandChars and holds at most
// maxBarPairs slot:spellID pairs in total.
func (g *Service) BarCommands(reqs []BarRequest) (any, error) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return unavailable(HintSync), nil
	}
	known := readKnownSpells(ch.data["spells"])
	if len(known) == 0 {
		return unavailable("No tengo tu libro de hechizos: escribe /compa sync en el juego"), nil
	}
	byID := map[int64]knownSpell{}
	byName := map[string]knownSpell{}
	for _, sp := range known {
		byID[sp.id] = sp
		key := strings.ToLower(sp.name)
		if prev, ok := byName[key]; !ok || sp.id > prev.id {
			byName[key] = sp
		}
	}

	var pairs []string
	var errs []string
	usedSlots := map[int64]bool{}
	for _, r := range reqs {
		if r.Slot < 1 || r.Slot > maxBarSlots {
			errs = append(errs, fmt.Sprintf("ranura %d fuera de 1 a %d", r.Slot, maxBarSlots))
			continue
		}
		if usedSlots[r.Slot] {
			errs = append(errs, fmt.Sprintf("la ranura %d aparece dos veces", r.Slot))
			continue
		}
		var sp knownSpell
		var found bool
		label := r.Spell
		if r.SpellID > 0 {
			sp, found = byID[r.SpellID]
			label = fmt.Sprint(r.SpellID)
		} else {
			sp, found = byName[strings.ToLower(strings.TrimSpace(r.Spell))]
		}
		if !found {
			errs = append(errs, fmt.Sprintf("«%s» no está en tu libro de hechizos (ranura %d)", label, r.Slot))
			continue
		}
		usedSlots[r.Slot] = true
		pairs = append(pairs, fmt.Sprintf("%d:%d", r.Slot, sp.id))
	}
	if len(pairs) > maxBarPairs {
		errs = append(errs, fmt.Sprintf("máximo %d pares por distribución: se dejan fuera %d", maxBarPairs, len(pairs)-maxBarPairs))
		pairs = pairs[:maxBarPairs]
	}
	return available(sourceSV, ch.updatedAt, map[string]any{
		"comandos": barCommands(pairs),
		"pares":    len(pairs),
		"errores":  errs,
		"aviso":    "Solo fuera de combate. /compa barras deshacer vuelve a la distribución anterior.",
	}), nil
}

// barCommands joins the pairs into commands of at most maxBarCommandChars characters.
func barCommands(pairs []string) []string {
	var out []string
	cur := ""
	for _, p := range pairs {
		switch {
		case cur == "":
			cur = barCommandPrefix + " " + p
		case len(cur)+1+len(p) <= maxBarCommandChars:
			cur += "," + p
		default:
			out = append(out, cur)
			cur = barCommandPrefix + " " + p
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
