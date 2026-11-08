package ui

import "strings"

// SpanStyle is the formatting of one piece of text in the conversation.
type SpanStyle uint16

const (
	StyleBold SpanStyle = 1 << iota
	StyleCode
	StyleLink
	StyleGold
	StyleMeta
	StyleTip
	StyleError
)

// Span is a piece of text with one style. A link run holds its URL as the text.
type Span struct {
	Text  string
	Style SpanStyle
}

// Markdown converts the Markdown subset the assistant uses into styled runs. Nothing is
// interpreted as HTML: the text is only split into runs. Supported:
//   - **bold** (an unmatched ** stays literal)
//   - `code` spans
//   - "- " or "* " bullet lines (shown as "• ")
//   - "#" headings (shown bold and gold)
//   - [label](https://...) links, shown as "label (https://...)"
//   - bare http and https URLs
//
// Lines are separated by a run whose text is "\n".
func Markdown(src string) []Span {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var out []Span
	for i, line := range strings.Split(src, "\n") {
		if i > 0 {
			out = append(out, Span{Text: "\n"})
		}
		out = append(out, markdownLine(line)...)
	}
	return out
}

func markdownLine(line string) []Span {
	t := strings.TrimLeft(line, " \t")
	switch {
	case strings.HasPrefix(t, "- "), strings.HasPrefix(t, "* "):
		return append([]Span{{Text: "• "}}, inline(t[2:], 0)...)
	case strings.HasPrefix(t, "#"):
		return inline(strings.TrimLeft(t, "# \t"), StyleGold|StyleBold)
	}
	return inline(line, 0)
}

// inline splits one line into runs. base is added to every run.
func inline(s string, base SpanStyle) []Span {
	var out []Span
	var buf strings.Builder
	bold := false
	style := func() SpanStyle {
		if bold {
			return base | StyleBold
		}
		return base
	}
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, Span{Text: buf.String(), Style: style()})
			buf.Reset()
		}
	}
	for i := 0; i < len(s); {
		rest := s[i:]
		switch {
		case strings.HasPrefix(rest, "**") && (bold || strings.Contains(rest[2:], "**")):
			flush()
			bold = !bold
			i += 2
		case rest[0] == '`' && strings.IndexByte(rest[1:], '`') >= 0:
			end := strings.IndexByte(rest[1:], '`')
			flush()
			out = append(out, Span{Text: rest[1 : 1+end], Style: style() | StyleCode})
			i += end + 2
		case rest[0] == '[':
			if label, url, n, ok := parseLink(rest); ok {
				flush()
				out = append(out,
					Span{Text: label, Style: style()},
					Span{Text: " (", Style: style()},
					Span{Text: url, Style: base | StyleLink},
					Span{Text: ")", Style: style()},
				)
				i += n
				continue
			}
			buf.WriteByte('[')
			i++
		case bareURLLen(rest) > 0:
			n := bareURLLen(rest)
			flush()
			out = append(out, Span{Text: rest[:n], Style: base | StyleLink})
			i += n
		default:
			buf.WriteByte(rest[0])
			i++
		}
	}
	flush()
	return out
}

// parseLink reads "[label](url)" at the start of s. n is the number of bytes consumed.
func parseLink(s string) (label, url string, n int, ok bool) {
	closeBracket := strings.IndexByte(s, ']')
	if closeBracket < 1 || closeBracket+1 >= len(s) || s[closeBracket+1] != '(' {
		return "", "", 0, false
	}
	end := strings.IndexByte(s[closeBracket+2:], ')')
	if end < 0 {
		return "", "", 0, false
	}
	u := strings.TrimSpace(s[closeBracket+2 : closeBracket+2+end])
	if !isHTTPURL(u) || strings.ContainsAny(u, " \t") {
		return "", "", 0, false
	}
	return s[1:closeBracket], u, closeBracket + 2 + end + 1, true
}

// bareURLLen returns the length of a http or https URL at the start of s, without
// trailing punctuation, or 0 when s does not start with one.
func bareURLLen(s string) int {
	if !isHTTPURL(s) {
		return 0
	}
	end := len(s)
	if i := strings.IndexAny(s, " \t\r\n<>\"'"); i >= 0 {
		end = i
	}
	u := s[:end]
	for len(u) > 0 {
		last := u[len(u)-1]
		if strings.IndexByte(".,;:!?", last) >= 0 {
			u = u[:len(u)-1]
			continue
		}
		if last == ')' && strings.Count(u, ")") > strings.Count(u, "(") {
			u = u[:len(u)-1]
			continue
		}
		break
	}
	if !isHTTPURL(u) || len(u) <= len("https://") {
		return 0
	}
	return len(u)
}

// isHTTPURL reports whether s starts with http:// or https://, ignoring case.
func isHTTPURL(s string) bool {
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}
