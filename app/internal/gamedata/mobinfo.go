package gamedata

import (
	"fmt"
	"math"
	"strings"
)

// Mob info (info_enemigo): what the recorded fights say about one enemy and whether the
// character can take it, alone or with two or three of its kind. Observed numbers come from
// the combats the addon recorded against the mob; the time-to-kill comes from the model hook
// when it has one. Every figure is labelled with where it comes from.

const (
	// mobMinFights is how many recorded fights make the observed numbers count for a verdict.
	mobMinFights = 1
	// mobLowLifePct is the end-of-fight health (percent) below which a win is called risky.
	mobLowLifePct = 40.0
	// mobGoodLifePct is the end-of-fight health from which a win is called comfortable.
	mobGoodLifePct = 60.0
	// mobNowLowPct is the current health below which the verdict asks to recover first.
	mobNowLowPct = 60
)

// MobQuery is what info_enemigo was asked for. Name and NpcID are both optional; with neither,
// the current target is used.
type MobQuery struct {
	Name  string
	NpcID int64
	Group int // the group size of the main verdict, 1 to 3
}

// mobFight is one recorded fight against the mob.
type mobFight struct {
	duration  float64
	died      bool
	killer    bool // the fight's killer is this mob
	taken     float64
	endHealth int // -1 when unknown
	level     int
	class     string
}

// MobInfo answers info_enemigo.
func (g *Service) MobInfo(q MobQuery) (any, error) {
	if q.Group < 1 || q.Group > 3 {
		q.Group = 1
	}
	live, liveOK := g.Live()
	name, npcID, level, classif, hostile := "", int64(0), 0, "", false
	source := sourceSV
	at := live.ReadAt
	if q.Name == "" && q.NpcID == 0 {
		if !liveOK || !live.HasTarget {
			return unavailable("No tienes un objetivo seleccionado. Dime el nombre o el npcID del enemigo, o selecciónalo en el juego."), nil
		}
		npcID = int64(live.TargetNpcID)
		hostile = live.TargetHostile
		switch {
		case live.TargetLevel == 255:
			level = -1
			classif = "boss"
		case live.TargetLevel > 0:
			level = int(live.TargetLevel)
		}
		source = sourceLive
		if g.store != nil && npcID != 0 {
			if n, err := g.store.GetNpc(npcID); err == nil && n != nil {
				name = n.Name
			}
		}
		if name == "" {
			name = fmt.Sprintf("npc %d", npcID)
		}
	} else {
		if q.NpcID != 0 {
			npcID = q.NpcID
		}
		if g.store != nil {
			if npcID != 0 {
				if n, err := g.store.GetNpc(npcID); err == nil && n != nil {
					name = n.Name
				}
			} else if q.Name != "" {
				list, err := g.store.SearchNpcs(q.Name, 5)
				if err != nil {
					return nil, fmt.Errorf("buscar el enemigo: %w", err)
				}
				for _, n := range list {
					if fold(n.Name) == fold(q.Name) {
						npcID, name = n.NpcID, n.Name
						break
					}
				}
				if npcID == 0 && len(list) > 0 {
					npcID, name = list[0].NpcID, list[0].Name
				}
			}
		}
		if name == "" {
			name = q.Name
		}
	}

	// The recorded fights against this mob, from the addon's combats block.
	ch, _, hasChar := g.activeCharacter()
	var fights []mobFight
	if hasChar {
		at = ch.updatedAt
		fights = mobFights(ch.data["combats"], name, npcID)
	}
	// The character's level and health now.
	myLevel, myHealth := 0, -1
	if c, err := g.Character(); err == nil {
		if m := asMap(c); truthy(m["available"]) {
			myLevel = int(numberOf(m["level"]))
		}
	}
	if liveOK && live.HealthPct != 255 {
		myHealth = int(live.HealthPct)
	}
	if level == 0 {
		for _, f := range fights {
			if f.level > 0 {
				level = f.level
				break
			}
		}
	}
	if classif == "" {
		for _, f := range fights {
			if f.class != "" {
				classif = f.class
				break
			}
		}
	}
	if classif == "" {
		classif = "normal"
	}

	obs := observedStatsOf(fights)
	pred := g.predictedKill(level)
	verdict, reasons := mobVerdict(obs, classif, level, myLevel, myHealth, q.Group)

	out := map[string]any{
		"nombre":         name,
		"npcId":          npcID,
		"nivel":          level,
		"clasificacion":  classif,
		"hostil":         hostile,
		"nivelJugador":   myLevel,
		"observado":      obs.view(),
		"grupos":         groupRows(obs),
		"veredicto":      verdict,
		"motivos":        reasons,
		"grupoAnalizado": q.Group,
	}
	if myHealth >= 0 {
		out["vidaAhora"] = myHealth
	}
	if pred != nil {
		out["predicho"] = pred
	}
	out["texto"] = mobText(name, verdict, obs, reasons)
	return available(source, at, out), nil
}

