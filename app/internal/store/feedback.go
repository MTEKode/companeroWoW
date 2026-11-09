package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Feedback is one 👍 (Rating +1) or 👎 (Rating -1) on an assistant answer, with the
// question and answer it refers to. SkillVersion is the companion skill that was in
// force; ToolCalls lists the tool names the provider reported, empty when unknown.
type Feedback struct {
	ID           int64     `json:"id"`
	CreatedAt    time.Time `json:"createdAt"`
	Rating       int       `json:"rating"`
	Reason       string    `json:"reason"`
	Question     string    `json:"question"`
	Answer       string    `json:"answer"`
	Provider     string    `json:"provider"`
	SkillVersion string    `json:"skillVersion"`
	ToolCalls    []string  `json:"toolCalls"`
}

// AddFeedback stores a rating and returns its id. The rating must be +1 or -1. A zero
// CreatedAt means now; ToolCalls may be nil.
func (s *Store) AddFeedback(f Feedback) (int64, error) {
	if f.Rating != 1 && f.Rating != -1 {
		return 0, fmt.Errorf("add feedback: rating must be +1 or -1, got %d", f.Rating)
	}
	created := f.CreatedAt
	if created.IsZero() {
		created = s.now()
	}
	calls := f.ToolCalls
	if calls == nil {
		calls = []string{}
	}
	tools, err := json.Marshal(calls)
	if err != nil {
		return 0, fmt.Errorf("add feedback: encode tool calls: %w", err)
	}
	res, err := s.db.Exec(
		`INSERT INTO feedback (created_at, rating, reason, question, answer, provider, skill_version, tool_calls)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		created.UnixNano(), f.Rating, strings.TrimSpace(f.Reason), f.Question, f.Answer,
		f.Provider, f.SkillVersion, string(tools))
	if err != nil {
		return 0, fmt.Errorf("add feedback: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("add feedback id: %w", err)
	}
	return id, nil
}

// SetFeedbackReason sets the optional reason of a rating that was stored earlier.
func (s *Store) SetFeedbackReason(id int64, reason string) error {
	res, err := s.db.Exec(`UPDATE feedback SET reason = ? WHERE id = ?`, strings.TrimSpace(reason), id)
	if err != nil {
		return fmt.Errorf("set feedback reason: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set feedback reason: no rating with id %d", id)
	}
	return nil
}

// ListFeedback returns every rating, oldest first.
func (s *Store) ListFeedback() ([]Feedback, error) {
	rows, err := s.db.Query(
		`SELECT id, created_at, rating, reason, question, answer, provider, skill_version, tool_calls
		 FROM feedback ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	defer rows.Close()
	out := []Feedback{}
	for rows.Next() {
		f, err := scanFeedback(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	return out, nil
}

func scanFeedback(sc rowScanner) (Feedback, error) {
	var (
		f        Feedback
		created  int64
		toolsRaw string
	)
	if err := sc.Scan(&f.ID, &created, &f.Rating, &f.Reason, &f.Question, &f.Answer,
		&f.Provider, &f.SkillVersion, &toolsRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Feedback{}, err
		}
		return Feedback{}, fmt.Errorf("scan feedback: %w", err)
	}
	f.CreatedAt = time.Unix(0, created)
	f.ToolCalls = []string{}
	if toolsRaw != "" {
		if err := json.Unmarshal([]byte(toolsRaw), &f.ToolCalls); err != nil {
			return Feedback{}, fmt.Errorf("scan feedback tool calls: %w", err)
		}
	}
	return f, nil
}

// FeedbackCount is how many ratings a provider received, split by sign.
type FeedbackCount struct {
	Provider string `json:"provider"`
	Up       int    `json:"up"`
	Down     int    `json:"down"`
}

// FeedbackSummary counts the ratings in total and per provider (providers sorted by name;
// an empty provider is shown as "desconocido").
func (s *Store) FeedbackSummary() (up, down int, byProvider []FeedbackCount, err error) {
	rows, err := s.db.Query(`SELECT provider, rating, COUNT(*) FROM feedback GROUP BY provider, rating`)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("feedback summary: %w", err)
	}
	defer rows.Close()
	counts := map[string]*FeedbackCount{}
	for rows.Next() {
		var provider string
		var rating, n int
		if err := rows.Scan(&provider, &rating, &n); err != nil {
			return 0, 0, nil, fmt.Errorf("feedback summary: %w", err)
		}
		if provider == "" {
			provider = "desconocido"
		}
		c := counts[provider]
		if c == nil {
			c = &FeedbackCount{Provider: provider}
			counts[provider] = c
		}
		if rating > 0 {
			c.Up += n
			up += n
		} else {
			c.Down += n
			down += n
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, nil, fmt.Errorf("feedback summary: %w", err)
	}
	for _, c := range counts {
		byProvider = append(byProvider, *c)
	}
	sort.Slice(byProvider, func(i, j int) bool { return byProvider[i].Provider < byProvider[j].Provider })
	return up, down, byProvider, nil
}

// FeedbackSummaryMarkdown is the "Feedback de usuarios" section of the evaluation report:
// totals per provider and the reasons given for 👎. It returns "" when there is no
// feedback, so the report omits the section.
func (s *Store) FeedbackSummaryMarkdown() (string, error) {
	up, down, byProvider, err := s.FeedbackSummary()
	if err != nil {
		return "", err
	}
	if up+down == 0 {
		return "", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Feedback de usuarios\n\n")
	fmt.Fprintf(&b, "Total: %d 👍 y %d 👎.\n\n", up, down)
	b.WriteString("| Proveedor | 👍 | 👎 |\n|---|---|---|\n")
	for _, c := range byProvider {
		fmt.Fprintf(&b, "| %s | %d | %d |\n", c.Provider, c.Up, c.Down)
	}
	reasons, err := s.downReasons(10)
	if err != nil {
		return "", err
	}
	if len(reasons) > 0 {
		b.WriteString("\nMotivos de los 👎 (los más recientes):\n\n")
		for _, r := range reasons {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	return b.String(), nil
}

func (s *Store) downReasons(limit int) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT reason FROM feedback WHERE rating < 0 AND reason <> '' ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("feedback reasons: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, fmt.Errorf("feedback reasons: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
