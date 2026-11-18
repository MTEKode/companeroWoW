package gamedata

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Macro checks: the syntax and the names of a macro body, checked against the player's spellbook
// and bags. The rules are the ones of the game (one action per press, the conditionals of the
// client, 255 characters, 16 characters for the name) and the refusals of things that automate
// play (/click, scripts). The checks never invent a name: a spell that is not in the spellbook is
// reported, and the closest known name is suggested when there is one.

const (
	MacroMaxBody = 255 // characters of a macro body (WoW's limit, counted in bytes)
	MacroMaxName = 16  // characters of a macro name
)

// SpellRef is one spell of the player's spellbook: its id and its localized name.
type SpellRef struct {
	ID   int64
	Name string
}

// MacroBook is what a macro may name: the spellbook (localized names) and the item names of the
// bags and the equipment. HaveSpells and HaveItems tell whether those lists exist at all: without
// them the names cannot be checked and the check says so instead of refusing.
type MacroBook struct {
	Spells     []SpellRef
	Items      []string
	HaveSpells bool
	HaveItems  bool
}

// MacroCheck is the result of checking one macro body.
type MacroCheck struct {
	Lines    []string // the body's lines, trimmed, without empty lines
	Length   int      // bytes of the body as the game counts them (lines joined by \n)
	Names    []string // spell and item names the lines use, as the book writes them
	Problems []string // reasons the game would not run the macro, or it is refused: the macro is not usable
	Warnings []string // advice that does not stop the macro
}

// OK reports whether the body can be used: no problems.
func (c MacroCheck) OK() bool { return len(c.Problems) == 0 }

// blockedCommands are refused outright: they automate play or run code.
var blockedCommands = map[string]string{
	"/click":      "/click encadena acciones y automatiza el juego: no lo genero",
	"/run":        "/run ejecuta código Lua: no lo genero",
	"/script":     "/script ejecuta código Lua: no lo genero",
	"/dostring":   "/dostring ejecuta código Lua: no lo genero",
	"/runmacro":   "/runmacro encadena macros: no lo genero",
	"/macro":      "/macro encadena macros: no lo genero",
	"/console":    "/console cambia opciones del cliente: no lo genero",
	"/setbinding": "/setbinding cambia teclas desde una macro: no lo genero",
}

// knownCommands are the slash commands a macro may use.
var knownCommands = map[string]bool{
	"/cast": true, "/castsequence": true, "/use": true, "/equip": true, "/equipslot": true,
	"/target": true, "/targetenemy": true, "/cleartarget": true, "/stopattack": true, "/startattack": true,
	"/cancelaura": true, "/cancelform": true, "/dismount": true, "/petattack": true, "/stopmacro": true,
	"#showtooltip": true,
}

// castCommands spend the global cooldown; a press may run only one of them.
var castCommands = map[string]bool{"/cast": true, "/castsequence": true, "/use": true}

// nameCommands take a spell or item name that the book must hold.
var nameCommands = map[string]bool{"/cast": true, "/castsequence": true, "/use": true, "#showtooltip": true}

// itemCommands take an item name only.
var itemCommands = map[string]bool{"/equip": true}

// knownConditions are the conditionals of the client, as they are written in [...].
var knownConditions = map[string]bool{
	"harm": true, "help": true, "dead": true, "exists": true, "combat": true, "stealth": true,
	"indoors": true, "outdoors": true, "group": true, "party": true, "raid": true, "pet": true,
	"channeling": true, "mounted": true, "flying": true, "swimming": true, "vehicle": true,
	"nocombat": true, "noharm": true, "nohelp": true, "nodead": true, "noexists": true,
	"nostealth": true, "nogroup": true, "noparty": true, "noraid": true, "nopet": true,
	"nochanneling": true, "nomounted": true, "noflying": true, "noswimming": true, "novehicle": true,
	"noindoors": true, "nooutdoors": true,
}

var (
	conditionRE  = regexp.MustCompile(`^(mod|modifier)(:(shift|ctrl|alt|lshift|rshift|lctrl|rctrl|lalt|ralt)(/(shift|ctrl|alt))*)?$`)
	valuedCondRE = regexp.MustCompile(`^(stance|form|spec|actionbar|equipped|target|bonusbar|btn|mod)(:|=)[A-Za-z0-9_/ .-]+$`)
	targetCondRE = regexp.MustCompile(`^@[A-Za-z][A-Za-z0-9]*[0-9]*$`)
	resetRE      = regexp.MustCompile(`^reset=\S+\s*`)
	parenRE      = regexp.MustCompile(`\s*\([^)]*\)\s*$`)
)

// macroLine is one parsed line: its command, its alternatives and their conditionals.
type macroLine struct {
	text string
	cmd  string
	alts []macroAlt
}

// macroAlt is one ";"-separated alternative: its conditionals and the names it uses.
type macroAlt struct {
	conds []string
	names []string
}

