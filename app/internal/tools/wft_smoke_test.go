package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/store"
	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

// siteCounter counts the requests that go to wowforevertalents.com through the default transport.
type siteCounter struct {
	n    *atomic.Int64
	next http.RoundTripper
}

func (c siteCounter) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Hostname(), "wowforevertalents.com") {
		c.n.Add(1)
	}
	return c.next.RoundTrip(r)
}

// TestWFTLiveSmoke runs the site tools against the real site, reading the store named by
// WFT_LIVE_DB (pages already cached there cost no request). It is skipped unless WFT_LIVE=1.
// Each output is saved to WFT_LIVE_OUT (one file per tool) and the requests to the site are printed.
func TestWFTLiveSmoke(t *testing.T) {
	if os.Getenv("WFT_LIVE") != "1" {
		t.Skip("set WFT_LIVE=1 to run the site tools against the real site")
	}
	st, err := store.Open(os.Getenv("WFT_LIVE_DB"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var gets atomic.Int64
	orig := http.DefaultTransport
	http.DefaultTransport = siteCounter{n: &gets, next: orig}
	defer func() { http.DefaultTransport = orig }()

	ws := websearch.New([]string{"wowforevertalents.com", "warcraft.wiki.gg"}, st)
	reg := Build(nil, ws)
	out := os.Getenv("WFT_LIVE_OUT")
	ctx := context.Background()

	calls := []struct{ name, args string }{
		{"hechizos_clase", `{"clase":"Sacerdote","nivel":10}`},
		{"hechizos_clase", `{"clase":"priest"}`},
		{"builds_clase", `{"clase":"priest","rol":"Disciplina"}`},
		{"builds_clase", `{"clase":"priest","url":"https://wowforevertalents.com/priest/builds/discipline-19-18-0-6a17f2/"}`},
		{"leer_pagina", `{"url":"https://wowforevertalents.com/dungeons/ahn-qiraj-temple/","buscar":"bosses"}`},
		{"buscar_web", `{"consulta":"talentos Mind Blast sacerdote"}`},
	}
	for i, c := range calls {
		res, err := reg.Call(ctx, c.name, json.RawMessage(c.args))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		text := wftText(t, res)
		t.Logf("%s %s: %d chars, error=%v", c.name, c.args, len(text), res.IsError)
		if out != "" {
			file := filepath.Join(out, fmt.Sprintf("%02d-%s.txt", i, c.name))
			_ = os.WriteFile(file, []byte(text), 0o644)
		}
	}

	// The dungeon and profession enrichments are internal helpers; they run on the same site cache.
	d, err := wftDungeon(ctx, ws, "Ahn'Qiraj Temple")
	if err != nil {
		t.Errorf("info_mazmorra (wowforever): %v", err)
	} else if out != "" {
		b, _ := json.MarshalIndent(d, "", "  ")
		_ = os.WriteFile(filepath.Join(out, "info_mazmorra-wowforever.txt"), b, 0o644)
	}
	p, err := wftRecipes(ctx, ws, "Sastrería", 0)
	if err != nil {
		t.Errorf("ruta_profesion (wowforever): %v", err)
	} else if out != "" {
		b, _ := json.MarshalIndent(p, "", "  ")
		_ = os.WriteFile(filepath.Join(out, "ruta_profesion-wowforever.txt"), b, 0o644)
	}
	t.Logf("requests to wowforevertalents.com during the smoke run: %d", gets.Load())
}
