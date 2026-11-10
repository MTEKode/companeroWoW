package websearch

import (
	"sort"
	"strings"
	"unicode"
)

// wftPhrases and wftWords turn the Spanish words of a question into the English words that
// the site's paths use ("Sastrería" → "tailoring", "talentos" → "talents"). Keys are folded:
// lower case, no accents. Phrases are replaced first, longest first.
var wftPhrases = map[string]string{
	"primeros auxilios":      "first aid",
	"caballero de la muerte": "death knight",
	"cazador de demonios":    "demon hunter",
}

var wftWords = map[string]string{
	// Classes.
	"sacerdote": "priest", "guerrero": "warrior", "picaro": "rogue", "mago": "mage",
	"brujo": "warlock", "cazador": "hunter", "chaman": "shaman", "druida": "druid",
	"paladin": "paladin",
	// Professions.
	"herreria": "blacksmithing", "peleteria": "leatherworking", "sastreria": "tailoring",
	"encantamiento": "enchanting", "ingenieria": "engineering", "alquimia": "alchemy",
	"mineria": "mining", "herboristeria": "herbalism", "desuello": "skinning",
	"cocina": "cooking", "pesca": "fishing",
	// Game concepts.
	"talento": "talents", "talentos": "talents", "hechizo": "spells", "hechizos": "spells",
	"habilidad": "abilities", "habilidades": "abilities", "rotacion": "rotation",
	"mazmorra": "dungeons", "mazmorras": "dungeons", "banda": "raid", "bandas": "raids",
	"build": "builds", "builds": "builds", "especializacion": "spec",
	"reputacion": "reputation", "razas": "races", "raza": "race",
	"entrenador": "trainer", "nivel": "level", "misiones": "quests", "mision": "quest",
	"tanque": "tank", "sanador": "healer", "dano": "damage", "curacion": "healing",
	"pvp": "pvp", "honor": "honor", "ranura": "slot", "equipo": "gear",
}

// wftStopWords carry no meaning for the path match.
var wftStopWords = map[string]bool{
	"de": true, "la": true, "el": true, "en": true, "los": true, "las": true, "del": true,
	"y": true, "a": true, "un": true, "una": true, "para": true, "con": true, "por": true,
	"que": true, "mi": true, "su": true, "al": true, "o": true, "the": true, "of": true,
	"and": true, "for": true, "in": true, "forever": true, "wow": true, "como": true,
	"cual": true, "donde": true, "es": true, "se": true,
}

// foldText lower-cases s and removes the accents of Spanish letters.
func foldText(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'á', 'à', 'ä', 'â':
			r = 'a'
		case 'é', 'è', 'ë', 'ê':
			r = 'e'
		case 'í', 'ì', 'ï', 'î':
			r = 'i'
		case 'ó', 'ò', 'ö', 'ô':
			r = 'o'
		case 'ú', 'ù', 'ü', 'û':
			r = 'u'
		case 'ñ':
			r = 'n'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// translateWords returns the English words of a query, without stop words and without
// duplicates, in the order they were written.
func translateWords(query string) []string {
	s := foldText(query)
	phrases := make([]string, 0, len(wftPhrases))
	for p := range wftPhrases {
		phrases = append(phrases, p)
	}
	sort.Slice(phrases, func(i, j int) bool { return len(phrases[i]) > len(phrases[j]) })
	for _, p := range phrases {
		s = strings.ReplaceAll(s, p, wftPhrases[p])
	}
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if en, ok := wftWords[w]; ok {
			w = en
		}
		if wftStopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}
