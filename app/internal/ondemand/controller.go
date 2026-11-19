// Package ondemand decides when the app reads the game in the on-demand mode (the default):
// only while the panel is open or a question is in flight, plus a hold after the last of them
// ended. It also waits for fresh game data before a question, and keeps the last known data on
// disk so that an answer after a restart still has something to say.
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
// running, and how long it stays on after the addon's share was last seen (see Touch).
const HoldAfterHide = 20 * time.Second

// PulseHold is how long a pulse (the game-mode key, see Pulse) keeps the reader on. The player
// opens the in-game frame with the key and types the question there, so the hold covers the
// typing; a share seen while it runs extends it (see Touch).
const PulseHold = 60 * time.Second

// Controller turns the panel visibility and the questions in flight into the active state of
// the game reader. Active is true while the panel is visible or a question runs, and for the hold
// after the last of them ended. onChange is called on each transition, once, with the new state.
type Controller struct {
	now      func() time.Time
	after    func(d time.Duration, f func()) (cancel func())
	onChange func(active bool)
	logf     func(format string, args ...any)

	mu           sync.Mutex
	panelVisible bool
	questions    int
	active       bool
	activatedAt  time.Time
	// holdUntil is when the reader goes off while nothing keeps it on (the panel is hidden and no
	// question runs). Zero while the panel or a question keeps it on.
	holdUntil time.Time
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
	return &Controller{now: now, after: after, onChange: onChange}
}

// SetLogf sets where the transitions are reported, with the reason of each one (nil: silent).
// The app logs them to companero.log.
func (c *Controller) SetLogf(logf func(format string, args ...any)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logf = logf
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

// HoldUntil is when the reader goes off if nothing else keeps it on (zero while the panel or a
// question keeps it on, or while it is inactive).
func (c *Controller) HoldUntil() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.holdUntil
}

// PanelShown records that the panel is visible: the reader is on.
func (c *Controller) PanelShown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.panelVisible = true
	c.holdUntil = time.Time{}
	c.cancelTimerLocked()
	c.setLocked(true, "panel abierto")
}

// PanelHidden records that the panel is hidden. The reader stays on for HoldAfterHide when no
// question runs.
func (c *Controller) PanelHidden() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.panelVisible = false
	c.enterIdleLocked()
}

// Pulse activates the reader for PulseHold without opening the panel: the game-mode panel key
// (uiMode juego) calls it, so the player can type a question in the game and the answer has the
// data. With the panel open or a question running there is nothing to hold; the call is then
// a no-op. Otherwise the reader stays on for PulseHold from now.
func (c *Controller) Pulse() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.panelVisible || c.questions > 0 {
		return
	}
	c.holdUntil = c.now().Add(PulseHold)
	c.cancelTimerLocked()
	c.setLocked(true, "tecla del juego")
	c.armLocked()
}

// Touch extends the hold to HoldAfterHide from now while the reader is on for the hold. The reader
// calls it when a read shows the addon sharing: the share is then on its way, and a question may
// follow it. It does nothing while the panel or a question keeps the reader on.
func (c *Controller) Touch() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active || c.panelVisible || c.questions > 0 {
		return
	}
	until := c.now().Add(HoldAfterHide)
	if until.After(c.holdUntil) {
		c.holdUntil = until
		c.armLocked()
	}
}

// BeginQuestion records a question in flight: the reader is on until EndQuestion.
func (c *Controller) BeginQuestion() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.questions++
	c.holdUntil = time.Time{}
	c.cancelTimerLocked()
	c.setLocked(true, "pregunta en curso")
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
// advance the clock. It does nothing while the panel is visible or a question runs, and it
// re-arms the timer when the hold was extended and has not run out yet.
func (c *Controller) Check() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelTmr = nil
	if !c.active || c.panelVisible || c.questions > 0 || c.holdUntil.IsZero() {
		return
	}
	if c.now().Before(c.holdUntil) {
		c.armLocked()
		return
	}
	c.holdUntil = time.Time{}
	c.setLocked(false, "retención agotada")
}

// enterIdleLocked starts the hold when the panel is hidden and no question runs.
func (c *Controller) enterIdleLocked() {
	if c.panelVisible || c.questions > 0 {
		return
	}
	if !c.active {
		return
	}
	c.holdUntil = c.now().Add(HoldAfterHide)
	c.cancelTimerLocked()
	c.armLocked()
}

// armLocked schedules Check for the end of the hold. The caller holds mu and has no timer running.
func (c *Controller) armLocked() {
	c.cancelTimerLocked()
	d := c.holdUntil.Sub(c.now())
	if d < 0 {
		d = 0
	}
	c.cancelTmr = c.after(d, c.Check)
}

func (c *Controller) cancelTimerLocked() {
	if c.cancelTmr != nil {
		c.cancelTmr()
		c.cancelTmr = nil
	}
}

// setLocked changes the state and reports it, with the reason. The caller holds mu; onChange runs
// under it, so it must be quick and must not call back into the controller.
func (c *Controller) setLocked(on bool, reason string) {
	if c.active == on {
		return
	}
	c.active = on
	if on {
		c.activatedAt = c.now()
		if c.logf != nil {
			hold := ""
			if !c.holdUntil.IsZero() {
				hold = ", retención hasta " + c.holdUntil.Format("15:04:05")
			}
			c.logf("lectura del juego: activa (%s%s)", reason, hold)
		}
	} else if c.logf != nil {
		c.logf("lectura del juego: en reposo (%s, activa %s)", reason, c.now().Sub(c.activatedAt).Round(time.Millisecond))
	}
	c.onChange(on)
}
