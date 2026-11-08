package ui

import "strings"

// The assistant ends a map location with a line like "📍 /compa marca 85 51.2 38.7 Entregar".
// The overlay shows that line highlighted, and a click copies the command (without the
// emoji) to the clipboard so the player can paste it into the game chat. The app never
// sends the command to the game.

const (
	// markLinePrefix starts the line that carries a mark command.
	markLinePrefix = "📍 /compa marca"
	// markCommandPrefix is the start of the command itself.
	markCommandPrefix = "/compa marca"
	// copyHint is the visible action after the command; clicking the line copies it.
	copyHint = "· Copiar marca"
	// CopiedNotice is the status message after a successful copy.
	CopiedNotice = "Copiado: pégalo en el chat del juego"
)

// MarkCommands returns the mark commands of text, in order, without the emoji.
func MarkCommands(text string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if cmd, ok := markLineCommand(line); ok {
			out = append(out, cmd)
		}
	}
	return out
}

// markLineCommand returns the command of a mark line.
func markLineCommand(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, markLinePrefix) {
		return "", false
	}
	cmd := strings.TrimSpace(strings.TrimPrefix(t, "📍"))
	return cmd, strings.HasPrefix(cmd, markCommandPrefix)
}

// MarkRuns converts an assistant answer into styled runs. Mark lines are highlighted and
// clickable (their text is the command plus the copy hint); other lines use the Markdown
// subset. Lines are separated by a run whose text is "\n", as in Markdown.
func MarkRuns(src string) []Span {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var out []Span
	for i, line := range strings.Split(src, "\n") {
		if i > 0 {
			out = append(out, Span{Text: "\n"})
		}
		if cmd, ok := markLineCommand(line); ok {
			out = append(out,
				Span{Text: "📍 ", Style: StyleMark},
				Span{Text: cmd + "  " + copyHint, Style: StyleMark | StyleLink | StyleBold},
			)
			continue
		}
		out = append(out, markdownLine(line)...)
	}
	return out
}

// CommandFromLink returns the command of a clicked mark line. ok is false for any other
// link (those open in the browser).
func CommandFromLink(text string) (string, bool) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, markCommandPrefix) {
		return "", false
	}
	if i := strings.Index(t, copyHint); i >= 0 {
		t = t[:i]
	}
	cmd := strings.TrimSpace(t)
	return cmd, cmd != markCommandPrefix && cmd != ""
}
