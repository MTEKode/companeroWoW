package gamedata

import (
	"fmt"
	"strconv"
	"strings"
)

// Rotation advice: the character's class, its specialisation, its level and the spells it knows,
// read from the spellbook export and the talents. The assistant builds the rotation from these
// spells only; this file never invents one.

const (
	// HintBars tells the player how to get the action bars into the app.
	HintBars = "No tengo tus barras de acción: escribe /compa sync en el juego"
	// HintCombats tells the player how to get the combat record into the app.
	HintCombats = "No tengo combates grabados: juega un combate y escribe /compa sync en el juego"

	maxRotationSpells = 60
	maxReviewCombats  = 10
)

// rotationInstructions is returned with the rotation so that the assistant follows it.
const rotationInstructions = "Construye la rotación solo con los hechizos de esta lista (nombre e id exactos). " +
	"Si un hechizo no está aquí, no lo recomiendes. Las etiquetas (daño, curación, defensivo, control, utilidad) " +
	"salen del nombre y son aproximadas. Para fuentes de la comunidad, busca en inglés con buscar_web usando " +
	"busquedaWeb y cita la fuente; no tomes datos de Classic ni de Retail."

// knownSpell is one entry of the spellbook export.
type knownSpell struct {
	id    int64
	name  string
	rank  string
	roles []string
}

// readKnownSpells decodes the spells export of a character, in the order the addon wrote it.
func readKnownSpells(v any) []knownSpell {
	var out []knownSpell
	for _, item := range listOf(v) {
		m := asMap(item)
		if m == nil {
			continue
		}
		name := str(m["name"])
		if name == "" {
			continue
		}
		out = append(out, knownSpell{
			id:    int64(numberOf(m["spellID"])),
			name:  name,
			rank:  str(m["rank"]),
			roles: spellRoles(name),
		})
	}
	return out
}

// hasRole reports whether the spell carries the role tag.
func (s knownSpell) hasRole(role string) bool {
	for _, r := range s.roles {
		if r == role {
			return true
		}
	}
	return false
}

// Rotation returns what the assistant needs to build a rotation: class, level, specialisation
// (as the addon wrote it, or the talent tree with the most points), talents and the known spells.
func (g *Service) Rotation() (any, error) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return unavailable(HintSync), nil
	}
	spells := readKnownSpells(ch.data["spells"])
	if len(spells) == 0 {
		return unavailable("No tengo tu libro de hechizos: escribe /compa sync en el juego"), nil
	}
	char := asMap(ch.data["character"])
	talents := asMap(ch.data["talents"])
	level := int(numberOf(char["level"]))
	if live, ok := g.Live(); ok && live.Level > 0 {
		level = int(live.Level)
	}
	class := str(char["class"])
	spec, guessed := specOf(char, talents)

	list := make([]map[string]any, 0, len(spells))
	for i, sp := range spells {
		if i >= maxRotationSpells {
			break
		}
		item := map[string]any{"nombre": sp.name, "id": sp.id}
		if sp.rank != "" {
			item["rango"] = sp.rank
		}
		if len(sp.roles) > 0 {
			item["etiquetas"] = sp.roles
		}
		list = append(list, item)
	}
	search := "WoW leveling rotation Forever"
	if class != "" {
		search = class + " leveling rotation Forever"
	}
	fields := map[string]any{
		"clase":                   class,
		"nivel":                   level,
		"especializacion":         spec,
		"especializacionSupuesta": guessed,
		"hechizos":                list,
		"numeroHechizos":          len(spells),
		"busquedaWeb":             search,
		"instrucciones":           rotationInstructions,
	}
	if t := talentSummary(talents); t != "" {
		fields["talentos"] = t
	}
	return available(sourceSV, ch.updatedAt, fields), nil
}

// specOf names the specialisation: the addon's own summary when it has one, otherwise the talent
// tree with the most points. guessed is true for the second case.
func specOf(char, talents map[string]any) (string, bool) {
	if s := str(char["specOrTalentSummary"]); s != "" {
		return s, false
	}
	pts := treePoints(talents)
	best := -1
	for i, p := range pts {
		if p > 0 && (best < 0 || p > pts[best]) {
			best = i
		}
	}
	if best < 0 {
		return "", false
	}
	return fmt.Sprintf("árbol %d (%s)", best+1, talentSummary(talents)), true
}

// treePoints returns the points spent in each talent tree, in order.
func treePoints(talents map[string]any) []int {
	raw := asMap(talents["raw"])
	var out []int
	for _, v := range listOf(raw["points"]) {
		out = append(out, int(numberOf(v)))
	}
	return out
}

// talentSummary is the points per tree as "31/5/0", or "" when unknown.
func talentSummary(talents map[string]any) string {
	pts := treePoints(talents)
	if len(pts) == 0 {
		return ""
	}
	parts := make([]string, len(pts))
	for i, p := range pts {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, "/")
}
