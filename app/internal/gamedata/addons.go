package gamedata

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Readers of the blocks that the addon exports from the other addons (WhatsTraining,
// RXPGuides, SkillUpForever) and from its own diagnostics. Each block is optional: a
// missing block, or one that says available=false, answers with a Spanish hint, never with
// an error. The contract is in the addon's Integrations.lua.

const (
	hintTraining = "No tengo datos de entrenamiento: activa WhatsTraining en el addon y escribe /compa sync en el juego"
	hintGuide    = "No tengo datos de la guía: activa RXPGuides en el addon y escribe /compa sync en el juego"
	hintProfPlan = "No tengo la ruta de profesiones: activa SkillUpForever en el addon y escribe /compa sync en el juego"
	hintHealth   = "El addon todavía no ha informado de su estado: escribe /compa sync en el juego"

	maxRouteSteps = 5 // recipes per profession, as the addon exports them
	maxRouteItems = 20
	maxTrainers   = 3 // trainers listed by que_entrenar
	maxGuideText  = 160
)

// addonBlock returns the block `key` of the active character when it is present and
// available. Otherwise block is nil and unav is the unavailable answer to return: the
// hint, plus the addon's own reason when it gave one.
func (g *Service) addonBlock(key, hint string) (block map[string]any, at time.Time, unav map[string]any) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return nil, time.Time{}, unavailable(HintSync)
	}
	m := asMap(ch.data[key])
	if m == nil {
		return nil, ch.updatedAt, unavailable(hint)
	}
	if !truthy(m["available"]) {
		u := unavailable(hint)
		if r := str(m["reason"]); r != "" {
			u["motivo"] = r
		}
		return nil, ch.updatedAt, u
	}
	at = unixTime(numberOf(m["exportedAt"]))
	if at.IsZero() {
		at = ch.updatedAt
	}
	return m, at, nil
}

// ---- WhatsTraining: spells ---------------------------------------------------

// spellRow is one trainable spell as the addon exports it. cost is in copper.
type spellRow struct {
	id     int64
	name   string
	rank   string
	level  int
	cost   int64
	status string // AVAILABLE, NEXTLEVEL or OTHER
}

// readSpells decodes the spells, keeps at most maxSpells and orders them: available now
// first, then the next level, then the rest; by level and name inside each group.
func readSpells(v any) []spellRow {
	var out []spellRow
	for _, item := range listOf(v) {
		m := asMap(item)
		if m == nil || str(m["name"]) == "" {
			continue
		}
		out = append(out, spellRow{
			id:     int64(numberOf(m["spellID"])),
			name:   str(m["name"]),
			rank:   str(m["rank"]),
			level:  int(numberOf(m["level"])),
			cost:   int64(numberOf(m["cost"])),
			status: str(m["status"]),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := statusOrder(out[i].status), statusOrder(out[j].status)
		if a != b {
			return a < b
		}
		if out[i].level != out[j].level {
			return out[i].level < out[j].level
		}
		return out[i].name < out[j].name
	})
	if len(out) > maxSpells {
		out = out[:maxSpells]
	}
	return out
}

// maxSpells is the most spells the addon exports (see the contract).
const maxSpells = 40

func statusOrder(s string) int {
	switch s {
	case "AVAILABLE":
		return 0
	case "NEXTLEVEL":
		return 1
	}
	return 2
}

// spellView is one spell in an answer.
func spellView(sp spellRow) map[string]any {
	m := map[string]any{
		"spellID":    sp.id,
		"nombre":     sp.name,
		"nivel":      sp.level,
		"coste":      sp.cost,
		"costeTexto": moneyText(sp.cost),
	}
	if sp.rank != "" {
		m["rango"] = sp.rank
	}
	return m
}

// spellViews converts a list, in order.
func spellViews(list []spellRow) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, sp := range list {
		out = append(out, spellView(sp))
	}
	return out
}

// sumCost is the total cost of the spells in copper.
func sumCost(list []spellRow) int64 {
	var total int64
	for _, sp := range list {
		total += sp.cost
	}
	return total
}

// Training returns every spell the addon exported, with its status.
func (g *Service) Training() (any, error) {
	blk, at, unav := g.addonBlock("training", hintTraining)
	if blk == nil {
		return unav, nil
	}
	spells := readSpells(blk["spells"])
	list := make([]map[string]any, 0, len(spells))
	for _, sp := range spells {
		v := spellView(sp)
		v["estado"] = sp.status
		list = append(list, v)
	}
	return available("savedvariables", at, map[string]any{"hechizos": list, "total": len(list)}), nil
}

