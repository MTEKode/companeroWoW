package gamedata

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// buscar_objeto: where an item comes from, its stats, whether the class can use it, and
// whether it is an upgrade. It looks in the candidates, the equipment, the bags, the quest
// rewards, the dungeon loot and the knowledge the addon keeps from quests it has seen.

// linkNameRE reads the name inside an item link: |h[Name]|h.
var linkNameRE = regexp.MustCompile(`\[([^\]]+)\]`)

// itemHit is what one block knows about one item id. quests lists the knowledge quests whose
// rewards hold the item.
type itemHit struct {
	name   string
	count  int
	quests []int64
}

// ItemLookup answers buscar_objeto. Either nombre (a name or part of one) or itemID is set.
func (g *Service) ItemLookup(nombre string, itemID int64) (any, error) {
	gd, ok := g.loadGearData()
	if !ok {
		return unavailable(HintSync), nil
	}
	needle := fold(nombre)
	if needle == "" && itemID <= 0 {
		return unavailable("Indica el nombre o el item_id del objeto."), nil
	}
	bags := g.bagItems()
	knowledge := g.knowledgeItems()

	// Every item id that matches, with the best name known for it.
	names := map[int64]string{}
	found := func(id int64, name string) {
		if id <= 0 {
			return
		}
		if _, seen := names[id]; !seen || (names[id] == "" && name != "") {
			names[id] = name
		}
	}
	matches := func(id int64, name string) bool {
		return (itemID > 0 && id == itemID) || (needle != "" && strings.Contains(fold(name), needle))
	}
	for id, c := range gd.Candidates {
		if matches(id, c.item.Name) {
			found(id, c.item.Name)
		}
	}
	for _, it := range gd.Equipped {
		if matches(it.ItemID, it.Name) {
			found(it.ItemID, it.Name)
		}
	}
	for id, hit := range bags {
		if matches(id, hit.name) {
			found(id, hit.name)
		}
	}
	for id, hit := range knowledge {
		if matches(id, hit.name) {
			found(id, hit.name)
		}
	}
	if itemID > 0 && gd.knownID(itemID, bags, knowledge) {
		found(itemID, "")
	}
	if len(names) == 0 {
		return available(sourceSV, gd.At, map[string]any{
			"encontrado": false,
			"nota":       "No tengo ese objeto en tus datos (equipo, bolsas, misiones, mazmorras ni conocimiento). Prueba con buscar_web o indica el item_id.",
		}), nil
	}

	ids := make([]int64, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	primary := ids[0]
	if itemID > 0 {
		primary = itemID
	}
	fields := gd.itemView(primary, names[primary], bags, knowledge)
	var others []string
	for _, id := range ids {
		if id != primary {
			others = append(others, fmt.Sprintf("%s (item_id %d)", gd.bestName(id, names[id]), id))
		}
	}
	if len(others) > 0 {
		fields["otrasCoincidencias"] = others
	}
	return available(sourceSV, gd.At, fields), nil
}

// knownID reports whether any block mentions the item id.
func (gd gearData) knownID(id int64, bags, knowledge map[int64]itemHit) bool {
	if _, ok := gd.Candidates[id]; ok {
		return true
	}
	if _, ok := bags[id]; ok {
		return true
	}
	if _, ok := knowledge[id]; ok {
		return true
	}
	for _, it := range gd.Equipped {
		if it.ItemID == id {
			return true
		}
	}
	for _, q := range gd.Quests {
		if containsID(q.RewardChoice, id) || containsID(q.RewardFixed, id) {
			return true
		}
	}
	for _, d := range gd.Dungeons {
		for _, b := range d.Bosses {
			if containsID(b.Loot, id) {
				return true
			}
		}
	}
	return false
}

// bestName is the name of an item: the one given, else the candidate's, else a placeholder.
func (gd gearData) bestName(id int64, name string) string {
	if name != "" {
		return name
	}
	if c, ok := gd.Candidates[id]; ok && c.item.Name != "" {
		return c.item.Name
	}
	return fmt.Sprintf("objeto %d", id)
}

// itemView builds the answer for one item id.
func (gd gearData) itemView(id int64, name string, bags, knowledge map[int64]itemHit) map[string]any {
	obj := map[string]any{"itemID": id, "nombre": gd.bestName(id, name)}
	fields := map[string]any{"encontrado": true, "objeto": obj}

	var grp gearGroup
	var inGroup bool
	c, isCandidate := gd.Candidates[id]
	if isCandidate {
		it := c.item
		obj["ilvl"] = it.ItemLevel
		obj["requiereNivel"] = it.RequiredLevel
		obj["calidad"] = qualityName(it.Quality)
		if s := statsText(it.Stats); s != "" {
			obj["estadisticas"] = s
		}
		if grp, inGroup = groupOf(it); inGroup {
			obj["ranura"] = grp.Name
		}
		usable, why := gd.usable(it)
		obj["usable"] = usable
		if !usable {
			obj["motivoNoUsable"] = why
		}
	}
	for slotID, it := range gd.Equipped {
		if it.ItemID != id {
			continue
		}
		if slot, ok := groupForSlot(slotID); ok {
			fields["equipado"] = slot.Name
		}
		if !isCandidate {
			obj["ilvl"] = it.ItemLevel
			obj["calidad"] = qualityName(it.Quality)
			if s := statsText(it.Stats); s != "" {
				obj["estadisticas"] = s
			}
		}
	}
	if hit, ok := bags[id]; ok {
		fields["enInventario"] = hit.count
	}
	if hit, ok := knowledge[id]; ok && obj["nombre"] == fmt.Sprintf("objeto %d", id) && hit.name != "" {
		obj["nombre"] = hit.name
	}

	// Sources: the candidate's own, the quest rewards, and the dungeon loot. Each once.
	var sources []itemSource
	seen := map[itemSource]bool{}
	add := func(s itemSource) {
		if !seen[s] {
			seen[s] = true
			sources = append(sources, s)
		}
	}
	if isCandidate {
		for _, s := range c.Sources {
			add(s)
		}
	}
	for _, qid := range sortedQuestIDs(gd.Quests) {
		q := gd.Quests[qid]
		if containsID(q.RewardChoice, id) || containsID(q.RewardFixed, id) {
			add(itemSource{Kind: "quest", QuestID: qid, Choice: containsID(q.RewardChoice, id)})
		}
	}
	for _, d := range gd.Dungeons {
		for _, b := range d.Bosses {
			if containsID(b.Loot, id) {
				add(itemSource{Kind: "dungeon", DungeonKey: d.Key, Boss: b.Name})
			}
		}
	}
	for _, qid := range knowledge[id].quests {
		add(itemSource{Kind: "quest", QuestID: qid})
	}
	fuentes := []map[string]any{}
	for _, s := range sources {
		fuentes = append(fuentes, gd.sourceView(s, gd.sourceState(s)))
	}
	fields["fuentes"] = fuentes

	if isCandidate && inGroup {
		fields["mejoraTuEquipo"] = gd.upgradeText(c, grp)
	}
	if len(gd.Notes) > 0 {
		fields["avisos"] = gd.Notes
	}
	return fields
}

// upgradeText says whether a candidate improves its slot, and how.
func (gd gearData) upgradeText(c gearCandidate, grp gearGroup) map[string]any {
	up, keep := gd.evaluate(c.item, grp)
	if !keep {
		return map[string]any{"si": false, "motivo": "no supera a lo que llevas en esa ranura"}
	}
	win, _ := gd.itemWindow(c)
	if win == "" {
		win = "fuera de tu nivel"
	}
	return map[string]any{
		"si":        true,
		"ranura":    grp.Name,
		"ilvlDelta": signed(float64(up.Delta)),
		"cambios":   changesText(up.Changes),
		"ventana":   win,
	}
}

// groupForSlot is the slot group that holds an inventory slot id.
func groupForSlot(slotID int) (gearGroup, bool) {
	for _, grp := range gearGroups {
		for _, s := range grp.Slots {
			if s == slotID {
				return grp, true
			}
		}
	}
	return gearGroup{}, false
}

func containsID(list []int64, id int64) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// bagItems reads the bags of the active character: the name from the link and the count.
func (g *Service) bagItems() map[int64]itemHit {
	out := map[int64]itemHit{}
	ch, _, ok := g.activeCharacter()
	if !ok {
		return out
	}
	for _, item := range listOf(asMap(ch.data["inventory"])["items"]) {
		m := asMap(item)
		id := int64(numberOf(m["itemID"]))
		if m == nil || id == 0 {
			continue
		}
		hit := out[id]
		hit.count += int(numberOf(m["count"]))
		if hit.name == "" {
			hit.name = linkName(str(m["link"]))
		}
		out[id] = hit
	}
	return out
}

// knowledgeItems reads the item rewards of the quests the addon has seen: the store's quests first, the
// SavedVariables knowledge block when the store has none.
func (g *Service) knowledgeItems() map[int64]itemHit {
	out := map[int64]itemHit{}
	if g.store != nil {
		if quests, err := g.store.SearchQuests("", 5000); err == nil && len(quests) > 0 {
			for _, q := range quests {
				var rewards map[string]any
				if err := json.Unmarshal([]byte(q.RewardsJSON), &rewards); err != nil {
					continue
				}
				addRewardItems(out, q.QuestID, rewards)
			}
			return out
		}
	}
	sv := g.savedVars()
	if sv == nil {
		return out
	}
	for _, e := range entriesOf(asMap(sv.Raw["knowledge"])["quests"]) {
		qm := asMap(e.value)
		addRewardItems(out, questIDOf(qm, e.key), asMap(qm["rewards"]))
	}
	return out
}

// addRewardItems adds the items of a quest's rewards to out, naming the quest qid as a source.
func addRewardItems(out map[int64]itemHit, qid int64, rewards map[string]any) {
	for _, item := range listOf(rewards["items"]) {
		m := asMap(item)
		id := int64(numberOf(m["itemID"]))
		if m == nil || id == 0 {
			continue
		}
		hit := out[id]
		if hit.name == "" {
			hit.name = linkName(str(m["link"]))
		}
		if qid != 0 && !containsID(hit.quests, qid) {
			hit.quests = append(hit.quests, qid)
		}
		out[id] = hit
	}
}

// linkName is the name inside an item link, or "" when there is none.
func linkName(link string) string {
	if m := linkNameRE.FindStringSubmatch(link); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}
