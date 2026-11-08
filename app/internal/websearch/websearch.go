// Package websearch searches and reads WoW Forever web sources. Only the domains of
// the configured allow-list are ever queried or fetched; results from any other host
// are dropped. Responses are cached in the knowledge store (search 24 h, pages 7 days).
package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/store"
)

const (
	userAgent          = "CompaneroWoW/0.1 (+https://github.com/MTEKode/companeroWoW)"
	requestTimeout     = 15 * time.Second
	maxBodyBytes       = 2 << 20
	maxHits            = 8
	maxRedirects       = 5
	maxPageRunes       = 12000
	searchTTL          = 24 * time.Hour
	pageTTL            = 7 * 24 * time.Hour
	defaultMinInterval = time.Second

	wikiHost    = "warcraft.wiki.gg"
	wikiSiteURL = "https://warcraft.wiki.gg"
	wikiAPIURL  = "https://warcraft.wiki.gg/api.php"
	ddgURL      = "https://html.duckduckgo.com/html/"
)

// Hit is one search result.
type Hit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	// Source names the engine that produced the hit: "wiki.gg API" or "DuckDuckGo".
	Source string `json:"source"`
}

// Client searches and fetches allow-listed web sources. It is safe for concurrent use.
type Client struct {
	allowed     []string
	st          *store.Store
	http        *http.Client
	wikiAPI     string // MediaWiki api.php endpoint (injectable for tests)
	ddgEndpoint string // DuckDuckGo HTML endpoint (injectable for tests)
	minInterval time.Duration

	mu   sync.Mutex
	next map[string]time.Time // earliest next request time per host
}

// New creates a client that only talks to allowedDomains. st may be nil (no cache).
func New(allowedDomains []string, st *store.Store) *Client {
	c := &Client{
		st:          st,
		wikiAPI:     wikiAPIURL,
		ddgEndpoint: ddgURL,
		minInterval: defaultMinInterval,
		next:        make(map[string]time.Time),
	}
	c.http = &http.Client{
		Timeout:       requestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return c.checkRedirect(req, via) },
	}
	for _, d := range allowedDomains {
		if h := cleanEntry(d); h != "" {
			c.allowed = append(c.allowed, h)
		}
	}
	return c
}

// checkRedirect vets every redirect hop. A hop must use http or https and land on an
// allow-listed host. The only exception is a search request: a redirect that stays on the
// host of the search endpoint it started from (DuckDuckGo or the wiki.gg API) is allowed.
// Fetch always starts on an allow-listed host, so the exception never applies to pages.
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return fmt.Errorf("demasiadas redirecciones (máximo %d)", maxRedirects)
	}
	if s := req.URL.Scheme; s != "http" && s != "https" {
		return fmt.Errorf("redirección a esquema no permitido: %s", s)
	}
	host := req.URL.Hostname()
	if _, ok := c.match(host); ok {
		return nil
	}
	if len(via) > 0 && c.isSearchEndpoint(via[0].URL) && normHost(host) == normHost(via[0].URL.Hostname()) {
		return nil
	}
	return fmt.Errorf("redirección a dominio no permitido: %s", host)
}

// isSearchEndpoint reports whether u points at the DuckDuckGo or wiki.gg API endpoint.
func (c *Client) isSearchEndpoint(u *url.URL) bool {
	h := normHost(u.Hostname())
	for _, endpoint := range []string{c.ddgEndpoint, c.wikiAPI} {
		if eu, err := url.Parse(endpoint); err == nil && normHost(eu.Hostname()) == h {
			return true
		}
	}
	return false
}