// TrainingPlan answers "what do I train now": the spells available at the current level
// with their cost, the next level's spells, whether the player can pay, and where the
// trainer of the player's class is (from Questie or the knowledge store) when known.
func (g *Service) TrainingPlan() (any, error) {
	blk, at, unav := g.addonBlock("training", hintTraining)
	if blk == nil {
		return unav, nil
	}
	now, next := splitTraining(readSpells(blk["spells"]))
	clase := g.className()
	out := map[string]any{
		"clase":           clase,
		"ahora":           spellViews(now),
		"costeAhora":      sumCost(now),
		"costeAhoraTexto": moneyText(sumCost(now)),
	}
	if len(now) == 0 {
		out["nota"] = "No tienes hechizos de entrenamiento nuevos para tu nivel."
	}
	if len(next) > 0 {
		out["proximoNivel"] = map[string]any{
			"nivel":      minLevel(next),
			"hechizos":   spellViews(next),
			"coste":      sumCost(next),
			"costeTexto": moneyText(sumCost(next)),
		}
	}
	if mo, err := g.Money(); err == nil {
		if m := asMap(mo); truthy(m["available"]) {
			copper := int64(numberOf(m["copper"]))
			out["dinero"] = moneyText(copper)
			if len(now) > 0 {
				out["puedesPagarAhora"] = copper >= sumCost(now)
			}
		}
	}
	if trs := trainerViewsFor(g.knownTrainers(), g.playerPosition(), clase); clase != "" && len(trs) > 0 {
		if len(trs) > maxTrainers {
			trs = trs[:maxTrainers]
		}
		out["entrenadores"] = trs
	} else {
		out["nota"] = joinNotes(out["nota"], noClassTrainerNote(clase))
	}
	return available("savedvariables", at, out), nil
}

// splitTraining separates the spells available now from those of the next level.
func splitTraining(spells []spellRow) (now, next []spellRow) {
	for _, sp := range spells {
		switch sp.status {
		case "AVAILABLE":
			now = append(now, sp)
		case "NEXTLEVEL":
			next = append(next, sp)
		}
	}
	return now, next
}

func minLevel(list []spellRow) int {
	lowest := 0
	for _, sp := range list {
		if lowest == 0 || sp.level < lowest {
			lowest = sp.level
		}
	}
	return lowest
}

// className is the class of the active character, as the addon reports it.
func (g *Service) className() string {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return ""
	}
	return str(asMap(ch.data["character"])["class"])
}

// trainingSummary is the compact view for the player context: counts and the top spell.
func trainingSummary(g *Service) map[string]any {
	blk, _, _ := g.addonBlock("training", hintTraining)
	if blk == nil {
		return nil
	}
	now, next := splitTraining(readSpells(blk["spells"]))
	if len(now) == 0 && len(next) == 0 {
		return nil
	}
	out := map[string]any{"ahora": len(now), "costeAhora": moneyText(sumCost(now))}
	if len(now) > 0 {
		out["top"] = spellLabel(now[0])
	}
	if len(next) > 0 {
		out["proximoNivel"] = minLevel(next)
	}
	return out
}

func spellLabel(sp spellRow) string {
	if sp.rank == "" {
		return sp.name
	}
	return fmt.Sprintf("%s (%s)", sp.name, sp.rank)
}

// ---- RXPGuides: current step -------------------------------------------------

