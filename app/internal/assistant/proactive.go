package assistant

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// Tip is one proactive suggestion shown to the player.
type Tip struct {
	Title     string
	Text      string
	EventCode uint8
	At        time.Time
}

const (
	// debounceWindow coalesces the events that arrive close together into one tip.
	debounceWindow = 8 * time.Second
	// minTipGap is the least time between two tips.
	minTipGap = 60 * time.Second
	// maxPendingAge drops queued events that waited too long (for example during combat).
	maxPendingAge = 5 * time.Minute
	// tickEvery is how often the loop checks the deadlines when no event arrives.
	tickEvery = time.Second
	// maxTipSentences limits each tip.
	maxTipSentences = 3
)

// tipPriority decides which event wins when a batch holds several codes.
var tipPriority = []uint8{1, 2, 3, 6, 5, 4}

// proactor is the debounce and combat-queue state machine. It has no goroutines and
// no real clock, so the tests drive it directly.
type proactor struct {
	now     func() time.Time
	ask     func(ctx context.Context, req ai.Request) (ai.Response, error)
	combat  func() bool
	notify  func(Tip)
	persist func(Tip, string)

	pending     []uint8
	windowStart time.Time
	hasWindow   bool
	lastTip     time.Time
	hasLast     bool
}

// newProactor wires the state machine to the assistant's clock, chain and store.
func (a *Assistant) newProactor(notify func(Tip)) *proactor {
	return &proactor{
		now: a.now,
		ask: func(ctx context.Context, req ai.Request) (ai.Response, error) {
			if a.asker == nil {
				return ai.Response{}, errors.New("no hay proveedores de IA configurados")
			}
			return a.asker.Ask(ctx, req)
		},
		combat: func() bool {
			if a.g == nil {
				return false
			}
			live, ok := a.g.Live()
			return ok && live.InCombat
		},
		notify:  notify,
		persist: a.persistTip,
	}
}

// onEvent records an event code. Codes without a tip prompt (such as 7) are ignored.
func (p *proactor) onEvent(ev bridge.LiveState) {
	code := ev.LastEventCode
	if _, ok := tipPrompts[code]; !ok {
		return
	}
	if !p.hasWindow {
		p.windowStart = p.now()
		p.hasWindow = true
	}
	for _, c := range p.pending {
		if c == code {
			return
		}
	}
	p.pending = append(p.pending, code)
}

// tick flushes the pending batch when the debounce window has passed, the player is out
// of combat and the minimum gap since the last tip is over.
func (p *proactor) tick(ctx context.Context) {
	if !p.hasWindow {
		return
	}
	now := p.now()
	if now.Sub(p.windowStart) < debounceWindow {
		return
	}
	if now.Sub(p.windowStart) > maxPendingAge {
		p.clear()
		return
	}
	if p.combat() {
		return
	}
	if p.hasLast && now.Sub(p.lastTip) < minTipGap {
		return
	}
	code := pickCode(p.pending)
	p.clear()
	p.lastTip = now
	p.hasLast = true

	tip, provider, err := p.generate(ctx, code, now)
	if err != nil {
		log.Printf("assistant: proactive tip (evento %d): %v", code, err)
		return
	}
	p.notify(tip)
	if p.persist != nil {
		p.persist(tip, provider)
	}
}

func (p *proactor) clear() {
	p.pending = nil
	p.hasWindow = false
}

// generate asks the chain for the tip of one event code.
func (p *proactor) generate(ctx context.Context, code uint8, now time.Time) (Tip, string, error) {
	req := ai.Request{
		System: SystemPrompt,
		Prompt: tipPrompts[code] + " " + proactiveInstruction,
	}
	resp, err := p.ask(ctx, req)
	if err != nil {
		return Tip{}, "", err
	}
	text := limitSentences(resp.Text, maxTipSentences)
	if text == "" {
		return Tip{}, "", errors.New("respuesta vacía")
	}
	return Tip{Title: tipTitles[code], Text: text, EventCode: code, At: now}, resp.Provider, nil
}

// persistTip stores the tip as an assistant message with Provider "proactivo:<provider>".
func (a *Assistant) persistTip(t Tip, provider string) {
	a.persist(store.ChatMsg{Role: "assistant", Content: formatTipHistory(t), Provider: "proactivo:" + provider})
}

// pickCode returns the highest-priority code of the batch.
func pickCode(codes []uint8) uint8 {
	for _, want := range tipPriority {
		for _, c := range codes {
			if c == want {
				return c
			}
		}
	}
	return codes[0]
}

// RunProactive consumes the bridge events and calls notify with each tip. It returns
// when ctx ends or events is closed. Tips are skipped while the player is in combat;
// they are queued and delivered once combat ends.
func (a *Assistant) RunProactive(ctx context.Context, events <-chan bridge.LiveState, notify func(Tip)) {
	if notify == nil {
		notify = func(Tip) {}
	}
	p := a.newProactor(notify)
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			p.onEvent(ev)
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

// limitSentences keeps at most n sentences of text, on one line. A sentence ends at
// '.', '!' or '?' followed by a space or the end of the text.
func limitSentences(text string, n int) string {
	flat := strings.Join(strings.Fields(text), " ")
	if flat == "" || n <= 0 {
		return ""
	}
	count := 0
	runes := []rune(flat)
	for i, r := range runes {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		if i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) {
			continue
		}
		count++
		if count == n {
			return string(runes[:i+1])
		}
	}
	return flat
}

// formatTipHistory is the text stored in the history for a tip.
func formatTipHistory(t Tip) string {
	return fmt.Sprintf("%s: %s", t.Title, t.Text)
}
