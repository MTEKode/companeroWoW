package ondemand

import (
	"testing"
	"time"
)

// fakeClock is the controller's clock; the timer is recorded and fired by the test.
type fakeClock struct {
	t       time.Time
	pending []func()
	stopped int
}

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func (c *fakeClock) after(d time.Duration, f func()) func() {
	idx := len(c.pending)
	c.pending = append(c.pending, f)
	return func() {
		c.stopped++
		c.pending[idx] = nil
	}
}

// fire runs the timers that are still pending, as time.AfterFunc would once their delay ran out.
func (c *fakeClock) fire() {
	list := c.pending
	c.pending = nil
	for _, f := range list {
		if f != nil {
			f()
		}
	}
}

type transitions struct{ seq []bool }

func (tr *transitions) on(a bool) { tr.seq = append(tr.seq, a) }

func newTest(t *testing.T) (*Controller, *fakeClock, *transitions) {
	t.Helper()
	clk := &fakeClock{t: time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)}
	tr := &transitions{}
	return NewController(clk.now, clk.after, tr.on), clk, tr
}

func TestControllerStartsInactive(t *testing.T) {
	c, _, tr := newTest(t)
	if c.Active() {
		t.Fatal("a new controller must be inactive: no panel, no question")
	}
	if len(tr.seq) != 0 {
		t.Fatalf("no transition expected at start, got %v", tr.seq)
	}
}

func TestPanelShownActivatesAndHideHoldsTwentySeconds(t *testing.T) {
	c, clk, tr := newTest(t)
	c.PanelShown()
	if !c.Active() {
		t.Fatal("panel shown must activate")
	}
	clk.advance(5 * time.Second)
	c.PanelHidden()
	if !c.Active() {
		t.Fatal("hiding the panel keeps the reader on for the hold")
	}
	// Check before the hold has run out keeps the reader on.
	clk.advance(HoldAfterHide - time.Second)
	c.Check()
	if !c.Active() {
		t.Fatal("reader went off before the 20 s hold ended")
	}
	clk.advance(time.Second)
	c.Check()
	if c.Active() {
		t.Fatal("reader must be off 20 s after the panel was hidden")
	}
	if want := []bool{true, false}; !equalSeq(tr.seq, want) {
		t.Fatalf("transitions = %v, want %v", tr.seq, want)
	}
}

func TestTimerFiresDeactivation(t *testing.T) {
	c, clk, _ := newTest(t)
	c.PanelShown()
	c.PanelHidden()
	clk.advance(HoldAfterHide)
	clk.fire() // what time.AfterFunc does once the hold is over
	if c.Active() {
		t.Fatal("the hold timer must switch the reader off")
	}
}

func TestQuestionKeepsReaderOnAfterPanelHidden(t *testing.T) {
	c, clk, _ := newTest(t)
	c.PanelShown()
	c.BeginQuestion()
	c.PanelHidden()
	clk.advance(HoldAfterHide * 3)
	c.Check()
	if !c.Active() {
		t.Fatal("a question in flight keeps the reader on, whatever the panel does")
	}
	c.EndQuestion()
	// The hold starts when the question ends, not when the panel closed.
	if !c.Active() {
		t.Fatal("ending the question starts the hold; it does not switch off at once")
	}
	clk.advance(HoldAfterHide - time.Second)
	c.Check()
	if !c.Active() {
		t.Fatal("hold must run from the end of the question")
	}
	clk.advance(time.Second)
	c.Check()
	if c.Active() {
		t.Fatal("reader must be off 20 s after the last question with the panel hidden")
	}
}

func TestQuestionAloneActivatesWithoutPanel(t *testing.T) {
	c, clk, _ := newTest(t)
	c.BeginQuestion()
	if !c.Active() {
		t.Fatal("a question must activate the reader even with the panel hidden")
	}
	c.EndQuestion()
	clk.advance(HoldAfterHide)
	c.Check()
	if c.Active() {
		t.Fatal("a question alone must not keep the reader on forever")
	}
}

func TestReopeningPanelDuringHoldCancelsIt(t *testing.T) {
	c, clk, tr := newTest(t)
	c.PanelShown()
	c.PanelHidden()
	clk.advance(10 * time.Second)
	c.PanelShown()
	clk.advance(HoldAfterHide)
	c.Check()
	if !c.Active() {
		t.Fatal("an open panel must stay active")
	}
	if want := []bool{true}; !equalSeq(tr.seq, want) {
		t.Fatalf("transitions = %v, want %v (no switch off while open)", tr.seq, want)
	}
}

func TestActivatedAtIsTheLastActivation(t *testing.T) {
	c, clk, _ := newTest(t)
	c.PanelShown()
	first := c.ActivatedAt()
	c.PanelHidden()
	clk.advance(HoldAfterHide)
	c.Check()
	clk.advance(time.Minute)
	c.BeginQuestion()
	if got := c.ActivatedAt(); !got.Equal(first.Add(HoldAfterHide + time.Minute)) {
		t.Fatalf("ActivatedAt = %v, want the second activation", got)
	}
}

func TestRepeatedShowDoesNotNotifyTwice(t *testing.T) {
	c, _, tr := newTest(t)
	c.PanelShown()
	c.PanelShown()
	c.BeginQuestion()
	if want := []bool{true}; !equalSeq(tr.seq, want) {
		t.Fatalf("transitions = %v, want %v", tr.seq, want)
	}
}

func equalSeq(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
