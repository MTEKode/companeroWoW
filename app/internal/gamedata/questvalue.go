package gamedata

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// Quest value (valor_misiones): how much XP (or gold) each accessible quest gives per minute,
// and a route through them. The XP is the addon's own number when the knowledge store has it
// (GetRewardXP, captured when the quest window was open); otherwise it is estimated from the
// quest level and the player's level. The minutes are estimated: walking between the stops at
// an approximate speed plus the kills at the time-to-kill. Every number says where it comes from.

const (
	// questYardsPerUnit is the approximate size of one map unit (0..100 across a zone) in yards.
	questYardsPerUnit = 12.0
	// questSpeedYPS is the walking speed in yards per second used for the travel estimate.
	questSpeedYPS = 7.0
	// questMapHopSec is the time added when two stops are on different maps (roads, portals).
	questMapHopSec = 90.0
	// questDefaultKillSec is the time-to-kill used when there is neither a hook nor observed fights.
	questDefaultKillSec = 20.0
	// questMinMinutes floors the time of a quest so that a zero-distance quest does not divide by zero.
	questMinMinutes = 1.0
	// questTopN is how many quests the ranking lists.
	questTopN = 12
	// questZonePenalty is the extra distance (in map units) a route pays to change zone, so that
	// the route finishes one area before it moves on.
	questZonePenalty = 25.0
)

// KillTimeFunc gives the expected seconds to kill one mob of the given level, from a model such
// as the theory engine. ok is false when the model has no answer. Set it with SetKillTime.
type KillTimeFunc func(level int) (seconds float64, ok bool)

// SetKillTime connects a time-to-kill model for valor_misiones and the mob verdict. Nil removes it.
func (g *Service) SetKillTime(f KillTimeFunc) {
	g.killTime = f
}

// questStop is a place a quest needs the player to go to.
type questStop struct {
	MapID int
	X, Y  float64
}

// questCandidate is one quest the player can do now, with what the estimate needs.
type questCandidate struct {
	ID          int64
	Name        string
	Zone        string
	Level       int
	PlayerLevel int
	InLog       bool
	Starter     *questStop // where it is accepted, when not in the log yet
	Finisher    *questStop // where it is turned in
	Objectives  []questStop
	Kills       int
	XP          int     // from the addon when XPKnown
	XPKnown     bool    // true when XP is the addon's number, false when estimated
	Copper      int64   // gold reward in copper, when known
	HasCopper   bool    //
	KillSec     float64 // seconds per kill for this quest
}

// questEstimate is the value of one quest.
type questEstimate struct {
	Candidate  questCandidate
	TravelSec  float64
	CombatSec  float64
	Minutes    float64
	XPPerMin   float64
	CopperMin  float64
	NoCoords   bool
	PrimaryPos *questStop // the stop the player goes to first; nil when the quest has no place
}

// classicQuestXP estimates the XP of a quest from its level and the player's level. It is a
// rough approximation of the usual curve (about 60 XP per quest level at full value), reduced
// when the quest is well below the player's level. The result is always labelled "estimado".
func classicQuestXP(questLevel, playerLevel int) int {
	if questLevel <= 0 {
		return 0
	}
	base := 60.0 * float64(questLevel)
	factor := 1.0
	if diff := playerLevel - questLevel; diff > 4 {
		factor = math.Max(0.1, 1-0.1*float64(diff-4))
	}
	return int(math.Round(base*factor/5) * 5)
}

// dist returns the distance between two stops in map units, and whether they share a map.
// Stops on different maps have no distance; the caller adds questMapHopSec for them.
func dist(a, b questStop) (units float64, sameMap bool) {
	if a.MapID != b.MapID {
		return 0, false
	}
	return math.Hypot(a.X-b.X, a.Y-b.Y) * 100, true
}

// travelSeconds is the time to walk from a to b: the distance at walking speed, or the map hop.
func travelSeconds(a, b questStop) float64 {
	units, same := dist(a, b)
	if !same {
		return questMapHopSec
	}
	return units * questYardsPerUnit / questSpeedYPS
}

