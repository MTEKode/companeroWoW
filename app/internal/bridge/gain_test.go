package bridge

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestColourGainAlertAfterTwoSeconds checks the streak of colour-gain reads: no alert before
// ColourGainAfter, an alert once it has lasted that long, and no alert after a read that is not a
// gain error (the streak is broken).
func TestColourGainAlertAfterTwoSeconds(t *testing.T) {
	r := NewReader(time.Millisecond)
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	clock := t0
	r.now = func() time.Time { return clock }
	r.SetActive(true)

	gainErr := fmt.Errorf("v3: %w", &ColourGainError{Gain: 1.6, Clipped: true})
	r.note(0, gainErr, true)
	clock = t0.Add(time.Second)
	r.note(0, gainErr, true)
	if _, ok := r.ColourGainAlert(clock); ok {
		t.Fatal("alert after 1 s: the gain error must persist for ColourGainAfter first")
	}

	clock = t0.Add(ColourGainAfter + 100*time.Millisecond)
	r.note(0, gainErr, true)
	g, ok := r.ColourGainAlert(clock)
	if !ok || g != 1.6 {
		t.Fatalf("alert = (%v, %v), want (1.6, true) after %v", g, ok, ColourGainAfter)
	}
	if st := r.Stats(); st.Gain != 1.6 {
		t.Errorf("Stats().Gain = %v, want 1.6", st.Gain)
	}

	clock = clock.Add(100 * time.Millisecond)
	r.note(0, errors.New("pantalla: sin ventana de WoW"), true)
	if _, ok := r.ColourGainAlert(clock); ok {
		t.Fatal("alert after a read that is not a gain error")
	}
	if st := r.Stats(); st.Gain != 0 {
		t.Errorf("Stats().Gain = %v after a clean failure, want 0", st.Gain)
	}
}

// TestColourGainAlertNeedsAnActiveReader checks that an inactive reader never alerts.
func TestColourGainAlertNeedsAnActiveReader(t *testing.T) {
	r := NewReader(time.Millisecond)
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	clock := t0
	r.now = func() time.Time { return clock }
	gainErr := &ColourGainError{Gain: 0.4}
	r.note(0, gainErr, true)
	clock = t0.Add(3 * time.Second)
	r.note(0, gainErr, true)
	r.SetActive(false)
	if _, ok := r.ColourGainAlert(clock); ok {
		t.Fatal("an inactive reader must not alert")
	}
}
