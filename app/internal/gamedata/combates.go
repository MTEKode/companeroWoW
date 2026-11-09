package gamedata

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Combat review: the last encounters the addon recorded (contract: COMBAT REVIEW). The getter
// returns the encounters and computed hints. A hint only states what the data shows; the
// assistant turns it into advice.

const (
	maxReviewHints   = 10
	reviewShareLimit = 5 // spells per combat in the view
	reviewSourceMax  = 3 // damage sources per combat in the view
	lowSharePercent  = 3.0
	longFightSeconds = 60.0
)

// hitRow is one spell entry of damageDone or healingDone.
type hitRow struct {
	name   string
	id     int64
	amount float64
	hits   int64
}

// srcRow is one entry of damageTaken.sources.
type srcRow struct {
	name   string
	amount float64
}

// lastHit is one of the last hits before a death.
type lastHit struct {
	source string
	spell  string
	amount float64
}

// combatRec is one recorded encounter.
type combatRec struct {
	id        int64
	at        time.Time
	duration  float64
	dps       float64
	damage    float64
	healing   float64
	taken     float64
	died      bool
	zone      string
	targets   []string
	dmg       []hitRow
	heal      []hitRow
	sources   []srcRow
	killer    string
	lastHits  []lastHit
	healthPct int // -1 when unknown
}

// parseCombats decodes the combats block, newest first.
func parseCombats(v any) []combatRec {
	var out []combatRec
	for _, item := range listOf(v) {
		m := asMap(item)
		if m == nil {
			continue
		}
		c := combatRec{
			id:        int64(numberOf(m["id"])),
			at:        unixTime(numberOf(m["startedAt"])),
			duration:  numberOf(m["duration"]),
			died:      truthy(m["died"]),
			zone:      str(m["zone"]),
			healthPct: -1,
		}
		for _, t := range listOf(m["targets"]) {
			if tm := asMap(t); tm != nil && str(tm["name"]) != "" {
				c.targets = append(c.targets, str(tm["name"]))
			}
		}
		dd := asMap(m["damageDone"])
		c.damage = numberOf(dd["total"])
		c.dps = numberOf(dd["dps"])
		if c.dps == 0 && c.duration > 0 {
			c.dps = c.damage / c.duration
		}
		c.dmg = readHits(dd["spells"])
		hd := asMap(m["healingDone"])
		c.healing = numberOf(hd["total"])
		c.heal = readHits(hd["spells"])
		dt := asMap(m["damageTaken"])
		c.taken = numberOf(dt["total"])
		for _, s := range listOf(dt["sources"]) {
			if sm := asMap(s); sm != nil {
				c.sources = append(c.sources, srcRow{name: str(sm["name"]), amount: numberOf(sm["amount"])})
			}
		}
		if d := asMap(m["death"]); d != nil {
			c.killer = str(d["killer"])
			for _, h := range listOf(d["lastHits"]) {
				if hm := asMap(h); hm != nil {
					c.lastHits = append(c.lastHits, lastHit{
						source: str(hm["source"]),
						spell:  str(hm["spellName"]),
						amount: numberOf(hm["amount"]),
					})
				}
			}
		}
		if r := asMap(m["resourcesAtEnd"]); r != nil {
			if _, has := r["healthPct"]; has {
				c.healthPct = int(numberOf(r["healthPct"]))
			}
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].id > out[j].id })
	return out
}

// readHits decodes a spell list of damageDone or healingDone.
func readHits(v any) []hitRow {
	var out []hitRow
	for _, item := range listOf(v) {
		m := asMap(item)
		if m == nil {
			continue
		}
		out = append(out, hitRow{
			name:   str(m["name"]),
			id:     int64(numberOf(m["spellID"])),
			amount: numberOf(m["amount"]),
			hits:   int64(numberOf(m["hits"])),
		})
	}
	return out
}

// CombatReview returns the recorded encounters, at most n of the newest (1 to 10). ultimo keeps
// only the newest one and muertes only the encounters that ended in a death. The hints always use
// the whole window of n encounters, so that a death is judged against the usual play.
func (g *Service) CombatReview(n int, ultimo, muertes bool) (any, error) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return unavailable(HintSync), nil
	}
	all := parseCombats(ch.data["combats"])
	if len(all) == 0 {
		return unavailable(HintCombats), nil
	}
	if n <= 0 || n > maxReviewCombats {
		n = maxReviewCombats
	}
	window := all
	if len(window) > n {
		window = window[:n]
	}
	shown := window
	if ultimo {
		shown = window[:1]
	}
	if muertes {
		var deaths []combatRec
		for _, c := range shown {
			if c.died {
				deaths = append(deaths, c)
			}
		}
		shown = deaths
	}
	views := make([]map[string]any, 0, len(shown))
	for _, c := range shown {
		views = append(views, combatView(c))
	}
	known := readKnownSpells(ch.data["spells"])
	fields := map[string]any{
		"combates":          views,
		"analizados":        len(window),
		"muertesEnAnalisis": countDeaths(window),
		"pistas":            combatHints(window, known),
	}
	if len(shown) == 0 && muertes {
		fields["nota"] = fmt.Sprintf("En los últimos %d combates grabados no has muerto.", len(window))
	}
	return available(sourceSV, ch.updatedAt, fields), nil
}

