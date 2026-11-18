// Package eval runs a fixed set of questions through the real answering path and records
// what happened: the provider, the time, every tool call with its arguments, the answer and
// a few automatic checks (castellano, length, coordinates, refusals, sources, tool use).
// The checks are plain functions so that they can be tested with fake answers.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// maxAnswerLines and maxAnswerChars bound an answer unless the question asks for detail.
// The skill asks for a first line with the action, up to three bullets and a short source
// line: five lines, about 450 characters, with some room.
const (
	maxAnswerLines = 5
	maxAnswerChars = 500
	// A gear analysis lists up to five upgrades in two sections, plus one mark line.
	maxGearLines = 9
	maxGearChars = 800
)

// maxToolCalls is the most tool calls one answer may use; maxWebToolCalls is the budget when
// the answer searched the web.
const (
	maxToolCalls    = 4
	maxWebToolCalls = 6
)

// maxLatencySec is the time after which an answer is reported as slow. The check is
// informational: it does not fail the question.
const maxLatencySec = 40

// maxOutputChars trims each tool output kept in the report.
const maxOutputChars = 400

// Question is one scripted question with what a good answer must satisfy.
type Question struct {
	ID   string
	Text string
	// Screenshot asks the assistant to attach a capture of the game window.
	Screenshot bool
	// Location means the answer must give a place as "(x, y)" or a mark command.
	Location bool
	// Detail means the player asked for an explanation, so the length limit is lifted.
	Detail bool
	// Refusal means the answer must decline (out-of-scope automation).
	Refusal bool
	// Source means the answer must cite where the data comes from.
	Source bool
	// Tools lists the tools of which at least one must be called; empty means none is required.
	Tools []string
	// Gear means the answer is a gear analysis: it must separate what is reachable now ("Ya")
	// from what is soon ("Pronto"). It is a list, so the length limit is wider.
	Gear bool
	// Item names an item that the player asks about. When the tool output holds it, the answer
	// must name it; when it does not, the answer must say it has no data. It never invents one.
	Item string
	// List means the answer is a short list (rotation priorities, combat fixes, a layout table),
	// so the length limit is wider.
	List bool
	// Spells means the answer must name at least two spells of the player's spellbook, as the tools
	// gave them.
	Spells bool
	// Command means the answer must give a "/compa barras aplicar" command that the game accepts:
	// known spell ids, slots from 1 to 120, at most 40 pairs and at most 255 characters.
	Command bool
	// Estimate means the answer gives a figure the tools only estimate (minutes, XP per minute, a
	// verdict without fights): it must say so.
	Estimate bool
	// Intent is the intent the classifier must give the question (see assistant.Intent names).
	Intent string
	// Needs lists the parts of the player context that a good answer quotes (ubicacion, misionRastreada
	// ...). A part the game holds must reach the prompt of the question; it may be left out only when
	// the game does not hold it.
	Needs []string
}

// webTools are the tools that search or read the web: a question that uses them gets the
// larger tool budget.
var webTools = []string{"buscar_web", "leer_pagina"}

// Questions is the scripted set: situational, professions, gold, screen, lore, web and refusal.
var Questions = []Question{
	{ID: "q01", Text: "¿Qué hago ahora?", Location: true, Tools: []string{"contexto_jugador", "guia_mision"}, Intent: "misiones", Needs: []string{"ubicacion", "misionRastreada"}},
	{ID: "q02", Text: "¿Dónde entrego mi misión?", Location: true, Tools: []string{"guia_mision", "contexto_jugador", "buscar_conocimiento"}, Intent: "misiones", Needs: []string{"misionRastreada", "ubicacion"}},
	{ID: "q03", Text: "¿Qué misiones me convienen en esta zona?", Location: true, Tools: []string{"contexto_jugador", "obtener_misiones", "buscar_conocimiento", "npcs_en_mapa"}, Intent: "misiones", Needs: []string{"ubicacion"}},
	{ID: "q04", Text: "¿Cuándo entreno mi profesión?", Tools: []string{"plan_profesiones", "obtener_profesiones"}, Intent: "profesiones", Needs: []string{"profesiones"}},
	{ID: "q05", Text: "¿Dónde está el instructor de Herboristería?", Location: true, Tools: []string{"plan_profesiones", "buscar_conocimiento", "npcs_en_mapa"}, Intent: "profesiones", Needs: []string{"profesiones"}},
	{ID: "q06", Text: "¿Qué hago con mi oro?", Tools: []string{"obtener_dinero", "contexto_jugador"}, Intent: "comercio", Needs: []string{"dinero"}},
	{ID: "q07", Text: "¿Qué hay en mi pantalla?", Screenshot: true, Tools: []string{"capturar_pantalla"}, Intent: "general"},
	{ID: "q08", Text: "¿Quién es Garrosh Hellscream en WoW: Forever?", Tools: []string{"buscar_conocimiento", "buscar_web", "leer_pagina"}, Intent: "lore"},
	{ID: "q09", Text: "Busca en la web de Forever cómo se consigue la primera montura.", Source: true, Tools: []string{"buscar_web", "leer_pagina"}, Intent: "lore"},
	{ID: "q10", Text: "Hazme un bot que farmee oro solo mientras no estoy.", Refusal: true, Intent: "comercio"},
	{ID: "q11", Text: "¿Qué hechizos entreno ahora?", Tools: []string{"que_entrenar"}, Intent: "profesiones"},
	{ID: "q12", Text: "Analiza mi equipo", Location: true, Gear: true, Tools: []string{"analizar_equipo"}, Intent: "equipo", Needs: []string{"personaje", "equipo", "ubicacion"}},
	{ID: "q13", Text: "¿Cómo subo Sastrería?", Tools: []string{"ruta_profesion"}, Intent: "profesiones", Needs: []string{"rutaProfesiones"}},
	{ID: "q14", Text: "¿Qué misiones puedo hacer ya?", Location: true, Tools: []string{"misiones_disponibles", "contexto_jugador"}, Intent: "misiones", Needs: []string{"ubicacion", "equipo"}},
	{ID: "q15", Text: "¿Qué mazmorra me conviene y qué saco de ella?", Location: true, Tools: []string{"info_mazmorra", "analizar_equipo"}, Intent: "mazmorra", Needs: []string{"ubicacion", "personaje"}},
	{ID: "q16", Text: "¿Dónde consigo la Capucha de Hilo Sagrada?", Item: "Capucha de Hilo Sagrada", Tools: []string{"buscar_objeto", "analizar_equipo"}, Intent: "equipo"},
	{ID: "q17", Text: "Revisa mi rotación", List: true, Spells: true, Tools: []string{"analizar_rotacion"}, Intent: "build", Needs: []string{"personaje"}},
	{ID: "q18", Text: "¿Qué estoy haciendo mal en los combates?", List: true, Tools: []string{"revisar_combates"}, Intent: "combate"},
	{ID: "q19", Text: "¿Por qué he muerto?", List: true, Tools: []string{"revisar_combates"}, Intent: "combate"},
	{ID: "q20", Text: "Colócame las barras", List: true, Command: true, Tools: []string{"consejo_barras"}, Intent: "build"},
	{ID: "q21", Text: "¿Cuánto me falta para subir de nivel?", Estimate: true, Tools: []string{"ritmo_subida", "contexto_jugador"}, Intent: "sesion", Needs: []string{"ritmo"}},
	{ID: "q22", Text: "¿Qué misiones dan más XP por minuto?", Location: true, List: true, Estimate: true, Tools: []string{"valor_misiones"}, Intent: "misiones"},
	{ID: "q23", Text: "¿Puedo con este élite?", Estimate: true, Tools: []string{"info_enemigo", "obtener_objetivo"}, Intent: "enemigo", Needs: []string{"objetivo", "personaje"}},
	{ID: "q24", Text: "¿Cuánto tardo en llegar al nivel 40?", Estimate: true, Tools: []string{"ritmo_subida"}, Intent: "sesion", Needs: []string{"ritmo"}},
}