// observedStats are the numbers of the recorded fights against one mob.
type observedStats struct {
	Fights      int
	Deaths      int
	KillerDeath int     // deaths where this mob was the killer
	MedianSec   float64 // median fight duration
	WonSec      float64 // median duration of the won fights (0 when none)
	TakenPerSec float64 // damage taken from this mob per second of fight
	TakenFight  float64 // average damage taken from this mob per fight
	EndHealth   float64 // median health at the end of the won fights (-1 when unknown)
}

func observedStatsOf(fights []mobFight) observedStats {
	s := observedStats{Fights: len(fights), EndHealth: -1}
	if len(fights) == 0 {
		return s
	}
	var durs, won, ends []float64
	var takenSum, durSum float64
	for _, f := range fights {
		if f.died {
			s.Deaths++
		}
		if f.killer {
			s.KillerDeath++
		}
		durs = append(durs, f.duration)
		durSum += f.duration
		takenSum += f.taken
		if !f.died {
			won = append(won, f.duration)
			if f.endHealth >= 0 {
				ends = append(ends, float64(f.endHealth))
			}
		}
	}
	s.MedianSec = median(durs)
	if len(won) > 0 {
		s.WonSec = median(won)
	}
	if durSum > 0 {
		s.TakenPerSec = takenSum / durSum
	}
	s.TakenFight = takenSum / float64(len(fights))
	if len(ends) > 0 {
		s.EndHealth = median(ends)
	}
	return s
}

// view is the observed block of the answer.
func (s observedStats) view() map[string]any {
	m := map[string]any{"combates": s.Fights, "muertesEnElCombate": s.Deaths, "muertesCausadas": s.KillerDeath}
	if s.Fights == 0 {
		m["nota"] = "Sin combates grabados contra este enemigo."
		return m
	}
	m["duracionMediaSeg"] = round1(s.MedianSec)
	m["danoRecibidoPorSeg"] = round1(s.TakenPerSec)
	m["danoRecibidoPorCombate"] = math.Round(s.TakenFight)
	if s.EndHealth >= 0 {
		m["vidaFinalMediaPct"] = round1(s.EndHealth)
	}
	return m
}

// groupRows give the damage factor for two and three of the mob at once: they all hit you
// while you kill them one by one, so the damage of a group of k is k(k+1)/2 times the damage
// of one (1, 3 and 6). The factor is a model, not a measure.
func groupRows(s observedStats) []map[string]any {
	rows := []map[string]any{}
	for k := 2; k <= 3; k++ {
		factor := k * (k + 1) / 2
		row := map[string]any{
			"cantidad":    k,
			"factorDano":  factor,
			"fuente":      "estimado",
			"explicacion": fmt.Sprintf("con %d a la vez recibes unas %d veces el daño de uno solo, mientras los matas uno a uno", k, factor),
		}
		if s.Fights > 0 {
			row["danoRecibidoEstimadoPorCombate"] = math.Round(s.TakenFight * float64(factor))
		}
		rows = append(rows, row)
	}
	return rows
}

// predictedKill is the model's answer for the time to kill a mob of this level, or nil.
func (g *Service) predictedKill(level int) map[string]any {
	if g.killTime == nil || level <= 0 {
		return nil
	}
	sec, ok := g.killTime(level)
	if !ok || sec <= 0 {
		return nil
	}
	return map[string]any{"tiempoMatarSeg": round1(sec), "fuente": "modelo de combate"}
}

