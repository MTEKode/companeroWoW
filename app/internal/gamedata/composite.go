package gamedata

import (
	"fmt"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// The composite getters answer whole questions in one call: where am I and what do I do
// now (PlayerContext), the step by step of a quest (QuestGuide) and the professions with
// their trainers (ProfessionPlan). They only combine the facade's sources.

// Step kinds: what the next step asks the player to do.
const (
	stepAccept   = "aceptar"
	stepObjetivo = "objetivo"
	stepEntregar = "entregar"
	stepUnknown  = "desconocido"
)

// staleAddonSec is the SavedVariables age after which the context asks for /compa sync.
const staleAddonSec = 600

// PlayerContext returns the location, character, tracked quest, target, professions and
// data freshness in one map. Missing parts are left out, and avisos says what to do.
func (g *Service) PlayerContext() (any, error) {
	out := map[string]any{}
	notes := []string{}
	pos := g.playerPosition()

	if loc, err := g.Location(); err == nil {
		if m := asMap(loc); truthy(m["available"]) {
			lv := map[string]any{"zona": str(m["zone"]), "mapId": m["mapId"]}
			if sub := str(m["subZone"]); sub != "" {
				lv["subZona"] = sub
			}
			if inst := str(m["instanceName"]); inst != "" {
				lv["instancia"] = inst
			}
			if x, okX := numOK(m["x"]); okX {
				if y, okY := numOK(m["y"]); okY {
					lv["x"], lv["y"] = round1(x*100), round1(y*100)
					lv["coords"] = coordsText(x, y)
				}
			}
			out["ubicacion"] = lv
		}
	}
	if ch, err := g.Character(); err == nil {
		if m := asMap(ch); truthy(m["available"]) {
			out["personaje"] = map[string]any{
				"nombre":      m["name"],
				"clase":       m["class"],
				"raza":        m["race"],
				"nivel":       m["level"],
				"xpPct":       m["xpPct"],
				"enCombate":   m["inCombat"],
				"muerto":      m["dead"],
				"descansando": m["resting"],
			}
		}
	}
	if mo, err := g.Money(); err == nil {
		if m := asMap(mo); truthy(m["available"]) {
			out["dinero"] = fmt.Sprintf("%d oro, %d plata, %d cobre",
				int64(numberOf(m["gold"])), int64(numberOf(m["silver"])), int64(numberOf(m["copperRemainder"])))
		}
	}
	if qs, err := g.Quests(); err == nil {
		if m := asMap(qs); truthy(m["available"]) {
			tracked := int64(numberOf(m["trackedQuestId"]))
			if tracked > 0 {
				if guide, ok := g.guideFor(tracked, pos); ok {
					out["misionRastreada"] = map[string]any{
						"questId":       tracked,
						"titulo":        guide["title"],
						"completa":      guide["completa"],
						"objetivos":     guide["objetivos"],
						"siguientePaso": guide["nextStep"],
					}
				}
			} else {
				notes = append(notes, "No hay misión rastreada: rastrea una en el juego o pregúntame por una concreta.")
			}
		}
	}
	if tg, err := g.Target(); err == nil {
		if m := asMap(tg); truthy(m["available"]) && truthy(m["hasTarget"]) {
			t := map[string]any{"hostil": m["hostile"], "nivel": m["level"]}
			if npc := asMap(m["npc"]); npc != nil {
				t["nombre"] = npc["name"]
			}
			out["objetivo"] = t
		}
	}
	if pr, err := g.Professions(); err == nil {
		if m := asMap(pr); truthy(m["available"]) {
			var list []map[string]any
			for _, p := range listOf(m["professions"]) {
				if pm := asMap(p); pm != nil {
					list = append(list, map[string]any{"nombre": pm["name"], "nivel": pm["rank"], "max": pm["maxRank"]})
				}
			}
			if list != nil {
				out["profesiones"] = list
			}
		}
	}

	fresh := map[string]any{"questieDisponible": false}
	if st, err := g.Status(); err == nil {
		if m := asMap(st); m != nil {
			fresh["puenteConectado"] = m["bridgeConnected"]
			if age, ok := m["savedVariablesAgeSec"]; ok {
				fresh["svHaceSeg"] = age
				if n, _ := numOK(age); n > staleAddonSec {
					notes = append(notes, "Los datos del addon tienen más de 10 minutos: pide /compa sync en el juego.")
				}
			} else {
				notes = append(notes, "No hay datos del addon: escribe /compa sync en el juego.")
			}
		}
	}
	if ch, _, ok := g.activeCharacter(); ok {
		qx := readQuestie(ch.data)
		if qx.Available {
			fresh["questieDisponible"] = true
			fresh["questieHaceSeg"] = ageSeconds(qx.ExportedAt)
		} else {
			notes = append(notes, "Questie no exportó datos de misiones: no tengo coordenadas de misiones.")
		}
	}
	out["datos"] = fresh
	if !pos.OK {
		notes = append(notes, "No sé la posición exacta: abre WoW en modo ventana o sin bordes.")
	}
	if len(notes) > 0 {
		out["avisos"] = notes
	}
	if out["ubicacion"] == nil && out["personaje"] == nil {
		return unavailable(HintSync), nil
	}
	return available(sourceLiveOrSV(g), time.Now(), out), nil
}

// sourceLiveOrSV names the source of the composite answer.
func sourceLiveOrSV(g *Service) string {
	if _, ok := g.Live(); ok {
		return sourceLive
	}
	return sourceSV
}

// QuestGuide returns the step-by-step view of one quest. id 0 means the tracked quest.
func (g *Service) QuestGuide(id int64) (any, error) {
	if id == 0 {
		qs, err := g.Quests()
		if err != nil {
			return nil, err
		}
		m := asMap(qs)
		if !truthy(m["available"]) {
			return unavailable(HintSync), nil
		}
		id = int64(numberOf(m["trackedQuestId"]))
		if id == 0 {
			return unavailable("No hay misión rastreada. Indica quest_id o rastrea una misión en el juego."), nil
		}
	}
	guide, ok := g.guideFor(id, g.playerPosition())
	if !ok {
		return unavailable(fmt.Sprintf("No tengo datos de la misión %d. %s", id, HintSync)), nil
	}
	return available(sourceSV, guideTime(g), guide), nil
}

// guideTime is the time of the active character's SavedVariables, when known.
func guideTime(g *Service) time.Time {
	if ch, _, ok := g.activeCharacter(); ok {
		return ch.updatedAt
	}
	return time.Time{}
}

// guideFor builds the guide map of a quest from the log, the knowledge database and the
// Questie export. ok is false when none of them knows the quest.
func (g *Service) guideFor(id int64, pos position) (map[string]any, bool) {
	var logEntry map[string]any
	var qx questieExport
	if ch, _, ok := g.activeCharacter(); ok {
		for _, e := range entriesOf(ch.data["quests"]) {
			if qm := asMap(e.value); qm != nil && questIDOf(qm, e.key) == id {
				logEntry = copyMap(qm)
				break
			}
		}
		qx = readQuestie(ch.data)
	}
	var k *store.KQuest
	if g.store != nil {
		if kq, err := g.store.GetQuest(id); err == nil {
			k = kq
		}
	}
	qq, hasQ := qx.quest(id)
	if logEntry == nil && k == nil && !hasQ {
		return nil, false
	}

	out := map[string]any{"questId": id, "enRegistro": logEntry != nil, "questieDisponible": hasQ}
	title := str(logEntry["title"])
	if title == "" && k != nil {
		title = k.Title
	}
	if title == "" {
		title = qq.Name
	}
	out["title"] = title
	if lvl := numberOf(logEntry["level"]); lvl > 0 {
		out["nivel"] = int(lvl)
	} else if k != nil && k.Level > 0 {
		out["nivel"] = k.Level
	}

	var objectives []map[string]any
	complete := false
	nextObjective := -1
	if logEntry != nil {
		complete = truthy(logEntry["isComplete"])
		for _, o := range listOf(logEntry["objectives"]) {
			om := asMap(o)
			if om == nil {
				continue
			}
			done := truthy(om["finished"])
			progreso := ""
			if req := int(numberOf(om["numRequired"])); req > 0 {
				progreso = fmt.Sprintf("%d/%d", int(numberOf(om["numFulfilled"])), req)
			}
			if !done && nextObjective < 0 {
				nextObjective = len(objectives)
			}
			objectives = append(objectives, map[string]any{"texto": str(om["text"]), "hecho": done, "progreso": progreso})
		}
	}
	out["completa"] = complete
	out["objetivos"] = objectives
	if k != nil && k.ObjectivesText != "" {
		out["objetivoTexto"] = k.ObjectivesText
	}
	if hasQ {
		out["inicio"] = sourcesView(qq.Starters, pos)
		out["entrega"] = sourcesView(qq.Finishers, pos)
		out["objetivosQuestie"] = objectivesView(qq.Objectives, pos)
	}
	out["nextStep"] = nextStep(title, logEntry != nil, complete, nextObjective, objectives, qq, hasQ, k, pos)
	return out, true
}

// nextStep is the single thing to do now, with its place and mark command when known.
// Order: deliver when complete, else the first unfinished objective, else accept.
func nextStep(title string, inLog, complete bool, nextObj int, objectives []map[string]any,
	qq questieQuest, hasQ bool, k *store.KQuest, pos position) map[string]any {
	switch {
	case inLog && complete:
		var spawns []spawn
		label := ""
		if hasQ {
			for _, f := range qq.Finishers {
				spawns = append(spawns, f.Spawns...)
				if label == "" {
					label = f.Name
				}
			}
		}
		if len(spawns) == 0 && k != nil && k.TurnInMapID > 0 && (k.TurnInX != 0 || k.TurnInY != 0) {
			spawns = []spawn{{MapID: k.TurnInMapID, X: k.TurnInX, Y: k.TurnInY}}
			label = k.TurnInName
		}
		return stepFrom(stepEntregar, "Entrega «"+title+"»", label, spawns, pos)
	case inLog && nextObj >= 0:
		text := "Completa «" + title + "»"
		if t := str(objectives[nextObj]["texto"]); t != "" {
			text = "Haz: " + t
		}
		var spawns []spawn
		label := ""
		if hasQ {
			for _, o := range qq.Objectives {
				if o.Index == nextObj+1 {
					spawns, label = o.Spawns, o.Name
					break
				}
			}
		}
		return stepFrom(stepObjetivo, text, label, spawns, pos)
	case inLog:
		return map[string]any{"tipo": stepUnknown, "texto": "Todos los objetivos están hechos; entrega la misión cuando puedas."}
	}
	var spawns []spawn
	label := ""
	if hasQ {
		for _, s := range qq.Starters {
			spawns = append(spawns, s.Spawns...)
			if label == "" {
				label = s.Name
			}
		}
	}
	if len(spawns) == 0 && k != nil && k.GiverMapID > 0 && (k.GiverX != 0 || k.GiverY != 0) {
		spawns = []spawn{{MapID: k.GiverMapID, X: k.GiverX, Y: k.GiverY}}
		label = k.GiverName
	}
	return stepFrom(stepAccept, "Acepta «"+title+"»", label, spawns, pos)
}

// stepFrom builds a step with the nearest spawn. Without spawns it says the place is unknown.
func stepFrom(kind, text, label string, spawns []spawn, pos position) map[string]any {
	step := map[string]any{"tipo": kind, "texto": text}
	if label != "" {
		step["donde"] = label
	}
	n, ok := nearest(spawns, pos)
	if !ok {
		step["ubicacion"] = "desconocida: escribe /compa sync en el juego"
		return step
	}
	step["mapId"] = n.Spawn.MapID
	step["x"] = round1(n.Spawn.X * 100)
	step["y"] = round1(n.Spawn.Y * 100)
	step["coords"] = coordsText(n.Spawn.X, n.Spawn.Y)
	step["sameMap"] = n.SameMap
	if n.SameMap {
		step["distancia"] = n.Distance
	}
	step["marca"] = marcaCommand(n.Spawn, label)
	return step
}

// ProfessionPlan lists the professions with rank and cap, the known trainers of each
// profession with coordinates, and whether to train now.
func (g *Service) ProfessionPlan() (any, error) {
	pr, err := g.Professions()
	if err != nil {
		return nil, err
	}
	pm := asMap(pr)
	if !truthy(pm["available"]) {
		return unavailable(HintSync), nil
	}
	trainers := g.knownTrainers()
	pos := g.playerPosition()

	plan := []map[string]any{}
	for _, p := range listOf(pm["professions"]) {
		m := asMap(p)
		if m == nil {
			continue
		}
		name := str(m["name"])
		rank := int(numberOf(m["rank"]))
		maxRank := int(numberOf(m["maxRank"]))
		entry := map[string]any{"nombre": name, "nivel": rank, "max": maxRank}
		left := maxRank - rank
		entry["entrenarYa"] = maxRank > 0 && left <= nearMaxRankLeft
		switch {
		case maxRank <= 0:
			entry["consejo"] = "No conozco el máximo de esta profesión; míralo en el juego."
		case left <= nearMaxRankLeft:
			entry["consejo"] = fmt.Sprintf("Te faltan %d puntos para el máximo (%d): entrena el siguiente rango ya.", left, maxRank)
		default:
			entry["consejo"] = fmt.Sprintf("Te faltan %d puntos para el máximo (%d): puedes esperar a acercarte.", left, maxRank)
		}
		matches := []map[string]any{}
		for _, t := range trainers {
			if t.matches(name) {
				matches = append(matches, t.view(pos))
			}
		}
		entry["entrenadores"] = matches
		if len(matches) == 0 {
			entry["nota"] = "No tengo entrenador guardado para " + name + ". Escribe /compa sync cerca del entrenador o busca en la web."
		}
		plan = append(plan, entry)
	}
	return available(sourceSV, guideTime(g), map[string]any{
		"profesiones":           plan,
		"entrenadoresConocidos": len(trainers),
	}), nil
}

// trainerRef is a trainer from the knowledge database or the Questie export.
type trainerRef struct {
	id     int64
	name   string
	kind   string // trainer type (knowledge) or subtitle (Questie)
	mapID  int
	x, y   float64
	source string
}

func (t trainerRef) matches(profession string) bool {
	p := strings.ToLower(profession)
	return p != "" && (strings.Contains(strings.ToLower(t.kind), p) || strings.Contains(strings.ToLower(t.name), p))
}

func (t trainerRef) view(pos position) map[string]any {
	sp := spawn{MapID: t.mapID, X: t.x, Y: t.y}
	m := map[string]any{
		"npcId":  t.id,
		"nombre": t.name,
		"fuente": t.source,
		"mapId":  t.mapID,
		"x":      round1(t.x * 100),
		"y":      round1(t.y * 100),
		"coords": coordsText(t.x, t.y),
		"marca":  marcaCommand(sp, t.name),
	}
	if t.kind != "" {
		m["tipo"] = t.kind
	}
	n, _ := nearest([]spawn{sp}, pos)
	m["sameMap"] = n.SameMap
	if n.SameMap {
		m["distancia"] = n.Distance
	}
	return m
}

// knownTrainers merges the knowledge database trainers with the zone trainers of the
// Questie export. Duplicates by id and name are dropped.
func (g *Service) knownTrainers() []trainerRef {
	var out []trainerRef
	seen := map[string]bool{}
	add := func(t trainerRef) {
		key := fmt.Sprintf("%d|%s", t.id, t.name)
		if seen[key] || t.mapID <= 0 {
			return
		}
		seen[key] = true
		out = append(out, t)
	}
	if g.store != nil {
		if list, err := g.store.Trainers(200); err == nil {
			for _, n := range list {
				if n.X == 0 && n.Y == 0 {
					continue
				}
				add(trainerRef{id: n.NpcID, name: n.Name, kind: n.TrainerType, mapID: n.MapID, x: n.X, y: n.Y, source: "conocimiento"})
			}
		}
	}
	if ch, _, ok := g.activeCharacter(); ok {
		for _, n := range readQuestie(ch.data).NPCs {
			if n.Role != "trainer" {
				continue
			}
			add(trainerRef{id: n.ID, name: n.Name, kind: n.Subtitle, mapID: n.MapID, x: n.X, y: n.Y, source: "questie"})
		}
	}
	return out
}