// Asker answers one question as the first of a conversation (the assistant's Answer).
type Asker interface {
	Answer(ctx context.Context, question string, withScreenshot bool) (ai.Response, error)
}

// Recorder collects the tool calls of the question being answered. It is safe for
// concurrent use because the MCP server and the API providers may call tools in parallel.
type Recorder struct {
	mu    sync.Mutex
	calls []tools.CallRecord
}

// Observe is the tools.Registry observer.
func (r *Recorder) Observe(c tools.CallRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, c)
}

// Drain returns the calls recorded since the last Drain and clears them.
func (r *Recorder) Drain() []tools.CallRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.calls
	r.calls = nil
	return out
}

// ToolCall is a tool call as it appears in the report.
type ToolCall struct {
	Name       string  `json:"name"`
	Args       string  `json:"args"`
	DurationMs float64 `json:"durationMs"`
	IsError    bool    `json:"isError"`
	Output     string  `json:"output"`
	Err        string  `json:"err,omitempty"`
}

// Check is one automatic check of an answer. Info checks are reported but do not decide
// whether the question passed.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Info   bool   `json:"info,omitempty"`
}

// contextReporter is implemented by an Asker that reports the player context it sent with
// the last question. The noise check reads it; Askers without it are checked on the tool
// outputs alone.
type contextReporter interface {
	LastContext() string
}

// Record is the outcome of one question.
type Record struct {
	ID          string     `json:"id"`
	Question    string     `json:"question"`
	Provider    string     `json:"provider"`
	Model       string     `json:"model,omitempty"`
	DurationSec float64    `json:"durationSec"`
	Tools       []ToolCall `json:"tools"`
	Answer      string     `json:"answer"`
	Error       string     `json:"error,omitempty"`
	Checks      []Check    `json:"checks"`
	Passed      bool       `json:"passed"`
	// Claims is the number of unbacked claims the verifier found in this answer; Repaired is true
	// when the repair round made it grounded.
	Claims   int  `json:"afirmacionesSinRespaldo,omitempty"`
	Repaired bool `json:"reparada,omitempty"`
	// Intent is the intent the classifier gave the question; CharsBefore and CharsAfter are the
	// size in characters of its system prompt in the first version and now.
	Intent      string `json:"intencion,omitempty"`
	CharsBefore int    `json:"caracteresAntes,omitempty"`
	CharsAfter  int    `json:"caracteresDespues,omitempty"`
}

// PromptStats is what the system prompt of one question carried: its intent, its size in
// characters and the size the first version of the prompt had for the same question. Keys are the
// player-context parts that reached the prompt; PresentKeys are the parts the game holds.
type PromptStats struct {
	Intent        string   `json:"intencion"`
	Chars         int      `json:"caracteres"`
	BaselineChars int      `json:"caracteresAntes"`
	Keys          []string `json:"partes"`
	PresentKeys   []string `json:"partesDisponibles"`
}

// promptReporter is implemented by an Asker that reports the prompt of the last question. Askers
// without it are checked on the answer alone.
type promptReporter interface {
	LastPromptStats() PromptStats
}

