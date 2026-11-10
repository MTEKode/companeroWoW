package websearch

// wowforevertalents.com is an Astro site with server-rendered HTML. Its talents, spells,
// builds, dungeons and profession pages are generated from the WoW: Forever client, so
// they are the first source for class, spell and dungeon questions. This file holds the
// polite fetch path for it: robots.txt (never /api/), one request per second per host,
// pages cached for three days and invalidated when the patch on the front page changes,
// conditional GETs when the server gives validators, and a sitemap index downloaded at
// most once a day.

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/store"
	"golang.org/x/net/html"
)

const (
	wftHost         = "wowforevertalents.com"
	wftRootURL      = "https://" + wftHost
	wftPageTTL      = 3 * 24 * time.Hour
	wftSitemapTTL   = 24 * time.Hour
	wftPatchTTL     = 6 * time.Hour
	wftRobotsTTL    = 24 * time.Hour
	wftMaxBody      = 4 << 20
	wftUnknownPatch = "desconocido"
	// wftRetryAfter delays a new try after a failed sitemap or robots download, so that a
	// broken site or a missing network does not turn every search into a request.
	wftRetryAfter = time.Hour
)

// wftClasses are the class slugs of the site.
var wftClasses = map[string]bool{
	"warrior": true, "paladin": true, "hunter": true, "rogue": true, "priest": true,
	"shaman": true, "mage": true, "warlock": true, "druid": true,
}

// wftRobotsAgent is the product token that robots.txt groups are matched against.
const wftRobotsAgent = "companerowow"

// ---- robots.txt --------------------------------------------------------------

type robotRule struct {
	allow bool
	path  string
}

// robotsRules are the Allow/Disallow rules that apply to this app.
type robotsRules struct {
	rules []robotRule
}

// parseRobots reads robots.txt. The group of our own product token wins over the "*" group,
// as the standard says; with neither, everything is allowed.
func parseRobots(body string) *robotsRules {
	type group struct {
		agents []string
		rules  []robotRule
	}
	var groups []*group
	var cur *group
	lastWasAgent := false
	for _, raw := range strings.Split(body, "\n") {
		line := raw
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "user-agent":
			if cur == nil || !lastWasAgent {
				cur = &group{}
				groups = append(groups, cur)
			}
			cur.agents = append(cur.agents, strings.ToLower(val))
			lastWasAgent = true
			continue
		case "allow", "disallow":
			if cur != nil {
				// An empty Disallow allows everything; keep it as an empty-path rule.
				cur.rules = append(cur.rules, robotRule{allow: key == "allow", path: val})
			}
		}
		lastWasAgent = false
	}
	var specific, generic []robotRule
	hasSpecific := false
	for _, g := range groups {
		for _, a := range g.agents {
			if a == wftRobotsAgent {
				specific = append(specific, g.rules...)
				hasSpecific = true
			} else if a == "*" {
				generic = append(generic, g.rules...)
			}
		}
	}
	if hasSpecific {
		return &robotsRules{rules: specific}
	}
	return &robotsRules{rules: generic}
}

// allowed reports whether path may be fetched: the longest matching rule wins, and Allow
// wins a tie. A path that no rule matches is allowed.
func (r *robotsRules) allowed(path string) bool {
	if r == nil {
		return true
	}
	best := -1
	ok := true
	for _, rule := range r.rules {
		if rule.path == "" {
			continue
		}
		if !strings.HasPrefix(path, rule.path) {
			continue
		}
		n := len(rule.path)
		if n > best || (n == best && rule.allow) {
			best = n
			ok = rule.allow
		}
	}
	return ok
}

// ---- small metadata helpers (store when present, memory otherwise) -------------

