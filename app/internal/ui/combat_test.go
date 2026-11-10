package ui

import (
	"testing"
	"time"
)

// The tests below use a panel the user opened, unless they say otherwise.

func TestCombatHiderHidesAndRestores(t *testing.T) {
	c := newCombatHider(true)
	t0 := time.Unix(1000, 0)
	if got := c.Observe(true, true, true, t0); got != CombatHide {
		t.Fatalf("combat began with the panel visible: %v, want CombatHide", got)
	}
	if got := c.Observe(true, false, true, t0.Add(10*time.Second)); got != CombatNone {
		t.Errorf("still in combat: %v, want CombatNone", got)
	}
	if got := c.Observe(false, false, true, t0.Add(20*time.Second)); got != CombatNone {
		t.Errorf("combat just ended: %v, want CombatNone (restore waits 2 s)", got)
	}
	if got := c.Observe(false, false, true, t0.Add(21*time.Second)); got != CombatNone {
		t.Errorf("1 s after combat: %v, want CombatNone", got)
	}
	if got := c.Observe(false, false, true, t0.Add(22*time.Second)); got != CombatRestore {
		t.Errorf("2 s after combat: %v, want CombatRestore", got)
	}
	if got := c.Observe(false, true, true, t0.Add(30*time.Second)); got != CombatNone {
		t.Errorf("after restore: %v, want CombatNone", got)
	}
}

func TestCombatHiderNoRestoreWhenNotHidden(t *testing.T) {
	c := newCombatHider(true)
	t0 := time.Unix(1000, 0)
	// The panel was closed before combat: nothing to hide and nothing to restore.
	if got := c.Observe(true, false, true, t0); got != CombatNone {
		t.Errorf("combat with the panel hidden: %v, want CombatNone", got)
	}
	c.Observe(false, false, true, t0.Add(time.Second))
	if got := c.Observe(false, false, true, t0.Add(time.Minute)); got != CombatNone {
		t.Errorf("after combat with nothing hidden: %v, want CombatNone", got)
	}
}

func TestCombatHiderDisabledNeverHides(t *testing.T) {
	c := newCombatHider(false)
	t0 := time.Unix(1000, 0)
	if got := c.Observe(true, true, true, t0); got != CombatNone {
		t.Errorf("disabled: combat with panel visible = %v, want CombatNone", got)
	}
	if got := c.Observe(false, true, true, t0.Add(time.Minute)); got != CombatNone {
		t.Errorf("disabled: after combat = %v, want CombatNone", got)
	}
}

func TestCombatHiderRestoresWhenTurnedOffMidCombat(t *testing.T) {
	c := newCombatHider(true)
	t0 := time.Unix(1000, 0)
	c.Observe(true, true, true, t0)
	c.enabled = false
	if got := c.Observe(true, false, true, t0.Add(time.Second)); got != CombatRestore {
		t.Errorf("setting turned off while hidden for combat: %v, want CombatRestore", got)
	}
	if got := c.Observe(true, false, true, t0.Add(2*time.Second)); got != CombatNone {
		t.Errorf("second observation: %v, want CombatNone", got)
	}
}

// A new fight during the two-second wait cancels the pending restore.
func TestCombatHiderReentryCancelsRestore(t *testing.T) {
	c := newCombatHider(true)
	t0 := time.Unix(1000, 0)
	c.Observe(true, true, true, t0)
	c.Observe(false, false, true, t0.Add(time.Second))
	if got := c.Observe(true, false, true, t0.Add(2*time.Second)); got != CombatNone {
		t.Errorf("re-entry during the wait: %v, want CombatNone", got)
	}
	c.Observe(false, false, true, t0.Add(3*time.Second))
	if got := c.Observe(false, false, true, t0.Add(4*time.Second)); got != CombatNone {
		t.Errorf("2 s after the second fight would be needed: %v, want CombatNone", got)
	}
	if got := c.Observe(false, false, true, t0.Add(5*time.Second)); got != CombatRestore {
		t.Errorf("2 s after the second fight: %v, want CombatRestore", got)
	}
}

func TestCombatHiderInCombatFlag(t *testing.T) {
	c := newCombatHider(true)
	t0 := time.Unix(1000, 0)
	if c.InCombat() {
		t.Fatal("new hider reports combat")
	}
	c.Observe(true, false, true, t0)
	if !c.InCombat() {
		t.Error("InCombat false during combat")
	}
	c.Observe(false, false, true, t0.Add(time.Second))
	if c.InCombat() {
		t.Error("InCombat true after combat")
	}
}

// A panel that a tip opened is hidden for the fight but is not brought back after it.
func TestCombatHiderTipOpenedPanelNotRestored(t *testing.T) {
	c := newCombatHider(true)
	t0 := time.Unix(1000, 0)
	if got := c.Observe(true, true, false, t0); got != CombatHide {
		t.Fatalf("tip-opened panel at combat start: %v, want CombatHide", got)
	}
	c.Observe(false, false, false, t0.Add(time.Second))
	if got := c.Observe(false, false, false, t0.Add(time.Minute)); got != CombatNone {
		t.Errorf("tip-opened panel after combat: %v, want CombatNone (no restore)", got)
	}
}

// A panel the user opens during a fight stays open, so it is not restored either.
func TestCombatHiderUserOpenedDuringFightStays(t *testing.T) {
	c := newCombatHider(true)
	t0 := time.Unix(1000, 0)
	if got := c.Observe(true, false, false, t0); got != CombatNone {
		t.Fatalf("hidden at combat start: %v, want CombatNone", got)
	}
	// The user opens it in combat; nothing changes on the transitions.
	if got := c.Observe(true, true, true, t0.Add(time.Second)); got != CombatNone {
		t.Errorf("opened during the fight: %v, want CombatNone", got)
	}
	c.Observe(false, true, true, t0.Add(2*time.Second))
	if got := c.Observe(false, true, true, t0.Add(time.Minute)); got != CombatNone {
		t.Errorf("after the fight: %v, want CombatNone", got)
	}
}