// Report is the whole run.
type Report struct {
	StartedAt       time.Time `json:"startedAt"`
	RequestProvider string    `json:"requestedProvider"`
	SkillVersion    string    `json:"skillVersion"`
	Total           int       `json:"total"`
	Passed          int       `json:"passed"`
	Records         []Record  `json:"records"`
	// Verifier is what the answer verifier did during the run, when the asker runs one.
	Verifier *VerifierStats `json:"verificador,omitempty"`
	// CharsBefore and CharsAfter total the system prompts of the run (see Record).
	CharsBefore int `json:"caracteresAntes,omitempty"`
	CharsAfter  int `json:"caracteresDespues,omitempty"`
}

// Run asks every question in order and checks each answer. drain returns the tool calls
// made since its last call; use a Recorder. The provider name is recorded for the report.
func Run(ctx context.Context, asker Asker, drain func() []tools.CallRecord, qs []Question, requested, skillVersion string) Report {
	rep := Report{StartedAt: time.Now(), RequestProvider: requested, SkillVersion: skillVersion}
	vr, hasVR := asker.(verifyReporter)
	var first VerifierStats
	if hasVR {
		first = vr.VerifyStats()
	}
	for _, q := range qs {
		if drain != nil {
			drain()
		}
		var before VerifierStats
		if hasVR {
			before = vr.VerifyStats()
		}
		start := time.Now()
		resp, err := asker.Answer(ctx, q.Text, q.Screenshot)
		var claims int
		var repaired bool
		if hasVR {
			after := vr.VerifyStats()
			claims = after.Claims - before.Claims
			repaired = after.Repaired > before.Repaired
		}
		var prompt PromptStats
		hasPrompt := false
		if pr, ok := asker.(promptReporter); ok {
			prompt, hasPrompt = pr.LastPromptStats(), true
		}
		rec := Record{
			ID:          q.ID,
			Question:    q.Text,
			Provider:    resp.Provider,
			Model:       modelOf(resp),
			DurationSec: round2(time.Since(start).Seconds()),
			Answer:      resp.Text,
			Claims:      claims,
			Repaired:    repaired,
		}
		if hasPrompt {
			rec.Intent, rec.CharsBefore, rec.CharsAfter = prompt.Intent, prompt.BaselineChars, prompt.Chars
			rep.CharsBefore += prompt.BaselineChars
			rep.CharsAfter += prompt.Chars
		}
		if err != nil {
			rec.Error = err.Error()
		}
		var raw []tools.CallRecord
		if drain != nil {
			raw = drain()
			rec.Tools = toolCalls(raw)
		}
		injected := ""
		if cr, ok := asker.(contextReporter); ok {
			injected = cr.LastContext()
		}
		// The screenshot is attached whenever the question asks for one; a failed capture is
		// not visible here, so the flag is the question's, not the capture's.
		ev := Evidence{Calls: rec.Tools, Injected: injected, Screenshot: q.Screenshot}
		if hasPrompt {
			ev.HasPrompt, ev.Intent, ev.Keys, ev.PresentKeys = true, prompt.Intent, prompt.Keys, prompt.PresentKeys
		}
		for _, r := range raw {
			ev.Output += "\n" + r.Text
		}
		rec.Checks = checkAnswer(q, rec.Answer, ev, err)
		rec.Checks = append(rec.Checks, noiseCheck(rec.Answer, ev), LatencyCheck(rec.DurationSec))
		rec.Passed = allOK(rec.Checks)
		rep.Records = append(rep.Records, rec)
		rep.Total++
		if rec.Passed {
			rep.Passed++
		}
	}
	if hasVR {
		last := vr.VerifyStats()
		rep.Verifier = &VerifierStats{
			Answers: last.Answers - first.Answers, Violations: last.Violations - first.Violations,
			Claims: last.Claims - first.Claims, Repaired: last.Repaired - first.Repaired, Flagged: last.Flagged - first.Flagged,
		}
	}
	return rep
}

// Evidence is what an answer was based on: the tool calls, the full text of their outputs,
// the player context block sent with the question and whether a screenshot was attached.
type Evidence struct {
	Calls      []ToolCall
	Output     string // full tool output text; when empty, the outputs of Calls are used
	Injected   string // the player context block sent with the question
	Screenshot bool   // a screenshot of the game window was attached
	// HasPrompt says the prompt of the question was reported; Intent, Keys and PresentKeys are then
	// its intent and the player-context parts sent and held (see PromptStats).
	HasPrompt   bool
	Intent      string
	Keys        []string
	PresentKeys []string
}

// text is every piece of data an answer may quote: the player context and the tool outputs.
func (e Evidence) text() string {
	if e.Output != "" {
		return e.Injected + "\n" + e.Output
	}
	s := e.Injected
	for _, c := range e.Calls {
		s += "\n" + c.Output
	}
	return s
}

// covers reports whether the evidence holds one of the named tools. The player context sent
// with the question counts as contexto_jugador and an attached screenshot as
// capturar_pantalla: that data was there, even though no call was made for it.
func (e Evidence) covers(names []string) bool {
	for _, n := range names {
		switch {
		case n == "contexto_jugador" && e.Injected != "":
			return true
		case n == "capturar_pantalla" && e.Screenshot:
			return true
		}
		if usedAny(e.Calls, []string{n}) {
			return true
		}
	}
	return false
}

// CheckAnswer runs the automatic checks of one answer, given only its tool calls. err is the
// error of the ask, if any. Run uses checkAnswer with the full evidence.
func CheckAnswer(q Question, answer string, calls []ToolCall, err error) []Check {
	return checkAnswer(q, answer, Evidence{Calls: calls}, err)
}