// estimateQuest computes the travel and combat time of one quest from the player's position
// pos: the walk to the starter when it is not in the log, the objectives (nearest first) and
// the turn-in. Missing coordinates make the travel zero and the estimate is marked NoCoords.
func estimateQuest(c questCandidate, pos questStop, hasPos bool) questEstimate {
	e := questEstimate{Candidate: c}
	// The visiting order: the starter when the quest is not in the log yet, the objectives
	// (nearest first, from the last stop), then the turn-in.
	var seq []questStop
	cur := pos
	if !hasPos {
		cur = questStop{MapID: -1}
	}
	if !c.InLog && c.Starter != nil {
		seq = append(seq, *c.Starter)
		cur = *c.Starter
	}
	objs := append([]questStop(nil), c.Objectives...)
	for len(objs) > 0 {
		best := 0
		for i := range objs {
			if travelSeconds(cur, objs[i]) < travelSeconds(cur, objs[best]) {
				best = i
			}
		}
		seq = append(seq, objs[best])
		cur = objs[best]
		objs = append(objs[:best], objs[best+1:]...)
	}
	if c.Finisher != nil {
		seq = append(seq, *c.Finisher)
	}
	e.NoCoords = len(seq) == 0
	// The walk starts at the player. Without a position, the first stop is where the walk
	// starts, so it costs nothing.
	walk := pos
	if !hasPos {
		if len(seq) > 0 {
			walk = seq[0]
			seq = seq[1:]
		}
	}
	for _, s := range seq {
		e.TravelSec += travelSeconds(walk, s)
		walk = s
	}
	e.CombatSec = float64(c.Kills) * c.KillSec
	e.Minutes = math.Max((e.TravelSec+e.CombatSec)/60, questMinMinutes)
	e.XPPerMin = float64(c.XP) / e.Minutes
	if c.HasCopper {
		e.CopperMin = float64(c.Copper) / e.Minutes
	}
	// The place the player goes to first, for the route and the marca command.
	switch {
	case !c.InLog && c.Starter != nil:
		s := *c.Starter
		e.PrimaryPos = &s
	case len(c.Objectives) > 0:
		s := c.Objectives[0]
		e.PrimaryPos = &s
	case c.Finisher != nil:
		s := *c.Finisher
		e.PrimaryPos = &s
	}
	return e
}

// orderRoute visits the estimates with a nearest-neighbour route from start. Changing zone costs
// questZonePenalty extra units, so the route finishes an area first. It returns the indexes of
// es in visiting order; estimates without a primary position are left out.
func orderRoute(start questStop, hasStart bool, es []questEstimate) []int {
	var todo []int
	for i, e := range es {
		if e.PrimaryPos != nil {
			todo = append(todo, i)
		}
	}
	var order []int
	cur := start
	curZone := ""
	if !hasStart {
		cur = questStop{MapID: -1}
	}
	for len(todo) > 0 {
		best, bestCost := -1, math.MaxFloat64
		for k, i := range todo {
			p := *es[i].PrimaryPos
			cost := routeCost(cur, p)
			if es[i].Candidate.Zone != curZone {
				cost += questZonePenalty
			}
			if cost < bestCost || (cost == bestCost && best >= 0 && es[i].Candidate.ID < es[todo[best]].Candidate.ID) {
				best, bestCost = k, cost
			}
		}
		i := todo[best]
		order = append(order, i)
		cur = *es[i].PrimaryPos
		curZone = es[i].Candidate.Zone
		todo = append(todo[:best], todo[best+1:]...)
	}
	return order
}

// routeCost is the distance used to choose the next stop: map units on the same map, and a
// large fixed cost across maps so that the route stays on one map while it can.
func routeCost(a, b questStop) float64 {
	if a.MapID < 0 {
		return 0
	}
	units, same := dist(a, b)
	if !same {
		return 1000 + units
	}
	return units
}

// killSecondsFor returns the seconds per kill for a level: the model's answer when it has one,
// else the median of the fights that were won in the recorded combats, else the default.
// source says which one it was.
func (g *Service) killSecondsFor(level int, wonFights []float64) (float64, string) {
	if g.killTime != nil {
		if s, ok := g.killTime(level); ok && s > 0 {
			return s, "modelo"
		}
	}
	if len(wonFights) > 0 {
		return median(wonFights), "observado"
	}
	return questDefaultKillSec, "supuesto"
}

// killsRe reads the number of kills from an objective text such as "Kill 8 Boars" or
// "Mata 8 jabalíes". It is the fallback when the quest has no kill objective from Questie.
var killsRe = regexp.MustCompile(`(?i)(?:kill|slay|mata[rs]?|derrota[rs]?|elimina[rs]?)\s+(\d+)`)

