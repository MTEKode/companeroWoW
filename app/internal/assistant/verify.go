package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/eval"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// The answer verifier. Before an answer is shown, its coordinates, quoted names and barras
// commands are checked against what the tools returned during the question and the player
// context that was sent with it. If something is not backed, the same question gets one
// repair round ("corrige solo estos datos"); if the repaired answer is still not backed, it is
// shown with a short warning footer that names the data.

// repairMinTime is the time that must be left before the deadline for a repair round: a round
// is one more provider call.
const repairMinTime = 25 * time.Second

// footerRunes caps the warning footer, so that a long list of violations stays short.
const footerRunes = 240

// verifyWarning introduces the footer that lists the data that could not be verified.
const verifyWarning = "⚠ datos sin verificar: "

// SetCaptures connects the per-question capture of tool calls. Without it the verifier sees
// only the player context and finds nothing to check the tool output against, so the answers
// that cite data from the tools would be flagged; the capture is what makes it work.
func (a *Assistant) SetCaptures(c *tools.Captures) {
	a.captures = c
}

// VerifyStats returns what the verifier did since the assistant started.
func (a *Assistant) VerifyStats() eval.VerifierStats {
	return a.verifier.Snapshot()
}

// verifyOn reports whether answers are verified (the verifyAnswers setting, default on).
func (a *Assistant) verifyOn() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.verify
}

// askVerified asks the chain and verifies the answer. The request's System holds the game
// state and the player context, which is the evidence the answer may quote; the tool outputs
// of the question come from the capture. It returns the answer to show.
func (a *Assistant) askVerified(ctx context.Context, req ai.Request) (ai.Response, error) {
	return a.askVerifiedStream(ctx, req, ai.Stream{})
}

// askVerifiedStream is askVerified with the first answer written to s as it comes. A repair round
// is not streamed: when it changes the answer, the response carries the final text and Corrected.
func (a *Assistant) askVerifiedStream(ctx context.Context, req ai.Request, s ai.Stream) (ai.Response, error) {
	resp, calls, err := a.askCaptured(ctx, req, s)
	if err != nil {
		return resp, err
	}
	if !a.verifyOn() {
		return resp, nil
	}
	injected := req.System
	violations := groundingOf(resp.Text, eval.EvidenceFor(outputsOf(calls), injected))
	if len(violations) == 0 {
		a.verifier.Add(1, 0, false, false)
		return resp, nil
	}
	found := eval.Claims(violations)

	if canRepair(ctx) {
		repair := repairRequest(req, resp.Text, violations)
		r2, calls2, rerr := a.askCaptured(ctx, repair, ai.Stream{})
		if rerr == nil && strings.TrimSpace(r2.Text) != "" {
			all := append(outputsOf(calls), outputsOf(calls2)...)
			left := groundingOf(r2.Text, eval.EvidenceFor(all, injected))
			if len(left) == 0 {
				a.verifier.Add(1, found, true, false)
				r2.Corrected = true
				return r2, nil
			}
			// Still not backed: the repaired text is shown when it has fewer unbacked claims.
			if eval.Claims(left) < eval.Claims(violations) {
				resp, violations = r2, left
			}
		}
	}
	resp.Text = withVerifyFooter(resp.Text, violations)
	resp.Corrected = true
	a.verifier.Add(1, found, false, true)
	return resp, nil
}

// groundingOf is eval.Grounding plus the macro rule: every spell and item that a /compa macro
// command names must be in the evidence (the sheet, the bags or the context the tools gave).
func groundingOf(answer string, ev eval.Evidence) []eval.Violation {
	out := eval.Grounding(answer, ev)
	if v, ok := macroGrounding(answer, ev); ok {
		out = append(out, v)
	}
	return out
}

// macroGrounding checks the names of the macros in an answer. A macro is read from its
// /compa macro crear line, or from its /compa macro parte lines when they are all there.
// ok is false when the answer has no macro or every name is backed.
func macroGrounding(answer string, ev eval.Evidence) (eval.Violation, bool) {
	haystack := strings.ToLower(ev.Output + "\n" + ev.Injected)
	var missing []string
	for _, body := range macroBodiesIn(answer) {
		for _, name := range gamedata.MacroNames(body) {
			if !strings.Contains(haystack, strings.ToLower(name)) {
				missing = append(missing, name)
			}
		}
	}
	if len(missing) == 0 {
		return eval.Violation{}, false
	}
	return eval.Violation{
		Check:  "macro",
		Detail: "nombres de la macro sin respaldo en el libro de hechizos ni en las bolsas: " + strings.Join(missing, ", "),
		Claims: len(missing),
	}, true
}