// CheckMacro checks a macro body against the book. The body may hold real line breaks or the
// literal \n; both separate lines.
func CheckMacro(body string, book MacroBook) MacroCheck {
	lines := macroLines(body)
	res := MacroCheck{Lines: lines}
	res.Length = len(strings.Join(lines, "\n"))
	if len(lines) == 0 {
		res.Problems = append(res.Problems, "la macro está vacía")
		return res
	}
	if res.Length > MacroMaxBody {
		res.Problems = append(res.Problems, fmt.Sprintf("la macro tiene %d caracteres (máximo %d)", res.Length, MacroMaxBody))
	}
	castsWithoutConds := 0
	seenNames := map[string]bool{}
	noBookWarned := false
	for i, line := range lines {
		pl, problem := parseLine(line)
		if problem != "" {
			res.Problems = append(res.Problems, problem)
			continue
		}
		if pl.cmd == "#showtooltip" && i > 0 {
			res.Warnings = append(res.Warnings, "#showtooltip debe ir en la primera línea para que el icono sea el del hechizo")
		}
		for _, alt := range pl.alts {
			for _, c := range alt.conds {
				if msg := checkCondition(c); msg != "" {
					res.Problems = append(res.Problems, msg)
				}
			}
			if castCommands[pl.cmd] && len(alt.conds) == 0 {
				castsWithoutConds++
			}
			for _, n := range alt.names {
				if n == "" {
					if pl.cmd != "#showtooltip" {
						res.Problems = append(res.Problems, fmt.Sprintf("falta el hechizo o el objeto en «%s»", line))
					}
					continue
				}
				if !seenNames[n] {
					seenNames[n] = true
					res.Names = append(res.Names, n)
				}
				if !book.HaveSpells && !book.HaveItems {
					if !noBookWarned {
						res.Warnings = append(res.Warnings, "no tengo tu libro de hechizos ni tus bolsas: no compruebo los nombres")
						noBookWarned = true
					}
					continue
				}
				if msg := nameProblem(n, pl.cmd, book); msg != "" {
					res.Problems = append(res.Problems, msg)
				}
			}
		}
	}
	if castsWithoutConds >= 2 {
		res.Problems = append(res.Problems, "hay más de un hechizo o acción sin condiciones: en cada pulsación solo se ejecuta el primero. Usa [condición] Hechizo; Otro para dar prioridad")
	}
	if hasCast(lines) && !hasLinePrefix(lines, "#showtooltip") {
		res.Warnings = append(res.Warnings, "falta #showtooltip: el icono de la macro no será el del hechizo")
	}
	return res
}

// MacroNames returns the spell and item names a macro body uses (the names of /cast, /castsequence,
// /use, /equip and #showtooltip, without conditionals or ranks). It is what the answer verifier
// checks against the sheet and the bags.
func MacroNames(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range macroLines(body) {
		pl, problem := parseLine(line)
		if problem != "" {
			continue
		}
		if !nameCommands[pl.cmd] && !itemCommands[pl.cmd] {
			continue
		}
		for _, alt := range pl.alts {
			for _, n := range alt.names {
				if n != "" && !seen[n] {
					seen[n] = true
					out = append(out, n)
				}
			}
		}
	}
	return out
}

// macroLines splits a body into trimmed, non-empty lines. The literal \n also separates lines.
func macroLines(body string) []string {
	s := strings.ReplaceAll(body, "\r\n", "\n")
	s = strings.ReplaceAll(s, `\n`, "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// parseLine splits one line into its command and alternatives. problem is non-empty when the
// line cannot be a macro line.
func parseLine(line string) (macroLine, string) {
	pl := macroLine{text: line}
	if !strings.HasPrefix(line, "/") && !strings.HasPrefix(line, "#") {
		return pl, fmt.Sprintf("«%s» no es un comando de macro (debe empezar por / o #)", line)
	}
	cmd, rest := splitCommand(line)
	cmd = strings.ToLower(cmd)
	if why, blocked := blockedCommands[cmd]; blocked {
		return pl, why
	}
	if !knownCommands[cmd] {
		return pl, fmt.Sprintf("comando no reconocido %s en «%s»", cmd, line)
	}
	pl.cmd = cmd
	if cmd == "/equipslot" || cmd == "/target" || cmd == "/targetenemy" || cmd == "/cleartarget" ||
		cmd == "/stopattack" || cmd == "/startattack" || cmd == "/cancelaura" || cmd == "/cancelform" ||
		cmd == "/dismount" || cmd == "/petattack" || cmd == "/stopmacro" {
		// Sin nombres que comprobar: solo se miran las condiciones.
		for _, alt := range strings.Split(rest, ";") {
			conds, _ := splitConds(strings.TrimSpace(alt))
			pl.alts = append(pl.alts, macroAlt{conds: conds})
		}
		return pl, ""
	}
	for _, alt := range strings.Split(rest, ";") {
		conds, tail := splitConds(strings.TrimSpace(alt))
		if cmd == "/castsequence" {
			tail = resetRE.ReplaceAllString(tail, "")
		}
		var names []string
		if cmd == "/castsequence" {
			for _, n := range strings.Split(tail, ",") {
				names = append(names, cleanName(n))
			}
		} else {
			names = []string{cleanName(tail)}
		}
		pl.alts = append(pl.alts, macroAlt{conds: conds, names: names})
	}
	return pl, ""
}

// splitCommand splits "/cast [x] Name" into "/cast" and the rest of the line.
func splitCommand(line string) (string, string) {
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimSpace(line[i:])
}

// splitConds takes the leading [...] blocks of an alternative. It returns the conditions (each
// [...] block split by commas) and the text after the blocks.
func splitConds(alt string) ([]string, string) {
	var conds []string
	s := strings.TrimSpace(alt)
	for strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			break
		}
		for _, c := range strings.Split(s[1:end], ",") {
			if t := strings.TrimSpace(c); t != "" {
				conds = append(conds, t)
			} else {
				conds = append(conds, "")
			}
		}
		s = strings.TrimSpace(s[end+1:])
	}
	return conds, s
}

