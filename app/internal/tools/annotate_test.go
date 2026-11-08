package tools

import (
	"encoding/json"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

func TestAnnotateHitsAddsSpanishNamesAndKeepsTheInput(t *testing.T) {
	in := []websearch.Hit{{
		Title:   "Stormwind City",
		URL:     "https://warcraft.wiki.gg/wiki/Stormwind_City",
		Snippet: "Mount trainers are in Stormwind and Ironforge.",
		Source:  "wiki.gg API",
	}}
	out := annotateHits(in)
	if len(out) != 1 {
		t.Fatalf("hits = %d", len(out))
	}
	if out[0].Title != "Stormwind City (Ventormenta)" {
		t.Errorf("title = %q", out[0].Title)
	}
	if out[0].Snippet != "Mount trainers are in Stormwind (Ventormenta) and Ironforge (Forjaz)." {
		t.Errorf("snippet = %q", out[0].Snippet)
	}
	if out[0].URL != in[0].URL || out[0].Source != in[0].Source {
		t.Errorf("non-text fields changed: %+v", out[0])
	}
	if in[0].Title != "Stormwind City" {
		t.Error("the input slice must not be modified")
	}
}

func TestAnnotateHitsNeverReturnsNil(t *testing.T) {
	if out := annotateHits(nil); out == nil || len(out) != 0 {
		t.Errorf("nil input must give an empty slice, got %#v", out)
	}
	data, err := json.Marshal(annotateHits(nil))
	if err != nil || string(data) != "[]" {
		t.Errorf("empty result must encode as [] (got %s, %v)", data, err)
	}
}
