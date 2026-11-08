package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/store"
)

const footerPrefix = "\n[Fuente: "

var allowed = []string{"warcraft.wiki.gg", "www.icy-veins.com"}

type fakes struct {
	wikiCalls atomic.Int32
	ddgCalls  atomic.Int32
	pageCalls atomic.Int32
	wiki      *httptest.Server
	ddg       *httptest.Server
	site      *httptest.Server
}

// newFakes starts a fake MediaWiki API, a fake DuckDuckGo HTML endpoint and a plain
// site on 127.0.0.1. Nothing leaves the loopback interface.
func newFakes(t *testing.T) *fakes {
	t.Helper()
	f := &fakes{}
	f.wiki = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("action") == "query" && q.Get("list") == "search":
			f.wikiCalls.Add(1)
			// Same results for both the plain and the "Forever" query: exercises de-duplication.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"query":{"search":[
				{"title":"Herbs for the Shamans","snippet":"<span class=\"searchmatch\">Herbs</span> quest in Forever"},
				{"title":"Classic Herbs","snippet":"Classic era quest"}
			]}}`))
		case q.Get("action") == "parse" && q.Get("page") == "Herbs for the Shamans":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"parse":{"title":"Herbs for the Shamans","text":{"*":"<h2>Objetivos</h2><ul><li>Recoger hierbas</li></ul><script>alert(1)</script>"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	f.ddg = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.ddgCalls.Add(1)
		q := r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "text/html")
		if !strings.Contains(q, "site:www.icy-veins.com") {
			_, _ = w.Write([]byte(`<html><body></body></html>`))
			return
		}
		_, _ = w.Write([]byte(`<html><body>
			<div class="result"><h2><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fwww.icy-veins.com%2Fwow%2Fguide&amp;rut=x">Icy Guide</a></h2>
			<a class="result__snippet" href="#">Guía de Forever</a></div>
			<div class="result"><a class="result__a" href="https://evil.example.com/steal">Evil</a>
			<a class="result__snippet" href="#">no permitido</a></div>
			</body></html>`))
	}))
	f.site = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.pageCalls.Add(1)
		switch r.URL.Path {
		case "/clean":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>T</title><style>.x{}</style></head><body>
				<nav>menu principal</nav><header>cabecera</header>
				<main><h1>Título</h1><p>Texto   con
				espacios</p><script>alert('no')</script></main>
				<aside>publicidad</aside><footer>pie de página</footer></body></html>`))
		case "/long":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><body><main><p>" + strings.Repeat("a", 30000) + "</p></main></body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		f.wiki.Close()
		f.ddg.Close()
		f.site.Close()
	})
	return f
}

func newClient(t *testing.T, f *fakes, domains []string, st *store.Store) *Client {
	t.Helper()
	c := New(domains, st)
	c.wikiAPI = f.wiki.URL + "/api.php"
	c.ddgEndpoint = f.ddg.URL + "/html/"
	c.minInterval = 0
	return c
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSearchMergesWikiAndDDGAndFiltersDomains(t *testing.T) {
	f := newFakes(t)
	c := newClient(t, f, allowed, nil)

	hits, err := c.Search(context.Background(), "herbs", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("got %d hits, want 3 (wiki deduplicated + icy-veins, evil dropped): %+v", len(hits), hits)
	}
	if hits[0].Title != "Herbs for the Shamans" {
		t.Errorf("first hit = %q, want the Forever-related one", hits[0].Title)
	}
	if hits[0].URL != "https://warcraft.wiki.gg/wiki/Herbs_for_the_Shamans" {
		t.Errorf("wiki URL = %q", hits[0].URL)
	}
	seen := map[string]bool{}
	for _, h := range hits {
		seen[h.URL] = true
		if strings.Contains(h.URL, "evil.example.com") {
			t.Errorf("non-allowed host leaked: %s", h.URL)
		}
	}
	if !seen["https://www.icy-veins.com/wow/guide"] {
		t.Errorf("DuckDuckGo redirect not unwrapped; hits: %+v", hits)
	}
	if n := f.wikiCalls.Load(); n != 2 {
		t.Errorf("wiki calls = %d, want 2 (plain + Forever query)", n)
	}
}

func TestSearchRejectsSiteOutsideAllowList(t *testing.T) {
	f := newFakes(t)
	c := newClient(t, f, allowed, nil)
	_, err := c.Search(context.Background(), "herbs", "evil.example.com")
	if err == nil || !strings.Contains(err.Error(), "sitio no permitido") {
		t.Fatalf("err = %v, want sitio no permitido", err)
	}
	if f.wikiCalls.Load()+f.ddgCalls.Load() != 0 {
		t.Error("a disallowed site must not trigger any request")
	}
}

func TestSearchSiteRestrictsToThatDomain(t *testing.T) {
	f := newFakes(t)
	c := newClient(t, f, allowed, nil)
	hits, err := c.Search(context.Background(), "guia", "www.icy-veins.com")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].URL != "https://www.icy-veins.com/wow/guide" {
		t.Fatalf("hits = %+v, want only the icy-veins guide", hits)
	}
	if f.wikiCalls.Load() != 0 {
		t.Error("wiki must not be queried when another site is requested")
	}
}

func TestSearchCacheAvoidsSecondRequest(t *testing.T) {
	f := newFakes(t)
	c := newClient(t, f, allowed, openStore(t))

	first, err := c.Search(context.Background(), "herbs", "")
	if err != nil {
		t.Fatalf("first Search: %v", err)
	}
	wikiAfterFirst, ddgAfterFirst := f.wikiCalls.Load(), f.ddgCalls.Load()

	second, err := c.Search(context.Background(), "herbs", "")
	if err != nil {
		t.Fatalf("second Search: %v", err)
	}
	if f.wikiCalls.Load() != wikiAfterFirst || f.ddgCalls.Load() != ddgAfterFirst {
		t.Errorf("cache miss: wiki %d->%d, ddg %d->%d", wikiAfterFirst, f.wikiCalls.Load(), ddgAfterFirst, f.ddgCalls.Load())
	}
	if len(second) != len(first) {
		t.Errorf("cached result differs: %d vs %d hits", len(second), len(first))
	}
}

