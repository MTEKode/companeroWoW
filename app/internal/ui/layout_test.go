package ui

import (
	"strings"
	"testing"
)

func TestComputeLayoutStacksWithoutOverlap(t *testing.T) {
	for _, tc := range []struct {
		w, h  int
		scale float64
	}{{420, 560, 1}, {300, 300, 1}, {630, 840, 1.5}, {840, 1120, 2}} {
		l := computeLayout(tc.w, tc.h, tc.scale)
		all := map[string]Rect{"header": l.Header, "conv": l.Conv, "input": l.Input, "check": l.Check, "send": l.Send, "status": l.Status}
		for name, r := range all {
			if r.X < 0 || r.Y < 0 || r.X+r.W > tc.w || r.Y+r.H > tc.h {
				t.Errorf("%dx%d@%.1f: %s %+v is outside the client area", tc.w, tc.h, tc.scale, name, r)
			}
		}
		// Vertical order: header, conversation, input, row (checkbox, send), status.
		if !(l.Header.Y+l.Header.H <= l.Conv.Y && l.Conv.Y+l.Conv.H <= l.Input.Y &&
			l.Input.Y+l.Input.H <= l.Check.Y && l.Check.Y+l.Check.H <= l.Status.Y) {
			t.Errorf("%dx%d@%.1f: parts overlap vertically: %+v", tc.w, tc.h, tc.scale, l)
		}
		if l.Conv.H <= 0 {
			t.Errorf("%dx%d@%.1f: conversation has no height", tc.w, tc.h, tc.scale)
		}
		if l.Send.X+l.Send.W > tc.w-l.Conv.X+1 {
			t.Errorf("%dx%d@%.1f: send button leaves the padding", tc.w, tc.h, tc.scale)
		}
	}
}

func TestComputeLayoutDefaultSize(t *testing.T) {
	l := computeLayout(420, 560, 1)
	if l.Header.H != 28 || l.Conv.W != 400 || l.Conv.H < 300 {
		t.Errorf("default layout = %+v", l)
	}
}

func TestHeaderButtonsInsideHeader(t *testing.T) {
	btns := headerButtons(420, 1)
	if len(btns) != 3 {
		t.Fatalf("buttons = %d", len(btns))
	}
	hdr := computeLayout(420, 560, 1).Header
	for _, b := range btns {
		if b.Rect.Y < hdr.Y || b.Rect.Y+b.Rect.H > hdr.Y+hdr.H || b.Rect.X+b.Rect.W > 420 {
			t.Errorf("button %q %+v leaves the header %+v", b.Label, b.Rect, hdr)
		}
	}
	// Close is the rightmost button, clear the leftmost.
	if btns[2].Action != actionClose || btns[0].Action != actionClear {
		t.Errorf("button order = %+v", btns)
	}
}

func TestHitHeaderButton(t *testing.T) {
	btns := headerButtons(420, 1)
	closeRect := btns[2].Rect
	if b, ok := hitHeaderButton(closeRect.X+1, closeRect.Y+1, 420, 1); !ok || b.Action != actionClose {
		t.Errorf("click on × = %+v, %v", b, ok)
	}
	if _, ok := hitHeaderButton(5, 5, 420, 1); ok {
		t.Error("click on the title should not hit a button")
	}
}

func TestHitEdge(t *testing.T) {
	const w, h, grip = 420, 560, 6
	cases := []struct {
		x, y int
		want int
	}{
		{0, 0, htTopLeft},
		{419, 0, htTopRight},
		{0, 559, htBottomLeft},
		{419, 559, htBottomRight},
		{0, 300, htLeft},
		{419, 300, htRight},
		{200, 0, htTop},
		{200, 559, htBottom},
		{200, 300, htClient},
		{grip, grip, htClient},
	}
	for _, c := range cases {
		if got := hitEdge(c.x, c.y, w, h, grip); got != c.want {
			t.Errorf("hitEdge(%d,%d) = %d, want %d", c.x, c.y, got, c.want)
		}
	}
}

func TestRectContains(t *testing.T) {
	r := Rect{X: 10, Y: 10, W: 5, H: 5}
	if !r.Contains(10, 10) || !r.Contains(14, 14) || r.Contains(15, 10) || r.Contains(9, 10) {
		t.Error("Contains is off by one")
	}
}

func TestFormatStatusConnected(t *testing.T) {
	s := Status{Bridge: "connected", Level: 12, Zone: "Ventormenta", SavedVarsAgeSec: 300}
	got := formatStatus(s, "", false)
	want := "● WoW conectado · nivel 12 · Ventormenta · SavedVariables hace 5 min"
	if got != want {
		t.Errorf("formatStatus = %q, want %q", got, want)
	}
}

func TestFormatStatusHintsAndNotices(t *testing.T) {
	s := Status{Bridge: "no_wow", SavedVarsAgeSec: -1, Hints: []string{"Abre WoW"}, Notice: "MCP caído"}
	got := formatStatus(s, "sin atajo", false)
	for _, part := range []string{"○ WoW no detectado", "SavedVariables sin datos", "Abre WoW", "MCP caído", "sin atajo"} {
		if !strings.Contains(got, part) {
			t.Errorf("formatStatus = %q, missing %q", got, part)
		}
	}
}

func TestFormatStatusBusyReplacesHint(t *testing.T) {
	s := Status{Bridge: "no_strip", SavedVarsAgeSec: 30, Hints: []string{"Activa el addon"}}
	got := formatStatus(s, "", true)
	if !strings.HasPrefix(got, "Pensando…") || strings.Contains(got, "Activa el addon") {
		t.Errorf("busy status = %q", got)
	}
	if !strings.Contains(got, "Sin franja de píxeles") {
		t.Errorf("busy status lost the bridge state: %q", got)
	}
}

func TestHumanAge(t *testing.T) {
	cases := map[int64]string{
		10:      "hace menos de 1 min",
		120:     "hace 2 min",
		7200:    "hace 2 h",
		172800:  "hace 2 d",
		3599:    "hace 59 min",
		86399:   "hace 23 h",
		0:       "hace menos de 1 min",
		3600:    "hace 1 h",
		86400:   "hace 1 d",
		60:      "hace 1 min",
		5 * 60:  "hace 5 min",
		24 * 60: "hace 24 min",
	}
	for sec, want := range cases {
		if got := humanAge(sec); got != want {
			t.Errorf("humanAge(%d) = %q, want %q", sec, got, want)
		}
	}
}
