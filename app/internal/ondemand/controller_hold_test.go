package ondemand

import (
	"testing"
	"time"
)

// A pulse keeps the reader on for PulseHold, the time the player needs to open the in-game frame
// and type the question.
func TestPulseHoldIsLongerThanTheShareWindow(t *testing.T) {
	if PulseHold <= HoldAfterHide {
		t.Fatalf("PulseHold %v must outlast HoldAfterHide %v", PulseHold, HoldAfterHide)
	}
}

// A share seen while the hold runs extends it to HoldAfterHide from that read: a question sent at
// the end of the share is still read.
func TestTouchExtendsTheHold(t *testing.T) {
	c, clk, tr := newTest(t)
	c.Pulse()
	clk.advance(PulseHold - 5*time.Second)
	c.Touch() // the addon is sharing: the strip decoded with shareRemainingSec > 0
	clk.advance(10 * time.Second)
	c.Check()
	if !c.Active() {
		t.Fatal("reader went off before the extended hold ended")
	}
	clk.advance(HoldAfterHide - 10*time.Second)
	c.Check()
	if c.Active() {
		t.Fatal("reader must be off HoldAfterHide after the last share")
	}
	if want := []bool{true, false}; !equalSeq(tr.seq, want) {
		t.Fatalf("transitions = %v, want %v", tr.seq, want)
	}
}

// A share does not extend a hold that already lasts longer than HoldAfterHide.
func TestTouchNeverShortensTheHold(t *testing.T) {
	c, clk, _ := newTest(t)
	c.Pulse()
	until := c.HoldUntil()
	clk.advance(time.Second)
	c.Touch()
	if got := c.HoldUntil(); got.Before(until) {
		t.Fatalf("hold moved back from %v to %v", until, got)
	}
}

// Touch does nothing while the panel is open or the reader is off.
func TestTouchWithoutHoldIsNoOp(t *testing.T) {
	c, _, tr := newTest(t)
	c.Touch()
	if c.Active() || len(tr.seq) != 0 {
		t.Fatalf("touch with the reader off must not activate it (transitions %v)", tr.seq)
	}
	c.PanelShown()
	c.Touch()
	if !c.HoldUntil().IsZero() {
		t.Fatal("a panel that is open keeps no hold")
	}
}

// The transitions are logged with their reason, and the hold is what Check tests.
func TestTransitionsReportTheirReason(t *testing.T) {
	c, clk, _ := newTest(t)
	var lines []string
	c.SetLogf(func(format string, args ...any) { lines = append(lines, format) })
	c.Pulse()
	clk.advance(PulseHold)
	c.Check()
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want 2: %v", len(lines), lines)
	}
}