// cleanName drops the rank in parentheses and the spaces around a name.
func cleanName(n string) string {
	t := strings.TrimSpace(n)
	t = parenRE.ReplaceAllString(t, "")
	return strings.TrimSpace(t)
}

// checkCondition returns the problem of one conditional, or "" when the client knows it.
func checkCondition(c string) string {
	if c == "" {
		return "condición vacía en la macro"
	}
	low := strings.ToLower(strings.TrimSpace(c))
	if knownConditions[low] || conditionRE.MatchString(low) || targetCondRE.MatchString(c) {
		return ""
	}
	if valuedCondRE.MatchString(low) {
		return ""
	}
	return fmt.Sprintf("condición no reconocida [%s]", c)
}

// nameProblem returns the problem of a name the macro uses, or "" when the book holds it or the
// list that would decide it is missing (then the check cannot tell, and CheckMacro says so).
func nameProblem(name, cmd string, book MacroBook) string {
	key := strings.ToLower(name)
	if !itemCommands[cmd] && book.HaveSpells {
		for _, sp := range book.Spells {
			if strings.ToLower(sp.Name) == key {
				return ""
			}
		}
	}
	if book.HaveItems {
		for _, it := range book.Items {
			if strings.ToLower(it) == key {
				return ""
			}
		}
	}
	switch {
	case itemCommands[cmd]:
		if !book.HaveItems {
			return ""
		}
		return fmt.Sprintf("«%s» no está en tus bolsas ni en tu equipo%s", name, closeHint(name, book, "items"))
	case cmd == "/use":
		if !book.HaveItems || !book.HaveSpells {
			return ""
		}
		return fmt.Sprintf("«%s» no está en tu libro de hechizos ni en tus bolsas%s", name, closeHint(name, book, "both"))
	default:
		if !book.HaveSpells {
			return ""
		}
		return fmt.Sprintf("«%s» no está en tu libro de hechizos%s", name, closeHint(name, book, "spells"))
	}
}

// closeHint is the suggestion of a close name, in the form " (¿quisiste decir «X»?)", or "".
func closeHint(name string, book MacroBook, mode string) string {
	if close := closestName(name, book, mode); close != "" {
		return fmt.Sprintf(" (¿quisiste decir «%s»?)", close)
	}
	return ""
}

// closestName suggests the book name nearest to name (distance up to 2, or 1 for short names).
// mode is "spells" (cast), "items" (equip) or "both" (use): the lists the name may come from.
func closestName(name string, book MacroBook, mode string) string {
	var cands []string
	if mode != "items" && book.HaveSpells {
		for _, sp := range book.Spells {
			cands = append(cands, sp.Name)
		}
	}
	if mode != "spells" && book.HaveItems {
		cands = append(cands, book.Items...)
	}
	best, bestD := "", 1<<30
	lname := strings.ToLower(name)
	limit := 2
	if utf8.RuneCountInString(name) <= 5 {
		limit = 1
	}
	for _, c := range cands {
		d := levenshtein(lname, strings.ToLower(c))
		if d < bestD {
			best, bestD = c, d
		}
	}
	if bestD <= limit {
		return best
	}
	return ""
}

// levenshtein is the edit distance between two strings, by runes.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func hasCast(lines []string) bool {
	for _, l := range lines {
		if pl, p := parseLine(l); p == "" && castCommands[pl.cmd] {
			return true
		}
	}
	return false
}

func hasLinePrefix(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(strings.ToLower(l), prefix) {
			return true
		}
	}
	return false
}
