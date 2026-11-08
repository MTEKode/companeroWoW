// Package glossary maps the main English zone and city names of World of Warcraft to the
// Spanish names of the Forever client. The web sources are in English; the assistant must
// answer with the in-game Spanish names. Annotate adds the Spanish name next to an English
// one in the text the web search returns, so that the model never has to translate it.
package glossary

import (
	"regexp"
	"sort"
	"strings"
)

// Entry is one zone or city: its English name as the wiki writes it and its Spanish name
// in the game. An empty Spanish name means the name is not confirmed yet: the entry is
// listed, but nothing is annotated with it.
type Entry struct {
	EN string
	ES string
}

// Entries is the glossary. Names that are the same in both languages are listed too, so that
// the model knows they are right as they are; Annotate skips them.
var Entries = []Entry{
	{"Stormwind", "Ventormenta"},
	{"Ironforge", "Forjaz"},
	{"Darnassus", "Darnassus"},
	{"Orgrimmar", "Orgrimmar"},
	{"Thunder Bluff", "Cima del Trueno"},
	{"Undercity", "Entrañas"},
	{"Elwynn Forest", "Bosque de Elwynn"},
	{"Westfall", "Páramos de Poniente"},
	{"Redridge Mountains", "Montañas Crestagrana"},
	{"Duskwood", "Bosque del Ocaso"},
	{"Loch Modan", ""},
	{"Dun Morogh", "Dun Morogh"},
	{"Teldrassil", "Teldrassil"},
	{"Darkshore", "Costa Oscura"},
	{"Durotar", "Durotar"},
	{"Mulgore", "Mulgore"},
	{"Tirisfal Glades", "Claros de Tirisfal"},
	{"Silverpine Forest", "Bosque de Argénteos"},
	{"The Barrens", "Los Baldíos"},
	{"Stonetalon Mountains", "Sierra Espolón"},
	{"Ashenvale", "Vallefresno"},
	{"Wetlands", "Los Humedales"},
	{"Hillsbrad Foothills", "Laderas de Trabalomas"},
}

// cityWord is the optional word that some capital names carry in the wiki ("Stormwind City").
const cityWord = ` City`

var (
	spanish = map[string]string{}
	matcher *regexp.Regexp
)

func init() {
	names := make([]string, 0, len(Entries))
	for _, e := range Entries {
		if e.ES == "" || e.ES == e.EN {
			continue
		}
		spanish[e.EN] = e.ES
		names = append(names, e.EN)
	}
	// Longest first, so that "Thunder Bluff" wins over a shorter overlapping name.
	sort.SliceStable(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	matcher = regexp.MustCompile(`\b(?:` + strings.Join(quoted, "|") + `)(?:` + cityWord + `)?\b`)
}

// Spanish returns the Spanish name of an English zone or city, or "" when the glossary has
// none. The lookup is exact and case-sensitive, as the names are written in the wiki.
func Spanish(en string) string {
	return spanish[en]
}

// Annotate returns text with the Spanish name in parentheses after each English zone or
// city that has one: "Stormwind City" becomes "Stormwind City (Ventormenta)". A name that
// already carries a parenthesis after it is left as it is, so that annotating twice changes
// nothing.
func Annotate(text string) string {
	idx := matcher.FindAllStringIndex(text, -1)
	if len(idx) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, m := range idx {
		b.WriteString(text[last:m[1]])
		last = m[1]
		if strings.HasPrefix(text[m[1]:], " (") {
			continue
		}
		b.WriteString(" (" + spanish[baseName(text[m[0]:m[1]])] + ")")
	}
	b.WriteString(text[last:])
	return b.String()
}

// baseName strips the optional " City" suffix from a matched name.
func baseName(match string) string {
	return strings.TrimSuffix(match, cityWord)
}
