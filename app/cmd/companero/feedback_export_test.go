package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/store"
)

func TestBuildFeedbackExportCounts(t *testing.T) {
	now := time.Unix(1793880000, 0)
	items := []store.Feedback{
		{ID: 1, Rating: 1, Question: "q1", Answer: "a1", Provider: "codex"},
		{ID: 2, Rating: -1, Reason: "vago", Question: "q2", Answer: "a2", Provider: "api"},
		{ID: 3, Rating: 1, Question: "q3", Answer: "a3", Provider: "codex"},
	}
	exp := buildFeedbackExport(items, now, "9.9.9")
	if exp.Count != 3 || exp.Up != 2 || exp.Down != 1 {
		t.Errorf("counts = %d/%d/%d, want 3/2/1", exp.Count, exp.Up, exp.Down)
	}
	if exp.Version != "9.9.9" || !exp.ExportedAt.Equal(now) {
		t.Errorf("header = %+v", exp)
	}
	data, err := json.Marshal(exp)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"exportedAt", "version", "count", "up", "down", "items"} {
		if _, ok := back[key]; !ok {
			t.Errorf("export lacks %q", key)
		}
	}
}

func TestBuildFeedbackExportEmptyIsArray(t *testing.T) {
	data, err := json.Marshal(buildFeedbackExport(nil, time.Now(), "x"))
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Items []any `json:"items"`
	}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Items == nil {
		t.Error("items should be [] when empty, not null")
	}
}