func countDeaths(list []combatRec) int {
	n := 0
	for _, c := range list {
		if c.died {
			n++
		}
	}
	return n
}

// combatView is one encounter as the assistant reads it.
func combatView(c combatRec) map[string]any {
	view := map[string]any{
		"id":            c.id,
		"duracion":      round1(c.duration),
		"murio":         c.died,
		"zona":          c.zone,
		"dps":           round1(c.dps),
		"danoTotal":     c.damage,
		"curacionTotal": c.healing,
		"danoRecibido":  c.taken,
		"hechizos":      shareRows(c.dmg, c.damage, reviewShareLimit),
		"fuentesDano":   sourceShares(c.sources, c.taken, reviewSourceMax),
	}
	if !c.at.IsZero() {
		view["inicio"] = c.at.Format(time.RFC3339)
	}
	if len(c.targets) > 0 {
		view["enemigos"] = c.targets
	}
	if c.healthPct >= 0 {
		view["saludFinal"] = c.healthPct
	}
	if c.died {
		hits := make([]map[string]any, 0, len(c.lastHits))
		for _, h := range c.lastHits {
			hits = append(hits, map[string]any{"origen": h.source, "hechizo": h.spell, "cantidad": h.amount})
		}
		view["muerte"] = map[string]any{"asesino": c.killer, "ultimosGolpes": hits}
	}
	return view
}

// shareRows lists the first limit spells with their share of total.
func shareRows(rows []hitRow, total float64, limit int) []map[string]any {
	out := make([]map[string]any, 0, limit)
	for i, r := range rows {
		if i >= limit {
			break
		}
		out = append(out, map[string]any{
			"nombre":     r.name,
			"porcentaje": percentOf(r.amount, total),
			"hits":       r.hits,
		})
	}
	return out
}

// sourceShares lists the largest damage sources with their share of the damage taken.
func sourceShares(list []srcRow, total float64, limit int) []map[string]any {
	sorted := append([]srcRow(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].amount > sorted[j].amount })
	out := make([]map[string]any, 0, limit)
	for i, s := range sorted {
		if i >= limit {
			break
		}
		out = append(out, map[string]any{"nombre": s.name, "porcentaje": percentOf(s.amount, total)})
	}
	return out
}