func (c *Client) metaGet(key string) string {
	if c.st != nil {
		v, err := c.st.WFTMetaGet(key)
		if err == nil {
			return v
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wftMem[key]
}

func (c *Client) metaSet(key, value string) {
	if c.st != nil && c.st.WFTMetaSet(key, value) == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wftMem == nil {
		c.wftMem = map[string]string{}
	}
	c.wftMem[key] = value
}

// metaTime reads a timestamp stored as Unix milliseconds.
func (c *Client) metaTime(key string) time.Time {
	var ms int64
	if _, err := fmt.Sscan(c.metaGet(key), &ms); err != nil || ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func (c *Client) metaSetTime(key string, t time.Time) {
	c.metaSet(key, fmt.Sprint(t.UnixMilli()))
}

// now is the clock of the site cache; tests replace it through wftNow.
func (c *Client) now() time.Time {
	if c.wftNow != nil {
		return c.wftNow()
	}
	return time.Now()
}

// ---- host and path rules ------------------------------------------------------

// wftHostname is the host the site is served from (the configured root, which tests redirect).
func (c *Client) wftHostname() string {
	if u, err := url.Parse(c.wftRoot); err == nil {
		return normHost(u.Hostname())
	}
	return wftHost
}

// wftEnabled reports whether the allow-list includes wowforevertalents.com.
func (c *Client) wftEnabled() bool {
	_, ok := c.match(c.wftHostname())
	return ok
}

// isWFT reports whether u is on the site.
func (c *Client) isWFT(u *url.URL) bool {
	return normHost(u.Hostname()) == c.wftHostname()
}

// wftCheckPath refuses the API and any path that the site's robots.txt disallows.
func (c *Client) wftCheckPath(ctx context.Context, p string) error {
	if p == "/api" || strings.HasPrefix(p, "/api/") {
		return errors.New("wowforevertalents.com no permite /api/: no se consulta")
	}
	rules, err := c.wftRobots(ctx)
	if err != nil {
		return err
	}
	if !rules.allowed(p) {
		return fmt.Errorf("robots.txt de wowforevertalents.com no permite %s", p)
	}
	return nil
}

// wftRobots returns the parsed robots.txt. A copy is kept for a day; a failed download is not
// retried for an hour, and until a download succeeds the site is refused (a missing robots.txt,
// 404 or 410, allows everything).
func (c *Client) wftRobots(ctx context.Context) (*robotsRules, error) {
	at := c.metaTime("wft.robots_at")
	if !at.IsZero() && c.now().Sub(at) < wftRobotsTTL {
		return parseRobots(c.metaGet("wft.robots")), nil
	}
	if c.retryWait("wft.robots_try") {
		if !at.IsZero() {
			return parseRobots(c.metaGet("wft.robots")), nil
		}
		return nil, errors.New("robots.txt de wowforevertalents.com no disponible: se reintenta en una hora")
	}
	status, body, _, err := c.wftGet(ctx, c.wftRoot+"/robots.txt", nil)
	switch {
	case err == nil:
		c.metaSet("wft.robots", string(body))
		c.metaSetTime("wft.robots_at", c.now())
		c.retryClear("wft.robots_try")
		return parseRobots(string(body)), nil
	case status == http.StatusNotFound || status == http.StatusGone:
		c.metaSet("wft.robots", "")
		c.metaSetTime("wft.robots_at", c.now())
		c.retryClear("wft.robots_try")
		return &robotsRules{}, nil
	default:
		c.retryLater("wft.robots_try")
		if !at.IsZero() {
			return parseRobots(c.metaGet("wft.robots")), nil
		}
		return nil, fmt.Errorf("no pude leer robots.txt de wowforevertalents.com: %w", err)
	}
}

// retryWait reports whether a failed download must still wait before the next try.
func (c *Client) retryWait(key string) bool {
	t := c.metaTime(key)
	return !t.IsZero() && c.now().Before(t)
}

// retryLater marks a failed download: nothing is tried again for wftRetryAfter.
func (c *Client) retryLater(key string) {
	c.metaSetTime(key, c.now().Add(wftRetryAfter))
}

// retryClear forgets a failed download, after a success.
func (c *Client) retryClear(key string) {
	c.metaSet(key, "")
}

// wftGet is a rate-limited GET for the site. It sends the validators when given, returns
// the status and the body of a 200 or 304 response, and refuses a redirect into /api/.
// The returned status is 0 when the request did not get a response.
func (c *Client) wftGet(ctx context.Context, rawURL string, hdr map[string]string) (int, []byte, http.Header, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("URL no válida: %w", err)
	}
	if err := c.wait(ctx, u.Hostname()); err != nil {
		return 0, nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("preparar petición: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xml;q=0.9,*/*;q=0.8")
	for k, v := range hdr {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("pedir %s: %w", u.Hostname(), err)
	}
	defer resp.Body.Close()
	if p := resp.Request.URL.Path; p == "/api" || strings.HasPrefix(p, "/api/") {
		return resp.StatusCode, nil, nil, errors.New("redirección a /api/ refusada")
	}
	if resp.StatusCode == http.StatusNotModified {
		return resp.StatusCode, nil, resp.Header, nil
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil, nil, fmt.Errorf("%s respondió HTTP %d", u.Hostname(), resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, wftMaxBody))
	if err != nil {
		return resp.StatusCode, nil, nil, fmt.Errorf("leer respuesta de %s: %w", u.Hostname(), err)
	}
	return resp.StatusCode, body, resp.Header, nil
}

// ---- patch and page cache -----------------------------------------------------

// patchRe finds the date of the latest patch notes on the front page ("Patch notes 9 Nov 2026"),
// whatever the separator between the words.
var patchRe = regexp.MustCompile(`(?i)patch notes[\s:·–-]*([0-9]{1,2}\s+[A-Za-z]{3,9}\.?(?:\s+[0-9]{4})?|[0-9]{4}-[0-9]{2}-[0-9]{2}|[A-Za-z]{3,9}\.?\s+[0-9]{1,2},?(?:\s+[0-9]{4})?)`)

// extractPatch returns the patch-notes date of a front page, or "" when it has none.
func extractPatch(html string) string {
	text := plainText(html)
	if m := patchRe.FindStringSubmatch(text); m != nil {
		return strings.Join(strings.Fields(m[1]), " ")
	}
	return ""
}

// plainText returns every text node of an HTML document in order, header and navigation
// included (unlike htmlToText), but without the contents of scripts and styles.
func plainText(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "template") {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	return strings.Join(strings.Fields(b.String()), " ")
}

// wftPatch returns the current patch, reading the front page at most every six hours. A
// patch that differs from the last one invalidates every cached page of the site.
func (c *Client) wftPatch(ctx context.Context) (string, error) {
	at := c.metaTime("wft.patch_at")
	if !at.IsZero() && c.now().Sub(at) < wftPatchTTL {
		if p := c.metaGet("wft.patch"); p != "" {
			return p, nil
		}
	}
	root := c.wftRoot + "/"
	if err := c.wftCheckPath(ctx, "/"); err != nil {
		return "", err
	}
	page, err := c.wftFetchPage(ctx, root, wftPatchTTL, c.metaGet("wft.patch"), false)
	if err != nil {
		if p := c.metaGet("wft.patch"); p != "" {
			return p, nil
		}
		return "", err
	}
	patch := extractPatch(page)
	if patch == "" {
		patch = wftUnknownPatch
	}
	c.metaSet("wft.patch", patch)
	c.metaSetTime("wft.patch_at", c.now())
	return patch, nil
}

// wftFetchPage returns the raw HTML of a site page, from the store when it is fresh enough.
// When the cached copy was read under another patch it is not reused: the page is fetched
// again without validators. A stale copy under the same patch is revalidated with its ETag
// or Last-Modified, and a 304 keeps it. The returned body is the page HTML.
func (c *Client) wftFetchPage(ctx context.Context, rawURL string, ttl time.Duration, patch string, checkPatch bool) (string, error) {
	cur, have, err := c.storePage(rawURL)
	if err != nil {
		return "", err
	}
	samePatch := have && (!checkPatch || cur.Patch == patch)
	if have && samePatch && c.now().Sub(cur.FetchedAt) < ttl {
		return cur.Body, nil
	}
	hdr := map[string]string{}
	if have && samePatch {
		hdr["If-None-Match"] = cur.ETag
		hdr["If-Modified-Since"] = cur.LastModified
	}
	status, body, h, err := c.wftGet(ctx, rawURL, hdr)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotModified && have {
		c.storeTouch(rawURL, patch)
		return cur.Body, nil
	}
	page := storePage{URL: rawURL, Body: string(body), Patch: patch, FetchedAt: c.now()}
	if h != nil {
		page.ETag = h.Get("ETag")
		page.LastModified = h.Get("Last-Modified")
	}
	c.storePut(page)
	return page.Body, nil
}

// storePage mirrors store.WFTPage without importing it into every call site.
type storePage = store.WFTPage

func (c *Client) storePage(rawURL string) (storePage, bool, error) {
	if c.st == nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		p, ok := c.wftPages[rawURL]
		return p, ok, nil
	}
	return c.st.WFTPageGet(rawURL)
}

func (c *Client) storePut(p storePage) {
	if c.st != nil {
		_ = c.st.WFTPagePut(p)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wftPages == nil {
		c.wftPages = map[string]storePage{}
	}
	c.wftPages[p.URL] = p
}

func (c *Client) storeTouch(rawURL, patch string) {
	if c.st != nil {
		_ = c.st.WFTPageTouch(rawURL, patch)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.wftPages[rawURL]; ok {
		p.Patch = patch
		p.FetchedAt = c.now()
		c.wftPages[rawURL] = p
	}
}

// WFTPage returns the HTML of a page of wowforevertalents.com, using the cache, the patch
// check and the politeness rules. rawURL must be on the site.
func (c *Client) WFTPage(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !c.isWFT(u) {
		return "", errors.New("la página no es de wowforevertalents.com")
	}
	if err := c.wftCheckPath(ctx, u.Path); err != nil {
		return "", err
	}
	patch, err := c.wftPatch(ctx)
	if err != nil {
		return "", err
	}
	return c.wftFetchPage(ctx, u.String(), wftPageTTL, patch, true)
}

// WFTPatch returns the patch the cached site pages belong to ("" when unknown).
func (c *Client) WFTPatch(ctx context.Context) (string, error) {
	return c.wftPatch(ctx)
}

// WFTURL is one sitemap entry of the site (see store.WFTURL).
type WFTURL = store.WFTURL

// ---- sitemap ------------------------------------------------------------------

type sitemapDoc struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

// wftEnsureSitemap downloads the sitemap when the index is older than a day. A failed download
// keeps the old index and is not retried for wftRetryAfter.
func (c *Client) wftEnsureSitemap(ctx context.Context) error {
	at := c.metaTime("wft.sitemap_at")
	if !at.IsZero() && c.now().Sub(at) < wftSitemapTTL {
		return nil
	}
	if c.retryWait("wft.sitemap_try") {
		return errors.New("el mapa del sitio se reintenta más tarde")
	}
	if err := c.wftCheckPath(ctx, "/sitemap-0.xml"); err != nil {
		return err
	}
	_, body, _, err := c.wftGet(ctx, c.wftRoot+"/sitemap-0.xml", nil)
	if err != nil {
		c.retryLater("wft.sitemap_try")
		return err
	}
	entries, err := parseSitemap(body, c.wftHostname())
	if err != nil {
		c.retryLater("wft.sitemap_try")
		return err
	}
	if c.st != nil {
		if err := c.st.WFTSitemapReplace(entries); err != nil {
			return err
		}
	} else {
		c.mu.Lock()
		c.wftIndex = entries
		c.mu.Unlock()
	}
	c.metaSetTime("wft.sitemap_at", c.now())
	c.retryClear("wft.sitemap_try")
	return nil
}

// parseSitemap reads a sitemap and derives the title and section of each page from its path.
// Only URLs on host are kept.
func parseSitemap(body []byte, host string) ([]WFTURL, error) {
	var doc sitemapDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("leer el mapa del sitio: %w", err)
	}
	var out []WFTURL
	seen := map[string]bool{}
	for _, e := range doc.URLs {
		u, err := url.Parse(strings.TrimSpace(e.Loc))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || normHost(u.Hostname()) != normHost(host) {
			continue
		}
		if seen[u.Path] {
			continue
		}
		seen[u.Path] = true
		out = append(out, describeSitePath(u.String(), u.Path))
	}
	return out, nil
}

// describeSitePath derives the slug, title, section and class of a site page from its path.
// "/priest/talents/mind-blast/" → class priest, section talents, title "Priest · Talents · Mind Blast".
func describeSitePath(rawURL, p string) WFTURL {
	var segs []string
	for _, s := range strings.Split(strings.Trim(p, "/"), "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	e := WFTURL{URL: rawURL, Slug: strings.Join(segs, "/")}
	section := "pagina"
	switch {
	case len(segs) == 0:
		section = "inicio"
	case segs[0] == "abilities" && len(segs) > 1:
		section = "abilities"
		e.Class = segs[1]
	case wftClasses[segs[0]]:
		e.Class = segs[0]
		section = "clase"
		if len(segs) > 1 {
			section = segs[1]
		}
	default:
		section = segs[0]
	}
	e.Section = section
	var parts []string
	for _, s := range segs {
		parts = append(parts, humanSlug(s))
	}
	e.Title = strings.Join(parts, " · ")
	if e.Title == "" {
		e.Title = "WoW: Forever"
	}
	return e
}

// humanSlug turns "mind-blast" into "Mind Blast".
func humanSlug(s string) string {
	words := strings.Fields(strings.ReplaceAll(s, "-", " "))
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// wftIndex returns the sitemap entries, from the store or the memory copy.
func (c *Client) wftIndexEntries() []WFTURL {
	if c.st != nil {
		entries, err := c.st.WFTSitemapAll()
		if err == nil {
			return entries
		}
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]WFTURL(nil), c.wftIndex...)
}

// wftLocal returns the sitemap pages whose path and title match the query. Spanish terms
// are translated to English first, because the site's paths are in English. A page
// matches when at least half of the query's words are in its path, and the best matches
// come first. It never returns more than five hits.
func (c *Client) wftLocal(ctx context.Context, query string) []Hit {
	if !c.wftEnabled() {
		return nil
	}
	if err := c.wftEnsureSitemap(ctx); err != nil && ctx.Err() != nil {
		return nil
	}
	words := translateWords(query)
	if len(words) == 0 {
		return nil
	}
	need := (len(words) + 1) / 2
	type scored struct {
		e     WFTURL
		score int
	}
	var best []scored
	for _, e := range c.wftIndexEntries() {
		have := map[string]bool{}
		for _, w := range strings.FieldsFunc(e.Slug, isSlugSep) {
			have[w] = true
		}
		n := 0
		for _, w := range words {
			if have[w] {
				n++
			}
		}
		if n >= need {
			best = append(best, scored{e, n})
		}
	}
	// Highest score first, then the shorter (more general) path.
	for i := 1; i < len(best); i++ {
		for j := i; j > 0 && (best[j].score > best[j-1].score ||
			(best[j].score == best[j-1].score && len(best[j].e.Slug) < len(best[j-1].e.Slug))); j-- {
			best[j], best[j-1] = best[j-1], best[j]
		}
	}
	var hits []Hit
	for _, s := range best {
		if len(hits) == 5 {
			break
		}
		hits = append(hits, Hit{
			Title:   s.e.Title,
			URL:     s.e.URL,
			Snippet: "Página de wowforevertalents.com (índice local del sitio)",
			Source:  "Índice de WoW Forever",
		})
	}
	return hits
}

func isSlugSep(r rune) bool { return r == '/' || r == '-' || r == ' ' }

// ---- WoW Forever pages as text ------------------------------------------------------

// WFTText returns the readable text of a site page (full, not capped), for the section
// selection of leer_pagina.
func (c *Client) WFTText(ctx context.Context, rawURL string) (string, error) {
	html, err := c.WFTPage(ctx, rawURL)
	if err != nil {
		return "", err
	}
	return htmlToText(html), nil
}