// Guide returns the RXPGuides step the player is on, with its place and the mark commands.
func (g *Service) Guide() (any, error) {
	blk, at, unav := g.addonBlock("guide", hintGuide)
	if blk == nil {
		return unav, nil
	}
	out := map[string]any{
		"guia":      str(blk["guideName"]),
		"grupo":     str(blk["group"]),
		"paso":      int(numberOf(blk["stepIndex"])),
		"idPaso":    str(blk["stepId"]),
		"marcaGuia": "/compa marca guia",
	}
	if txt := str(blk["stepText"]); txt != "" {
		out["texto"] = txt
	}
	pos := g.playerPosition()
	mapID, x, y, ok := readPoint(map[string]any{"mapID": blk["stepMapID"], "x": blk["stepX"], "y": blk["stepY"]})
	if !ok {
		out["nota"] = "El paso no trae coordenadas: sigue el texto del paso o mira la guía en el juego."
		return available("savedvariables", at, out), nil
	}
	sp := spawn{MapID: mapID, X: x, Y: y}
	out["mapId"] = mapID
	if _, sv, okc := g.activeCharacter(); okc {
		if zona, err := g.zoneName(mapID, sv); err == nil && zona != "" {
			out["zona"] = zona
		}
	}
	out["x"], out["y"] = round1(x*100), round1(y*100)
	out["coords"] = coordsText(x, y)
	n, _ := nearest([]spawn{sp}, pos)
	out["mismoMapa"] = n.SameMap
	if n.SameMap {
		out["distancia"] = n.Distance
	}
	out["marca"] = markCommandFor(sp, pos)
	return available("savedvariables", at, out), nil
}

// markCommandFor is /compa marca with x and y only when the player is on the spawn's map,
// and with the map id otherwise, so that the mark lands on the right map.
func markCommandFor(s spawn, pos position) string {
	if pos.OK && pos.MapID == s.MapID {
		return fmt.Sprintf("/compa marca %.1f %.1f", s.X*100, s.Y*100)
	}
	return fmt.Sprintf("/compa marca %d %.1f %.1f", s.MapID, s.X*100, s.Y*100)
}

// guideSummary is the compact view of the guide for the player context.
func guideSummary(g *Service) map[string]any {
	blk, _, _ := g.addonBlock("guide", hintGuide)
	if blk == nil {
		return nil
	}
	out := map[string]any{"guia": str(blk["guideName"]), "paso": int(numberOf(blk["stepIndex"])), "marcaGuia": "/compa marca guia"}
	if txt := str(blk["stepText"]); txt != "" {
		out["texto"] = trimText(txt, maxGuideText)
	}
	return out
}

// ---- SkillUpForever: profession routes ----------------------------------------

// profEntry is a profession of the player's own list, keyed by its skill line.
type profEntry struct {
	name      string
	rank, max int
}

// blockProfessions indexes the player's professions by skill line id.
func (g *Service) blockProfessions() map[int64]profEntry {
	out := map[int64]profEntry{}
	pr, err := g.Professions()
	if err != nil {
		return out
	}
	for _, p := range listOf(asMap(pr)["professions"]) {
		m := asMap(p)
		if m == nil {
			continue
		}
		out[int64(numberOf(m["skillLineID"]))] = profEntry{
			name: str(m["name"]),
			rank: int(numberOf(m["rank"])),
			max:  int(numberOf(m["maxRank"])),
		}
	}
	return out
}

// profPlanByID returns the SkillUpForever professions of the active character by skill line.
// It is empty when the block is missing or unavailable.
func profPlanByID(g *Service) map[int64]map[string]any {
	out := map[int64]map[string]any{}
	blk, _, _ := g.addonBlock("profPlan", hintProfPlan)
	if blk == nil {
		return out
	}
	for _, p := range listOf(blk["professions"]) {
		if pm := asMap(p); pm != nil {
			if id := int64(numberOf(pm["skillLineID"])); id > 0 {
				out[id] = pm
			}
		}
	}
	return out
}

// routeSummary is the short view of one profession's route for plan_profesiones: how many
// steps, how many items to buy, and the next recipe.
func routeSummary(plans map[int64]map[string]any, id int64) (map[string]any, bool) {
	pm, ok := plans[id]
	if !ok {
		return nil, false
	}
	steps := listOf(pm["nextSteps"])
	out := map[string]any{"pasos": len(steps), "compras": len(listOf(pm["shopping"]))}
	if len(steps) > 0 {
		out["siguiente"] = stepLabel(asMap(steps[0]))
	}
	return out, true
}

// routeOverview is the compact view of all routes for the player context.
func routeOverview(g *Service) map[string]any {
	blk, _, _ := g.addonBlock("profPlan", hintProfPlan)
	if blk == nil {
		return nil
	}
	professions, steps, items := 0, 0, 0
	first := ""
	for _, p := range listOf(blk["professions"]) {
		pm := asMap(p)
		if pm == nil {
			continue
		}
		n := len(listOf(pm["nextSteps"]))
		if n > 0 {
			professions++
			steps += n
			if first == "" {
				first = str(pm["name"]) + ": " + stepLabel(asMap(listOf(pm["nextSteps"])[0]))
			}
		}
		items += len(listOf(pm["shopping"]))
	}
	if professions == 0 && items == 0 {
		return nil
	}
	out := map[string]any{"profesiones": professions, "pasos": steps, "compras": items}
	if first != "" {
		out["siguiente"] = first
	}
	return out
}

