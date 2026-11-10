package websearch

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// outlineLimit caps the page outline that a long page starts with.
const outlineLimit = 1200

// section is one part of a page: a heading line and the lines under it.
type section struct {
	heading string // "" for the text before the first heading
	text    string // the whole section, heading included
}

// searchTerms turns the words of a question into the terms that select sections.
func searchTerms(buscar string) []string {
	return translateWords(buscar)
}

// selectSections returns text if it fits in limit runes. A longer text is cut into sections by
// its headings and the result is: an outline of the headings, then the sections that mention
// the terms (most mentions first, in page order), or else the sections from the top of the
// page. Each section is cut to the room left, and the answer says when text was left out.
func selectSections(text string, terms []string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	secs := splitSections(text)
	outline := outlineOf(secs)
	note := "[Página larga: lo que sigue es parte del texto. Índice de secciones: " + outline + "]"
	if utf8.RuneCountInString(note) > outlineLimit+80 {
		note = truncateRunes(note, outlineLimit+80)
	}
	budget := limit - utf8.RuneCountInString(note) - 1
	if budget <= 0 {
		return truncateRunes(note, limit)
	}

	chosen := map[int]bool{}
	used := 0
	if len(terms) > 0 {
		type scored struct{ idx, score int }
		var ranked []scored
		for i, s := range secs {
			if n := termScore(s, terms); n > 0 {
				ranked = append(ranked, scored{i, n})
			}
		}
		sort.SliceStable(ranked, func(a, b int) bool { return ranked[a].score > ranked[b].score })
		for _, r := range ranked {
			size := utf8.RuneCountInString(secs[r.idx].text)
			if used+size > budget {
				continue
			}
			chosen[r.idx] = true
			used += size
		}
	}
	// Whatever room is left goes to the sections from the top of the page.
	for i, s := range secs {
		if chosen[i] {
			continue
		}
		size := utf8.RuneCountInString(s.text)
		if used+size > budget {
			break
		}
		chosen[i] = true
		used += size
	}

	var b strings.Builder
	b.WriteString(note)
	b.WriteString("\n")
	for i, s := range secs {
		if !chosen[i] {
			continue
		}
		b.WriteString(s.text)
		b.WriteString("\n")
	}
	out := strings.TrimRight(b.String(), "\n")
	if utf8.RuneCountInString(out) > limit {
		out = truncateRunes(out, limit)
	}
	return out
}

// splitSections cuts text at its heading lines (lines that start with "#").
func splitSections(text string) []section {
	var out []section
	cur := section{}
	var lines []string
	flush := func() {
		if len(lines) == 0 && cur.heading == "" {
			return
		}
		cur.text = strings.Join(lines, "\n")
		out = append(out, cur)
		lines = nil
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "#") {
			flush()
			cur = section{heading: strings.TrimSpace(strings.TrimLeft(line, "#"))}
			lines = []string{line}
			continue
		}
		lines = append(lines, line)
	}
	flush()
	return out
}

// outlineOf lists the headings of the sections, in order, cut to outlineLimit runes.
func outlineOf(secs []section) string {
	var heads []string
	for _, s := range secs {
		if s.heading != "" {
			heads = append(heads, s.heading)
		}
	}
	out := strings.Join(heads, " · ")
	if out == "" {
		return "(sin encabezados)"
	}
	return truncateRunes(out, outlineLimit)
}

// termScore counts the mentions of the terms in a section. A heading mention counts three times.
func termScore(s section, terms []string) int {
	body := foldText(s.text)
	head := foldText(s.heading)
	n := 0
	for _, t := range terms {
		n += strings.Count(body, t)
		if t != "" {
			n += 2 * strings.Count(head, t)
		}
	}
	return n
}
