package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxQuestionRunes is the longest question text the addon sends (its frame box limit).
const MaxQuestionRunes = 500

// Question is one question typed in the in-game frame (msgType 15). The addon numbers the
// questions of each character with an increasing id, kept across sessions; it sends the same
// question again every 2 s during the share window, so the id is what tells them apart.
type Question struct {
	ID     uint64  `json:"id"`
	Text   string  `json:"text"`
	At     int64   `json:"at"`
	MapID  int     `json:"mapID"`
	Target *string `json:"target"`
}

// ErrBadQuestion reports a question body that cannot be answered: no id, or no text.
var ErrBadQuestion = errors.New("bridge: pregunta no válida")

// ParseQuestion decodes the JSON body of a question message. The text is trimmed and cut to
// MaxQuestionRunes; a body without an id (or 0) or without text is rejected.
func ParseQuestion(body []byte) (Question, error) {
	var q Question
	if err := json.Unmarshal(body, &q); err != nil {
		return Question{}, fmt.Errorf("%w: %v", ErrBadQuestion, err)
	}
	if q.ID == 0 {
		return Question{}, fmt.Errorf("%w: falta el id", ErrBadQuestion)
	}
	q.Text = strings.TrimSpace(q.Text)
	if q.Text == "" {
		return Question{}, fmt.Errorf("%w: texto vacío", ErrBadQuestion)
	}
	if utf8.RuneCountInString(q.Text) > MaxQuestionRunes {
		q.Text = string([]rune(q.Text)[:MaxQuestionRunes])
	}
	return q, nil
}
