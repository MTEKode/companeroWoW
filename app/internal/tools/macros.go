package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
)

// Macro tools. crear_macro builds a macro from what the player wants and returns the /compa macro
// commands the player pastes in the game; revisar_macros checks the macros the addon exported. The
// names come from the player's spellbook and bags, never from memory, and nothing is sent to the game.

const (
	descCrearMacro    = "Crea una macro de WoW: Forever validada, con los nombres de hechizo de tu libro (en tu idioma) y sus reglas: una acción por pulsación, condiciones entre corchetes, 255 caracteres y nombre de 16. Úsala para «crea una macro», «quiero una macro para…». Escribe cada línea con su comando (/cast, /use, /castsequence, /equip, /stopattack, /target…) y pon el hechizo con {clave} (p. ej. {overpower}, {charge}, {heroic_strike}) o {id:N} (id del hechizo): la herramienta lo sustituye por el nombre de tu libro. Sin nombres inventados: si un hechizo no está en tu libro, la macro sale con problemas. Devuelve el nombre, el icono, el cuerpo, la explicación y los comandos /compa macro crear (partidos si no caben en el chat) y, si das ranura, /compa macro barra. Argumentos: objetivo (obligatorio), nombre (16 caracteres como máximo), icono, lineas (lista de líneas), hechizos (lista de nombres o de {hechizo, condicion}, p. ej. {\"hechizo\": \"{overpower}\", \"condicion\": \"stance:2\"}) y ranura (1 a 120)."
	descRevisarMacros = "Revisa las macros que el addon exportó (de cuenta y de personaje): hechizos o objetos que no están en tu libro o en tus bolsas (con la sugerencia más parecida: útil tras un cambio de nombre), líneas que el juego no ejecuta, condiciones desconocidas, más de una acción sin condiciones, comandos que no se pueden usar (/click, scripts) y longitud. Úsala para «revisa mis macros» o «qué macros tengo mal»."

	macroPrefixCreate = "/compa macro crear "
	macroPrefixPart   = "/compa macro parte "
	macroChatMax      = 255 // longitud maxima de una linea del chat del juego (bytes)
	macroDefaultIcon  = "INV_MISC_QUESTIONMARK"
	macroNameMax      = gamedata.MacroMaxName
	macroSlotMax      = 120
	macroMaxParts     = 99
)

// crearMacro is the crear_macro tool.
func crearMacro(g *gamedata.Service) Def {
	return Def{
		Name:        "crear_macro",
		Description: descCrearMacro,
		InputSchema: schema(map[string]any{
			"objetivo": strProp("Qué debe hacer la macro, en castellano (para el nombre y la explicación)."),
			"nombre":   strProp("Nombre de la macro, de 16 caracteres como máximo. Si no lo das, sale del objetivo."),
			"icono":    strProp("Icono: nombre del icono (p. ej. INV_SWORD_04) o fileID. Por defecto, una interrogación."),
			"lineas": map[string]any{
				"type":        "array",
				"description": "Líneas de la macro, en orden, tal cual irán en el juego (p. ej. «/cast [stance:2] {overpower}»).",
				"items":       map[string]any{"type": "string"},
			},
			"hechizos": map[string]any{
				"type":        "array",
				"description": "Hechizos a lanzar, en orden: nombre, {clave} o {id:N}; o {hechizo, condicion} con una condición como mod:shift o stance:2. Cada uno es una línea /cast.",
				"items":       map[string]any{},
			},
			"ranura": intProp("Ranura de barra donde poner la macro (1 a 120), si el jugador la quiere en una barra.", 1),
		}, "objetivo"),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			objetivo, _ := a.text("objetivo", "description", "descripcion", "descripción")
			if strings.TrimSpace(objetivo) == "" {
				return Result{}, fmt.Errorf("falta objetivo: qué debe hacer la macro")
			}
			if g == nil {
				return Result{}, errNoGame
			}
			book, ok := g.MacroBookOf()
			if !ok {
				return jsonResult(map[string]any{"disponible": false, "hint": gamedata.HintSync})
			}
			req, err := readMacroRequest(a)
			if err != nil {
				return Result{}, err
			}
			return jsonResult(buildMacro(objetivo, req, book))
		},
	}
}

// macroRequest is what the assistant asked for: the name, icon, lines, spells and slot.
type macroRequest struct {
	name, icon string
	lines      []string
	spells     []macroSpell
	slot       int
}

// macroSpell is one spell of hechizos: a name or placeholder, and an optional condition.
type macroSpell struct {
	spell, cond string
}