// stepLabel is "Recipe (68→75)".
func stepLabel(s map[string]any) string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("%s (%d→%d)", str(s["recipe"]), int(numberOf(s["fromSkill"])), int(numberOf(s["toSkill"])))
}

// ProfessionRoute answers "how do I level this profession": the next recipes with their
// materials, the shopping list, and the trainers and advice from plan_profesiones. With no
// name or skill line id it lists every route. Names match the addon's (Spanish) name or the
// game's name of the profession.
func (g *Service) ProfessionRoute(name string, skillLineID int64) (any, error) {
	blk, at, unav := g.addonBlock("profPlan", hintProfPlan)
	if blk == nil {
		return unav, nil
	}
	own := g.blockProfessions()
	trainers := g.knownTrainers()
	pos := g.playerPosition()
	needle := strings.ToLower(strings.TrimSpace(name))
	filtered := needle != "" || skillLineID > 0

	routes := []map[string]any{}
	names := []string{}
	for _, p := range listOf(blk["professions"]) {
		pm := asMap(p)
		if pm == nil {
			continue
		}
		id := int64(numberOf(pm["skillLineID"]))
		local := own[id]
		names = append(names, str(pm["name"]))
		if skillLineID > 0 && id != skillLineID {
			continue
		}
		if needle != "" && !professionMatches(needle, str(pm["name"]), local.name) {
			continue
		}
		routes = append(routes, buildRoute(pm, local, trainers, pos))
	}
	if !filtered {
		return available("savedvariables", at, map[string]any{"profesiones": routes, "total": len(routes)}), nil
	}
	if len(routes) == 0 {
		return available("savedvariables", at, map[string]any{
			"encontrada":  false,
			"disponibles": names,
			"nota":        notFoundNote(name, skillLineID, names),
		}), nil
	}
	out := copyMap(routes[0])
	out["encontrada"] = true
	return available("savedvariables", at, out), nil
}

// buildRoute is one profession's route: steps, shopping list, trainers and training advice.
func buildRoute(pm map[string]any, local profEntry, trainers []trainerRef, pos position) map[string]any {
	name := str(pm["name"])
	if name == "" {
		name = local.name
	}
	rank := int(numberOf(pm["rank"]))
	if pm["rank"] == nil {
		rank = local.rank
	}
	maxRank := int(numberOf(pm["maxRank"]))
	if pm["maxRank"] == nil {
		maxRank = local.max
	}
	r := map[string]any{
		"nombre":      name,
		"skillLineID": int64(numberOf(pm["skillLineID"])),
		"nivel":       rank,
		"max":         maxRank,
	}
	steps := []map[string]any{}
	for _, item := range listOf(pm["nextSteps"]) {
		if len(steps) >= maxRouteSteps {
			break
		}
		sm := asMap(item)
		if sm == nil {
			continue
		}
		steps = append(steps, map[string]any{
			"receta":     str(sm["recipe"]),
			"desde":      int(numberOf(sm["fromSkill"])),
			"hasta":      int(numberOf(sm["toSkill"])),
			"materiales": itemViews(sm["materials"], maxRouteItems),
		})
	}
	r["pasos"] = steps
	r["compras"] = itemViews(pm["shopping"], maxRouteItems)
	r["entrenarYa"], r["consejo"] = trainAdvice(rank, maxRank)

	names := []string{name}
	if local.name != "" && local.name != name {
		names = append(names, local.name)
	}
	trs := trainerViewsFor(trainers, pos, names...)
	r["entrenadores"] = trs
	if len(trs) == 0 {
		r["nota"] = noTrainerNote(name)
	}
	return r
}

// itemViews lists addon items (name, count, itemID) up to limit.
func itemViews(v any, limit int) []map[string]any {
	out := []map[string]any{}
	for _, item := range listOf(v) {
		if len(out) >= limit {
			break
		}
		m := asMap(item)
		if m == nil {
			continue
		}
		out = append(out, map[string]any{
			"nombre":   str(m["name"]),
			"cantidad": int(numberOf(m["count"])),
			"itemID":   int64(numberOf(m["itemID"])),
		})
	}
	return out
}

