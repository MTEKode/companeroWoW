//go:build windows

package ui

import (
	"log"
	"time"
)

// reasonProcCB is the subclass procedure of the reason box; it is set once.
var reasonProcCB uintptr

// paintFeedback draws the 👍/👎 row under the newest answer. Nothing is drawn when the
// newest entry is not an answer, when no store is attached, or while the reason box shows.
func (w *window) paintFeedback(hdc uintptr, l Layout) {
	if w.feedback == nil || w.reasonFor >= 0 {
		return
	}
	idx := lastRateable(w.doc.entries)
	if idx < 0 {
		return
	}
	lab := feedbackLabelRect(l, w.scale)
	if e := w.doc.entries[idx]; e.Rating != 0 {
		if lab.W > 0 {
			drawText(hdc, "¡Gracias!", lab, dtRight|dtVCenter|dtSingleLine|dtNoPrefix, w.fonts.small, colMeta)
		}
		return
	}
	if lab.W > 0 {
		drawText(hdc, "¿Útil?", lab, dtRight|dtVCenter|dtSingleLine|dtNoPrefix, w.fonts.small, colMeta)
	}
	up, down := feedbackThumbs(l, w.scale)
	for _, b := range []struct {
		r     Rect
		label string
	}{{up, "👍"}, {down, "👎"}} {
		fillRect(hdc, b.r, w.brushes.button)
		frameRect(hdc, b.r, w.brushes.gold)
		drawText(hdc, b.label, b.r, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix, w.fonts.ui, colText)
	}
}

// feedbackClick handles a click in the feedback row. It reports whether the click was on
// the row, so the header and the panel's drag do not also see it.
func (w *window) feedbackClick(x, y, cw, ch int) bool {
	if w.feedback == nil || w.reasonFor >= 0 {
		return false
	}
	l := computeLayout(cw, ch, w.scale)
	if !l.Feedback.Contains(x, y) {
		return false
	}
	idx := lastRateable(w.doc.entries)
	if idx < 0 {
		return false
	}
	up, down := feedbackThumbs(l, w.scale)
	switch {
	case up.Contains(x, y):
		w.rate(idx, ratingUp)
	case down.Contains(x, y):
		w.rate(idx, ratingDown)
	}
	return true
}

// rate stores a 👍 or 👎 on the answer at idx. Each answer is rated once. A 👎 opens the
// one-line reason box.
func (w *window) rate(idx, rating int) {
	e := &w.doc.entries[idx]
	if e.Rating != 0 || w.feedback == nil {
		return
	}
	fb := newFeedback(rating, e.Question, e.Text, e.Provider, time.Now())
	id, err := w.feedback.AddFeedback(fb)
	if err != nil {
		log.Printf("ui: guardar la valoración: %v", err)
		w.setNotice("No pude guardar tu valoración")
		w.refreshStatus()
		return
	}
	e.Rating = rating
	e.FeedbackID = id
	log.Printf("ui: valoración %+d guardada (id %d, %s)", rating, id, e.Provider)
	if rating == ratingDown {
		w.openReason(idx)
	}
	w.invalidateFeedback()
}

// openReason shows the reason box in the feedback area and gives it the keyboard; the
// user chose 👎, so the focus move is theirs.
func (w *window) openReason(idx int) {
	if w.reasonEdit == 0 {
		return
	}
	w.reasonFor = idx
	setWindowText(w.reasonEdit, "")
	l := w.layout()
	call(procMoveWindow, w.reasonEdit, uintptr(l.Feedback.X), uintptr(l.Feedback.Y),
		uintptr(l.Feedback.W), uintptr(l.Feedback.H), 1)
	call(procShowWindow, w.reasonEdit, swShow)
	call(procSetFocus, w.reasonEdit)
	w.invalidateFeedback()
}

// saveReason stores the typed reason (if any) for the 👎 being explained and closes the box.
func (w *window) saveReason() {
	if w.reasonFor < 0 || w.reasonFor >= len(w.doc.entries) {
		w.closeReason()
		return
	}
	text := getWindowText(w.reasonEdit)
	e := w.doc.entries[w.reasonFor]
	if text != "" && e.FeedbackID != 0 && w.feedback != nil {
		if err := w.feedback.SetFeedbackReason(e.FeedbackID, text); err != nil {
			log.Printf("ui: guardar el motivo: %v", err)
			w.setNotice("No pude guardar el motivo")
		} else {
			w.setNotice("Gracias, lo tendremos en cuenta")
		}
	}
	w.closeReason()
}

// closeReason hides the reason box and returns the keyboard to the question box.
func (w *window) closeReason() {
	w.reasonFor = -1
	setWindowText(w.reasonEdit, "")
	call(procShowWindow, w.reasonEdit, swHide)
	w.invalidateFeedback()
	if w.wiz == nil && w.input != 0 {
		call(procSetFocus, w.input)
	}
	w.refreshStatus()
}

// invalidateFeedback repaints the feedback row.
func (w *window) invalidateFeedback() {
	rc := toRect(w.layout().Feedback)
	call(procInvalidateRect, w.hwnd, ptrOf(&rc), 1)
}

// layout returns the panel's current layout.
func (w *window) layout() Layout {
	cw, ch := w.clientSize()
	return computeLayout(cw, ch, w.scale)
}

// reasonProc is the subclass procedure of the reason box: Enter saves the reason, Escape
// closes the box, and neither makes a beep.
func reasonProc(hwnd, msg, wp, lp uintptr) uintptr {
	if w := current; w != nil {
		switch msg {
		case wmKeyDown:
			if wp == vkReturn {
				w.saveReason()
				return 0
			}
			if wp == vkEscape {
				w.closeReason()
				return 0
			}
		case wmChar:
			if wp == vkReturn || wp == vkEscape {
				return 0
			}
		}
		return call(procCallWindowProcW, w.origReasonProc, hwnd, msg, wp, lp)
	}
	return call(procDefWindowProcW, hwnd, msg, wp, lp)
}