// combatHints computes what the window shows. Each hint names its numbers.
func combatHints(window []combatRec, known []knownSpell) []string {
	var out []string
	n := len(window)

	// Names seen in damage or healing across the window, lower-cased.
	seen := map[string]bool{}
	var totalDmg float64
	share := map[string]float64{}
	appear := map[string]int{}
	for _, c := range window {
		for _, h := range c.dmg {
			seen[strings.ToLower(h.name)] = true
			share[h.name] += h.amount
			appear[h.name]++
		}
		for _, h := range c.heal {
			seen[strings.ToLower(h.name)] = true
		}
		totalDmg += c.damage
	}

	// 1. Known spells that never show up in the window.
	if n >= 3 {
		var unused []string
		for _, sp := range known {
			if !seen[strings.ToLower(sp.name)] {
				unused = append(unused, sp.name)
			}
		}
		if len(unused) > 0 {
			out = append(out, fmt.Sprintf(
				"No aparecen en ninguno de tus últimos %d combates (entre los 12 hechizos principales de cada uno): %s. "+
					"Si son de daño, no los estás usando; si son de apoyo o de utilidad, es normal.",
				n, listNames(unused, 6)))
		}
	}

	// 2. Damage spells with a small share of the total.
	if totalDmg > 0 {
		type low struct {
			name string
			pct  float64
		}
		var lows []low
		for name, amt := range share {
			if appear[name] < 2 || isAutoAttack(name) {
				continue
			}
			if pct := percentOf(amt, totalDmg); pct > 0 && pct < lowSharePercent {
				lows = append(lows, low{name, pct})
			}
		}
		sort.Slice(lows, func(i, j int) bool { return lows[i].pct < lows[j].pct })
		if len(lows) > 0 {
			parts := make([]string, 0, 3)
			for i, l := range lows {
				if i >= 3 {
					break
				}
				parts = append(parts, fmt.Sprintf("%s (%.1f %%)", l.name, l.pct))
			}
			out = append(out, fmt.Sprintf(
				"Aportan poco daño en la ventana de %d combates: %s. Si no los necesitas, quítalos de la rotación.",
				n, strings.Join(parts, ", ")))
		}
	}

	// 3. Long fights with low damage per second.
	durs, dpss := []float64{}, []float64{}
	for _, c := range window {
		if c.duration > 0 {
			durs = append(durs, c.duration)
			dpss = append(dpss, c.dps)
		}
	}
	if len(durs) >= 3 {
		medDur, medDps := median(durs), median(dpss)
		for _, c := range window {
			if c.duration > math.Max(longFightSeconds, 1.5*medDur) && medDps > 0 && c.dps < 0.6*medDps {
				out = append(out, fmt.Sprintf(
					"Combate %d: %.0f s a %.0f dps (mediana %.0f dps). Un combate largo con poco daño suele indicar "+
						"que estabas fuera de rango o sin tus hechizos de daño; revisa ese combate.",
					c.id, c.duration, c.dps, medDps))
			}
		}
	}

	// 4. One source that dominates the damage taken.
	counts := map[string]int{}
	pctSum := map[string]float64{}
	for _, c := range window {
		if c.taken <= 0 || len(c.sources) == 0 {
			continue
		}
		top := topSource(c.sources)
		if pct := percentOf(top.amount, c.taken); pct >= 40 {
			counts[top.name]++
			pctSum[top.name] += pct
		}
	}
	var dominant []string
	for name, k := range counts {
		if k >= 3 {
			dominant = append(dominant, name)
		}
	}
	sort.Strings(dominant)
	for _, name := range dominant {
		k := counts[name]
		out = append(out, fmt.Sprintf(
			"Te pega sobre todo «%s»: en %d de %d combates es más del 40 %% del daño recibido (de media %.0f %%).",
			name, k, n, pctSum[name]/float64(k)))
	}

	// 5. Each death: who killed the player, the last hits and the defensives that the fight shows.
	defs := []string{}
	for _, sp := range known {
		if sp.hasRole("defensivo") {
			defs = append(defs, sp.name)
		}
	}
	for _, c := range window {
		if !c.died {
			continue
		}
		out = append(out, deathHint(c, defs))
	}
	if len(out) > maxReviewHints {
		out = out[:maxReviewHints]
	}
	return out
}

// deathHint describes one death and what the fight shows about the defensives.
func deathHint(c combatRec, defs []string) string {
	killer := "no sé quién te mató"
	if c.killer != "" {
		killer = "te mató «" + c.killer + "»"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Muerte en el combate %d (%s, %.0f s): %s.", c.id, orDash(c.zone), c.duration, killer)
	if len(c.lastHits) > 0 {
		parts := make([]string, 0, 3)
		for i, h := range c.lastHits {
			if i >= 3 {
				break
			}
			spell := h.spell
			if spell == "" {
				spell = "golpe"
			}
			parts = append(parts, fmt.Sprintf("%s con %s (%.0f)", h.source, spell, h.amount))
		}
		fmt.Fprintf(&b, " Últimos golpes: %s.", strings.Join(parts, "; "))
	}
	switch {
	case len(defs) == 0:
		b.WriteString(" No tienes defensivos conocidos en tu libro de hechizos.")
	default:
		used := map[string]bool{}
		for _, h := range append(append([]hitRow{}, c.dmg...), c.heal...) {
			used[strings.ToLower(h.name)] = true
		}
		var shown []string
		for _, d := range defs {
			if used[strings.ToLower(d)] {
				shown = append(shown, d)
			}
		}
		if len(shown) == 0 {
			fmt.Fprintf(&b, " En esa pelea no aparece ninguno de tus defensivos conocidos (%s). "+
				"Los defensivos que no hacen daño ni curan no se registran, así que confirma si los usaste.",
				listNames(defs, 4))
		} else {
			fmt.Fprintf(&b, " En esa pelea aparecen defensivos o curación (%s); revisa cuándo los usaste.",
				listNames(shown, 4))
		}
	}
	return b.String()
}

// topSource is the source with the largest amount.
func topSource(list []srcRow) srcRow {
	best := list[0]
	for _, s := range list[1:] {
		if s.amount > best.amount {
			best = s
		}
	}
	return best
}

func isAutoAttack(name string) bool {
	low := strings.ToLower(name)
	return low == "auto attack" || low == "ataque automático" || low == "ataque automatico"
}

// percentOf returns part as a percentage of total, rounded to one decimal; 0 when total is 0.
func percentOf(part, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return round1(part / total * 100)
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 0 {
		return (s[m-1] + s[m]) / 2
	}
	return s[m]
}

// listNames joins up to limit names, with "y N más" when there are more.
func listNames(names []string, limit int) string {
	if len(names) <= limit {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s y %d más", strings.Join(names[:limit], ", "), len(names)-limit)
}

func orDash(s string) string {
	if s == "" {
		return "zona desconocida"
	}
	return s
}
