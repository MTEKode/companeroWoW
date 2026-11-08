package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// TestProactiveTipCarriesStateBlock: a tip is answered with the same state block that a
// question carries, so the model is not guessing where the player is.
func TestProactiveTipCarriesStateBlock(t *testing.T) {
	fake := &fakeAsker{resp: ai.Response{Text: "Ve a Dalaran.", Provider: "gemini"}}
	a := newWithAsker(fake, nil, openStore(t), nil)
	p := a.newProactor(func(Tip) {})
	if _, _, err := p.generate(context.Background(), 1, time.Now()); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(fake.reqs) != 1 {
		t.Fatalf("asked %d times, want 1", len(fake.reqs))
	}
	if !strings.HasPrefix(fake.reqs[0].System, "Estado actual") {
		t.Errorf("tip system = %q, want the state block", fake.reqs[0].System)
	}
}

// TestHistoryExcludesProactiveTips: tips are stored and shown, but the model does not get
// them back as conversation.
func TestHistoryExcludesProactiveTips(t *testing.T) {
	st := openStore(t)
	a := newWithAsker(&fakeAsker{}, nil, st, nil)
	must := func(_ int64, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.AddMessage(store.ChatMsg{Role: "user", Content: "¿Dónde voy?"}))
	must(st.AddMessage(store.ChatMsg{Role: "assistant", Content: "Ve a Elwynn.", Provider: "codex"}))
	must(st.AddMessage(store.ChatMsg{Role: "assistant", Content: "Consejo: entrena.", Provider: "proactivo:gemini"}))

	got := a.history()
	if len(got) != 2 {
		t.Fatalf("history has %d messages, want 2 (the tip excluded): %+v", len(got), got)
	}
	for _, m := range got {
		if strings.Contains(m.Content, "Consejo") {
			t.Errorf("proactive tip sent as history: %q", m.Content)
		}
	}
	stored, err := st.RecentMessages(10)
	if err != nil || len(stored) != 3 {
		t.Fatalf("tip not stored: %d messages, err %v", len(stored), err)
	}
}

// deadlineAsker records the deadline of the context it is given.
type deadlineAsker struct{ deadline time.Time }

func (d *deadlineAsker) Ask(ctx context.Context, _ ai.Request) (ai.Response, error) {
	d.deadline, _ = ctx.Deadline()
	return ai.Response{Text: "ok", Provider: "gemini"}, nil
}

// TestAskHasQuestionDeadline: one question cannot wait longer than questionDeadline.
func TestAskHasQuestionDeadline(t *testing.T) {
	d := &deadlineAsker{}
	a := newWithAsker(d, nil, nil, nil)
	before := time.Now()
	if _, err := a.Ask(context.Background(), "¿qué hago?", false); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if d.deadline.IsZero() {
		t.Fatal("the question context has no deadline")
	}
	got := d.deadline.Sub(before)
	if got > questionDeadline || got < questionDeadline-5*time.Second {
		t.Errorf("deadline in %v, want about %v", got, questionDeadline)
	}
}