func TestFetchRejectsDisallowedDomainAndSchemes(t *testing.T) {
	c := New(allowed, nil)
	for _, raw := range []string{"https://evil.example.com/a", "file:///etc/passwd", "ftp://warcraft.wiki.gg/x"} {
		if _, err := c.Fetch(context.Background(), raw); err == nil {
			t.Errorf("Fetch(%q) succeeded, want an error", raw)
		}
	}
	_, err := c.Fetch(context.Background(), "https://evil.example.com/a")
	if err == nil || !strings.Contains(err.Error(), "dominio no permitido") {
		t.Fatalf("err = %v, want dominio no permitido", err)
	}
}

func TestFetchWikiPageUsesParseAPIAndStripsScripts(t *testing.T) {
	f := newFakes(t)
	c := newClient(t, f, allowed, nil)
	page := "https://warcraft.wiki.gg/wiki/Herbs_for_the_Shamans"
	text, err := c.Fetch(context.Background(), page)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(text, "Recoger hierbas") || !strings.Contains(text, "## Objetivos") {
		t.Errorf("wiki text missing content:\n%s", text)
	}
	if strings.Contains(text, "alert") {
		t.Errorf("script leaked into text:\n%s", text)
	}
	if !strings.HasSuffix(text, footerPrefix+page+"]") {
		t.Errorf("missing source footer, got end: %q", tail(text, 80))
	}
}

func TestFetchGenericPageKeepsMainText(t *testing.T) {
	f := newFakes(t)
	c := newClient(t, f, []string{"127.0.0.1"}, nil)
	page := f.site.URL + "/clean"
	text, err := c.Fetch(context.Background(), page)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for _, bad := range []string{"menu principal", "cabecera", "publicidad", "pie de página", "alert", ".x{}"} {
		if strings.Contains(text, bad) {
			t.Errorf("text still contains %q:\n%s", bad, text)
		}
	}
	if !strings.Contains(text, "# Título") || !strings.Contains(text, "Texto con espacios") {
		t.Errorf("main text missing or not collapsed:\n%s", text)
	}
	if !strings.HasSuffix(text, footerPrefix+page+"]") {
		t.Errorf("missing footer: %q", tail(text, 80))
	}
}

func TestFetchCapsTextAt12000RunesIncludingFooter(t *testing.T) {
	f := newFakes(t)
	c := newClient(t, f, []string{"127.0.0.1"}, nil)
	page := f.site.URL + "/long"
	text, err := c.Fetch(context.Background(), page)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if n := len([]rune(text)); n > maxPageRunes {
		t.Errorf("text has %d runes, want at most %d", n, maxPageRunes)
	}
	if !strings.HasSuffix(text, footerPrefix+page+"]") {
		t.Errorf("footer lost after truncation: %q", tail(text, 80))
	}
}

func TestFetchCacheAvoidsSecondRequest(t *testing.T) {
	f := newFakes(t)
	c := newClient(t, f, []string{"127.0.0.1"}, openStore(t))
	page := f.site.URL + "/clean"
	if _, err := c.Fetch(context.Background(), page); err != nil {
		t.Fatal(err)
	}
	before := f.pageCalls.Load()
	if _, err := c.Fetch(context.Background(), page); err != nil {
		t.Fatal(err)
	}
	if f.pageCalls.Load() != before {
		t.Error("second fetch hit the network despite the cache")
	}
}

func TestHTMLToTextStripsScriptsAndKeepsStructure(t *testing.T) {
	in := `<html><head><title>x</title></head><body>
		<script>var a = "alert(1)";</script><style>p{color:red}</style>
		<h2>Título</h2>
		<ul><li>Uno</li><li>Dos   extra</li></ul>
		<table><tr><td>A</td><td>B</td></tr></table>
		<p>Hola<br>mundo</p>
		</body></html>`
	got := htmlToText(in)
	for _, bad := range []string{"alert", "color", "var a"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q leaked: %q", bad, got)
		}
	}
	for _, want := range []string{"## Título", "- Uno", "- Dos extra", "A | B", "Hola\nmundo"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestUnwrapDDG(t *testing.T) {
	cases := map[string]string{
		"//duckduckgo.com/l/?uddg=https%3A%2F%2Fa.example%2Fx&rut=1": "https://a.example/x",
		"https://www.icy-veins.com/wow":                              "https://www.icy-veins.com/wow",
		"javascript:alert(1)":                                        "",
		"/relative/path":                                             "",
	}
	for in, want := range cases {
		if got := unwrapDDG(in); got != want {
			t.Errorf("unwrapDDG(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRequestsToOneHostAreSpaced(t *testing.T) {
	f := newFakes(t)
	c := newClient(t, f, []string{"127.0.0.1"}, nil)
	c.minInterval = 60 * time.Millisecond
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := c.Fetch(context.Background(), f.site.URL+"/clean?n="+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 110*time.Millisecond {
		t.Errorf("3 requests took %v, want at least ~120ms with 60ms spacing", elapsed)
	}
}

func TestSearchRejectsEmptyQuery(t *testing.T) {
	c := New(allowed, nil)
	if _, err := c.Search(context.Background(), "   ", ""); err == nil {
		t.Fatal("empty query accepted")
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
