package ui

import (
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/skill"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// FeedbackSaver keeps the 👍/👎 ratings. *store.Store implements it.
type FeedbackSaver interface {
	AddFeedback(f store.Feedback) (int64, error)
	SetFeedbackReason(id int64, reason string) error
}

// Ratings as stored.
const (
	ratingUp   = 1
	ratingDown = -1
)

// reasonPlaceholder is the hint shown in the one-line reason box after a 👎.
const reasonPlaceholder = "¿Qué falló? (opcional, Enter para guardar)"

// rateable reports whether the feedback buttons belong under the entry: only real answers
// from the assistant, not errors, tips or notes.
func rateable(e entry) bool {
	return e.Kind == entryAssistant && strings.TrimSpace(e.Text) != ""
}

// lastRateable returns the index of the newest entry the buttons belong to, or -1.
func lastRateable(entries []entry) int {
	for i := len(entries) - 1; i >= 0; i-- {
		if rateable(entries[i]) {
			return i
		}
		if entries[i].Kind == entryUser {
			// The newest answer is older than the last question: it has no buttons.
			return -1
		}
	}
	return -1
}

// questionFor returns the text of the user message that the answer at i replies to, or
// "" when there is none. It looks back to the nearest user message.
func questionFor(entries []entry, i int) string {
	for j := i - 1; j >= 0; j-- {
		if entries[j].Kind == entryUser {
			return entries[j].Text
		}
	}
	return ""
}

// newFeedback is the record stored for a rating of an answer. The skill version is the one
// deployed now; the tool calls are not known to the panel, so they stay empty.
func newFeedback(rating int, question, answer, provider string, now time.Time) store.Feedback {
	return store.Feedback{
		CreatedAt:    now,
		Rating:       rating,
		Question:     question,
		Answer:       answer,
		Provider:     provider,
		SkillVersion: skill.Version(),
	}
}