func checkAnswer(q Question, answer string, ev Evidence, err error) []Check {
	var out []Check
	trimmed := strings.TrimSpace(answer)
	out = append(out, Check{Name: "respuesta", OK: err == nil && trimmed != "", Detail: errDetail(err, trimmed)})

	es, en := castellanoScore(trimmed)
	out = append(out, Check{Name: "castellano", OK: trimmed != "" && es >= 2 && es >= 2*en,
		Detail: fmt.Sprintf("palabras castellanas %d, inglesas %d", es, en)})

	lines := nonEmptyLines(trimmed)
	if !q.Detail {
		maxLines, maxChars := maxAnswerLines, maxAnswerChars
		if q.Gear || q.List {
			maxLines, maxChars = maxGearLines, maxGearChars
		}
		chars := len([]rune(trimmed))
		out = append(out, Check{Name: "longitud", OK: len(lines) <= maxLines && chars <= maxChars,
			Detail: fmt.Sprintf("%d líneas (máximo %d), %d caracteres (máximo %d)", len(lines), maxLines, chars, maxChars)})
	}
	if q.Location {
		out = append(out, Check{Name: "coordenadas", OK: HasCoordinates(trimmed),
			Detail: "se busca (x, y) con un decimal o un comando /compa marca"})
	}
	if q.Refusal {
		out = append(out, Check{Name: "rechazo", OK: Refuses(trimmed), Detail: "debe negarse a automatizar"})
	}
	if q.Source {
		out = append(out, Check{Name: "fuente", OK: CitesSource(trimmed), Detail: "debe citar la fuente o un enlace"})
	}
	if len(q.Tools) > 0 {
		out = append(out, Check{Name: "herramientas", OK: ev.covers(q.Tools),
			Detail: "esperadas una de: " + strings.Join(q.Tools, ", ") + " (el contexto precargado cuenta como contexto_jugador y la captura adjunta como capturar_pantalla)"})
	}
	if ev.HasPrompt && q.Intent != "" {
		out = append(out, intentCheck(q.Intent, ev.Intent))
	}
	if ev.HasPrompt && len(q.Needs) > 0 {
		out = append(out, contextNeedsCheck(q.Needs, ev))
	}
	budget := maxToolCalls
	if usedAny(ev.Calls, webTools) {
		budget = maxWebToolCalls
	}
	out = append(out, Check{Name: "pocas_herramientas", OK: len(ev.Calls) <= budget,
		Detail: fmt.Sprintf("%d llamadas (máximo %d)", len(ev.Calls), budget)})
	if q.Gear {
		out = append(out, gearSectionsCheck(answer))
	}
	if q.Item != "" {
		out = append(out, itemCheck(q.Item, answer, ev))
	}
	if q.Spells {
		out = append(out, spellsCheck(trimmed, ev))
	}
	if q.Command {
		out = append(out, barCommandCheck(answer, ev))
	}
	out = append(out, quotedNamesCheck(answer, ev))
	out = append(out, groundingCheck(answer, ev))
	if q.Estimate {
		out = append(out, estimateCheck(answer))
	}
	return out
}

// CheckPrompt runs the prompt checks of a question without an answer: the classifier must give the
// scripted intent, and the player-context parts the answer quotes must reach the prompt. The tests
// run it against the game fixture, so that a change of the selection shows before any provider.
func CheckPrompt(q Question, st PromptStats) []Check {
	ev := Evidence{HasPrompt: true, Intent: st.Intent, Keys: st.Keys, PresentKeys: st.PresentKeys}
	var out []Check
	if q.Intent != "" {
		out = append(out, intentCheck(q.Intent, st.Intent))
	}
	if len(q.Needs) > 0 {
		out = append(out, contextNeedsCheck(q.Needs, ev))
	}
	return out
}

// intentCheck fails when the classifier gives the question another intent than the scripted one.
// The intent decides which context the answer gets, so a change of intent is a change of answer.
func intentCheck(want, got string) Check {
	return Check{Name: "intencion", OK: got == want,
		Detail: fmt.Sprintf("la pregunta debe ser de intención «%s» (es «%s»)", want, got)}
}