// killsFromText returns the kills an objective text asks for, 0 when it names none.
func killsFromText(text string) int {
	m := killsRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(m[1], "%d", &n)
	return n
}

// wonFightSeconds lists the durations of the recorded fights that the player won.
func (g *Service) wonFightSeconds() []float64 {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return nil
	}
	var out []float64
	for _, c := range parseCombats(ch.data["combats"]) {
		if !c.died && c.duration > 0 {
			out = append(out, c.duration)
		}
	}
	return out
}

// QuestValueOptions are the arguments of valor_misiones.
type QuestValueOptions struct {
	Ventana string // ahora (default), pronto or ambas
	Zona    string // optional: only the quests of this zone
	Oro     bool   // rank by gold per minute instead of XP per minute
}

// QuestValue answers valor_misiones: the accessible quests ranked by XP (or gold) per estimated
// minute, and a route through them.
func (g *Service) QuestValue(opts QuestValueOptions) (any, error) {
	wins, err := windowsOf(opts.Ventana)
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
	killSecs, killSrc := g.killSecondsFor(gd.Level, g.wonFightSeconds())
	qx := g.questieOfActive()
	zoneNeedle := fold(opts.Zona)

	var ests []questEstimate
	for _, id := range sortedQuestIDs(gd.Quests) {
		q := gd.Quests[id]
		if gd.Completed[id] {
			continue
		}
		win := winSoon
		if q.Accessible || q.InLog {
			win = winNow
		}
		if !wins[win] {
			continue
		}
		if zoneNeedle != "" && !strings.Contains(fold(q.Zone), zoneNeedle) {
			continue
		}
		c := g.questCandidateOf(gd, q, qx, pos, killSecs)
		ests = append(ests, estimateQuest(c, questStop{MapID: pos.MapID, X: pos.X, Y: pos.Y}, pos.OK))
	}

	criterio := "xp por minuto"
	if opts.Oro {
		criterio = "oro por minuto"
	}
	sort.SliceStable(ests, func(i, j int) bool {
		if opts.Oro {
			return ests[i].CopperMin > ests[j].CopperMin
		}
		return ests[i].XPPerMin > ests[j].XPPerMin
	})
	ranking := make([]map[string]any, 0, questTopN)
	for i, e := range ests {
		if i == questTopN {
			break
		}
		ranking = append(ranking, questEstimateView(e))
	}

	route := []map[string]any{}
	order := orderRoute(questStop{MapID: pos.MapID, X: pos.X, Y: pos.Y}, pos.OK, ests)
	for n, i := range order {
		if n == questTopN {
			break
		}
		e := ests[i]
		p := e.PrimaryPos
		route = append(route, map[string]any{
			"orden":  n + 1,
			"id":     e.Candidate.ID,
			"titulo": e.Candidate.Name,
			"zona":   e.Candidate.Zone,
			"mapId":  p.MapID,
			"x":      round1(p.X * 100),
			"y":      round1(p.Y * 100),
			"marca":  marcaCommand(spawn{MapID: p.MapID, X: p.X, Y: p.Y}, e.Candidate.Name),
		})
	}

	supuestos := []string{
		fmt.Sprintf("Caminar a %.0f yardas por segundo y 1 unidad del mapa ≈ %.0f yardas (estimado).", questSpeedYPS, questYardsPerUnit),
		fmt.Sprintf("Un cambio de mapa cuenta %.0f s. Cada matanza cuenta %.0f s (%s).", questMapHopSec, killSecs, killSrc),
		"El XP es el del addon cuando lo ha visto; si no, se estima por nivel de misión frente a tu nivel («estimado»).",
	}
	out := map[string]any{
		"criterio":  criterio,
		"ranking":   ranking,
		"ruta":      route,
		"supuestos": supuestos,
		"total":     len(ests),
		"texto":     questValueText(ests, criterio),
	}
	if len(ests) == 0 {
		out["nota"] = "No hay misiones accesibles con esos filtros en los datos del addon."
	}
	return available(sourceSV, gd.At, out), nil
}

