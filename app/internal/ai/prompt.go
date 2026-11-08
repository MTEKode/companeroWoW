package ai

import (
	"fmt"
	"strings"
)

// maxHistoryTurns is how many of the most recent turns the CLI prompt carries.
const maxHistoryTurns = 10

// buildPrompt flattens a request into one text for the CLI providers:
// system prompt, the recent history, then the question. imageLine, when not empty,
// is a line placed before the question that tells the model about the attachment.
func buildPrompt(req Request, imageLine string) string {
	var b strings.Builder
	if sys := strings.TrimSpace(req.System); sys != "" {
		b.WriteString(sys)
		b.WriteString("\n\n")
	}
	hist := req.History
	if len(hist) > maxHistoryTurns {
		hist = hist[len(hist)-maxHistoryTurns:]
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
	b.WriteString(strings.TrimSpace(req.Prompt))
	b.WriteString("\n")
	return b.String()
}