// mobVerdict says whether the character can take the mob, from the observed fights when there
// are some and from the levels and classification when there are none. group is the number of
// mobs of this kind that would come at once.
func mobVerdict(s observedStats, classif string, level, myLevel, myHealth, group int) (string, []string) {
	var reasons []string
	if group < 1 {
		group = 1
	}
	elite := classif == "elite" || classif == "rare" || classif == "rareelite" || classif == "worldboss" || classif == "boss"

	var verdict string
	if s.Fights < mobMinFights {
		// No fights recorded: the levels and the classification are all there is.
		reasons = append(reasons, "No hay combates grabados contra él: el veredicto es por nivel y clasificación, no por datos.")
		diff := level - myLevel
		switch {
		case level == -1 || classif == "worldboss" || classif == "boss":
			verdict = "No (sin datos)"
			reasons = append(reasons, "Es un jefe o un enemigo de nivel ??: mejor con ayuda.")
		case elite || diff >= 2:
			verdict = "Con cuidado (sin datos)"
			reasons = append(reasons, fmt.Sprintf("Es %s y su nivel es %d (tú %d).", classLabel(classif), level, myLevel))
		case diff <= -2:
			verdict = "Probablemente sí (sin datos)"
		default:
			verdict = "Igualado: probablemente sí, con cuidado (sin datos)"
		}
	} else {
		survival := float64(s.Fights-s.Deaths) / float64(s.Fights)
		reasons = append(reasons, fmt.Sprintf("Según %d combate(s) grabados contra él, mueres en el %.0f %%.", s.Fights, (1-survival)*100))
		switch {
		case survival < 0.5:
			verdict = "No"
			reasons = append(reasons, "Has muerto en la mayoría de los combates con él: no lo tomes solo.")
		case s.Deaths > 0:
			verdict = "Con cuidado"
			reasons = append(reasons, fmt.Sprintf("Has muerto %d vez(es) con él.", s.Deaths))
		case s.EndHealth >= 0 && s.EndHealth < mobLowLifePct:
			verdict = "Sí, pero con cuidado"
			reasons = append(reasons, fmt.Sprintf("Ganas, pero acabas de media con %.0f %% de vida.", s.EndHealth))
		case s.EndHealth >= mobGoodLifePct || s.EndHealth < 0:
			verdict = "Sí"
			if s.EndHealth >= 0 {
				reasons = append(reasons, fmt.Sprintf("Ganas acabando de media con %.0f %% de vida.", s.EndHealth))
			}
		default:
			verdict = "Sí, con margen justo"
		}
	}

	if myHealth >= 0 && myHealth < mobNowLowPct && strings.HasPrefix(verdict, "Sí") {
		reasons = append(reasons, fmt.Sprintf("Ahora tienes %d %% de vida: descansa antes de empezar.", myHealth))
	}
	if group > 1 {
		factor := group * (group + 1) / 2
		reasons = append(reasons, fmt.Sprintf("Con %d a la vez el daño recibido sube unas %d veces (estimado).", group, factor))
		if strings.HasPrefix(verdict, "Sí") && group == 3 {
			verdict = "Con cuidado: en grupo"
		}
		if verdict == "Probablemente sí (sin datos)" || verdict == "Igualado: probablemente sí, con cuidado (sin datos)" {
			verdict = "Con cuidado: en grupo (sin datos)"
		}
	}
	return verdict, reasons
}

func classLabel(c string) string {
	switch c {
	case "elite":
		return "élite"
	case "rare", "rareelite":
		return "raro"
	case "worldboss", "boss":
		return "jefe"
	}
	return "normal"
}

func mobText(name, verdict string, s observedStats, reasons []string) string {
	line := fmt.Sprintf("%s: %s.", name, verdict)
	if s.Fights > 0 {
		line += fmt.Sprintf(" %d combate(s) grabados; dura unos %.0f s y te quita %.1f de vida por segundo (observado).",
			s.Fights, s.MedianSec, s.TakenPerSec)
	}
	if len(reasons) > 0 {
		line += " " + reasons[0]
	}
	return line
}

// mobFights reads the recorded combats that involved the mob: a target with the same name or
// npcID. It keeps the newest first, as the addon stores them.
func mobFights(v any, name string, npcID int64) []mobFight {
	var out []mobFight
	wantName := fold(name)
	for _, item := range listOf(v) {
		m := asMap(item)
		if m == nil {
			continue
		}
		var hit bool
		var level int
		var class string
		// The name the fight gives the mob: damage sources and killers are matched by it, so a
		// match by npcID still counts the damage of that mob.
		mobName := wantName
		for _, t := range listOf(m["targets"]) {
			tm := asMap(t)
			if tm == nil {
				continue
			}
			same := npcID != 0 && int64(numberOf(tm["npcID"])) == npcID
			if !same && wantName != "" && fold(str(tm["name"])) == wantName {
				same = true
			}
			if same {
				hit = true
				level = int(numberOf(tm["level"]))
				class = str(tm["classification"])
				mobName = fold(str(tm["name"]))
				break
			}
		}
		if !hit {
			continue
		}
		f := mobFight{
			duration:  numberOf(m["duration"]),
			died:      truthy(m["died"]),
			endHealth: -1,
			level:     level,
			class:     class,
		}
		// Only the damage this mob did counts, not the damage of the whole fight.
		dt := asMap(m["damageTaken"])
		for _, s := range listOf(dt["sources"]) {
			sm := asMap(s)
			if sm != nil && mobName != "" && fold(str(sm["name"])) == mobName {
				f.taken += numberOf(sm["amount"])
			}
		}
		if d := asMap(m["death"]); d != nil && mobName != "" && fold(str(d["killer"])) == mobName {
			f.killer = true
		}
		if r := asMap(m["resourcesAtEnd"]); r != nil {
			if _, has := r["healthPct"]; has {
				f.endHealth = int(numberOf(r["healthPct"]))
			}
		}
		out = append(out, f)
	}
	return out
}