// questCandidateOf builds the estimate input of a quest from the window data, the Questie
// export and the knowledge store.
func (g *Service) questCandidateOf(gd gearData, q windowQuest, qx questieExport, pos position, killSecs float64) questCandidate {
	c := questCandidate{
		ID: q.ID, Name: q.Name, Zone: q.Zone, Level: q.Level, PlayerLevel: gd.Level,
		InLog: q.InLog, KillSec: killSecs,
	}
	if c.Level == 0 {
		c.Level = q.RequiredLevel
	}
	if q.Starter.OK && !q.InLog {
		c.Starter = &questStop{MapID: q.Starter.MapID, X: q.Starter.X, Y: q.Starter.Y}
	}
	if q.Finisher.OK {
		c.Finisher = &questStop{MapID: q.Finisher.MapID, X: q.Finisher.X, Y: q.Finisher.Y}
	}
	// Objectives from Questie: the spawn nearest to the player of each objective, and the kills it asks for.
	if qq, ok := qx.quest(q.ID); ok {
		for _, o := range qq.Objectives {
			if isKillKind(o.Kind) && o.Required > 0 {
				c.Kills += o.Required
			}
			if len(o.Spawns) > 0 {
				s := o.Spawns[0]
				if n, found := nearest(o.Spawns, pos); found {
					s = n.Spawn
				}
				c.Objectives = append(c.Objectives, questStop{MapID: s.MapID, X: s.X, Y: s.Y})
			}
		}
	}
	if c.Kills == 0 && g.store != nil {
		// Without Questie's objectives, the kills named in the knowledge text ("Kill 8 Boars").
		if k, err := g.store.GetQuest(q.ID); err == nil && k != nil {
			c.Kills = killsFromText(k.ObjectivesText)
		}
	}
	xp, known, copper, hasCopper := g.questRewards(q.ID)
	c.XP, c.XPKnown = xp, known
	if !known {
		c.XP = classicQuestXP(c.Level, gd.Level)
	}
	c.Copper, c.HasCopper = copper, hasCopper
	return c
}

// questRewards reads the XP and gold a quest gives from the knowledge store. known is true for
// the XP the addon saw; the gold comes with it when present.
func (g *Service) questRewards(id int64) (xp int, known bool, copper int64, hasCopper bool) {
	if g.store == nil {
		return 0, false, 0, false
	}
	k, err := g.store.GetQuest(id)
	if err != nil || k == nil || k.RewardsJSON == "" {
		return 0, false, 0, false
	}
	var r map[string]any
	if json.Unmarshal([]byte(k.RewardsJSON), &r) != nil {
		return 0, false, 0, false
	}
	if v := numberOf(r["xp"]); v > 0 {
		xp, known = int(v), true
	}
	if v := numberOf(r["money"]); v > 0 {
		copper, hasCopper = int64(v), true
	}
	return xp, known, copper, hasCopper
}

// isKillKind reports whether a Questie objective kind is a kill of mobs or NPCs.
func isKillKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "monster", "npc", "mob", "creature", "kill":
		return true
	}
	return false
}

func questEstimateView(e questEstimate) map[string]any {
	c := e.Candidate
	m := map[string]any{
		"id":         c.ID,
		"titulo":     c.Name,
		"zona":       c.Zone,
		"nivel":      c.Level,
		"enRegistro": c.InLog,
		"xp":         c.XP,
		"xpFuente":   xpSourceText(c.XPKnown),
		"minutos":    round1(e.Minutes),
		"viajeMin":   round1(e.TravelSec / 60),
		"combateMin": round1(e.CombatSec / 60),
		"matas":      c.Kills,
		"xpPorMin":   round1(e.XPPerMin),
		"estimado":   "minutos y XP estimados",
	}
	if c.HasCopper {
		m["oroCobre"] = c.Copper
		m["oroPorMin"] = round1(e.CopperMin)
	}
	if e.NoCoords {
		m["sinCoordenadas"] = true
	}
	return m
}

func xpSourceText(known bool) string {
	if known {
		return "addon"
	}
	return "estimado"
}

func questValueText(ests []questEstimate, criterio string) string {
	if len(ests) == 0 {
		return "No hay misiones accesibles para valorar."
	}
	var parts []string
	for i, e := range ests {
		if i == 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%.0f %s, ~%.0f min)", e.Candidate.Name, e.XPPerMin, strings.Split(criterio, " ")[0], e.Minutes))
	}
	return "Mejores por " + criterio + " (estimado): " + strings.Join(parts, "; ") + "."
}

// questieOfActive is the Questie export of the active character (empty when there is none).
func (g *Service) questieOfActive() questieExport {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return questieExport{}
	}
	return readQuestie(ch.data)
}
