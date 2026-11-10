package ui

import "time"

// CombatRestoreDelay is how long after combat ends the hidden panel comes back.
const CombatRestoreDelay = 2 * time.Second

// CombatAction is what the window must do after an observation.
type CombatAction int

const (
	// CombatNone leaves the panel as it is.
	CombatNone CombatAction = iota
	// CombatHide hides the panel because combat started while it was visible.
	CombatHide
	// CombatRestore shows the panel again, without taking the focus.
	CombatRestore
)

// combatHider is the hide-in-combat state machine. It only acts on the transitions: a panel
// the user opens during a fight stays open, and a panel that was not visible when combat
// began is not shown afterwards. A panel that was open because of a tip is hidden for the
// fight but is not brought back after it: only a panel the user opened comes back. The
// window feeds it the live combat flag every half second and asks InCombat when a proactive
// tip arrives, so tips wait while the player is fighting. It is used on the UI thread only.
type combatHider struct {
	enabled  bool
	inCombat bool
	hidByUs  bool      // the panel was hidden for combat, was opened by the user, and is not restored yet
	leftAt   time.Time // when combat ended; zero while in combat or not pending
}

func newCombatHider(enabled bool) combatHider {
	return combatHider{enabled: enabled}
}

// Observe takes the live combat flag, whether the panel is visible now, whether the user
// opened the panel (not a tip) and the time, and returns what to do with the panel.
func (c *combatHider) Observe(inCombat, visible, byUser bool, now time.Time) CombatAction {
	if !c.enabled {
		// Turned off while the panel is hidden for combat: bring it back at once.
		c.inCombat = inCombat
		if c.hidByUs {
			c.hidByUs = false
			c.leftAt = time.Time{}
			return CombatRestore
		}
		return CombatNone
	}
	switch {
	case inCombat && !c.inCombat:
		c.inCombat = true
		c.leftAt = time.Time{}
		if visible {
			c.hidByUs = byUser
			return CombatHide
		}
		return CombatNone
	case inCombat:
		c.leftAt = time.Time{}
		return CombatNone
	case c.inCombat:
		// Combat has just ended; the restore waits for CombatRestoreDelay.
		c.inCombat = false
		c.leftAt = now
		return CombatNone
	}
	if c.hidByUs && !c.leftAt.IsZero() && now.Sub(c.leftAt) >= CombatRestoreDelay {
		c.hidByUs = false
		c.leftAt = time.Time{}
		return CombatRestore
	}
	return CombatNone
}

// InCombat reports whether the last observation was in combat.
func (c *combatHider) InCombat() bool { return c.inCombat }
