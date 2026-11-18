// Package assistant turns the companion's questions into prompts for the AI chain. It
// adds the Spanish system prompt, a compact "Estado actual" block built from the game
// data, the recent chat history and an optional screenshot, then persists the turns.
// It also produces proactive tips when the game reports noteworthy events.
package assistant

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/capture"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// historyTurns is how many previous messages are sent with each question.
const historyTurns = 10

// historyAnswerRunes caps each earlier assistant answer sent back as history; the full
// text stays in the store and in the overlay.
const historyAnswerRunes = 600

// questionDeadline bounds one question, from the window to the last provider. The chain
// tries providers in turn, so the bound is what keeps a stuck question from waiting
// forever.
const questionDeadline = 210 * time.Second

// proactivePrefix marks the provider of a stored proactive tip ("proactivo:<provider>").
// Tips are shown and stored, but are not sent back as conversation history.
const proactivePrefix = "proactivo:"

// asker is the part of ai.Chain the assistant uses. Tests inject a fake.
type asker interface {
	Ask(ctx context.Context, req ai.Request) (ai.Response, error)
}

// CaptureFunc takes the screenshot for questions that ask for it. Tests override it.
var CaptureFunc = capture.CaptureWoW

// Assistant answers questions and produces proactive tips.
type Assistant struct {
	asker asker
	g     *gamedata.Service
	st    *store.Store
	cfg   *config.Config
	now   func() time.Time

	mu      sync.Mutex
	lastCtx string // the player context block of the last question
}

// New creates the assistant over the provider chain, the game data, the store and the
// configuration. Any of them may be nil; the assistant then works with less context.
func New(chain *ai.Chain, g *gamedata.Service, st *store.Store, cfg *config.Config) *Assistant {
	a := &Assistant{g: g, st: st, cfg: cfg, now: time.Now}
	if chain != nil {
		a.asker = chain
	}
	return a
}

// newWithAsker is the test constructor: any asker, real or fake.
func newWithAsker(ask asker, g *gamedata.Service, st *store.Store, cfg *config.Config) *Assistant {
	a := New(nil, g, st, cfg)
	a.asker = ask
	return a
}

// Ask answers question. It loads the last historyTurns messages, adds the game state to
// the system prompt, attaches a screenshot when withScreenshot is set and asks the chain.
// On success the user question and the answer (with the provider that answered) are
// stored in the history. On failure nothing is stored, so the question can be retried.
func (a *Assistant) Ask(ctx context.Context, question string, withScreenshot bool) (ai.Response, error) {
	q := strings.TrimSpace(question)
	if q == "" {
		return ai.Response{}, errors.New("escribe una pregunta")
	}
	ctx, cancel := context.WithTimeout(ctx, questionDeadline)
	defer cancel()
	resp, err := a.ask(ctx, q, withScreenshot, a.history())
	if err != nil {
		return resp, err
	}
	a.persist(store.ChatMsg{Role: "user", Content: q})
	a.persist(store.ChatMsg{Role: "assistant", Content: resp.Text, Provider: resp.Provider})
	return resp, nil
}

// Answer asks one question as if it were the first of a conversation: no history is
// sent and nothing is stored. The evaluation harness uses it so that each scripted
// question is independent and the user's chat is left alone.
func (a *Assistant) Answer(ctx context.Context, question string, withScreenshot bool) (ai.Response, error) {
	q := strings.TrimSpace(question)
	if q == "" {
		return ai.Response{}, errors.New("escribe una pregunta")
	}
	return a.ask(ctx, q, withScreenshot, nil)
}

// ask builds the request (skill, game state, history, optional screenshot) and calls the
// chain. It stores nothing.
func (a *Assistant) ask(ctx context.Context, q string, withScreenshot bool, history []ai.Message) (ai.Response, error) {
	if a.asker == nil {
		return ai.Response{}, errors.New("no hay proveedores de IA configurados (revisa providerOrder y apiProviders)")
	}
	// The player context is consulted here, once, so the provider does not need a tool call
	// for the usual "where am I / what now" questions.
	req := ai.Request{
		Skill:   SystemPrompt,
		System:  a.systemPrompt(),
		History: history,
		Prompt:  q,
	}
	if withScreenshot {
		png, err := CaptureFunc()
		if err != nil {
			req.Prompt = q + "\n\n(Nota: no pude capturar la pantalla de WoW: " + err.Error() + ". Responde sin imagen.)"
		} else {
			req.ImagePNG = png
		}
	}
	return a.asker.Ask(ctx, req)
}

// systemPrompt is the game state and the player context block that every question
// carries, proactive tips included. The context is consulted here, once, so the provider
// does not need a tool call for the usual "where am I / what now" questions.
func (a *Assistant) systemPrompt() string {
	system := a.stateBlock()
	block := a.playerContextBlock()
	a.setLastContext(block)
	if block != "" {
		system += "\n\n" + block
	}
	return system
}

