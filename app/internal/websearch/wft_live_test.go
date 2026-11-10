package websearch

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// countingTransport counts the requests that really leave the process.
type countingTransport struct {
	n    *atomic.Int64
	next http.RoundTripper
}

func (t countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.n.Add(1)
	return t.next.RoundTrip(r)
}

// TestWFTLiveFetch talks to the real wowforevertalents.com. It is skipped unless WFT_LIVE=1,
// so that the normal test run never touches the network. WFT_LIVE_DB is the store the calls
// cache into (reused across runs, so a page is downloaded once), WFT_LIVE_OUT is the folder
// where each page's HTML is saved for the fixtures, and WFT_LIVE_URLS lists the pages to read
// (one per line, on the site). The number of requests made is printed at the end.
func TestWFTLiveFetch(t *testing.T) {
	if os.Getenv("WFT_LIVE") != "1" {
		t.Skip("set WFT_LIVE=1 to read the real site")
	}
	dbPath := os.Getenv("WFT_LIVE_DB")
	if dbPath == "" {
		t.Fatal("WFT_LIVE_DB is required")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	c := New([]string{wftHost}, st)
	var gets atomic.Int64
	c.http.Transport = countingTransport{n: &gets, next: http.DefaultTransport}
	ctx := context.Background()

	if os.Getenv("WFT_LIVE_STEP") == "index" {
		if _, err := c.wftRobots(ctx); err != nil {
			t.Fatalf("robots: %v", err)
		}
		if err := c.wftEnsureSitemap(ctx); err != nil {
			t.Fatalf("sitemap: %v", err)
		}
		c.metaSet("wft.patch_at", "0") // re-read the cached front page (no request)
		patch, err := c.WFTPatch(ctx)
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		if out := os.Getenv("WFT_LIVE_OUT"); out != "" {
			_ = os.WriteFile(filepath.Join(out, "robots.txt"), []byte(c.metaGet("wft.robots")), 0o644)
		}
		entries, _ := st.WFTSitemapAll()
		if out := os.Getenv("WFT_LIVE_OUT"); out != "" {
			var b strings.Builder
			for _, e := range entries {
				b.WriteString(strings.Join([]string{e.URL, e.Section, e.Class, e.Title}, "\t") + "\n")
			}
			_ = os.WriteFile(filepath.Join(out, "sitemap.tsv"), []byte(b.String()), 0o644)
		}
		t.Logf("patch=%q sitemap=%d urls, GETs=%d", patch, len(entries), gets.Load())
		return
	}

	out := os.Getenv("WFT_LIVE_OUT")
	for _, u := range strings.Split(strings.TrimSpace(os.Getenv("WFT_LIVE_URLS")), "\n") {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		html, err := c.WFTPage(ctx, u)
		if err != nil {
			t.Errorf("%s: %v", u, err)
			continue
		}
		if out != "" {
			name := strings.Trim(strings.ReplaceAll(strings.TrimPrefix(u, wftRootURL), "/", "_"), "_") + ".html"
			if err := os.WriteFile(filepath.Join(out, name), []byte(html), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("%s: %d bytes", u, len(html))
	}
	t.Logf("GETs made: %d", gets.Load())
}
