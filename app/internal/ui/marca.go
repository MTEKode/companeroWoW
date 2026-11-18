package ui

import "strings"

// The assistant ends a map location with a line like "📍 /compa marca 85 51.2 38.7 Entregar", and
// sets up action bars with a line like "⌨️ /compa barras aplicar 6:2054". A macro comes as a line
// "🧩 /compa macro crear …" (the command to paste in the chat), and its body as a line
// "🧩 cuerpo: /cast …\n/cast …" (monospace; "· Copiar macro" copies it with its line breaks, for the
// macro window). The overlay shows those lines highlighted, and a click copies the command (without
// the emoji) to the clipboard so the player can paste it into the game chat. The app never sends a
// command to the game.

const (
	// markCommandPrefix starts a map mark command.
	markCommandPrefix = "/compa marca"
	// barsCommandPrefix starts an action bar command.
	barsCommandPrefix = "/compa barras"
	// macroCommandPrefix starts a macro command (crear, parte, borrar, deshacer, barra, lista).
	macroCommandPrefix = "/compa macro"
	// macroBodyHead starts the line that shows the body of a macro.
	macroBodyHead = "🧩 cuerpo:"
	// copyHint is the visible action after a mark command; clicking the line copies it.
	copyHint = "· Copiar marca"
	// copyHintCommand is the visible action after an action bar or macro command.
	copyHintCommand = "· Copiar comando"
	// copyHintMacro is the visible action after a macro body; clicking copies the body.
	copyHintMacro = "· Copiar macro"
	// CopiedNotice is the status message after a successful copy.
	CopiedNotice = "Copiado: pégalo en el chat del juego"
)

// commandLineHeads are the marks that may open a command line, as the assistant writes them.
var commandLineHeads = []string{"📍", "⌨️", "⌨", "🧩"}

// isCommand reports whether cmd is one of the /compa commands the overlay copies.
func isCommand(cmd string) bool {
	return strings.HasPrefix(cmd, markCommandPrefix) || strings.HasPrefix(cmd, barsCommandPrefix) ||
		strings.HasPrefix(cmd, macroCommandPrefix)
}

// commandLine splits a command line into its mark and its command. ok is false for any other line.
func commandLine(line string) (head, cmd string, ok bool) {
	t := strings.TrimSpace(line)
	i := strings.Index(t, "/compa ")
	if i < 0 {
		return "", "", false
	}
	head = strings.TrimSpace(t[:i])
	cmd = t[i:]
	if !containsStr(commandLineHeads, head) {
		return "", "", false
	}
	return head, cmd, isCommand(cmd)
}

// macroBodyLine returns the body of a "🧩 cuerpo: …" line. ok is false for any other line.
func macroBodyLine(line string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), macroBodyHead)
	if !ok {
		return "", false
	}
	body := strings.TrimSpace(rest)
	return body, body != ""
}

// hintFor is the visible action after a command: the mark and the action bar commands say what they copy.
func hintFor(cmd string) string {
	if strings.HasPrefix(cmd, barsCommandPrefix) || strings.HasPrefix(cmd, macroCommandPrefix) {
		return copyHintCommand
	}
	return copyHint
}

// MarkCommands returns the mark, action bar and macro commands of text, in order, without the emoji.
func MarkCommands(text string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if _, cmd, ok := commandLine(line); ok {
			out = append(out, cmd)
		}
	}
	return out
}

// MarkRuns converts an assistant answer into styled runs. Command lines are highlighted and
// clickable (their text is the command plus the copy hint); a macro body is monospace and clickable
// too; other lines use the Markdown subset. Lines are separated by a run whose text is "\n", as in
// Markdown.
func MarkRuns(src string) []Span {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var out []Span
	for i, line := range strings.Split(src, "\n") {
		if i > 0 {
			out = append(out, Span{Text: "\n"})
		}
		if body, ok := macroBodyLine(line); ok {
			out = append(out,
				Span{Text: macroBodyHead + " ", Style: StyleMark},
				Span{Text: body + "  " + copyHintMacro, Style: StyleMark | StyleLink | StyleCode},
			)
			continue
		}
		if head, cmd, ok := commandLine(line); ok {
			out = append(out,
				Span{Text: head + " ", Style: StyleMark},
				Span{Text: cmd + "  " + hintFor(cmd), Style: StyleMark | StyleLink | StyleBold},
			)
			continue
		}
		out = append(out, markdownLine(line)...)
	}
	return out
}

// CommandFromLink returns the command of a clicked command line. ok is false for any other
// link (those open in the browser).
func CommandFromLink(text string) (string, bool) {
	t := strings.TrimSpace(text)
	if !isCommand(t) {
		return "", false
	}
	for _, hint := range []string{copyHint, copyHintCommand} {
		if i := strings.Index(t, hint); i >= 0 {
			t = t[:i]
		}
	}
	cmd := strings.TrimSpace(t)
	return cmd, cmd != markCommandPrefix && cmd != barsCommandPrefix && cmd != macroCommandPrefix && cmd != ""
}

// MacroBodyFromLink returns the body of a clicked macro body, with its line breaks: the literal \n
// of the line becomes a real line break, which is how the macro window takes a body. ok is false
// for any other link.
func MacroBodyFromLink(text string) (string, bool) {
	i := strings.Index(text, copyHintMacro)
	if i < 0 {
		return "", false
	}
	body := strings.TrimSpace(text[:i])
	if body == "" || strings.HasPrefix(body, "http") {
		return "", false
	}
	return strings.ReplaceAll(body, `\n`, "\n"), true
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