// LastContext returns the player context block that the last question carried, or "" when
// there was none. The evaluation harness reads it to check the answers.
func (a *Assistant) LastContext() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastCtx
}

func (a *Assistant) setLastContext(block string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastCtx = block
}

// ClearHistory deletes every stored chat message.
func (a *Assistant) ClearHistory() error {
	if a.st == nil {
		return nil
	}
	return a.st.ClearHistory()
}

// History returns the last n stored messages, oldest first.
func (a *Assistant) History(n int) ([]store.ChatMsg, error) {
	if a.st == nil {
		return []store.ChatMsg{}, nil
	}
	return a.st.RecentMessages(n)
}

// history converts the stored messages into the chain's history format. Earlier answers
// are cut to historyAnswerRunes so that the prompt stays small.
func (a *Assistant) history() []ai.Message {
	msgs, err := a.History(historyTurns)
	if err != nil {
		log.Printf("assistant: read history: %v", err)
		return nil
	}
	out := make([]ai.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if strings.HasPrefix(m.Provider, proactivePrefix) {
			continue
		}
		content := m.Content
		if m.Role == "assistant" {
			content = truncateRunes(content, historyAnswerRunes)
		}
		out = append(out, ai.Message{Role: m.Role, Content: content})
	}
	return out
}

// truncateRunes keeps at most n runes of s, marking the cut with an ellipsis.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

func (a *Assistant) persist(m store.ChatMsg) {
	if a.st == nil {
		return
	}
	if _, err := a.st.AddMessage(m); err != nil {
		log.Printf("assistant: save %s message: %v", m.Role, err)
	}
}

// stateBlock summarizes what the game data knows, so that even turns without tool calls
// have context. Missing data is reported as such.
func (a *Assistant) stateBlock() string {
	if a.g == nil {
		return "Estado actual: sin datos del juego."
	}
	var lines []string
	if loc, err := a.g.Location(); err == nil {
		if m := asMap(loc); m != nil && truthy(m["available"]) {
			zone, _ := m["zone"].(string)
			if zone == "" {
				zone = "desconocida"
			}
			line := "Zona: " + zone
			if sub, _ := m["subZone"].(string); sub != "" {
				line += " (" + sub + ")"
			}
			if id, ok := number(m["mapId"]); ok && id > 0 {
				line += fmt.Sprintf(", mapa %d", int(id))
			}
			lines = append(lines, line)
			x, okX := number(m["x"])
			y, okY := number(m["y"])
			if okX && okY {
				lines = append(lines, fmt.Sprintf("Coordenadas: (%.1f, %.1f)", displayCoord(x), displayCoord(y)))
			}
		}
	}
	trackedID := int64(0)
	if ch, err := a.g.Character(); err == nil {
		if m := asMap(ch); m != nil && truthy(m["available"]) {
			var parts []string
			if lvl, ok := number(m["level"]); ok && lvl > 0 {
				parts = append(parts, fmt.Sprintf("nivel %d", int(lvl)))
			}
			if xp, ok := number(m["xpPct"]); ok {
				parts = append(parts, fmt.Sprintf("xp %d%%", int(xp)))
			}
			if len(parts) > 0 {
				lines = append(lines, "Personaje: "+strings.Join(parts, ", "))
			}
			if combat, ok := m["inCombat"].(bool); ok {
				if combat {
					lines = append(lines, "En combate: sí")
				} else {
					lines = append(lines, "En combate: no")
				}
			}
		}
	}
	if qs, err := a.g.Quests(); err == nil {
		if m := asMap(qs); m != nil && truthy(m["available"]) {
			if id, ok := number(m["trackedQuestId"]); ok {
				trackedID = int64(id)
			}
			if trackedID > 0 {
				title := questTitle(m["quests"], trackedID)
				if title == "" {
					lines = append(lines, fmt.Sprintf("Misión rastreada: #%d", trackedID))
				} else {
					lines = append(lines, fmt.Sprintf("Misión rastreada: %s (#%d)", title, trackedID))
				}
			}
		}
	}
	if len(lines) == 0 {
		// The context block says what to do when data is missing; this line only states it.
		return "Estado actual: sin datos del juego en este momento."
	}
	return "Estado actual:\n- " + strings.Join(lines, "\n- ")
}

// displayCoord converts a 0..1 map coordinate to the 0..100 scale players use. Values
// already above 1 are taken as 0..100 and kept.
func displayCoord(v float64) float64 {
	if v <= 1 {
		return v * 100
	}
	return v
}

func questTitle(list any, id int64) string {
	var entries []map[string]any
	switch t := list.(type) {
	case []map[string]any:
		entries = t
	case []any:
		for _, e := range t {
			if m := asMap(e); m != nil {
				entries = append(entries, m)
			}
		}
	}
	for _, q := range entries {
		qid, _ := number(q["questID"])
		if int64(qid) == id {
			title, _ := q["title"].(string)
			return title
		}
	}
	return ""
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func truthy(v any) bool {
	b, _ := v.(bool)
	return b
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint8:
		return float64(n), true
	}
	return 0, false
}
