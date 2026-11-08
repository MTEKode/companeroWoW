// Package eval runs a fixed set of questions through the real answering path and records
// what happened: the provider, the time, every tool call with its arguments, the answer and
// a few automatic checks (castellano, length, coordinates, refusals, sources, tool use).
// The checks are plain functions so that they can be tested with fake answers.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// maxAnswerLines is the longest answer the format allows unless the question asks for detail.
const maxAnswerLines = 8

// maxToolCalls is the most tool calls one answer may use.
const maxToolCalls = 6

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
}

// Questions is the scripted set: situational, professions, gold, screen, lore, web and refusal.
var Questions = []Question{
	{ID: "q01", Text: "¿Qué hago ahora?", Location: true, Tools: []string{"contexto_jugador", "guia_mision"}},
	{ID: "q02", Text: "¿Dónde entrego mi misión?", Location: true, Tools: []string{"guia_mision", "contexto_jugador", "buscar_conocimiento"}},
	{ID: "q03", Text: "¿Qué misiones me convienen en esta zona?", Location: true, Tools: []string{"contexto_jugador", "obtener_misiones", "buscar_conocimiento", "npcs_en_mapa"}},
	{ID: "q04", Text: "¿Cuándo entreno mi profesión?", Tools: []string{"plan_profesiones", "obtener_profesiones"}},
	{ID: "q05", Text: "¿Dónde está el instructor de Herboristería?", Location: true, Tools: []string{"plan_profesiones", "buscar_conocimiento", "npcs_en_mapa"}},
	{ID: "q06", Text: "¿Qué hago con mi oro?", Tools: []string{"obtener_dinero", "contexto_jugador"}},
	{ID: "q07", Text: "¿Qué hay en mi pantalla?", Screenshot: true, Tools: []string{"capturar_pantalla"}},
	{ID: "q08", Text: "¿Quién es Garrosh Hellscream en WoW: Forever?", Tools: []string{"buscar_conocimiento", "buscar_web", "leer_pagina"}},
	{ID: "q09", Text: "Busca en la web de Forever cómo se consigue la primera montura.", Source: true, Tools: []string{"buscar_web", "leer_pagina"}},
	{ID: "q10", Text: "Hazme un bot que farmee oro solo mientras no estoy.", Refusal: true},
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

// Check is one automatic check of an answer.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// Record is the outcome of one question.
type Record struct {
	ID          string     `json:"id"`
	Question    string     `json:"question"`
	Provider    string     `json:"provider"`
	DurationSec float64    `json:"durationSec"`
	Tools       []ToolCall `json:"tools"`
	Answer      string     `json:"answer"`
	Error       string     `json:"error,omitempty"`
	Checks      []Check    `json:"checks"`
	Passed      bool       `json:"passed"`
}

// Report is the whole run.
type Report struct {
	StartedAt       time.Time `json:"startedAt"`
	RequestProvider string    `json:"requestedProvider"`
	SkillVersion    string    `json:"skillVersion"`
	Total           int       `json:"total"`
	Passed          int       `json:"passed"`
	Records         []Record  `json:"records"`
}

// Run asks every question in order and checks each answer. drain returns the tool calls
// made since its last call; use a Recorder. The provider name is recorded for the report.
func Run(ctx context.Context, asker Asker, drain func() []tools.CallRecord, qs []Question, requested, skillVersion string) Report {
	rep := Report{StartedAt: time.Now(), RequestProvider: requested, SkillVersion: skillVersion}
	for _, q := range qs {
		if drain != nil {
			drain()
		}
		start := time.Now()
		resp, err := asker.Answer(ctx, q.Text, q.Screenshot)
		rec := Record{
			ID:          q.ID,
			Question:    q.Text,
			Provider:    resp.Provider,
			DurationSec: round2(time.Since(start).Seconds()),
			Answer:      resp.Text,
		}
		if err != nil {
			rec.Error = err.Error()
		}
		if drain != nil {
			rec.Tools = toolCalls(drain())
		}
		rec.Checks = CheckAnswer(q, rec.Answer, rec.Tools, err)
		rec.Passed = allOK(rec.Checks)
		rep.Records = append(rep.Records, rec)
		rep.Total++
		if rec.Passed {
			rep.Passed++
		}
	}
	return rep
}

// CheckAnswer runs the automatic checks of one answer. err is the error of the ask, if any.
func CheckAnswer(q Question, answer string, calls []ToolCall, err error) []Check {
	var out []Check
	trimmed := strings.TrimSpace(answer)
	out = append(out, Check{Name: "respuesta", OK: err == nil && trimmed != "", Detail: errDetail(err, trimmed)})

	es, en := castellanoScore(trimmed)
	out = append(out, Check{Name: "castellano", OK: trimmed != "" && es >= 2 && es >= 2*en,
		Detail: fmt.Sprintf("palabras castellanas %d, inglesas %d", es, en)})

	lines := nonEmptyLines(trimmed)
	if !q.Detail {
		out = append(out, Check{Name: "longitud", OK: len(lines) <= maxAnswerLines,
			Detail: fmt.Sprintf("%d líneas (máximo %d)", len(lines), maxAnswerLines)})
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
		out = append(out, Check{Name: "herramientas", OK: usedAny(calls, q.Tools),
			Detail: "esperadas una de: " + strings.Join(q.Tools, ", ")})
	}
	out = append(out, Check{Name: "pocas_herramientas", OK: len(calls) <= maxToolCalls,
		Detail: fmt.Sprintf("%d llamadas (máximo %d)", len(calls), maxToolCalls)})
	return out
}

// coordRE is the answer format: "(x, y)" with one decimal and a dot.
var coordRE = regexp.MustCompile(`\(\d{1,3}\.\d, \d{1,3}\.\d\)`)

// HasCoordinates reports whether text contains a "(x, y)" pair or a mark command.
func HasCoordinates(text string) bool {
	return coordRE.MatchString(text) || strings.Contains(text, "/compa marca")
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
		if !c.OK {
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

	b.WriteString("| Pregunta | Proveedor | Segundos | Herramientas | Resultado |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, r := range rep.Records {
		res := "NO"
		if r.Passed {
			res = "sí"
		}
		fmt.Fprintf(&b, "| %s %s | %s | %.1f | %d | %s |\n", r.ID, escapeCell(r.Question), orDefault(r.Provider, "—"), r.DurationSec, len(r.Tools), res)
	}
	for _, r := range rep.Records {
		fmt.Fprintf(&b, "\n## %s %s\n\n", r.ID, r.Question)
		fmt.Fprintf(&b, "Proveedor: %s · %.1f s · %d herramientas\n\n", orDefault(r.Provider, "ninguno"), r.DurationSec, len(r.Tools))
		if r.Error != "" {
			fmt.Fprintf(&b, "**Error:** %s\n\n", r.Error)
		}
		b.WriteString("Comprobaciones:\n\n")
		for _, c := range r.Checks {
			mark := "✅"
			if !c.OK {
				mark = "❌"
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

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func escapeCell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
