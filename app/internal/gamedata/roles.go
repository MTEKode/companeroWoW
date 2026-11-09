package gamedata

import "strings"

// Role tags for spells, read from the spell name in English and Spanish. They are approximate:
// the assistant presents them as such and never adds a spell that is not in the spellbook.

// roleWords lists, per role, the words that mark a spell with it. Roles are reported in this order.
var roleWords = []struct {
	role  string
	words []string
}{
	{"defensivo", []string{
		"fade", "desvanecimiento", "power word: shield", "palabra del poder: escudo", "escudo del poder",
		"desperate prayer", "plegaria desesperada", "ice block", "bloque de hielo", "divine shield", "escudo divino",
		"evasion", "evasión", "vanish", "desaparición", "barkskin", "corteza", "cloak of shadows", "capa de las sombras",
		"shield wall", "muro de escudo", "last stand", "última resistencia", "lay on hands", "imposición de manos",
		"ice barrier", "barrera de hielo", "blessing of protection", "bendición de protección",
	}},
	{"curación", []string{
		"heal", "curación", "curacion", "renew", "renovar", "regrowth", "recrecimiento", "rejuvenation",
		"rejuvenecimiento", "lifebloom", "flor de vida", "prayer of healing", "plegaria de curación",
	}},
	{"control", []string{
		"scream", "grito", "polymorph", "polimorfia", "fear", "miedo", "sap", "somnolencia", "stun", "aturdir",
		"interrupt", "interrumpir", "silence", "silencio", "hex", "maleficio", "cyclone", "ciclón",
	}},
	{"utilidad", []string{
		"dispel", "disipar", "inner fire", "fuego interior", "fortitude", "fortaleza", "blessing", "bendición",
		"resurrection", "resurrección", "portal", "teleport", "teletransport", "mount", "montura", "summon", "invocar",
	}},
	{"daño", []string{
		"smite", "castigo", "mind blast", "explosión mental", "mind flay", "tortura mental",
		"shadow word: pain", "palabra de las sombras: dolor", "holy fire", "fuego sagrado",
		"lightning bolt", "rayo", "earth shock", "choque terrestre", "frost shock", "choque de escarcha",
		"flame shock", "choque de llamas", "fireball", "bola de fuego", "frostbolt", "rayo de escarcha",
		"shadow bolt", "descarga de sombras", "immolate", "inmolar", "corruption", "corrupción",
		"moonfire", "fuego lunar", "wrath", "ira", "starfire", "estrella",
	}},
}

// spellRoles returns the role tags of a spell name, in roleWords order.
func spellRoles(name string) []string {
	low := strings.ToLower(name)
	var out []string
	for _, rw := range roleWords {
		for _, w := range rw.words {
			if strings.Contains(low, w) {
				out = append(out, rw.role)
				break
			}
		}
	}
	return out
}
