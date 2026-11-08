package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// apiError is the error object returned by the MediaWiki API.
type apiError struct {
	Code string `json:"code"`
	Info string `json:"info"`
}

func (e *apiError) Error() string {
	if e.Info != "" {
		return "wiki: " + e.Info
	}
	return "wiki: " + e.Code
}

// wikiStep is one targeted search of the retry. boost is the extra rank of its hits.
type wikiStep struct {
	run   func(ctx context.Context) ([]Hit, error)
	boost int
}

// wikiRetry lists the targeted searches that run when the plain searches found nothing:
// titles that contain "Forever" and the query, the title prefix "Forever <query>", and the
// pages of Category:Forever that match the query (ranked first).
func (c *Client) wikiRetry(query string) []wikiStep {
	return []wikiStep{
		{run: func(ctx context.Context) ([]Hit, error) { return c.wikiSearchWith(ctx, query+" Forever", "title") }},
		{run: func(ctx context.Context) ([]Hit, error) { return c.wikiSearchWith(ctx, query, "title") }},
		{run: func(ctx context.Context) ([]Hit, error) { return c.wikiPrefixSearch(ctx, "Forever "+query) }},
		{run: func(ctx context.Context) ([]Hit, error) {
			return c.wikiSearchWith(ctx, `incategory:"Forever" `+query, "")
		}, boost: 3},
	}
}

// wikiSearch runs a MediaWiki full-text search on warcraft.wiki.gg.
func (c *Client) wikiSearch(ctx context.Context, q string) ([]Hit, error) {
	return c.wikiSearchWith(ctx, q, "")
}

// wikiSearchWith runs a search on warcraft.wiki.gg. what is "" for full text or "title" to
// match page titles only (srwhat).
func (c *Client) wikiSearchWith(ctx context.Context, q, what string) ([]Hit, error) {
	v := url.Values{}
	v.Set("action", "query")
	v.Set("list", "search")
	v.Set("srsearch", q)
	v.Set("srlimit", "8")
	v.Set("format", "json")
	if what != "" {
		v.Set("srwhat", what)
	}
	body, _, err := c.get(ctx, c.wikiAPI+"?"+v.Encode())
	if err != nil {
		return nil, err
	}
	var resp struct {
		Error *apiError `json:"error"`
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("respuesta del wiki no válida: %w", err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	hits := make([]Hit, 0, len(resp.Query.Search))
	for _, s := range resp.Query.Search {
		if s.Title == "" {
			continue
		}
		hits = append(hits, Hit{
			Title:   s.Title,
			URL:     wikiSiteURL + "/wiki/" + wikiTitlePath(s.Title),
			Snippet: collapse(htmlToText(s.Snippet)),
			Source:  "wiki.gg API",
		})
	}
	return hits, nil
}

// wikiPrefixSearch lists the pages whose titles start with q (list=prefixsearch).
func (c *Client) wikiPrefixSearch(ctx context.Context, q string) ([]Hit, error) {
	v := url.Values{}
	v.Set("action", "query")
	v.Set("list", "prefixsearch")
	v.Set("pssearch", q)
	v.Set("pslimit", "8")
	v.Set("format", "json")
	body, _, err := c.get(ctx, c.wikiAPI+"?"+v.Encode())
	if err != nil {
		return nil, err
	}
	var resp struct {
		Error *apiError `json:"error"`
		Query struct {
			PrefixSearch []struct {
				Title string `json:"title"`
			} `json:"prefixsearch"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("respuesta del wiki no válida: %w", err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	hits := make([]Hit, 0, len(resp.Query.PrefixSearch))
	for _, s := range resp.Query.PrefixSearch {
		if s.Title == "" {
			continue
		}
		hits = append(hits, Hit{
			Title:  s.Title,
			URL:    wikiSiteURL + "/wiki/" + wikiTitlePath(s.Title),
			Source: "wiki.gg API",
		})
	}
	return hits, nil
}

// fetchWiki returns the rendered article text of a warcraft.wiki.gg page.
func (c *Client) fetchWiki(ctx context.Context, u *url.URL) (string, error) {
	title := wikiPageTitle(u)
	if title == "" {
		return "", errors.New("página de wiki no reconocida: se esperaba /wiki/<título>")
	}
	v := url.Values{}
	v.Set("action", "parse")
	v.Set("page", title)
	v.Set("prop", "text")
	v.Set("format", "json")
	body, _, err := c.get(ctx, c.wikiAPI+"?"+v.Encode())
	if err != nil {
		return "", err
	}
	var resp struct {
		Error *apiError `json:"error"`
		Parse struct {
			Title string          `json:"title"`
			Text  json.RawMessage `json:"text"`
		} `json:"parse"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("respuesta del wiki no válida: %w", err)
	}
	if resp.Error != nil {
		return "", resp.Error
	}
	html := rawHTML(resp.Parse.Text)
	if html == "" {
		return "", fmt.Errorf("la página %q no tiene contenido", title)
	}
	name := resp.Parse.Title
	if name == "" {
		name = title
	}
	return "# " + name + "\n\n" + htmlToText(html), nil
}

// rawHTML accepts both the classic {"*": "..."} and the plain string forms.
func rawHTML(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj map[string]string
	if json.Unmarshal(raw, &obj) == nil {
		return obj["*"]
	}
	return ""
}

// wikiPageTitle extracts the article title from /wiki/<title> or index.php?title=<title>.
func wikiPageTitle(u *url.URL) string {
	if t := u.Query().Get("title"); t != "" {
		return t
	}
	const prefix = "/wiki/"
	if !strings.HasPrefix(u.Path, prefix) {
		return ""
	}
	t, err := url.PathUnescape(strings.TrimPrefix(u.Path, prefix))
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(t, "_", " ")
}

func wikiTitlePath(title string) string {
	return url.PathEscape(strings.ReplaceAll(title, " ", "_"))
}
