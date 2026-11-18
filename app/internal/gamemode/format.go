// Package gamemode answers the questions typed in the in-game frame. A question arrives on the
// v3 channel (msgType 15); the answer is formatted into the CWOW1 block the addon reads, put on
// the clipboard (the player pastes it into the frame with Ctrl+V) and announced with a sound.
// The package is platform-neutral; the clipboard and the sound are interfaces, and their
// Windows implementations live in sound_windows.go and the ui package.
package gamemode

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxBlockRunes is the longest CWOW1 block the addon accepts; a longer answer is trimmed.
const MaxBlockRunes = 4000

// trimmedNote ends an answer that was trimmed to fit MaxBlockRunes.
const trimmedNote = "(respuesta recortada)"

// blockOpen and blockClose frame the answer; the addon ignores pasted text without them.
const (
	blockOpen  = "CWOW1"
	blockClose = "[/CWOW1]"
)

// defaultIcon is the macro icon when the answer names none.
const defaultIcon = "inv_misc_questionmark"

// Answer is what FormatAnswer needs: the question it answers and the assistant's text.
type Answer struct {
	QuestionID uint64
	// Provider is the provider that answered ("antigravity:<model>", "codex"...); it is shown in
	// the block header, so it must not carry brackets or line breaks.
	Provider string
	// MapID is the map of the question. A mark command without its map id takes it from here.
	MapID int
	Text  string
}

// FormatAnswer turns an assistant answer into the CWOW1 block: the header with the question id,
// the body in the addon's markup (action lines, sources, bold, bullets, escaped pipes, no
// emoji) and the closing line. The whole block is at most MaxBlockRunes characters.
func FormatAnswer(a Answer) string {
	return frame(a.QuestionID, a.Provider, ConvertBody(a.Text, a.MapID))
}

// FormatError is the block for an answer that could not be given: the same frame, with a
// short Spanish message as the body.
func FormatError(questionID uint64, msg string) string {
	return frame(questionID, "app/error", Escape(StripEmoji(msg)))
}

// frame wraps body in the header and the closing line, and trims it to the limit.
func frame(questionID uint64, provider, body string) string {
	head := "[" + blockOpen + " #" + strconv.FormatUint(questionID, 10) + " " + headerProvider(provider) + "]"
	body = strings.Trim(body, "\n")
	if body == "" {
		return head + "\n" + blockClose
	}
	block := head + "\n" + body + "\n" + blockClose
	if utf8.RuneCountInString(block) <= MaxBlockRunes {
		return block
	}
	room := MaxBlockRunes - utf8.RuneCountInString(head+"\n\n"+blockClose+"\n"+trimmedNote)
	cut := dropOddPipe(strings.TrimRight(trimRunes(body, room), " \n"))
	if cut != "" {
		cut += "\n"
	}
	return head + "\n" + cut + trimmedNote + "\n" + blockClose
}

// headerProvider keeps the provider's name short and free of the characters the header uses.
func headerProvider(p string) string {
	p = strings.NewReplacer(":", "/", "[", "", "]", "", "\n", " ", "\r", " ").Replace(strings.TrimSpace(p))
	p = strings.TrimSpace(p)
	if p == "" {
		return "app"
	}
	return p
}

// trimRunes keeps at most n runes of s.
func trimRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// dropOddPipe removes one pipe when the text ends with an odd run of them: a cut between the
// two halves of an escaped "||" would leave a lone pipe, which the addon reads as a WoW escape.
func dropOddPipe(s string) string {
	n := len(s) - len(strings.TrimRight(s, "|"))
	if n%2 == 1 {
		return s[:len(s)-1]
	}
	return s
}

// ConvertBody converts the assistant's text into the body of a CWOW1 block. Command lines become
// action lines (@marca, @barras, @macro); a "Fuente:" line becomes @fuente; bullets become "- ";
// emoji are removed; every pipe is escaped as "||". mapID fills a mark command that has none.
func ConvertBody(text string, mapID int) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []string
	blank := false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, convertLine(t, mapID))
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// convertLine converts one non-empty line.
func convertLine(t string, mapID int) string {
	if line, ok := convertCommand(t, mapID); ok {
		return line
	}
	if src, ok := sourceLine(t); ok {
		return "@fuente " + Escape(StripEmoji(src))
	}
	t = StripEmoji(t)
	if m := bulletRe.FindStringSubmatch(t); m != nil {
		return "- " + Escape(strings.TrimSpace(m[1]))
	}
	return Escape(strings.TrimSpace(t))
}

// Escape writes every literal pipe as "||", the WoW escape for "|".
func Escape(s string) string {
	return strings.ReplaceAll(s, "|", "||")
}

