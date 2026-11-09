package store

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFeedbackRoundTrip(t *testing.T) {
	s := openTemp(t)
	at := time.Unix(1793880000, 123)
	id, err := s.AddFeedback(Feedback{
		CreatedAt:    at,
		Rating:       -1,
		Reason:       "  no era la zona correcta  ",
		Question:     "¿Dónde está el entrenador?",
		Answer:       "En Ventormenta.",
		Provider:     "codex",
		SkillVersion: "abc123",
		ToolCalls:    []string{"get_location", "search_npc"},
	})
	if err != nil {
		t.Fatalf("AddFeedback: %v", err)
	}
	if id == 0 {
		t.Fatal("AddFeedback returned id 0")
	}
	if _, err := s.AddFeedback(Feedback{Rating: 1, Question: "q2", Answer: "a2", Provider: "api"}); err != nil {
		t.Fatalf("AddFeedback without tool calls: %v", err)
	}
	list, err := s.ListFeedback()
	if err != nil {
		t.Fatalf("ListFeedback: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d rows, want 2", len(list))
	}
	first := list[0]
	if first.ID != id || first.Rating != -1 || first.Reason != "no era la zona correcta" {
		t.Errorf("first row = %+v", first)
	}
	if !first.CreatedAt.Equal(at) {
		t.Errorf("CreatedAt = %v, want %v", first.CreatedAt, at)
	}
	if !reflect.DeepEqual(first.ToolCalls, []string{"get_location", "search_npc"}) {
		t.Errorf("ToolCalls = %v", first.ToolCalls)
	}
	if list[1].ToolCalls == nil || len(list[1].ToolCalls) != 0 {
		t.Errorf("unknown tool calls must be an empty list, got %#v", list[1].ToolCalls)
	}
}

func TestFeedbackRejectsOtherRatings(t *testing.T) {
	s := openTemp(t)
	for _, r := range []int{0, 2, -2} {
		if _, err := s.AddFeedback(Feedback{Rating: r}); err == nil {
			t.Errorf("rating %d accepted, want error", r)
		}
	}
}

func TestSetFeedbackReason(t *testing.T) {
	s := openTemp(t)
	id, err := s.AddFeedback(Feedback{Rating: -1, Question: "q", Answer: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFeedbackReason(id, "respuesta vaga"); err != nil {
		t.Fatalf("SetFeedbackReason: %v", err)
	}
	list, _ := s.ListFeedback()
	if len(list) != 1 || list[0].Reason != "respuesta vaga" {
		t.Errorf("reason not stored: %+v", list)
	}
	if err := s.SetFeedbackReason(id+100, "x"); err == nil {
		t.Error("SetFeedbackReason on a missing id should fail")
	}
}

func TestFeedbackSummaryCountsAndMarkdown(t *testing.T) {
	s := openTemp(t)
	if md, err := s.FeedbackSummaryMarkdown(); err != nil || md != "" {
		t.Fatalf("empty store: md=%q err=%v, want empty section", md, err)
	}
	add := func(rating int, provider, reason string) {
		t.Helper()
		if _, err := s.AddFeedback(Feedback{Rating: rating, Provider: provider, Reason: reason}); err != nil {
			t.Fatal(err)
		}
	}
	add(1, "codex", "")
	add(1, "codex", "")
	add(-1, "codex", "muy largo")
	add(-1, "antigravity", "")
	add(1, "", "")

	up, down, by, err := s.FeedbackSummary()
	if err != nil {
		t.Fatal(err)
	}
	if up != 3 || down != 2 {
		t.Errorf("up=%d down=%d, want 3 and 2", up, down)
	}
	want := []FeedbackCount{
		{Provider: "antigravity", Up: 0, Down: 1},
		{Provider: "codex", Up: 2, Down: 1},
		{Provider: "desconocido", Up: 1, Down: 0},
	}
	if !reflect.DeepEqual(by, want) {
		t.Errorf("by provider = %+v, want %+v", by, want)
	}

	md, err := s.FeedbackSummaryMarkdown()
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"## Feedback de usuarios", "Total: 3", "| codex | 2 | 1 |", "- muy largo"} {
		if !strings.Contains(md, part) {
			t.Errorf("markdown lacks %q:\n%s", part, md)
		}
	}
}

// The feedback table is created by migration 3 and survives reopening the database.
func TestFeedbackMigrationKeepsDataOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "companero.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFeedback(Feedback{Rating: 1, Question: "q", Answer: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	list, err := s.ListFeedback()
	if err != nil || len(list) != 1 {
		t.Fatalf("after reopen: %d rows, err %v", len(list), err)
	}
}