// professionMatches reports whether the typed name is part of one of the profession names.
func professionMatches(needle string, names ...string) bool {
	for _, n := range names {
		l := strings.ToLower(strings.TrimSpace(n))
		if l == "" {
			continue
		}
		if l == needle || strings.Contains(l, needle) || strings.Contains(needle, l) {
			return true
		}
	}
	return false
}

func notFoundNote(name string, id int64, names []string) string {
	if len(names) == 0 {
		return "SkillUpForever no ha exportado ninguna profesión todavía: escribe /compa sync en el juego."
	}
	label := name
	if label == "" {
		label = fmt.Sprintf("la profesión %d", id)
	}
	return fmt.Sprintf("No tengo la ruta de «%s». Pregunta por una de: %s.", label, strings.Join(names, ", "))
}

// ---- trainers and training advice --------------------------------------------

// trainAdvice says whether to train the next rank now, and why, from the rank and cap.
func trainAdvice(rank, maxRank int) (bool, string) {
	left := maxRank - rank
	switch {
	case maxRank <= 0:
		return false, "No conozco el máximo de esta profesión; míralo en el juego."
	case left <= nearMaxRankLeft:
		return true, fmt.Sprintf("Te faltan %d puntos para el máximo (%d): entrena el siguiente rango ya.", left, maxRank)
	}
	return false, fmt.Sprintf("Te faltan %d puntos para el máximo (%d): puedes esperar a acercarte.", left, maxRank)
}

// trainerViews lists the known trainers whose kind or name match one of the names.
func trainerViews(trainers []trainerRef, pos position, name string) []map[string]any {
	return trainerViewsFor(trainers, pos, name)
}

// trainerViewsFor lists each known trainer once, when it matches any of the names. The
// result is never nil.
func trainerViewsFor(trainers []trainerRef, pos position, names ...string) []map[string]any {
	out := []map[string]any{}
	for _, t := range trainers {
		for _, n := range names {
			if n != "" && t.matches(n) {
				out = append(out, t.view(pos))
				break
			}
		}
	}
	return out
}

func noTrainerNote(name string) string {
	return "No tengo entrenador guardado para " + name + ". Escribe /compa sync cerca del entrenador o busca en la web."
}

func noClassTrainerNote(class string) string {
	if class == "" {
		return "No sé la clase del personaje: escribe /compa sync en el juego."
	}
	return "No tengo el entrenador de " + class + " guardado. Escribe /compa sync cerca de él o busca en la web."
}

// joinNotes joins a previous note (any value, nil included) and a new one.
func joinNotes(prev any, next string) string {
	if s, ok := prev.(string); ok && s != "" {
		return s + " " + next
	}
	return next
}

// ---- diagnostics ----------------------------------------------------------------

// Health returns the addon's own counter of swallowed errors (pcall failures) and the last
// one, for diagnosis.
func (g *Service) Health() (any, error) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return unavailable(HintSync), nil
	}
	h := asMap(ch.data["health"])
	if h == nil {
		return unavailable(hintHealth), nil
	}
	fields := map[string]any{"erroresInternos": int64(numberOf(h["luaErrors"]))}
	if last := str(h["lastError"]); last != "" {
		fields["ultimoError"] = last
	}
	return available("savedvariables", ch.updatedAt, fields), nil
}

// ---- text helpers ------------------------------------------------------------------

// moneyText writes copper in the largest units, in Spanish: "1 oro, 20 plata".
func moneyText(copper int64) string {
	if copper == 0 {
		return "0 cobre"
	}
	neg := copper < 0
	if neg {
		copper = -copper
	}
	gold, silver, cu := copper/10000, (copper%10000)/100, copper%100
	var parts []string
	if gold > 0 {
		parts = append(parts, fmt.Sprintf("%d oro", gold))
	}
	if silver > 0 {
		parts = append(parts, fmt.Sprintf("%d plata", silver))
	}
	if cu > 0 {
		parts = append(parts, fmt.Sprintf("%d cobre", cu))
	}
	text := strings.Join(parts, ", ")
	if neg {
		return "-" + text
	}
	return text
}

// trimText shortens s to at most n runes, ending with an ellipsis when it cut.
func trimText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
