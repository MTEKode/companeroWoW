// Package ondemand decides when the app reads the game in the on-demand mode (the default):
// only while the panel is open or a question is in flight, plus a short hold after the
// panel closes. It also waits for fresh game data before a question, and keeps the last
// known data on disk so that an answer after a restart still has something to say.
//
// The controller is a plain state machine. Its clock and its timer are seams, so the
// transitions are tested with a fake clock; in the app the timer is time.AfterFunc, which
// fires once per transition, so an idle app runs no timer and no loop.
package ondemand

import (
	"sync"
	"time"
)

// HoldAfterHide is how long the reader stays on after the panel closes and no question is
// running.
const HoldAfterHide = 20 * time.Second

// Controller turns the panel visibility and the questions in flight into the active state of
// the game reader. Active is true while the panel is visible or a question runs, and for
// HoldAfterHide after the last of them ended. onChange is called on each transition, once,
// with the new state.
type Controller struct {
	now      func() time.Time
	after    func(d time.Duration, f func()) (cancel func())
	onChange func(active bool)

	mu           sync.Mutex
	panelVisible bool
	questions    int
	active       bool
	activatedAt  time.Time
	// idleSince is when the controller became idle (panel hidden, no question); zero otherwise.
	idleSince time.Time
	cancelTmr func()
}

// NewController returns a controller that starts inactive. now is the clock (time.Now in the
// app), after schedules f once after d and returns a function that cancels it (time.AfterFunc
// in the app), and onChange receives each transition. A nil after is replaced by one that
// never fires, so that tests can drive Check themselves.
func NewController(now func() time.Time, after func(time.Duration, func()) func(), onChange func(bool)) *Controller {
	if now == nil {
		now = time.Now
	}
	if after == nil {
		after = func(time.Duration, func()) func() { return func() {} }
	}
	if onChange == nil {
		onChange = func(bool) {}
	}
	c := &Controller{now: now, after: after, onChange: onChange}
	c.idleSince = now()
	return c
}

// Active reports whether the game reader should be on.
func (c *Controller) Active() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

// ActivatedAt is when the last activation started (zero before the first one).
func (c *Controller) ActivatedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.activatedAt
}

// PanelShown records that the panel is visible: the reader is on.
func (c *Controller) PanelShown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.panelVisible = true
	c.idleSince = time.Time{}
	c.cancelTimerLocked()
	c.setLocked(true)
}

// PanelHidden records that the panel is hidden. The reader stays on for HoldAfterHide when no
// question runs.
func (c *Controller) PanelHidden() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.panelVisible = false
	c.enterIdleLocked()
}

// BeginQuestion records a question in flight: the reader is on until EndQuestion.
func (c *Controller) BeginQuestion() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.questions++
	c.idleSince = time.Time{}
	c.cancelTimerLocked()
	c.setLocked(true)
}

// EndQuestion records that a question finished (answered, failed or cancelled).
func (c *Controller) EndQuestion() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.questions > 0 {
		c.questions--
	}
	c.enterIdleLocked()
}

// Check ends the hold when it has run out. The timer calls it; tests call it after they
// advance the clock. It does nothing while the panel is visible or a question runs.
func (c *Controller) Check() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelTmr = nil
	if !c.active || c.panelVisible || c.questions > 0 || c.idleSince.IsZero() {
		return
	}
	if c.now().Sub(c.idleSince) >= HoldAfterHide {
		c.setLocked(false)
	}
}

// enterIdleLocked starts the hold when the panel is hidden and no question runs.
func (c *Controller) enterIdleLocked() {
	if c.panelVisible || c.questions > 0 {
		return
	}
	if c.idleSince.IsZero() {
		c.idleSince = c.now()
	}
	if !c.active {
		return
	}
	c.cancelTimerLocked()
	c.cancelTmr = c.after(HoldAfterHide, c.Check)
}

func (c *Controller) cancelTimerLocked() {
	if c.cancelTmr != nil {
		c.cancelTmr()
		c.cancelTmr = nil
	}
}

// setLocked changes the state and reports it. The caller holds mu; onChange runs under it,
// so it must be quick and must not call back into the controller.
func (c *Controller) setLocked(on bool) {
	if c.active == on {
		return
	}
	c.active = on
	if on {
		c.activatedAt = c.now()
	}
	c.onChange(on)
}
