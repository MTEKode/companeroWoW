package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// wikiRecorder answers the MediaWiki API from a table and records every request.
type wikiRecorder struct {
	mu    sync.Mutex
	calls []string // "list|srwhat|srsearch" or "prefix|pssearch"
}

func (w *wikiRecorder) count(prefix string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, c := range w.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// newRetryWiki serves the search answers given by answer, keyed by what and query.
func newRetryWiki(t *testing.T, answer func(list, what, q string) string) (*httptest.Server, *wikiRecorder) {
	t.Helper()
	rec := &wikiRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rw.Header().Set("Content-Type", "application/json")
		switch {
		case q.Get("action") == "query" && q.Get("list") == "search":
			key := "search|" + q.Get("srwhat") + "|" + q.Get("srsearch")
			rec.mu.Lock()
			rec.calls = append(rec.calls, key)
			rec.mu.Unlock()
			_, _ = rw.Write([]byte(answer("search", q.Get("srwhat"), q.Get("srsearch"))))
		case q.Get("action") == "query" && q.Get("list") == "prefixsearch":
			key := "prefix|" + q.Get("pssearch")
			rec.mu.Lock()
			rec.calls = append(rec.calls, key)
			rec.mu.Unlock()
			_, _ = rw.Write([]byte(answer("prefix", "", q.Get("pssearch"))))
		default:
			http.NotFound(rw, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func clientWithWiki(t *testing.T, wiki *httptest.Server) *Client {
	t.Helper()
	f := newFakes(t)
	c := newClient(t, f, allowed, nil)
	c.wikiAPI = wiki.URL + "/api.php"
	return c
}

func TestSearchRetriesWithTitlesAndPrefixesWhenFullTextIsEmpty(t *testing.T) {
	wiki, rec := newRetryWiki(t, func(list, what, q string) string {
		switch {
		case list == "search" && what == "title" && q == "mounts":
			return `{"query":{"search":[{"title":"Forever mounts","snippet":""}]}}`
		case list == "prefix" && q == "Forever first mount":
			return `{"query":{"prefixsearch":[{"title":"Forever first mount"}]}}`
		}
		return `{"query":{"search":[]}}`
	})
	c := clientWithWiki(t, wiki)

	hits, err := c.Search(context.Background(), "first mount", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if rec.count("search||") < 2 {
		t.Errorf("full-text searches = %d, want the plain and the Forever one", rec.count("search||"))
	}
	if rec.count("search|title|") == 0 {
		t.Errorf("title search was not used as a retry: %v", rec.calls)
	}
	if rec.count("prefix|") == 0 {
		t.Errorf("prefix search was not used as a retry: %v", rec.calls)
	}
	found := map[string]bool{}
	for _, h := range hits {
		found[h.URL] = true
	}
	if !found["https://warcraft.wiki.gg/wiki/Forever_first_mount"] {
		t.Errorf("prefix hit missing: %+v", hits)
	}
}

func TestSearchDoesNotRetryWhenFullTextFindsSomething(t *testing.T) {
	wiki, rec := newRetryWiki(t, func(list, what, q string) string {
		if list == "search" && what == "" {
			return `{"query":{"search":[{"title":"Herbs for the Shamans","snippet":"Forever"}]}}`
		}
		return `{"query":{"search":[]}}`
	})
	c := clientWithWiki(t, wiki)
	if _, err := c.Search(context.Background(), "herbs", ""); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if n := rec.count("search|title|") + rec.count("prefix|"); n != 0 {
		t.Errorf("retry ran although full text had hits: %v", rec.calls)
	}
}

func TestSearchRetryRunsOnceAndReturnsEmptyWithoutError(t *testing.T) {
	wiki, rec := newRetryWiki(t, func(list, what, q string) string {
		return `{"query":{"search":[]}}`
	})
	c := clientWithWiki(t, wiki)
	hits, err := c.Search(context.Background(), "nada de nada", "warcraft.wiki.gg")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %+v, want none", hits)
	}
	// Two full-text queries plus one round of the four retry searches, nothing more.
	if n := len(rec.calls); n != 6 {
		t.Errorf("wiki requests = %d (%v), want 6", n, rec.calls)
	}
}

func TestPrefixSearchParsesTitles(t *testing.T) {
	wiki, _ := newRetryWiki(t, func(list, what, q string) string {
		return `{"query":{"prefixsearch":[{"title":"Forever mounts"},{"title":""}]}}`
	})
	c := clientWithWiki(t, wiki)
	hits, err := c.wikiPrefixSearch(context.Background(), "Forever mounts")
	if err != nil {
		t.Fatalf("wikiPrefixSearch: %v", err)
	}
	if len(hits) != 1 || hits[0].URL != "https://warcraft.wiki.gg/wiki/Forever_mounts" {
		t.Errorf("hits = %+v", hits)
	}
}

func TestRankPutsForeverTitlesAndTargetedHitsFirst(t *testing.T) {
	c := New(allowed, nil)
	in := []Hit{
		{Title: "Classic herbs", URL: "https://warcraft.wiki.gg/wiki/Classic_herbs"},
		{Title: "Guide", Snippet: "a Forever guide", URL: "https://warcraft.wiki.gg/wiki/Guide"},
		{Title: "Forever mounts", URL: "https://warcraft.wiki.gg/wiki/Forever_mounts"},
		{Title: "Riding skill", URL: "https://warcraft.wiki.gg/wiki/Riding_skill"},
	}
	got := c.finish(in, "", map[string]int{"https://warcraft.wiki.gg/wiki/Riding_skill": 3})
	var order []string
	for _, h := range got {
		order = append(order, h.Title)
	}
	want := []string{"Forever mounts", "Riding skill", "Guide", "Classic herbs"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Errorf("order = %v, want %v", order, want)
	}
}
