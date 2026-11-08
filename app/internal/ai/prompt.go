package ai

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxHistoryTurns is how many of the most recent turns the CLI prompt carries.
const maxHistoryTurns = 10

// buildPrompt flattens a request into one text for the CLI providers:
// system prompt, the recent history, then the question. imageLine, when not empty,
// is a line placed before the question that tells the model about the attachment.
func buildPrompt(req Request, imageLine string) string {
	hist := req.History
	if len(hist) > maxHistoryTurns {
		hist = hist[len(hist)-maxHistoryTurns:]
	}
	return renderPrompt(systemText(req), hist, imageLine, req.Prompt)
}

// systemText is the instruction text of a request for a CLI: the skill (when the provider
// could not read it from a file) followed by the per-question context.
func systemText(req Request) string {
	skill := strings.TrimSpace(req.Skill)
	ctx := strings.TrimSpace(req.System)
	switch {
	case skill == "":
		return ctx
	case ctx == "":
		return skill
	}
	return skill + "\n\n" + ctx
}

// renderPrompt lays out the system prompt, the given history turns and the question.
func renderPrompt(system string, hist []Message, imageLine, question string) string {
	var b strings.Builder
	if sys := strings.TrimSpace(system); sys != "" {
		b.WriteString(sys)
		b.WriteString("\n\n")
	}
	if len(hist) > 0 {
		b.WriteString("Historial:\n")
		for _, m := range hist {
			label := "Usuario"
			if m.Role == "assistant" {
				label = "Asistente"
			}
			fmt.Fprintf(&b, "%s: %s\n", label, strings.TrimSpace(m.Content))
		}
		b.WriteString("\n")
	}
	if imageLine != "" {
		b.WriteString(imageLine)
		b.WriteString("\n\n")
	}
	b.WriteString("Pregunta: ")
	b.WriteString(strings.TrimSpace(question))
	b.WriteString("\n")
	return b.String()
}

// buildPromptCapped is buildPrompt limited to max runes. The oldest history turns are
// dropped first; when even the question and system prompt do not fit, the system prompt
// is cut from its end.
func buildPromptCapped(req Request, imageLine string, limit int) string {
	hist := req.History
	if len(hist) > maxHistoryTurns {
		hist = hist[len(hist)-maxHistoryTurns:]
	}
	system := systemText(req)
	text := renderPrompt(system, hist, imageLine, req.Prompt)
	for len(hist) > 0 && utf8.RuneCountInString(text) > limit {
		hist = hist[1:]
		text = renderPrompt(system, hist, imageLine, req.Prompt)
	}
	if utf8.RuneCountInString(text) > limit {
		r := []rune(system)
		over := utf8.RuneCountInString(text) - limit
		if over < len(r) {
			system = string(r[:len(r)-over])
		} else {
			system = ""
		}
		text = renderPrompt(system, nil, imageLine, req.Prompt)
	}
	if utf8.RuneCountInString(text) > limit {
		r := []rune(text)
		text = string(r[:limit])
	}
	return text
}
