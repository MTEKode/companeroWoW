package assistant

import (
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/skill"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// Intent is what a question is about. The classifier is deterministic: it scores Spanish
// keywords and phrases, with no AI call. Each intent gets only the context sub-blocks and the
// playbook excerpt that its questions need; the full skill is still sent to every provider.
type Intent string

const (
	IntentQuests      Intent = "misiones"
	IntentGear        Intent = "equipo"
	IntentBuild       Intent = "build"
	IntentCombat      Intent = "combate"
	IntentProfessions Intent = "profesiones"
	IntentTravel      Intent = "viaje"
	IntentTrade       Intent = "comercio"
	IntentDungeon     Intent = "mazmorra"
	IntentSession     Intent = "sesion"
	IntentEnemy       Intent = "enemigo"
	// IntentLore is a question about the world or the lore: it carries no game data.
	IntentLore Intent = "lore"
	// IntentGeneral is every question the classifier cannot place. It carries the short
	// orientation (where, who, tracked quest), so an unclear question still has its context.
	IntentGeneral Intent = "general"
)

// intentOrder breaks ties between intents with the same score: the more specific intents come first.
var intentOrder = []Intent{
	IntentProfessions, IntentDungeon, IntentEnemy, IntentGear, IntentBuild, IntentCombat,
	IntentTrade, IntentSession, IntentQuests, IntentTravel, IntentLore, IntentGeneral,
}

// intentContextKeys lists, in priority order, the parts of the player context that each intent
// carries. The parts are the keys of the contexto_jugador map (see gamedata.PlayerContext).
var intentContextKeys = map[Intent][]string{
	IntentQuests:      {"ubicacion", "misionRastreada", "equipo", "avisos", "datos"},
	IntentGear:        {"personaje", "equipo", "ubicacion", "avisos", "datos"},
	IntentBuild:       {"personaje", "avisos"},
	IntentCombat:      {"personaje", "objetivo", "avisos"},
	IntentProfessions: {"profesiones", "ubicacion", "entrenamiento", "rutaProfesiones", "personaje", "dinero", "avisos", "datos"},
	IntentTravel:      {"ubicacion", "avisos"},
	IntentTrade:       {"dinero", "profesiones", "personaje", "avisos"},
	IntentDungeon:     {"ubicacion", "personaje", "equipo", "avisos", "datos"},
	IntentSession:     {"ritmo", "personaje", "datos", "avisos"},
	IntentEnemy:       {"personaje", "objetivo", "avisos"},
	IntentLore:        nil,
	IntentGeneral:     {"ubicacion", "personaje", "misionRastreada", "avisos"},
}

// legacyContextKeys are the parts of the first version of the context block, in its order. The
// baseline that the eval compares with is that block (see compactContext).
var legacyContextKeys = []string{
	"ubicacion", "personaje", "misionRastreada", "ritmo", "datos", "avisos", "dinero", "objetivo",
	"profesiones", "entrenamiento", "rutaProfesiones",
}

// allContextKeys is every part the player context can carry: the legacy parts and the gear
// summary, which the first version never sent.
var allContextKeys = append(append([]string{}, legacyContextKeys...), "equipo")

// intentContextLimit caps the context block of one intent, header included, in runes. The
// block of the first version was allowed 1200 runes; most intents now fit in 600.
const intentContextLimit = 600

// cue is one clue of an intent: a word stem or a phrase, with its weight.
type cue struct {
	intent Intent
	weight int
	stem   string         // a word; matches a token that starts with it
	fuzzy  bool           // a stem with a typo allowance: a token one edit away also matches
	re     *regexp.Regexp // a phrase on the normalised question, padded with spaces
}

// cues is the keyword table. Stems are written without accents; a trailing "~" in a stem
// allows one typo (one edit, transposition included) in the token it is compared with.
var cues = buildCues()

func buildCues() []cue {
	var out []cue
	w := func(in Intent, weight int, stems ...string) {
		for _, s := range stems {
			c := cue{intent: in, weight: weight, stem: strings.TrimSuffix(s, "~")}
			c.fuzzy = strings.HasSuffix(s, "~") && len(c.stem) >= 6
			out = append(out, c)
		}
	}
	p := func(in Intent, weight int, pattern string) {
		out = append(out, cue{intent: in, weight: weight, re: regexp.MustCompile(pattern)})
	}

	// Professions and training: the trainers, the professions and what to learn.
	w(IntentProfessions, 3, "profesi~")
	w(IntentProfessions, 3, "herbor", "sastr", "alquim", "miner", "peleter", "herrer", "ingenier", "cocin", "pesca", "encantam")
	w(IntentProfessions, 2, "entren", "instructor", "recet")
	w(IntentProfessions, 1, "rango")
	p(IntentProfessions, 2, `\bprimeros auxilios\b`)

	// Dungeons and raids.
	w(IntentDungeon, 3, "mazmor~", "dungeon")
	w(IntentDungeon, 2, "instancia", "raid", "jefe", "botin")
	w(IntentDungeon, 1, "saco")

	// Enemies: one mob, an elite, a fight against it.
	w(IntentEnemy, 3, "enemig~", "elite")
	w(IntentEnemy, 2, "mob", "bicho", "monstruo", "hostil", "veredicto", "luch")
	p(IntentEnemy, 3, `\bpuedo con\b`)
	p(IntentEnemy, 2, `\b(dos|tres) a la vez\b`)

	// Gear: what is worn, what improves it and where an item comes from.
	w(IntentGear, 3, "equip")
	w(IntentGear, 2, "ranura", "casco", "armadura", "arma", "mejor", "consig", "conseg", "objeto", "ilvl")
	w(IntentGear, 1, "pecho", "anill", "ropa")
	p(IntentGear, 2, `\bnivel de objeto\b`)
	p(IntentGear, 2, `\bme sirve\b`)

	// Builds, talents, rotation and bars.
	w(IntentBuild, 3, "talent~", "rotacion~", "barra~", "macro", "build", "dps")
	w(IntentBuild, 2, "hechizo", "estadistic", "stat", "atajo", "tecla", "especializ", "coloc")
	w(IntentBuild, 1, "clase", "ordena")

	// Combat and deaths.
	w(IntentCombat, 3, "combat~", "muer", "mori")
	w(IntentCombat, 2, "mata", "defens", "vida", "cura", "fallo", "error", "dano", "pierd", "pocion")
	p(IntentCombat, 3, `\b(haciendo|hago|hace) mal\b`)

	// Gold, money and trade.
	w(IntentTrade, 3, "oro", "dinero")
	w(IntentTrade, 2, "plata", "cobre", "compr", "vend", "precio", "subasta", "tienda", "gast", "comerc", "mercado", "ahorr")
	w(IntentTrade, 1, "caro")

	// Pace and the play session.
	p(IntentSession, 3, `\bcuanto (me )?(falta|tardo|tardas|tarda|tardare|tardaria)\b`)
	p(IntentSession, 3, `\bque ritmo\b`)
	p(IntentSession, 3, `\bcomo ha ido\b`)
	w(IntentSession, 3, "ritmo", "sesion")
	w(IntentSession, 2, "resumen", "tiempo")
	w(IntentSession, 1, "minuto", "hora")

	// Quests: the log, the next step, leveling with quests and their value.
	w(IntentQuests, 3, "mision~", "quest", "entreg", "rastread")
	w(IntentQuests, 2, "encargo", "xp", "subi", "subo", "sube", "subes")
	w(IntentQuests, 1, "objetivo", "nivel")
	p(IntentQuests, 3, `\bque hago( ahora)?\b`)
	p(IntentQuests, 1, `\bque (me )?conviene\b`)

	// Travel and places: where something is, how to get there, which zone.
	p(IntentTravel, 3, `\bdonde estoy\b`)
	p(IntentTravel, 2, `\bdonde esta\b`)
	w(IntentTravel, 2, "lleg", "viaj", "ruta", "camino", "mapa", "ciudad", "ubicac", "coordenad", "zona",
		"marca", "lugar", "npc")
	w(IntentTravel, 1, "donde", "direcc")

	// Lore and general knowledge of Forever (answered from the web or the knowledge base).
	p(IntentLore, 3, `\bquien (es|fue|era|son|eran)\b`)
	p(IntentLore, 2, `\bque (es|son|significa|significan|fue)\b`)
	w(IntentLore, 3, "lore", "histor", "trasfond", "web")
	w(IntentLore, 2, "leyend", "cuent", "origen")

	// The screen: what is on it.
	w(IntentGeneral, 3, "pantalla", "captur")
	w(IntentGeneral, 2, "imagen")
	p(IntentGeneral, 3, `\bque ves\b`)
	return out
}

// foldQuestion lowercases a question, folds the accents and turns punctuation into spaces,
// so that "¿Qué hago?" and "que hago" read the same.
func foldQuestion(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		r = foldAccent(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

var accentFold = map[rune]rune{
	'á': 'a', 'à': 'a', 'ä': 'a', 'â': 'a', 'é': 'e', 'è': 'e', 'ë': 'e', 'ê': 'e',
	'í': 'i', 'ì': 'i', 'ï': 'i', 'î': 'i', 'ó': 'o', 'ò': 'o', 'ö': 'o', 'ô': 'o',
	'ú': 'u', 'ù': 'u', 'ü': 'u', 'û': 'u', 'ñ': 'n', 'ç': 'c',
}

func foldAccent(r rune) rune {
	if f, ok := accentFold[r]; ok {
		return f
	}
	return r
}

// classifyIntent returns the intent of a question. Each cue that matches adds its weight to its
// intent; the highest score wins, ties go to the intent that comes first in intentOrder, and a
// question with no cue is IntentGeneral.
func classifyIntent(question string) Intent {
	norm := foldQuestion(question)
	if norm == "" {
		return IntentGeneral
	}
	tokens := strings.Fields(norm)
	padded := " " + norm + " "
	score := map[Intent]int{}
	for _, c := range cues {
		hit := false
		if c.re != nil {
			hit = c.re.MatchString(padded)
		} else {
			hit = matchStem(tokens, c.stem, c.fuzzy)
		}
		if hit {
			score[c.intent] += c.weight
		}
	}
	best, bestScore := IntentGeneral, 0
	for _, in := range intentOrder {
		if score[in] > bestScore {
			best, bestScore = in, score[in]
		}
	}
	return best
}

// matchStem reports whether a token starts with stem. A fuzzy stem also accepts a token whose
// first letters are one edit away from it, so that "misoin" finds "misión".
func matchStem(tokens []string, stem string, fuzzy bool) bool {
	for _, t := range tokens {
		if strings.HasPrefix(t, stem) {
			return true
		}
		if fuzzy && nearStem(t, stem) {
			return true
		}
	}
	return false
}

// nearStem reports whether the start of tok is one edit away from stem, trying the prefix lengths
// around the stem's length.
func nearStem(tok, stem string) bool {
	tr, sr := []rune(tok), []rune(stem)
	for _, n := range []int{len(sr) - 1, len(sr), len(sr) + 1} {
		if n > 0 && n <= len(tr) && osaDistance(tr[:n], sr) <= 1 {
			return true
		}
	}
	return false
}

// osaDistance is the edit distance that counts a swap of two neighbouring letters as one edit.
func osaDistance(a, b []rune) int {
	la, lb := len(a), len(b)
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = minInt(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = minInt(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[la][lb]
}

func minInt(vs ...int) int {
	m := vs[0]
	for _, v := range vs[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

// The playbook excerpts come from the skill: each playbook block of SKILL.md sits between
// "<!-- pb:<intent> -->" and "<!-- /pb -->". The full skill is still sent to every provider.
const (
	pbOpen  = "<!-- pb:"
	pbClose = "<!-- /pb -->"
	pbEnd   = " -->"
)

var (
	playbookOnce sync.Once
	playbooks    map[string]string
)

// playbookPart is one playbook block of an intent: the SKILL marker name, and the words of the
// questions that need it. An intent's first part is its default, used when no cue matches.
type playbookPart struct {
	name string
	cue  *regexp.Regexp
}

// playbookParts lists, per intent, the playbook parts of SKILL.md (see the "pb:" markers) with
// their cues. A question gets the parts whose cue matches its normalised text, so that "¿Qué hago
// ahora?" gets the quest-step playbook and not the list of available quests.
var playbookParts = map[Intent][]playbookPart{
	IntentQuests: {
		{"misiones.estado", regexp.MustCompile(`que hago|entreg|rastread|siguiente|paso|ayuda`)},
		{"misiones.disponibles", regexp.MustCompile(`disponib|puedo hacer|hacer ya|\bya\b`)},
		{"misiones.zona", regexp.MustCompile(`zona|cerca|convien`)},
		{"misiones.valor", regexp.MustCompile(`\bxp\b|oro|minuto|valor|ruta|mas oro`)},
	},
	IntentGear: {
		{"equipo.analisis", regexp.MustCompile(`equip|mejor|conviene|pongo|analiz|ranura`)},
		{"equipo.donde", regexp.MustCompile(`consig|conseg|sirve|objeto|de donde|capucha`)},
	},
	IntentDungeon: {
		{"mazmorra.elegir", regexp.MustCompile(`mazmor|instancia|jefe|botin|saco`)},
	},
	IntentProfessions: {
		{"profesiones.entrenar", regexp.MustCompile(`cuando|entren|instructor|donde|entrenador`)},
		{"profesiones.subir", regexp.MustCompile(`subo|subir|ruta|sube|como|receta|compro|rango`)},
		{"profesiones.hechizos", regexp.MustCompile(`hechizo|que entreno|que aprendo`)},
	},
	IntentSession: {
		{"sesion.resumen", regexp.MustCompile(`sesion|resumen|como ha ido`)},
		{"sesion.ritmo", regexp.MustCompile(`falta|tard|ritmo|subir|tiempo`)},
	},
	IntentBuild: {
		{"build.talentos", regexp.MustCompile(`talent|build|especializ|stat|estadistic`)},
		{"build.hechizos", regexp.MustCompile(`hechizo|que uso|spell`)},
		{"build.rotacion", regexp.MustCompile(`rotacion|revisa|dps|prioridad`)},
		{"build.barras", regexp.MustCompile(`barra|coloc|ordena|atajo|tecla`)},
		{"build.macros", regexp.MustCompile(`macro`)},
	},
	IntentCombat: {
		{"combate.revisar", regexp.MustCompile(`mal|combate|fallo|error|revisa`)},
		{"combate.muerte", regexp.MustCompile(`muer|mata|muri|morir|por que he`)},
	},
	IntentTravel: {
		{"viaje.npc", regexp.MustCompile(`npc|donde|llego|ruta|mapa`)},
	},
	IntentTrade: {
		{"comercio.oro", regexp.MustCompile(`oro|dinero|gast|compr|vend|precio`)},
	},
	IntentEnemy: {
		{"enemigo.veredicto", regexp.MustCompile(`puedo|enemig|elite|mob|dos|tres|bicho`)},
	},
	IntentLore: {
		{"lore.dato", regexp.MustCompile(`.`)},
	},
	IntentGeneral: {
		{"general.pantalla", regexp.MustCompile(`pantalla|captur|ves|imagen`)},
	},
}

// playbookFor returns the playbook excerpt of question q of intent in, headed by the intent's
// name, or "" when the skill has no playbook for it. It is the first thing of the per-question
// system prompt. The parts whose cue matches q are taken, in the order of playbookParts; when none
// matches, the intent's first part is.
func playbookFor(in Intent, q string) string {
	playbookOnce.Do(func() { playbooks = parsePlaybooks(skill.Body()) })
	parts := playbookParts[in]
	if len(parts) == 0 {
		return ""
	}
	norm := " " + foldQuestion(q) + " "
	var names []string
	for _, p := range parts {
		if p.cue.MatchString(norm) {
			names = append(names, p.name)
		}
	}
	if len(names) == 0 {
		names = []string{parts[0].name}
	}
	var texts []string
	for _, n := range names {
		if t := playbooks[n]; t != "" {
			texts = append(texts, t)
		}
	}
	if len(texts) == 0 {
		return ""
	}
	return "Playbook (" + string(in) + "):\n" + strings.Join(texts, "\n")
}

// parsePlaybooks collects the playbook blocks of a skill body by intent name, in the order they
// appear.
func parsePlaybooks(body string) map[string]string {
	out := map[string]string{}
	rest := body
	for {
		i := strings.Index(rest, pbOpen)
		if i < 0 {
			break
		}
		rest = rest[i+len(pbOpen):]
		e := strings.Index(rest, pbEnd)
		if e < 0 {
			break
		}
		name := strings.TrimSpace(rest[:e])
		rest = rest[e+len(pbEnd):]
		j := strings.Index(rest, pbClose)
		if j < 0 {
			break
		}
		if block := strings.TrimSpace(rest[:j]); block != "" && name != "" {
			if out[name] != "" {
				out[name] += "\n"
			}
			out[name] += block
		}
		rest = rest[j+len(pbClose):]
	}
	return out
}

// historyFetch is how many stored messages are read before the turns are chosen: twice the
// turns asked for, so that proactive tips (not sent back) do not shrink the history.
const historyFetch = 2 * historyTurns * 2

// historyRecentTurns is how many of the latest turns are always kept, whatever their intent.
const historyRecentTurns = 2

// historyTurn is one exchange: a question and the answer that followed it.
type historyTurn struct {
	intent Intent
	msgs   []ai.Message
}

// pruneHistory turns the stored messages (oldest first) into the history of a question of
// intent in. A turn is a question with its answer; the last historyTurns turns are kept. Of
// those, a turn about another intent is dropped once it is older than historyRecentTurns, so
// that an old question about the quests does not weigh on a question about the trade.
// Proactive tips and messages of other roles are never sent back.
func pruneHistory(msgs []store.ChatMsg, in Intent) []ai.Message {
	var turns []historyTurn
	for _, m := range msgs {
		if strings.HasPrefix(m.Provider, proactivePrefix) {
			continue
		}
		switch m.Role {
		case "user":
			turns = append(turns, historyTurn{intent: classifyIntent(m.Content), msgs: []ai.Message{{Role: "user", Content: m.Content}}})
		case "assistant":
			if len(turns) == 0 {
				continue // an answer without its question is not sent
			}
			t := &turns[len(turns)-1]
			t.msgs = append(t.msgs, ai.Message{Role: "assistant", Content: truncateRunes(m.Content, historyAnswerRunes)})
		}
	}
	if len(turns) > historyTurns {
		turns = turns[len(turns)-historyTurns:]
	}
	var out []ai.Message
	for i, t := range turns {
		older := len(turns) - i // 1 for the latest turn
		if older > historyRecentTurns && t.intent != in {
			continue
		}
		out = append(out, t.msgs...)
	}
	return out
}
