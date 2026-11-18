package gamedata

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// cadena_mision: where a quest sits in its chain: the quests before it (and which of them the
// character still lacks), the quests it unlocks, and a suggested order. The chain comes from the
// world block (Questie's chains and the accept/turn-in order the addon observed) and from Questie
// for the quests in the character's log.

const worldMaxChain = 12 // how far the chain is followed in each direction

// chainStep is one quest of a chain with its state for the character.
type chainStep struct {
	ID     int64
	Titulo string
	Estado string // hecha, en registro or pendiente
}

// completedSet reads the completed quest ids of a character: the saved text form or the live list.
func completedSet(data map[string]any) map[int64]bool {
	out := map[int64]bool{}
	switch v := data["completedQuests"].(type) {
	case string:
		for _, part := range strings.Split(v, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && id > 0 {
				out[id] = true
			}
		}
	case []any:
		for _, item := range v {
			if id := int64(numberOf(item)); id > 0 {
				out[id] = true
			}
		}
	}
	return out
}

// ChainOf answers cadena_mision for a quest id, or for a name when the id is 0.
func (g *Service) ChainOf(questID int64, nombre string) (any, error) {
	if questID <= 0 && strings.TrimSpace(nombre) == "" {
		return nil, errors.New("falta quest_id o nombre de la misión")
	}
	w, _ := g.loadWorld()
	ch, _, hasChar := g.activeCharacter()
	completed := map[int64]bool{}
	if hasChar {
		completed = completedSet(ch.data)
	}
	questie := questieExport{}
	if hasChar {
		questie = readQuestie(ch.data)
	}
	id := questID
	if id <= 0 {
		id = g.findQuestID(nombre)
		if id <= 0 {
			return map[string]any{"available": true, "source": sourceSV, "encontrado": false,
				"nota": fmt.Sprintf("No encuentro la misión «%s» en lo que he visto. Di su nombre tal como sale en el juego o su quest_id.", nombre)}, nil
		}
	}
	inLog := g.inLogSet()
	title := func(q int64) string {
		if g.store != nil {
			if k, err := g.store.GetQuest(q); err == nil && k != nil && k.Title != "" {
				return k.Title
			}
		}
		if qq, ok := questie.quest(q); ok && qq.Name != "" {
			return qq.Name
		}
		if c, ok := w.Chains[q]; ok && c.Quest != "" {
			return c.Quest
		}
		return fmt.Sprintf("misión %d", q)
	}
	state := func(q int64) string {
		switch {
		case completed[q]:
			return "hecha"
		case inLog[q]:
			return "en registro"
		}
		return "pendiente"
	}

	// Backwards: every quest that must come before, oldest first.
	pre := g.chainPre(w, questie, id)
	// Forwards: the quests this one unlocks, in order.
	next := g.chainNext(w, questie, id)

	mine := chainStep{ID: id, Titulo: title(id), Estado: state(id)}
	var before, after []chainStep
	for _, q := range pre {
		before = append(before, chainStep{ID: q, Titulo: title(q), Estado: state(q)})
	}
	for _, q := range next {
		after = append(after, chainStep{ID: q, Titulo: title(q), Estado: state(q)})
	}
	// The direct prerequisites the character still lacks (the quests right before this one).
	var missing []chainStep
	for _, q := range g.directPre(w, questie, id) {
		if !completed[q] {
			missing = append(missing, chainStep{ID: q, Titulo: title(q), Estado: state(q)})
		}
	}

	order := make([]map[string]any, 0, len(before)+len(after)+1)
	for _, s := range append(append(append([]chainStep{}, before...), mine), after...) {
		order = append(order, map[string]any{"id": s.ID, "titulo": s.Titulo, "estado": s.Estado})
	}
	for i := range order {
		order[i]["orden"] = i + 1
	}

	out := map[string]any{
		"encontrado":    true,
		"mision":        map[string]any{"id": mine.ID, "titulo": mine.Titulo, "estado": mine.Estado},
		"anteriores":    stepViews(before),
		"siguientes":    stepViews(after),
		"faltan":        stepViews(missing),
		"ordenSugerido": order,
		"fuentes": map[string]any{
			"mundo":   w.Known && len(w.Chains) > 0,
			"questie": questie.Available,
		},
	}
	switch {
	case len(missing) > 0:
		out["resumen"] = fmt.Sprintf("Antes de «%s» faltan %d misión(es): %s.", mine.Titulo, len(missing), joinTitles(missing))
	case len(before) > 0:
		out["resumen"] = "Sus anteriores ya están hechas o en registro."
	}
	if len(after) > 0 {
		out["desbloquea"] = joinTitles(after)
	}
	if len(before) == 0 && len(after) == 0 {
		out["nota"] = "No tengo la cadena de esta misión: ni Questie ni el orden que el addon vio (aceptar y entregar en el mismo NPC) la traen."
	}
	out["supuestos"] = []string{
		"El orden sugerido es el de la cadena conocida: primero lo anterior, luego esta misión y después lo que desbloquea.",
		"«Hecha» viene de las misiones completadas del personaje; «en registro» de su registro.",
	}
	return available(sourceSV, w.At, out), nil
}

