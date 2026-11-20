package gamedata

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// Macros: the player's macros as the addon exported them (characters[key].macros, account and
// character lists), the book of names a macro may use, and the placeholders that let the assistant
// write a macro with a spell by key or by id: the name comes from the player's own spellbook, so a
// Spanish client gets Spanish names. Nothing here changes the game; /compa macro does, when the
// player types it.

// HintMacros tells the player how to get the macros into the app.
const HintMacros = "No tengo tus macros: escribe /compa sync en el juego"

// msgMacros is the live channel message type of the macros block (bridge.MsgMacros).
const msgMacros = bridge.MsgMacros

// macroAbilityIDs maps an ability key to the spell ids of its ranks. The ids are the warrior table of
// the theory model (theory/warrior/ids.go) and the Classic ids of the same spells; a rank the
// player does not know is simply not in the book. The key never reaches the answer: the name does.
var macroAbilityIDs = map[string][]int64{
	"heroic_strike":    {78, 284, 285, 1608, 11564, 11565, 11566, 11567, 25286, 29707, 30324},
	"overpower":        {7384, 7887, 11584, 11585},
	"execute":          {5308, 20658, 20660, 20661, 20662, 25234, 25236},
	"rend":             {772, 6546, 6547, 6548, 11572, 11573, 11574},
	"mortal_strike":    {12294, 21551, 21552, 21553, 25248, 30330},
	"bloodthirst":      {23881, 23892, 23893, 23894, 25251, 30335},
	"slam":             {1464, 8820, 11604, 11605, 25242},
	"whirlwind":        {1680},
	"cleave":           {845, 7369, 11608, 11609, 20569, 25231},
	"death_wish":       {12328},
	"sunder_armor":     {7386, 7405, 8380, 11596, 11597},
	"revenge":          {6572, 6574, 7379, 11600, 11601, 25288},
	"battle_shout":     {6673, 5242, 6192, 11549, 11550, 11551, 25289},
	"bloodrage":        {2687},
	"berserker_rage":   {18499},
	"victory_rush":     {34428},
	"recklessness":     {1719},
	"sweeping_strikes": {12292},
	"charge":           {100, 6178, 11578},
	"intercept":        {20252, 20616, 20617},
	"hamstring":        {1715},
	"battle_stance":    {2457},
	"defensive_stance": {71},
	"berserker_stance": {2458},
}

var placeholderRE = regexp.MustCompile(`\{([A-Za-z_]+|id:\d+)\}`)

// MacroBookOf reads the names a macro may use from the active character: the spellbook (localized
// names with their ids) and the item names of the bags and the equipment. ok is false when there
// is no active character.
func (g *Service) MacroBookOf() (MacroBook, bool) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return MacroBook{}, false
	}
	var book MacroBook
	spells := readKnownSpells(ch.data["spells"])
	for _, sp := range spells {
		book.Spells = append(book.Spells, SpellRef{ID: sp.id, Name: sp.name})
	}
	book.HaveSpells = len(spells) > 0
	seen := map[string]bool{}
	for _, sec := range []string{"inventory", "equipment"} {
		collectItemNames(ch.data[sec], 0, seen, &book.Items)
	}
	book.HaveItems = ch.data["inventory"] != nil
	return book, true
}

// collectItemNames walks an inventory or equipment section and collects the "name" of every item.
func collectItemNames(v any, depth int, seen map[string]bool, out *[]string) {
	if depth > 4 {
		return
	}
	switch x := v.(type) {
	case map[string]any:
		if n := str(x["name"]); n != "" && !seen[strings.ToLower(n)] {
			seen[strings.ToLower(n)] = true
			*out = append(*out, n)
		}
		for _, val := range x {
			collectItemNames(val, depth+1, seen, out)
		}
	case []any:
		for _, val := range x {
			collectItemNames(val, depth+1, seen, out)
		}
	}
}

