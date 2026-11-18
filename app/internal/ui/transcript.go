package ui

import (
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
)

// splitProvider separates the provider of an answer from the agy model that gave it:
// "antigravity:gemini-4-hydrogen-high" is ("antigravity", "gemini-4-hydrogen-high").
func splitProvider(p string) (provider, model string) {
	if name, m, ok := strings.Cut(p, ":"); ok && name == "antigravity" {
		return name, m
	}
	return p, ""
}

// entryKind says how one item of the conversation is shown.
type entryKind int

const (
	entryUser entryKind = iota
	entryAssistant
	entryTip
	entryError
	entryInfo
)

// entry is one item of the conversation.
type entry struct {
	Kind     entryKind
	Text     string
	Provider string
	At       string
	Title    string // tips only
	// Question is the user message the answer replies to (answers only); it is stored with
	// the rating.
	Question string
	// Rating is the user's 👍 (+1) or 👎 (-1) on this answer, 0 when none; FeedbackID is
	// its row in the store.
	Rating     int
	FeedbackID int64
}

// entryFromMsg turns a stored message into a conversation entry.
func entryFromMsg(m answerMsg) entry {
	switch m.Role {
	case "user":
		return entry{Kind: entryUser, Text: m.Content}
	case "assistant":
		return entry{Kind: entryAssistant, Text: m.Content, Provider: m.Provider, At: m.At}
	default:
		return entry{Kind: entryInfo, Text: m.Content}
	}
}

// entryRuns returns the styled text of one entry, ending with a blank line.
func entryRuns(e entry) []Span {
	switch e.Kind {
	case entryUser:
		return []Span{
			{Text: "Tú:", Style: StyleGold | StyleBold},
			{Text: "\n"},
			{Text: e.Text},
			{Text: "\n\n"},
		}
	case entryAssistant:
		provider, model := splitProvider(e.Provider)
		parts := []string{"Compañero"}
		if provider != "" {
			parts = append(parts, provider)
		}
		if e.At != "" {
			parts = append(parts, e.At)
		}
		out := []Span{{Text: strings.Join(parts, " · "), Style: StyleMeta}, {Text: "\n"}}
		out = append(out, MarkRuns(e.Text)...)
		if model != "" {
			// The model that answered, small, under the answer.
			out = append(out, Span{Text: "\n"}, Span{Text: "Modelo: " + ai.DisplayModel(model), Style: StyleMeta})
		}
		return append(out, Span{Text: "\n\n"})
	case entryTip:
		head := "💡 Consejo"
		if e.Title != "" {
			head += ": " + e.Title
		}
		return []Span{
			{Text: head, Style: StyleTip | StyleGold | StyleBold},
			{Text: "\n", Style: StyleTip},
			{Text: e.Text, Style: StyleTip},
			{Text: "\n\n"},
		}
	case entryError:
		return []Span{{Text: e.Text, Style: StyleError}, {Text: "\n\n"}}
	default:
		return []Span{{Text: e.Text, Style: StyleMeta}, {Text: "\n\n"}}
	}
}