var (
	// bulletRe matches a bullet written as "• x", "* x" or "◦ x" (not the bold marker "**").
	bulletRe = regexp.MustCompile(`^(?:[•◦▪●]|\*\s)\s*(.*)$`)
	// sourceRe matches "Fuente: x" and "Fuentes: x", with optional bold markers around the label.
	sourceRe = regexp.MustCompile(`(?i)^\**\s*fuentes?\s*\**\s*:\s*\**\s*(.+)$`)
	// intRe and numRe tell the map id and the coordinates of a mark apart.
	intRe = regexp.MustCompile(`^\d+$`)
	numRe = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

// sourceLine reports whether t is a source line and returns its text.
func sourceLine(t string) (string, bool) {
	m := sourceRe.FindStringSubmatch(t)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(strings.TrimRight(m[1], "*")), true
}

// convertCommand converts a line that holds a /compa command: the mark, the action bar and the
// macro commands become action lines. Other commands are not converted (ok is false).
func convertCommand(t string, mapID int) (string, bool) {
	i := strings.Index(t, "/compa ")
	if i < 0 {
		return "", false
	}
	cmd := strings.TrimSpace(t[i+len("/compa "):])
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "", false
	}
	switch fields[0] {
	case "marca":
		return markLine(fields[1:], mapID)
	case "barras":
		if len(fields) >= 3 && fields[1] == "aplicar" {
			return "@barras " + strings.Join(fields[2:], " "), true
		}
	case "macro":
		if len(fields) >= 2 && fields[1] == "crear" {
			rest := strings.TrimSpace(strings.TrimPrefix(cmd, "macro"))
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "crear"))
			return macroLine(rest), true
		}
	}
	return "", false
}

// markLine builds "@marca <mapID> <x> <y> <texto>" from the words after "marca". The map id is
// optional: when the first three words are numbers and the first is a whole number, it is the map
// id; otherwise the question's map id is used. Without any map id the mark is written as text.
func markLine(args []string, mapID int) (string, bool) {
	var id, x, y string
	var rest []string
	switch {
	case len(args) >= 4 && intRe.MatchString(args[0]) && numRe.MatchString(args[1]) && numRe.MatchString(args[2]):
		id, x, y, rest = args[0], args[1], args[2], args[3:]
	case len(args) >= 3 && numRe.MatchString(args[0]) && numRe.MatchString(args[1]):
		x, y, rest = args[0], args[1], args[2:]
		if mapID > 0 {
			id = strconv.Itoa(mapID)
		}
	default:
		return "", false
	}
	text := Escape(StripEmoji(strings.Join(rest, " ")))
	if id == "" {
		return "Marca: " + text + " (" + x + ", " + y + ")", true
	}
	return "@marca " + id + " " + x + " " + y + " " + text, true
}

// macroLine builds "@macro <nombre>|<icono>|<cuerpo>". The command's parts are "nombre|icono|cuerpo"
// when it has pipes; otherwise the first word is the name and the rest is the body. Newlines in the
// body are written as \n. The pipes that separate the parts are not escaped; those in the text are.
func macroLine(rest string) string {
	var name, icon, body string
	if strings.Contains(rest, "|") {
		parts := strings.SplitN(rest, "|", 3)
		name = strings.TrimSpace(parts[0])
		if len(parts) > 1 {
			icon = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			body = parts[2]
		}
	} else {
		words := strings.Fields(rest)
		if len(words) > 0 {
			name = words[0]
			body = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), name))
		}
	}
	if icon == "" {
		icon = defaultIcon
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", `\n`)
	return "@macro " + Escape(StripEmoji(name)) + "|" + Escape(StripEmoji(icon)) + "|" + Escape(StripEmoji(body))
}

// StripEmoji removes the emoji of s: the pictographs, the symbols that WoW's fonts lack, and
// the joiners and selectors that go with them. Runs of spaces left behind collapse to one.
func StripEmoji(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isEmoji(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// isEmoji reports whether r is an emoji or a part of one.
func isEmoji(r rune) bool {
	switch {
	case r == 0xFE0F, r == 0x200D, r == 0x20E3: // variation selector, zero-width joiner, keycap
		return true
	case r >= 0x1F000 && r <= 0x1FAFF: // pictographs, emoticons, transport, symbols
		return true
	case r >= 0x2300 && r <= 0x23FF: // ⌨ and the other technical symbols
		return true
	case r >= 0x2600 && r <= 0x27BF: // misc symbols and dingbats (⚠ ✅ ✨ ❌)
		return true
	case r >= 0x2B00 && r <= 0x2BFF: // ⭐ ⬆
		return true
	}
	return false
}