// spellByID returns the book's spell with the id, if the player knows it.
func (b MacroBook) spellByID(id int64) (SpellRef, bool) {
	for _, sp := range b.Spells {
		if sp.ID == id {
			return sp, true
		}
	}
	return SpellRef{}, false
}

// ResolveMacroPlaceholders replaces {clave} (an ability key, such as {overpower}) and {id:N} (a spell
// id) with the spell name from the player's book. For a key, the highest rank the player knows is
// used. It returns the resolved line and the problems: a placeholder the book cannot resolve.
func ResolveMacroPlaceholders(line string, book MacroBook) (string, []string) {
	var problems []string
	out := placeholderRE.ReplaceAllStringFunc(line, func(tok string) string {
		key := strings.Trim(tok, "{}")
		if rest, ok := strings.CutPrefix(key, "id:"); ok {
			id, err := strconv.ParseInt(rest, 10, 64)
			if err == nil {
				if sp, found := book.spellByID(id); found {
					return sp.Name
				}
			}
			problems = append(problems, fmt.Sprintf("el hechizo %s no está en tu libro de hechizos", rest))
			return tok
		}
		ids, known := macroAbilityIDs[key]
		if !known {
			problems = append(problems, fmt.Sprintf("«%s» no es un hechizo conocido por el asistente", key))
			return tok
		}
		best, found := SpellRef{}, false
		for _, id := range ids {
			if sp, ok := book.spellByID(id); ok && (!found || sp.ID > best.ID) {
				best, found = sp, true
			}
		}
		if !found {
			problems = append(problems, fmt.Sprintf("«%s» no está en tu libro de hechizos", key))
			return tok
		}
		return best.Name
	})
	return out, problems
}

// Macros returns the exported macros of the active character: the account list and the character
// list, each with its used and maximum counts.
func (g *Service) Macros() (any, error) {
	ch, _, ok := g.activeCharacter()
	if !ok {
		return unavailable(HintSync), nil
	}
	block := asMap(ch.data["macros"])
	if block == nil {
		return unavailable(HintMacros), nil
	}
	if v, has := block["available"]; has && !truthy(v) {
		u := unavailable(HintMacros)
		if r := str(block["reason"]); r != "" {
			u["motivo"] = r
		}
		return u, nil
	}
	fields := map[string]any{
		"cuenta":    asMap(block["account"]),
		"personaje": asMap(block["character"]),
	}
	return available(sourceSV, ch.updatedAt, fields), nil
}

// macroList returns the macros of one scope ("account" or "character") as name, icon and body.
func macroList(block map[string]any, scope string) []macroEntry {
	sec := asMap(block[scope])
	if sec == nil {
		return nil
	}
	var out []macroEntry
	for _, item := range listOf(sec["list"]) {
		m := asMap(item)
		if m == nil {
			continue
		}
		name := str(m["name"])
		if name == "" {
			continue
		}
		out = append(out, macroEntry{Name: name, Body: str(m["body"]), Scope: scope, Index: int(numberOf(m["index"]))})
	}
	return out
}

// macroEntry is one exported macro.
type macroEntry struct {
	Name  string
	Body  string
	Scope string
	Index int
}

// MacrosOf returns the exported macros of the active character (both scopes) and the counts.
func (g *Service) MacrosOf() (list []macroEntry, usedAccount, usedChar, maxAccount, maxChar int, ok bool, hint string) {
	ch, _, found := g.activeCharacter()
	if !found {
		return nil, 0, 0, 0, 0, false, HintSync
	}
	block := asMap(ch.data["macros"])
	if block == nil {
		return nil, 0, 0, 0, 0, false, HintMacros
	}
	if v, has := block["available"]; has && !truthy(v) {
		return nil, 0, 0, 0, 0, false, HintMacros
	}
	acc, chr := asMap(block["account"]), asMap(block["character"])
	list = append(macroList(block, "account"), macroList(block, "character")...)
	return list, int(numberOf(acc["used"])), int(numberOf(chr["used"])),
		int(numberOf(acc["max"])), int(numberOf(chr["max"])), true, ""
}