// Search looks for WoW Forever pages matching query. If site is non-empty it must be
// one of the allowed domains and the search is restricted to it. At most 8 hits are
// returned; hits that mention Forever come first.
func (c *Client) Search(ctx context.Context, query, site string) ([]Hit, error) {
	query = strings.Join(strings.Fields(query), " ")
	if query == "" {
		return nil, errors.New("la consulta está vacía")
	}
	siteEntry := ""
	if site = strings.TrimSpace(site); site != "" {
		e, ok := c.match(site)
		if !ok {
			return nil, fmt.Errorf("sitio no permitido: %s", site)
		}
		siteEntry = e
	}

	key := "search:" + cacheSite(siteEntry) + ":" + query
	if c.st != nil {
		if v, ok, err := c.st.CacheGet(key, searchTTL); err == nil && ok {
			var hits []Hit
			if json.Unmarshal([]byte(v), &hits) == nil {
				return hits, nil
			}
		}
	}

	var (
		hits     []Hit
		errs     []error
		attempts int
		boost    = map[string]int{} // extra rank for hits that a targeted search found
	)
	if c.wantsWiki(siteEntry) {
		// Two queries: the plain one and one biased towards Forever content.
		for _, q := range []string{query, query + " Forever"} {
			attempts++
			h, err := c.wikiSearch(ctx, q)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			hits = append(hits, h...)
		}
		if len(hits) == 0 {
			// Nothing matched in full text: retry once with the targeted searches. Titles and
			// prefixes match names the full-text search misses (the wiki writes titles in
			// English), and pages in Category:Forever are ranked first.
			for _, step := range c.wikiRetry(query) {
				attempts++
				h, err := step.run(ctx)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				if step.boost > 0 {
					for _, hit := range h {
						boost[hit.URL] += step.boost
					}
				}
				hits = append(hits, h...)
			}
		}
	}
	for _, dom := range c.otherDomains(siteEntry) {
		attempts++
		h, err := c.ddgSearch(ctx, query+" Forever", dom)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		hits = append(hits, h...)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if attempts == 0 {
		return nil, errors.New("no hay fuentes web configuradas")
	}
	if len(errs) == attempts {
		return nil, fmt.Errorf("no pude consultar las fuentes web: %w", errors.Join(errs...))
	}

	hits = c.finish(hits, siteEntry, boost)
	if c.st != nil && len(hits) > 0 {
		if b, err := json.Marshal(hits); err == nil {
			_ = c.st.CachePut(key, string(b))
		}
	}
	return hits, nil
}

// Fetch returns the clean text of an allow-listed page. The text is capped at 12000
// runes including the trailing "\n[Fuente: <url>]" line.
func (c *Client) Fetch(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("URL no válida: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("solo se admiten URLs http o https")
	}
	if _, ok := c.match(u.Hostname()); !ok {
		return "", fmt.Errorf("dominio no permitido: %s", u.Hostname())
	}
	source := u.String()
	key := "page:" + source
	if c.st != nil {
		if v, ok, err := c.st.CacheGet(key, pageTTL); err == nil && ok {
			return v, nil
		}
	}

	var text string
	if normHost(u.Hostname()) == wikiHost {
		text, err = c.fetchWiki(ctx, u)
	} else {
		text, err = c.fetchPage(ctx, u)
	}
	if err != nil {
		return "", err
	}
	footer := "\n[Fuente: " + source + "]"
	text = truncateRunes(text, maxPageRunes-len([]rune(footer))) + footer
	if c.st != nil {
		_ = c.st.CachePut(key, text)
	}
	return text, nil
}

// ---- allow-list ------------------------------------------------------------

// cleanEntry turns a configured domain (possibly a URL) into a lower-case host.
func cleanEntry(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	if strings.Contains(d, "://") {
		if u, err := url.Parse(d); err == nil {
			return u.Hostname()
		}
		return ""
	}
	if i := strings.IndexAny(d, "/?#"); i >= 0 {
		d = d[:i]
	}
	return d
}

// normHost lower-cases a host and drops a leading "www.".
func normHost(h string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(h)), "www.")
}

// match returns the configured entry that allows host (exact host or subdomain).
func (c *Client) match(host string) (string, bool) {
	h := normHost(host)
	if h == "" {
		return "", false
	}
	for _, a := range c.allowed {
		e := normHost(a)
		if h == e || strings.HasSuffix(h, "."+e) {
			return a, true
		}
	}
	return "", false
}

func (c *Client) wantsWiki(siteEntry string) bool {
	if siteEntry != "" {
		return normHost(siteEntry) == wikiHost
	}
	_, ok := c.match(wikiHost)
	return ok
}

// otherDomains lists the allowed domains that DuckDuckGo must search (not the wiki).
func (c *Client) otherDomains(siteEntry string) []string {
	if siteEntry != "" {
		if normHost(siteEntry) == wikiHost {
			return nil
		}
		return []string{siteEntry}
	}
	var out []string
	for _, a := range c.allowed {
		if normHost(a) != wikiHost {
			out = append(out, a)
		}
	}
	return out
}

func cacheSite(siteEntry string) string {
	if siteEntry == "" {
		return "*"
	}
	return siteEntry
}

// finish filters hits to allowed hosts (and the requested site), removes duplicates,
// ranks them and caps the list. boost holds extra rank by URL, from the targeted searches.
func (c *Client) finish(hits []Hit, siteEntry string, boost map[string]int) []Hit {
	seen := make(map[string]bool, len(hits))
	kept := make([]Hit, 0, len(hits))
	for _, h := range hits {
		u, err := url.Parse(h.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		if _, ok := c.match(u.Hostname()); !ok {
			continue
		}
		if siteEntry != "" {
			if e, ok := c.match(u.Hostname()); !ok || normHost(e) != normHost(siteEntry) {
				continue
			}
		}
		if seen[h.URL] {
			continue
		}
		seen[h.URL] = true
		kept = append(kept, h)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		return rank(kept[i], boost) > rank(kept[j], boost)
	})
	if len(kept) > maxHits {
		kept = kept[:maxHits]
	}
	return kept
}

// rank orders hits: a title with "Forever" beats a snippet that mentions it, and both beat
// anything else. The boost from a targeted search adds to that.
func rank(h Hit, boost map[string]int) int {
	score := boost[h.URL]
	if strings.Contains(strings.ToLower(h.Title), "forever") {
		score += 4
	}
	if strings.Contains(strings.ToLower(h.Snippet), "forever") {
		score += 2
	}
	return score
}

// ---- HTTP ------------------------------------------------------------------

// wait blocks until the next request to host is allowed (at most one per minInterval).
func (c *Client) wait(ctx context.Context, host string) error {
	if c.minInterval <= 0 {
		return ctx.Err()
	}
	c.mu.Lock()
	now := time.Now()
	slot := now
	if n, ok := c.next[host]; ok && n.After(now) {
		slot = n
	}
	c.next[host] = slot.Add(c.minInterval)
	c.mu.Unlock()

	d := slot.Sub(now)
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// get performs a rate-limited GET and returns the body (capped at 2 MB) and its content type.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("URL no válida: %w", err)
	}
	if err := c.wait(ctx, u.Hostname()); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("preparar petición: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/json;q=0.9,*/*;q=0.8")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("pedir %s: %w", u.Hostname(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s respondió HTTP %d", u.Hostname(), resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, "", fmt.Errorf("leer respuesta de %s: %w", u.Hostname(), err)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// ---- helpers ---------------------------------------------------------------

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