// macroBodiesIn returns the bodies of the /compa macro crear commands of an answer, in order. The
// parts of a chunked macro count only when all of them are in the answer.
func macroBodiesIn(answer string) []string {
	const create = "/compa macro crear "
	const part = "/compa macro parte "
	var bodies []string
	parts := map[int]string{}
	total := 0
	for _, line := range strings.Split(strings.ReplaceAll(answer, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if i := strings.Index(t, create); i >= 0 {
			if body := payloadBody(t[i+len(create):]); body != "" {
				bodies = append(bodies, body)
			}
			continue
		}
		if i := strings.Index(t, part); i >= 0 {
			// "n/total texto": the text is everything after the first space, as the addon reads it.
			head, text, found := strings.Cut(t[i+len(part):], " ")
			var n, of int
			if _, err := fmt.Sscanf(head, "%d/%d", &n, &of); found && err == nil && n >= 1 && of >= 1 {
				parts[n] = text
				total = of
			}
		}
	}
	if total > 0 && len(parts) == total {
		var b strings.Builder
		for n := 1; n <= total; n++ {
			b.WriteString(parts[n])
		}
		if body := payloadBody(b.String()); body != "" {
			bodies = append(bodies, body)
		}
	}
	return bodies
}

// payloadBody reads the body of a "nombre|icono|cuerpo" payload, with the literal \n as line breaks.
func payloadBody(payload string) string {
	parts := strings.SplitN(payload, "|", 3)
	if len(parts) < 3 {
		return ""
	}
	return strings.ReplaceAll(parts[2], "\\n", "\n")
}

// streamingAsker is implemented by the chain: its answer can be written as it comes.
type streamingAsker interface {
	AskStream(ctx context.Context, req ai.Request, s ai.Stream) (ai.Response, error)
}

// askCaptured asks the chain once, streaming to s when s wants it, and returns the tool calls that
// were made while it ran.
func (a *Assistant) askCaptured(ctx context.Context, req ai.Request, s ai.Stream) (ai.Response, []tools.CallRecord, error) {
	if a.asker == nil {
		return ai.Response{}, nil, errors.New("no hay proveedores de IA configurados (revisa providerOrder y apiProviders)")
	}
	var cp *tools.Capture
	if a.captures != nil {
		cp = a.captures.Begin()
	}
	var resp ai.Response
	var err error
	if st, ok := a.asker.(streamingAsker); ok && s.Delta != nil {
		resp, err = st.AskStream(ctx, req, s)
	} else {
		resp, err = a.asker.Ask(ctx, req)
	}
	return resp, cp.End(), err
}

// canRepair reports whether a repair round fits before the deadline. Without a deadline the
// providers' own timeouts bound it.
func canRepair(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	dl, ok := ctx.Deadline()
	return !ok || time.Until(dl) >= repairMinTime
}

// repairRequest asks the provider to correct only the unbacked data of its own answer, using
// the tools. It keeps the skill and the system context, so the same rules apply.
func repairRequest(orig ai.Request, answer string, violations []eval.Violation) ai.Request {
	var b strings.Builder
	b.WriteString("Corrige solo estos datos no respaldados por las herramientas o el contexto: ")
	b.WriteString(eval.FooterDetail(violations))
	b.WriteString(". Usa las herramientas para comprobarlos; si no puedes confirmarlos, quítalos. ")
	b.WriteString("No cambies el resto de la respuesta, que sigue el formato de siempre.\n\n")
	b.WriteString("Pregunta original: ")
	b.WriteString(orig.Prompt)
	b.WriteString("\n\nRespuesta anterior:\n")
	b.WriteString(answer)
	return ai.Request{
		Skill:  orig.Skill,
		System: orig.System,
		Prompt: b.String(),
	}
}

// withVerifyFooter adds the warning line that names the data that could not be verified.
func withVerifyFooter(text string, violations []eval.Violation) string {
	detail := eval.FooterDetail(violations)
	if utf8.RuneCountInString(detail) > footerRunes {
		detail = truncateRunes(detail, footerRunes)
	}
	return strings.TrimRight(text, " \n") + "\n" + verifyWarning + detail
}

// outputsOf returns the text of each tool output of the calls, in order. The rows of consultar_db
// are data the answer may quote (dbRowsText).
func outputsOf(calls []tools.CallRecord) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		if c.Text == "" {
			continue
		}
		if c.Name == "consultar_db" {
			out = append(out, dbRowsText(c.Text))
			continue
		}
		out = append(out, c.Text)
	}
	return out
}

// dbRowsText turns the JSON answer of consultar_db into its plain values: one line of column names
// and one line per row, the values separated by " | ". The JSON escapes would hide a quoted name
// (a quote or a backslash inside a value), and the rows are grounded data, so the check compares
// the values as they are. An answer that is not that JSON is returned as it is.
func dbRowsText(s string) string {
	var res struct {
		Columnas []string `json:"columnas"`
		Filas    [][]any  `json:"filas"`
	}
	if err := json.Unmarshal([]byte(s), &res); err != nil || res.Columnas == nil {
		return s
	}
	var b strings.Builder
	b.WriteString(strings.Join(res.Columnas, " | "))
	for _, row := range res.Filas {
		b.WriteString("\n")
		for i, v := range row {
			if i > 0 {
				b.WriteString(" | ")
			}
			if v != nil {
				b.WriteString(fmt.Sprint(v))
			}
		}
	}
	return b.String()
}

// limitTipLines is limitLines for a proactive tip, keeping the verifier's warning line: the
// warning is the last line of an unverified answer and must survive the tip's line limit.
func limitTipLines(s string) string {
	text := limitLines(s, maxTipLines)
	i := strings.LastIndex(s, verifyWarning)
	if i < 0 || strings.Contains(text, verifyWarning) {
		return text
	}
	body := limitLines(s[:i], maxTipLines-1)
	warn := strings.TrimSpace(s[i:])
	if body == "" {
		return warn
	}
	return body + "\n" + warn
}
