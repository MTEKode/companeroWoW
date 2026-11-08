package websearch

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// ddgSearch runs one DuckDuckGo HTML query restricted to domain (site: operator).
func (c *Client) ddgSearch(ctx context.Context, q, domain string) ([]Hit, error) {
	v := url.Values{}
	v.Set("q", q+" site:"+domain)
	body, _, err := c.get(ctx, c.ddgEndpoint+"?"+v.Encode())
	if err != nil {
		return nil, err
	}
	hits, err := parseDDG(body)
	if err != nil {
		return nil, fmt.Errorf("leer resultados de DuckDuckGo: %w", err)
	}
	return hits, nil
}

// parseDDG extracts results from the DuckDuckGo HTML page. Each result has an
// anchor with class "result__a" (title and redirect link) followed by an anchor with
// class "result__snippet".
func parseDDG(body []byte) ([]Hit, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	var hits []Hit
	cur := -1 // index of the hit that a following snippet belongs to
	var visit func(n *html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch {
			case hasClass(n, "result__a"):
				cur = -1
				if link := unwrapDDG(attr(n, "href")); link != "" {
					hits = append(hits, Hit{Title: textOf(n), URL: link, Source: "DuckDuckGo"})
					cur = len(hits) - 1
				}
			case hasClass(n, "result__snippet"):
				if cur >= 0 {
					hits[cur].Snippet = textOf(n)
					cur = -1
				}
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			visit(ch)
		}
	}
	visit(doc)
	return hits, nil
}

// unwrapDDG returns the target of a DuckDuckGo redirect link (uddg parameter), or the
// link itself when it is already absolute. Anything else yields "".
func unwrapDDG(href string) string {
	h := strings.TrimSpace(href)
	if strings.HasPrefix(h, "//") {
		h = "https:" + h
	}
	u, err := url.Parse(h)
	if err != nil {
		return ""
	}
	if strings.HasSuffix(u.Hostname(), "duckduckgo.com") && strings.HasPrefix(u.Path, "/l/") {
		if target := u.Query().Get("uddg"); target != "" {
			h = target
			if u2, err := url.Parse(h); err == nil {
				u = u2
			} else {
				return ""
			}
		}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	for _, f := range strings.Fields(attr(n, "class")) {
		if f == class {
			return true
		}
	}
	return false
}

// textOf returns the collapsed text content of n.
func textOf(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			sb.WriteString(x.Data)
			sb.WriteByte(' ')
		}
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return collapse(sb.String())
}
