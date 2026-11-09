package ui

import (
	"testing"
	"time"
)

func TestRateableAndLastRateable(t *testing.T) {
	entries := []entry{
		{Kind: entryUser, Text: "¿Dónde?"},
		{Kind: entryAssistant, Text: "En Ventormenta.", Provider: "codex"},
		{Kind: entryTip, Text: "consejo"},
	}
	if got := lastRateable(entries); got != 1 {
		t.Errorf("lastRateable = %d, want 1 (a tip after the answer does not move the buttons)", got)
	}
	entries = append(entries, entry{Kind: entryUser, Text: "otra"})
	if got := lastRateable(entries); got != -1 {
		t.Errorf("lastRateable after a new question = %d, want -1 until it is answered", got)
	}
	entries = append(entries, entry{Kind: entryError, Text: "No pude responder."})
	if got := lastRateable(entries); got != -1 {
		t.Errorf("error answer should have no buttons, got %d", got)
	}
	if rateable(entry{Kind: entryAssistant, Text: "   "}) {
		t.Error("an empty answer is rateable")
	}
}

func TestQuestionFor(t *testing.T) {
	entries := []entry{
		{Kind: entryUser, Text: "primera"},
		{Kind: entryAssistant, Text: "a1"},
		{Kind: entryUser, Text: "segunda"},
		{Kind: entryAssistant, Text: "a2"},
	}
	if got := questionFor(entries, 3); got != "segunda" {
		t.Errorf("questionFor(3) = %q, want segunda", got)
	}
	if got := questionFor(entries, 1); got != "primera" {
		t.Errorf("questionFor(1) = %q, want primera", got)
	}
	if got := questionFor(entries, 0); got != "" {
		t.Errorf("questionFor(0) = %q, want empty", got)
	}
}

func TestNewFeedbackFields(t *testing.T) {
	now := time.Unix(1793880000, 0)
	f := newFeedback(ratingDown, "q", "a", "api", now)
	if f.Rating != -1 || f.Question != "q" || f.Answer != "a" || f.Provider != "api" || !f.CreatedAt.Equal(now) {
		t.Errorf("newFeedback = %+v", f)
	}
	if f.SkillVersion == "" {
		t.Error("skill version is empty")
	}
	if f.ToolCalls != nil {
		t.Errorf("tool calls should stay unknown (nil), got %v", f.ToolCalls)
	}
}
