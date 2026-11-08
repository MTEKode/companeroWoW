package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// The DuckDuckGo part of a search is one request for all allowed domains, not one per domain.
func TestDDGSearchIsOneQueryWithSiteOperators(t *testing.T) {
	var calls atomic.Int32
	var seen string
	ddg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		seen = r.URL.Query().Get("q")
		_, _ = w.Write([]byte(`<html></html>`))
	}))
	t.Cleanup(ddg.Close)

	c := New([]string{"icy-veins.com", "wowhead.com"}, nil)
	c.ddgEndpoint = ddg.URL + "/html/"
	c.minInterval = 0
	_, _ = c.Search(context.Background(), "herbs", "")

	if n := calls.Load(); n != 1 {
		t.Fatalf("DuckDuckGo requests = %d, want 1", n)
	}
	if !strings.Contains(seen, "(site:icy-veins.com OR site:wowhead.com)") {
		t.Errorf("query = %q, want the OR of the allowed sites", seen)
	}
}