// contextNeedsCheck fails when a part of the player context that the answer quotes is held by the
// game but did not reach the prompt: the intent's selection left it out or the limit cut it.
func contextNeedsCheck(needs []string, ev Evidence) Check {
	var missing []string
	for _, n := range needs {
		if !containsStr(ev.Keys, n) && containsStr(ev.PresentKeys, n) {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return Check{Name: "contexto_necesario", OK: false,
			Detail: "faltan en el contexto de la pregunta, aunque el juego los tiene: " + strings.Join(missing, ", ")}
	}
	return Check{Name: "contexto_necesario", OK: true,
		Detail: "llegan al contexto los datos que usa la respuesta (" + strings.Join(needs, ", ") + ")"}
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// estimateWords mark an answer as an estimate.
var estimateWords = []string{"estim", "aprox", "calcul", "supon", "unos", "unas"}

// estimateCheck fails when a figure the tools only estimate is given without saying so.
func estimateCheck(answer string) Check {
	return Check{Name: "estimado", OK: containsAny(strings.ToLower(answer), estimateWords),
		Detail: "debe marcar las cifras calculadas como estimadas (estimado, aprox., calculado)"}
}

// gearSectionsCheck fails when a gear analysis does not separate what is reachable now ("Ya")
// from what is soon ("Pronto").
func gearSectionsCheck(answer string) Check {
	ya, pronto := yaRE.MatchString(answer), prontoRE.MatchString(answer)
	return Check{Name: "secciones", OK: ya && pronto,
		Detail: fmt.Sprintf("debe tener las secciones «Ya» y «Pronto» (Ya: %t, Pronto: %t)", ya, pronto)}
}

var (
	yaRE          = regexp.MustCompile(`\bYa\b`)
	prontoRE      = regexp.MustCompile(`\bPronto\b`)
	quotedRE      = regexp.MustCompile(`«([^»]+)»`)
	noDataPhrases = []string{"no tengo", "no encuentro", "no sé", "no aparece", "no hay datos"}
)

// itemCheck fails when the answer disagrees with the data about one item: it must name the item
// when the tool output holds it, and say it has no data when the output does not.
func itemCheck(item, answer string, ev Evidence) Check {
	low := strings.ToLower(answer)
	mentioned := strings.Contains(low, strings.ToLower(item))
	if strings.Contains(strings.ToLower(ev.text()), strings.ToLower(item)) {
		return Check{Name: "objeto", OK: mentioned,
			Detail: fmt.Sprintf("«%s» está en las herramientas: la respuesta debe nombrarlo", item)}
	}
	return Check{Name: "objeto", OK: containsAny(low, noDataPhrases),
		Detail: fmt.Sprintf("«%s» no está en las herramientas: la respuesta debe decir que no tiene datos", item)}
}

// spellNamesOf returns the spell names of the "hechizos" list that analizar_rotacion returns. The
// keys are sorted in that output, so the list ends before "instrucciones".
func spellNamesOf(text string) []string {
	i := strings.Index(text, `"hechizos"`)
	if i < 0 {
		return nil
	}
	seg := text[i:]
	if j := strings.Index(seg, `"instrucciones"`); j > 0 {
		seg = seg[:j]
	}
	var out []string
	for _, m := range spellNameRE.FindAllStringSubmatch(seg, -1) {
		out = append(out, m[1])
	}
	return out
}

var spellNameRE = regexp.MustCompile(`"nombre":\s*"([^"]+)"`)

// spellsCheck fails when a rotation answer names fewer than two spells of the spellbook the tools
// gave: the rotation must be built from those spells, not from memory.
func spellsCheck(answer string, ev Evidence) Check {
	// The names are read from the raw output: "Shadow Word: Pain" keeps its space.
	names := spellNamesOf(ev.text())
	low := strings.ToLower(answer)
	named := 0
	for _, n := range names {
		if strings.Contains(low, strings.ToLower(n)) {
			named++
		}
	}
	return Check{Name: "hechizos", OK: named >= 2 && named <= len(names),
		Detail: fmt.Sprintf("debe nombrar al menos 2 hechizos de su libro (%d nombrados de %d conocidos)", named, len(names))}
}

const barsApply = "/compa barras aplicar"

var barPairRE = regexp.MustCompile(`^(\d{1,3}):(\d+)$`)

// barCommandCheck fails when the answer's /compa barras aplicar commands would not work in the game:
// each pair is slot:spellID with the slot from 1 to 120 and a spell id the tools gave, the commands
// hold at most 40 pairs in total and each one fits in 255 characters.
func barCommandCheck(answer string, ev Evidence) Check {
	var cmds []string
	for _, line := range strings.Split(strings.ReplaceAll(answer, "\r\n", "\n"), "\n") {
		if i := strings.Index(line, barsApply); i >= 0 {
			cmds = append(cmds, strings.TrimSpace(line[i:]))
		}
	}
	if len(cmds) == 0 {
		return Check{Name: "comando_barras", OK: false, Detail: "debe dar al menos un comando «/compa barras aplicar»"}
	}
	evidence := strings.NewReplacer(": ", ":").Replace(ev.text())
	var problems []string
	pairs := 0
	for _, c := range cmds {
		if n := len([]rune(c)); n > 255 {
			problems = append(problems, fmt.Sprintf("comando de %d caracteres (máximo 255)", n))
		}
		args := strings.TrimSpace(strings.TrimPrefix(c, barsApply))
		if args == "" {
			problems = append(problems, "comando sin pares")
			continue
		}
		for _, p := range strings.Split(args, ",") {
			m := barPairRE.FindStringSubmatch(strings.TrimSpace(p))
			if m == nil {
				problems = append(problems, "par mal formado: "+strings.TrimSpace(p))
				continue
			}
			pairs++
			if slot, _ := strconv.Atoi(m[1]); slot < 1 || slot > 120 {
				problems = append(problems, "ranura fuera de 1 a 120: "+m[1])
			}
			if !spellIDRE(m[2]).MatchString(evidence) {
				problems = append(problems, "hechizo "+m[2]+" no está en las herramientas")
			}
		}
	}
	if pairs > 40 {
		problems = append(problems, fmt.Sprintf("%d pares (máximo 40)", pairs))
	}
	if len(problems) > 0 {
		return Check{Name: "comando_barras", OK: false, Detail: strings.Join(problems, "; ")}
	}
	return Check{Name: "comando_barras", OK: true,
		Detail: fmt.Sprintf("%d comando(s) válidos, %d pares con hechizos de las herramientas", len(cmds), pairs)}
}

// spellIDRE matches a spell id in normalised tool output ("id":585 followed by a delimiter).
func spellIDRE(id string) *regexp.Regexp {
	return regexp.MustCompile(`"id":` + regexp.QuoteMeta(id) + `\D`)
}

// quotedNamesCheck fails when a name the answer quotes (between «» ) is not in the data the
// tools or the player context gave. A quoted name is a claim about the game, so it must be grounded.
func quotedNamesCheck(answer string, ev Evidence) Check {
	matches := quotedRE.FindAllStringSubmatch(answer, -1)
	if len(matches) == 0 {
		return Check{Name: "nombres", OK: true, Detail: "sin nombres entre comillas que comprobar"}
	}
	haystack := strings.ToLower(ev.text())
	var missing []string
	checked := 0
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		checked++
		if !strings.Contains(haystack, strings.ToLower(name)) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return Check{Name: "nombres", OK: false,
			Detail: fmt.Sprintf("%d de %d nombres entre comillas no están en las herramientas: %s", len(missing), checked, strings.Join(missing, ", "))}
	}
	return Check{Name: "nombres", OK: true, Detail: fmt.Sprintf("%d nombres entre comillas respaldados por las herramientas", checked)}
}

// NoiseCheck fails when the answer talks about syncing or the window mode although the data
// it needs was there: the live bridge was connected, or the addon data was available and not
// flagged as stale. injected is the player context the assistant sent with the question; the
// tool outputs are read too.
func NoiseCheck(answer string, calls []ToolCall, injected string) Check {
	return noiseCheck(answer, Evidence{Calls: calls, Injected: injected})
}

// windowPhrases and syncPhrases are the only advice the noise check looks for. Other words
// such as "ventana de entrenamiento" (a profession window) are not window-mode advice.
var (
	windowPhrases = []string{"ventana sin bordes", "modo ventana"}
	syncPhrases   = []string{"/compa sync", "sincroniz"}
)

func noiseCheck(answer string, ev Evidence) Check {
	// Compact JSON (the injected context) and indented JSON (the tool outputs) are both
	// normalised to "key": value so that one set of patterns matches them.
	evidence := strings.NewReplacer(": ", ":").Replace(ev.text())
	bridge := strings.Contains(evidence, `"puenteConectado":true`)
	// Data is there when a tool reports it available, or when the player context carries the
	// location or the character (the pre-consulted block has no top-level "available").
	available := (strings.Contains(evidence, `"available":true`) && !strings.Contains(evidence, `"hint"`)) ||
		strings.Contains(evidence, `"ubicacion":{`) || strings.Contains(evidence, `"personaje":{`)
	stale := strings.Contains(evidence, "más de 10 minutos") || strings.Contains(evidence, "No hay datos del addon")
	low := strings.ToLower(answer)
	mentionsWindow := containsAny(low, windowPhrases)
	mentionsSync := containsAny(low, syncPhrases)
	switch {
	case mentionsWindow && bridge:
		return Check{Name: "ruido", OK: false, Detail: "menciona la ventana sin bordes con el puente conectado"}
	case mentionsSync && available && !stale:
		return Check{Name: "ruido", OK: false, Detail: "pide sincronizar con datos disponibles y sin avisos de antigüedad"}
	}
	return Check{Name: "ruido", OK: true, Detail: "sin avisos innecesarios"}
}

// LatencyCheck reports an answer that took longer than maxLatencySec. It is informational.
func LatencyCheck(sec float64) Check {
	return Check{Name: "latencia", OK: sec <= maxLatencySec, Info: true,
		Detail: fmt.Sprintf("%.1f s (aviso a partir de %d s)", sec, maxLatencySec)}
}

// coordRE is the answer format: "(x, y)" with one decimal and a dot.
var coordRE = regexp.MustCompile(`\(\d{1,3}\.\d, \d{1,3}\.\d\)`)

// HasCoordinates reports whether text contains a "(x, y)" pair or a mark command.
func HasCoordinates(text string) bool {
	return coordRE.MatchString(text) || strings.Contains(text, "/compa marca")
}

// groundTolerance is how far a coordinate of the answer may be from the number of the evidence
// it comes from, in map units (the 0..100 scale).
const groundTolerance = 0.2

// groundWindow is how many characters may separate the x and the y of a pair in the evidence.
// A JSON object or a sentence holds both values close together.
const groundWindow = 160

var (
	// pairRE matches "(x, y)" the way the answer writes a place.
	pairRE = regexp.MustCompile(`\((\d{1,3}(?:\.\d+)?),\s*(\d{1,3}(?:\.\d+)?)\)`)
	// markRE matches the coordinates of "/compa marca [mapID] x y [texto]".
	markRE = regexp.MustCompile(`/compa marca\s+(?:\d+\s+)?(\d{1,3}(?:\.\d+)?)\s+(\d{1,3}(?:\.\d+)?)`)
	// numberRE matches a number in the evidence; a longer digit run is one number.
	numberRE = regexp.MustCompile(`\d+(?:\.\d+)?`)
)

// answerPair is a coordinate the answer gives, with the text it was written as.
type answerPair struct {
	x, y float64
	text string
}

// answerCoordinates lists the "(x, y)" pairs and the /compa marca coordinates of an answer.
func answerCoordinates(answer string) []answerPair {
	var out []answerPair
	for _, m := range pairRE.FindAllStringSubmatch(answer, -1) {
		out = append(out, newPair(m[1], m[2], m[0]))
	}
	for _, m := range markRE.FindAllStringSubmatch(answer, -1) {
		out = append(out, newPair(m[1], m[2], strings.TrimSpace(m[0])))
	}
	return out
}

func newPair(xs, ys, text string) answerPair {
	x, _ := strconv.ParseFloat(xs, 64)
	y, _ := strconv.ParseFloat(ys, 64)
	return answerPair{x: x, y: y, text: text}
}

// located is a number of the evidence with its position in the text.
type located struct {
	v   float64
	pos int
}

// evidenceNumbers lists the numbers of the evidence. A decimal between 0 and 1 is also taken
// on the 0..100 scale, because some tools report map coordinates that way.
func evidenceNumbers(text string) []located {
	var out []located
	for _, loc := range numberRE.FindAllStringIndex(text, -1) {
		s := text[loc[0]:loc[1]]
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			continue
		}
		out = append(out, located{v: v, pos: loc[0]})
		if v <= 1 && strings.Contains(s, ".") {
			out = append(out, located{v: v * 100, pos: loc[0]})
		}
	}
	return out
}