// chainPre walks the prerequisites back from id: the quests before it, oldest first, each once.
func (g *Service) chainPre(w worldData, questie questieExport, id int64) []int64 {
	var out []int64
	seen := map[int64]bool{id: true}
	var walk func(q int64, depth int)
	walk = func(q int64, depth int) {
		if depth >= worldMaxChain {
			return
		}
		for _, p := range g.directPre(w, questie, q) {
			if seen[p] {
				continue
			}
			seen[p] = true
			walk(p, depth+1)
			out = append(out, p)
		}
	}
	walk(id, 0)
	return out
}

// chainNext follows the next quests from id, in order.
func (g *Service) chainNext(w worldData, questie questieExport, id int64) []int64 {
	var out []int64
	seen := map[int64]bool{id: true}
	cur := id
	for len(out) < worldMaxChain {
		nxt := g.nextOf(w, questie, cur)
		if nxt <= 0 || seen[nxt] {
			break
		}
		seen[nxt] = true
		out = append(out, nxt)
		cur = nxt
	}
	return out
}

// directPre lists the quests that come right before q: the world chain's pre list and Questie's.
func (g *Service) directPre(w worldData, questie questieExport, q int64) []int64 {
	var out []int64
	have := map[int64]bool{}
	add := func(p int64) {
		if p > 0 && p != q && !have[p] {
			have[p] = true
			out = append(out, p)
		}
	}
	if c, ok := w.Chains[q]; ok {
		for _, p := range c.Pre {
			add(p)
		}
	}
	if qq, ok := questie.quest(q); ok {
		for _, p := range qq.PreQuests {
			add(p)
		}
	}
	return out
}

// nextOf is the quest that follows q: the world chain's next, else Questie's.
func (g *Service) nextOf(w worldData, questie questieExport, q int64) int64 {
	if c, ok := w.Chains[q]; ok && c.Next > 0 {
		return c.Next
	}
	if qq, ok := questie.quest(q); ok && qq.NextQuest > 0 {
		return qq.NextQuest
	}
	return 0
}

// findQuestID finds a quest by its name in the knowledge store: the exact title first, else the
// only match. 0 when there is none or several partial matches.
func (g *Service) findQuestID(nombre string) int64 {
	if g.store == nil {
		return 0
	}
	list, err := g.store.SearchQuests(strings.TrimSpace(nombre), 10)
	if err != nil {
		return 0
	}
	needle := fold(nombre)
	for _, q := range list {
		if fold(q.Title) == needle {
			return q.QuestID
		}
	}
	if len(list) == 1 {
		return list[0].QuestID
	}
	return 0
}

// inLogSet is the set of quests in the active character's log.
func (g *Service) inLogSet() map[int64]bool {
	out := map[int64]bool{}
	gd, ok := g.loadGearData()
	if !ok {
		return out
	}
	for id, q := range gd.Quests {
		if q.InLog {
			out[id] = true
		}
	}
	return out
}

func stepViews(list []chainStep) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		out = append(out, map[string]any{"id": s.ID, "titulo": s.Titulo, "estado": s.Estado})
	}
	return out
}

func joinTitles(list []chainStep) string {
	names := make([]string, 0, len(list))
	for _, s := range list {
		names = append(names, "«"+s.Titulo+"»")
	}
	return strings.Join(names, ", ")
}