// readMacroRequest reads the optional arguments of crear_macro.
func readMacroRequest(a argValues) (macroRequest, error) {
	var req macroRequest
	req.name, _ = a.text("nombre", "name")
	req.icon, _ = a.text("icono", "icon")
	if raw, ok := a.raw("lineas", "lines"); ok {
		var lines []string
		if err := json.Unmarshal(raw, &lines); err != nil {
			return req, fmt.Errorf("lineas debe ser una lista de textos")
		}
		req.lines = lines
	}
	if raw, ok := a.raw("hechizos", "spells"); ok {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return req, fmt.Errorf("hechizos debe ser una lista de nombres o de {hechizo, condicion}")
		}
		for i, it := range items {
			var s string
			if json.Unmarshal(it, &s) == nil {
				req.spells = append(req.spells, macroSpell{spell: s})
				continue
			}
			var o struct {
				Hechizo   string `json:"hechizo"`
				Spell     string `json:"spell"`
				Condicion string `json:"condicion"`
				Cond      string `json:"cond"`
			}
			if json.Unmarshal(it, &o) != nil {
				return req, fmt.Errorf("hechizo %d: debe ser un nombre o {hechizo, condicion}", i+1)
			}
			name := o.Hechizo
			if name == "" {
				name = o.Spell
			}
			cond := o.Condicion
			if cond == "" {
				cond = o.Cond
			}
			req.spells = append(req.spells, macroSpell{spell: name, cond: cond})
		}
	}
	if n, ok, err := a.integer("ranura", "slot"); err != nil {
		return req, fmt.Errorf("la ranura debe ser un número de 1 a %d", macroSlotMax)
	} else if ok {
		if n < 1 || n > macroSlotMax {
			return req, fmt.Errorf("la ranura debe estar entre 1 y %d", macroSlotMax)
		}
		req.slot = int(n)
	}
	return req, nil
}

// buildMacro turns the request into the macro, checks it and returns the result for the assistant.
func buildMacro(objetivo string, req macroRequest, book gamedata.MacroBook) map[string]any {
	var problems []string
	lines := append([]string(nil), req.lines...)
	for _, s := range req.spells {
		if strings.TrimSpace(s.spell) == "" {
			problems = append(problems, "hay un hechizo sin nombre")
			continue
		}
		line := "/cast " + s.spell
		if c := strings.TrimSpace(s.cond); c != "" {
			line = "/cast [" + c + "] " + s.spell
		}
		lines = append(lines, line)
	}
	resolved := make([]string, 0, len(lines))
	for _, l := range lines {
		out, probs := gamedata.ResolveMacroPlaceholders(l, book)
		problems = append(problems, probs...)
		resolved = append(resolved, out)
	}
	body := strings.Join(resolved, "\n")
	if strings.Contains(body, "/cast") && !strings.Contains(strings.ToLower(body), "#showtooltip") {
		if first := firstCastName(resolved); first != "" {
			body = "#showtooltip " + first + "\n" + body
		}
	}

	name := strings.TrimSpace(req.name)
	if name == "" {
		name = shortName(objetivo)
	}
	// The addon counts bytes (as the game does for the macro name), so the check does too.
	if name == "" {
		problems = append(problems, "falta el nombre de la macro")
	} else if len(name) > macroNameMax {
		problems = append(problems, fmt.Sprintf("el nombre «%s» tiene %d caracteres (máximo %d): pásalo más corto en «nombre»", name, len(name), macroNameMax))
	} else if strings.Contains(name, "|") {
		problems = append(problems, "el nombre no puede llevar «|»")
	}
	icon := strings.TrimSpace(req.icon)
	if icon == "" {
		icon = macroDefaultIcon
	} else if strings.Contains(icon, "|") {
		problems = append(problems, "el icono no puede llevar «|»")
	}

	check := gamedata.CheckMacro(body, book)
	problems = append(problems, check.Problems...)
	res := map[string]any{
		"disponible":  true,
		"nombre":      name,
		"icono":       icon,
		"cuerpo":      strings.Join(check.Lines, "\n"),
		"caracteres":  check.Length,
		"nombres":     check.Names,
		"ok":          len(problems) == 0,
		"explicacion": explain(objetivo, check),
	}
	if len(check.Warnings) > 0 {
		res["avisos"] = check.Warnings
	}
	if len(problems) > 0 {
		res["problemas"] = problems
		res["no_usar"] = "La macro tiene problemas: no la des al jugador hasta corregirlos."
		return res
	}
	commands, err := macroCommands(name, icon, check.Lines)
	if err != nil {
		res["ok"] = false
		res["problemas"] = []string{err.Error()}
		return res
	}
	res["comandos"] = commands
	if req.slot > 0 {
		res["colocar"] = fmt.Sprintf("/compa macro barra %s %d", name, req.slot)
	}
	res["aviso"] = "Fuera de combate. /compa macro deshacer vuelve a la versión anterior."
	return res
}

// explain says what the macro does, in short, without repeating its lines.
func explain(objetivo string, c gamedata.MacroCheck) string {
	var b strings.Builder
	b.WriteString("Macro para: ")
	b.WriteString(strings.TrimSpace(objetivo))
	b.WriteString(". Cada línea es una acción y el juego usa la primera que puede hacer; las condiciones van entre corchetes.")
	if len(c.Names) > 0 {
		b.WriteString(" Usa: ")
		b.WriteString(strings.Join(c.Names, ", "))
		b.WriteString(".")
	}
	return b.String()
}