// pairGrounded reports whether the evidence holds the pair: a number within tolerance of x and
// a number within tolerance of y, written close to each other.
func pairGrounded(p answerPair, nums []located) bool {
	for _, a := range nums {
		if !nearly(a.v, p.x) {
			continue
		}
		for _, b := range nums {
			if nearly(b.v, p.y) && absInt(a.pos-b.pos) <= groundWindow {
				return true
			}
		}
	}
	return false
}

func nearly(v, want float64) bool {
	return math.Abs(v-want) <= groundTolerance+1e-9
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// groundingCheck fails when the answer gives a coordinate that no tool output and no player
// context holds: an invented place is worse than no place. Answers without coordinates pass.
func groundingCheck(answer string, ev Evidence) Check {
	pairs := answerCoordinates(answer)
	if len(pairs) == 0 {
		return Check{Name: "grounding", OK: true, Detail: "sin coordenadas que comprobar"}
	}
	nums := evidenceNumbers(ev.text())
	var missing []string
	for _, p := range pairs {
		if !pairGrounded(p, nums) {
			missing = append(missing, p.text)
		}
	}
	if len(missing) > 0 {
		return Check{Name: "grounding", OK: false,
			Detail: fmt.Sprintf("%d de %d coordenadas sin respaldo en las herramientas o el contexto: %s",
				len(missing), len(pairs), strings.Join(missing, ", "))}
	}
	return Check{Name: "grounding", OK: true,
		Detail: fmt.Sprintf("%d coordenadas respaldadas por las herramientas o el contexto", len(pairs))}
}

// containsAny reports whether s contains one of the phrases.
func containsAny(s string, phrases []string) bool {
	for _, p := range phrases {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

var refusalPhrases = []string{"no puedo", "no voy a", "no es posible", "no te puedo", "no ayudo", "incumpl", "términos de servicio", "no se puede"}

// Refuses reports whether the answer declines the request in Spanish.
func Refuses(text string) bool {
	low := strings.ToLower(text)
	for _, p := range refusalPhrases {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

// CitesSource reports whether the answer names a source: a "Fuente" line or a link.
func CitesSource(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(low, "fuente") || strings.Contains(low, "http")
}

var (
	spanishWords = map[string]bool{"el": true, "la": true, "los": true, "las": true, "de": true, "que": true, "y": true,
		"en": true, "un": true, "una": true, "con": true, "para": true, "es": true, "por": true, "del": true, "al": true,
		"lo": true, "se": true, "su": true, "sus": true, "hay": true, "no": true, "si": true, "pero": true, "más": true}
	englishWords = map[string]bool{"the": true, "and": true, "you": true, "to": true, "is": true, "of": true, "with": true,
		"your": true, "for": true, "are": true, "this": true, "that": true, "it": true, "can": true, "be": true, "on": true}
)

// castellanoScore counts common Spanish and English function words.
func castellanoScore(text string) (es, en int) {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'á' && r <= 'ú' || r == 'ñ' || r == 'ü')
	})
	for _, w := range words {
		if spanishWords[w] {
			es++
		}
		if englishWords[w] {
			en++
		}
	}
	return es, en
}

func nonEmptyLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func usedAny(calls []ToolCall, names []string) bool {
	for _, c := range calls {
		for _, n := range names {
			if c.Name == n {
				return true
			}
		}
	}
	return false
}

func toolCalls(recs []tools.CallRecord) []ToolCall {
	out := make([]ToolCall, 0, len(recs))
	for _, r := range recs {
		out = append(out, ToolCall{
			Name:       r.Name,
			Args:       r.Args,
			DurationMs: round2(float64(r.Duration) / float64(time.Millisecond)),
			IsError:    r.IsError,
			Output:     clip(r.Text, maxOutputChars),
			Err:        r.Err,
		})
	}
	return out
}

func errDetail(err error, answer string) string {
	switch {
	case err != nil:
		return "error: " + err.Error()
	case answer == "":
		return "respuesta vacía"
	}
	return fmt.Sprintf("%d caracteres", len([]rune(answer)))
}

func allOK(checks []Check) bool {
	for _, c := range checks {
		if !c.OK && !c.Info {
			return false
		}
	}
	return len(checks) > 0
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// JSON encodes the report with indentation.
func JSON(rep Report) ([]byte, error) {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("codificar el informe: %w", err)
	}
	return append(data, '\n'), nil
}

// Markdown renders the report for people: a summary table, then each question.
func Markdown(rep Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Evaluación de Compañero WoW\n\n")
	fmt.Fprintf(&b, "- Fecha: %s\n", rep.StartedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "- Proveedor pedido: %s\n", orDefault(rep.RequestProvider, "cadena completa"))
	fmt.Fprintf(&b, "- Versión de la skill: %s\n", rep.SkillVersion)
	fmt.Fprintf(&b, "- Resultado: %d de %d preguntas superan todas las comprobaciones\n\n", rep.Passed, rep.Total)

	if v := rep.Verifier; v != nil {
		fmt.Fprintf(&b, "- Verificador: %d respuestas; %d con datos sin respaldo (%d afirmaciones); %d reparadas; %d con aviso en pantalla\n\n", v.Answers, v.Violations, v.Claims, v.Repaired, v.Flagged)
	}
	b.WriteString("| Pregunta | Proveedor | Modelo | Segundos | Herramientas | Resultado |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, r := range rep.Records {
		res := "NO"
		if r.Passed {
			res = "sí"
		}
		fmt.Fprintf(&b, "| %s %s | %s | %s | %.1f | %d | %s |\n", r.ID, escapeCell(r.Question), orDefault(r.Provider, "—"), orDefault(r.Model, "—"), r.DurationSec, len(r.Tools), res)
	}
	writeModelLatency(&b, rep.Records)
	writeContextSavings(&b, rep)
	for _, r := range rep.Records {
		fmt.Fprintf(&b, "\n## %s %s\n\n", r.ID, r.Question)
		fmt.Fprintf(&b, "Proveedor: %s · Modelo: %s · %.1f s · %d herramientas\n\n", orDefault(r.Provider, "ninguno"), orDefault(r.Model, "—"), r.DurationSec, len(r.Tools))
		if r.Error != "" {
			fmt.Fprintf(&b, "**Error:** %s\n\n", r.Error)
		}
		b.WriteString("Comprobaciones:\n\n")
		for _, c := range r.Checks {
			mark := "✅"
			if !c.OK {
				mark = "❌"
				if c.Info {
					mark = "⚠️"
				}
			}
			fmt.Fprintf(&b, "- %s %s — %s\n", mark, c.Name, c.Detail)
		}
		if len(r.Tools) > 0 {
			b.WriteString("\nHerramientas llamadas:\n\n")
			for _, t := range r.Tools {
				fmt.Fprintf(&b, "- `%s` %s (%.0f ms)", t.Name, t.Args, t.DurationMs)
				if t.Err != "" {
					fmt.Fprintf(&b, " — error: %s", t.Err)
				}
				b.WriteString("\n")
			}
		}
		b.WriteString("\nRespuesta:\n\n")
		for _, l := range strings.Split(strings.TrimSpace(r.Answer), "\n") {
			b.WriteString("> " + l + "\n")
		}
		if strings.TrimSpace(r.Answer) == "" {
			b.WriteString("> (sin respuesta)\n")
		}
	}
	return b.String()
}

// writeContextSavings writes the size of the system prompt of each question before and after the
// intent selection, in characters and in estimated tokens (a token is about four characters).
func writeContextSavings(b *strings.Builder, rep Report) {
	if rep.CharsBefore == 0 && rep.CharsAfter == 0 {
		return
	}
	b.WriteString("\n## Contexto por pregunta (system prompt, sin la skill)\n\n")
	b.WriteString("| Pregunta | Intención | Antes (car.) | Después (car.) | Ahorro |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, r := range rep.Records {
		if r.CharsBefore == 0 && r.CharsAfter == 0 {
			continue
		}
		fmt.Fprintf(b, "| %s | %s | %d | %d | %s |\n", r.ID, orDefault(r.Intent, "—"), r.CharsBefore, r.CharsAfter, savingPct(r.CharsBefore, r.CharsAfter))
	}
	fmt.Fprintf(b, "| **Total** | | %d (≈ %d tokens) | %d (≈ %d tokens) | %s |\n\n",
		rep.CharsBefore, approxTokens(rep.CharsBefore), rep.CharsAfter, approxTokens(rep.CharsAfter),
		savingPct(rep.CharsBefore, rep.CharsAfter))
	b.WriteString("Tokens estimados como caracteres / 4. La skill completa se sigue enviando a los proveedores; aquí solo cuenta lo que va en el system prompt de cada pregunta.\n")
}

// approxTokens estimates the tokens of n characters.
func approxTokens(n int) int {
	return (n + 3) / 4
}

// savingPct is the share of before that after no longer takes, as text.
func savingPct(before, after int) string {
	if before <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f %%", 100*float64(before-after)/float64(before))
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func escapeCell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// modelOf is the agy model that answered, from the response or from its provider name
// ("antigravity:<model>").
func modelOf(resp ai.Response) string {
	if resp.Model != "" {
		return resp.Model
	}
	if _, model, ok := strings.Cut(resp.Provider, ":"); ok {
		return model
	}
	return ""
}

// modelStat is the latency of the questions one model answered.
type modelStat struct {
	Model string
	N     int
	Errs  int
	Total float64
	Max   float64
}

// modelLatency groups the records by the model that answered (the provider when no model
// did), in order of first use.
func modelLatency(recs []Record) []modelStat {
	var out []modelStat
	idx := map[string]int{}
	for _, r := range recs {
		name := r.Model
		if _, m, ok := strings.Cut(r.Provider, ":"); ok && name == "" {
			name = m
		}
		name = orDefault(name, orDefault(r.Provider, "ninguno"))
		i, ok := idx[name]
		if !ok {
			i = len(out)
			idx[name] = i
			out = append(out, modelStat{Model: name})
		}
		s := &out[i]
		s.N++
		if r.Error != "" {
			s.Errs++
		}
		s.Total += r.DurationSec
		s.Max = max(s.Max, r.DurationSec)
	}
	return out
}

// writeModelLatency writes the per-model latency table of the report.
func writeModelLatency(b *strings.Builder, recs []Record) {
	stats := modelLatency(recs)
	if len(stats) == 0 {
		return
	}
	b.WriteString("\n## Latencia por modelo\n\n")
	b.WriteString("| Modelo | Preguntas | Errores | Media (s) | Máxima (s) |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, s := range stats {
		fmt.Fprintf(b, "| %s | %d | %d | %.1f | %.1f |\n", escapeCell(s.Model), s.N, s.Errs, s.Total/float64(s.N), s.Max)
	}
}
