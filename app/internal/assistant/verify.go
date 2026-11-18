package assistant

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/eval"
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
	violations := eval.Grounding(resp.Text, eval.EvidenceFor(outputsOf(calls), injected))
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
			left := eval.Grounding(r2.Text, eval.EvidenceFor(all, injected))
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

// outputsOf returns the text of each tool output of the calls, in order.
func outputsOf(calls []tools.CallRecord) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		if c.Text != "" {
			out = append(out, c.Text)
		}
	}
	return out
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