// firstCastName is the name of the first spell of the first /cast line, without its conditionals.
func firstCastName(lines []string) string {
	for _, l := range lines {
		if !strings.HasPrefix(strings.ToLower(l), "/cast ") {
			continue
		}
		rest := strings.TrimSpace(l[len("/cast "):])
		for strings.HasPrefix(rest, "[") {
			end := strings.Index(rest, "]")
			if end < 0 {
				return ""
			}
			rest = strings.TrimSpace(rest[end+1:])
		}
		if i := strings.Index(rest, ";"); i >= 0 {
			rest = rest[:i]
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

// shortName takes the first words of the objective that fit in the name (macroNameMax bytes, as the
// game counts them). A first word that does not fit is cut at a character boundary.
func shortName(objetivo string) string {
	words := strings.Fields(objetivo)
	var out string
	for _, w := range words {
		cand := strings.TrimSpace(out + " " + w)
		if len(cand) > macroNameMax {
			break
		}
		out = cand
	}
	if out == "" && len(words) > 0 {
		cut := min(len(words[0]), macroNameMax)
		for cut > 0 && cut < len(words[0]) && !utf8.RuneStart(words[0][cut]) {
			cut--
		}
		out = words[0][:cut]
	}
	return out
}

// macroCommands returns the /compa macro crear command of the macro, or the parts of it when the
// command would not fit in one chat line. The parts are joined in order by the addon.
func macroCommands(name, icon string, lines []string) ([]string, error) {
	body := strings.Join(lines, `\n`) // the command keeps the line breaks as the literal \n
	payload := name + "|" + icon + "|" + body
	single := macroPrefixCreate + payload
	if len(single) <= macroChatMax {
		return []string{single}, nil
	}
	budget := macroChatMax - len("/compa macro parte 00/00 ")
	pieces := chunkPayload(payload, budget)
	if len(pieces) > macroMaxParts {
		return nil, fmt.Errorf("la macro es demasiado larga para pegarla en partes (%d partes)", len(pieces))
	}
	out := make([]string, 0, len(pieces))
	for i, p := range pieces {
		out = append(out, fmt.Sprintf("%s%d/%d %s", macroPrefixPart, i+1, len(pieces), p))
	}
	return out, nil
}

// chunkPayload cuts s into pieces of at most budget bytes. A cut never sits next to a space (the chat
// may drop a trailing space) and never splits a UTF-8 character. Joining the pieces gives s back.
func chunkPayload(s string, budget int) []string {
	var out []string
	for len(s) > 0 {
		if len(s) <= budget {
			out = append(out, s)
			break
		}
		cut := budget
		for cut > 0 && (s[cut-1] == ' ' || s[cut] == ' ' || !utf8.RuneStart(s[cut])) {
			cut--
		}
		if cut == 0 {
			cut = budget
			for cut > 0 && !utf8.RuneStart(s[cut]) {
				cut--
			}
		}
		out = append(out, s[:cut])
		s = s[cut:]
	}
	return out
}

// revisarMacros is the revisar_macros tool.
func revisarMacros(g *gamedata.Service) Def {
	return Def{
		Name:        "revisar_macros",
		Description: descRevisarMacros,
		InputSchema: schema(nil),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			if g == nil {
				return Result{}, errNoGame
			}
			list, usedA, usedC, maxA, maxC, ok, hint := g.MacrosOf()
			if !ok {
				return jsonResult(map[string]any{"disponible": false, "hint": hint})
			}
			book, _ := g.MacroBookOf()
			out := make([]map[string]any, 0, len(list))
			withProblems, withWarnings := 0, 0
			for _, m := range list {
				c := gamedata.CheckMacro(m.Body, book)
				item := map[string]any{
					"nombre":     m.Name,
					"ambito":     scopeName(m.Scope),
					"caracteres": c.Length,
					"nombres":    c.Names,
				}
				if len(c.Problems) > 0 {
					item["problemas"] = c.Problems
					withProblems++
				}
				if len(c.Warnings) > 0 {
					item["mejoras"] = c.Warnings
					withWarnings++
				}
				out = append(out, item)
			}
			return jsonResult(map[string]any{
				"disponible": true,
				"cuenta":     map[string]any{"usadas": usedA, "maximo": maxA},
				"personaje":  map[string]any{"usadas": usedC, "maximo": maxC},
				"macros":     out,
				"resumen": fmt.Sprintf("%d macros revisadas: %d con problemas y %d con mejoras.",
					len(list), withProblems, withWarnings),
			})
		},
	}
}

func scopeName(scope string) string {
	if scope == "account" {
		return "cuenta"
	}
	return "personaje"
}
