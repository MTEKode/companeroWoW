package websearch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// skipTags are dropped together with everything inside them.
var skipTags = map[string]bool{
	"script": true, "style": true, "nav": true, "header": true, "footer": true,
	"aside": true, "noscript": true, "iframe": true, "svg": true, "template": true,
	"head": true, "button": true, "select": true, "input": true, "textarea": true,
	"object": true, "canvas": true,
}

// blockTags start and end a line.
var blockTags = map[string]bool{
	"address": true, "article": true, "blockquote": true, "caption": true, "center": true,
	"dd": true, "details": true, "div": true, "dl": true, "dt": true, "figcaption": true,
	"figure": true, "hr": true, "main": true, "ol": true, "p": true, "pre": true,
	"section": true, "summary": true, "table": true, "tbody": true, "tfoot": true,
	"thead": true, "ul": true, "body": true, "html": true, "form": true,
}

// fetchPage returns the main text of a non-wiki HTML page.
func (c *Client) fetchPage(ctx context.Context, u *url.URL) (string, error) {
	body, ctype, err := c.get(ctx, u.String())
	if err != nil {
		return "", err
	}
	ctype = strings.ToLower(ctype)
	if strings.HasPrefix(ctype, "text/plain") {
		return strings.TrimSpace(string(body)), nil
	}
	if ctype != "" && !strings.Contains(ctype, "html") && !strings.Contains(ctype, "xml") {
		return "", fmt.Errorf("tipo de contenido no soportado: %s", ctype)
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("leer HTML de %s: %w", u.Hostname(), err)
	}
	text := renderText(mainNode(doc))
	if text == "" {
		return "", errors.New("la página no tiene texto legible")
	}
	return text, nil
}

// mainNode picks <main>, then <article>, then <body>, then the whole document.
func mainNode(doc *html.Node) *html.Node {
	for _, tag := range []string{"main", "article", "body"} {
		if n := findElement(doc, tag); n != nil {
			return n
		}
	}
	return doc
}

// htmlToText converts an HTML fragment or document to clean text.
func htmlToText(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return collapse(s)
	}
	return renderText(doc)
}

// renderText converts a node tree to lines: headings become "#" lines, list items
// "- " lines, table cells are separated by " | ", and whitespace is collapsed.
func renderText(n *html.Node) string {
	var b textBuilder
	b.walk(n)
	return cleanLines(b.sb.String())
}

type textBuilder struct {
	sb       strings.Builder
	cellOpen bool // a cell was already written in the current table row
}

func (b *textBuilder) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.text(n.Data)
		return
	case html.DocumentNode:
		b.children(n)
		return
	case html.ElementNode:
	default:
		return
	}
	tag := n.Data
	if skipTags[tag] {
		return
	}
	block := false
	switch {
	case tag == "br":
		b.newline()
		return
	case headingLevel(tag) > 0:
		level := headingLevel(tag)
		if level > 3 {
			level = 3
		}
		b.newline()
		b.write(strings.Repeat("#", level) + " ")
		block = true
	case tag == "li":
		b.newline()
		b.write("- ")
		block = true
	case tag == "tr":
		b.newline()
		b.cellOpen = false
		block = true
	case tag == "td" || tag == "th":
		if b.cellOpen {
			b.write(" | ")
		}
		b.cellOpen = true
	case blockTags[tag]:
		b.newline()
		block = true
	}
	b.children(n)
	if block {
		b.newline()
	}
}

func (b *textBuilder) children(n *html.Node) {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		b.walk(ch)
	}
}

func (b *textBuilder) write(s string) { b.sb.WriteString(s) }

func (b *textBuilder) newline() {
	s := b.sb.String()
	if len(s) == 0 || s[len(s)-1] == '\n' {
		return
	}
	b.sb.WriteByte('\n')
}

// text writes s with its whitespace collapsed, keeping one space at word boundaries.
func (b *textBuilder) text(s string) {
	if s == "" {
		return
	}
	atBoundary := b.atSpaceOrLineStart()
	words := strings.Fields(s)
	if len(words) == 0 {
		if !atBoundary {
			b.write(" ")
		}
		return
	}
	first, _ := utf8.DecodeRuneInString(s)
	last, _ := utf8.DecodeLastRuneInString(s)
	if unicode.IsSpace(first) && !atBoundary {
		b.write(" ")
	}
	b.write(strings.Join(words, " "))
	if unicode.IsSpace(last) {
		b.write(" ")
	}
}

func (b *textBuilder) atSpaceOrLineStart() bool {
	s := b.sb.String()
	if s == "" {
		return true
	}
	c := s[len(s)-1]
	return c == ' ' || c == '\n' || c == '\t'
}

// cleanLines trims every line, drops empty bullets and headings, and keeps at most
// one blank line in a row.
func cleanLines(s string) string {
	var out []string
	blank := true
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			if !blank {
				out = append(out, "")
				blank = true
			}
			continue
		}
		if t == "-" || strings.Trim(t, "#") == "" {
			continue
		}
		out = append(out, t)
		blank = false
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func headingLevel(tag string) int {
	if len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6' {
		return int(tag[1] - '0')
	}
	return 0
}

// collapse joins the words of s with single spaces.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// findElement returns the first element named tag in document order, or nil.
func findElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if f := findElement(ch, tag); f != nil {
			return f
		}
	}
	return nil
}
