package ui

import "testing"

func TestWizardButtonsInsideArea(t *testing.T) {
	area := Rect{X: 10, Y: 40, W: 400, H: 300}
	btns := wizardButtons(area, 1, false)
	if len(btns) != 4 {
		t.Fatalf("%d buttons, want 4", len(btns))
	}
	for _, b := range btns {
		if b.Rect.X < area.X || b.Rect.Y < area.Y ||
			b.Rect.X+b.Rect.W > area.X+area.W || b.Rect.Y+b.Rect.H > area.Y+area.H {
			t.Errorf("%s button %+v leaves the area %+v", b.Label, b.Rect, area)
		}
		if b.Action == wizInstall && b.Enabled {
			t.Error("install enabled without an install item")
		}
	}
	if btns := wizardButtons(area, 1, true); !btns[0].Enabled {
		t.Error("install should be enabled when offered")
	}
}

func TestWizardListAreaAboveButtons(t *testing.T) {
	conv := Rect{X: 10, Y: 34, W: 400, H: 380}
	list := wizardListArea(conv, 1)
	top := -1
	for _, b := range wizardButtons(conv, 1, false) {
		if top < 0 || b.Rect.Y < top {
			top = b.Rect.Y
		}
	}
	if list.Y+list.H > top {
		t.Errorf("list area ends at %d, below the buttons at %d", list.Y+list.H, top)
	}
}

func TestFeedbackThumbsPlacement(t *testing.T) {
	l := computeLayout(420, 560, 1)
	up, down := feedbackThumbs(l, 1)
	if up.X+up.W > down.X || down.X+down.W > l.Feedback.X+l.Feedback.W {
		t.Errorf("thumbs overlap or leave the area: up %+v down %+v area %+v", up, down, l.Feedback)
	}
	if l.Feedback.W <= 0 {
		t.Errorf("feedback area width = %d", l.Feedback.W)
	}
	if l.Feedback.X < l.Check.X+l.Check.W || l.Feedback.X+l.Feedback.W > l.Send.X {
		t.Errorf("feedback area %+v overlaps the check box or Enviar", l.Feedback)
	}
	if r := feedbackLabelRect(l, 1); r.W == 0 {
		t.Error("label should fit at the default width")
	}
	narrow := computeLayout(300, 300, 1)
	if r := feedbackLabelRect(narrow, 1); r.W != 0 {
		t.Errorf("narrow window still draws the label: %+v", r)
	}
}
