package ui

// Rect is a rectangle in client pixels.
type Rect struct{ X, Y, W, H int }

// Contains reports whether the point (x, y) lies inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

// Layout places the parts of the panel. Sizes are logical (96 DPI) and scaled.
type Layout struct {
	Header Rect
	Conv   Rect
	Input  Rect
	Check  Rect
	Send   Rect
	Status Rect
	// Feedback is the free space of the input row between the capture check and Enviar:
	// the 👍/👎 buttons and the one-line reason box sit here.
	Feedback Rect
}

const (
	headerHeight  = 28
	padding       = 10
	gap           = 6
	inputHeight   = 56
	rowHeight     = 26
	statusHeight  = 20
	checkWidth    = 130
	sendWidth     = 84
	buttonWidth   = 28
	clearWidth    = 72
	buttonSpacing = 2
	// gripWidth is the band along each edge that resizes the window.
	gripWidth = 6
	// thumbWidth is the width of a 👍 or 👎 button, at 96 DPI.
	thumbWidth = 30
	// feedbackLabelMin is the narrowest space in which the "¿Útil?" text is drawn.
	feedbackLabelMin = 50
)

// Hit-test codes, with the values of the Win32 HT* constants.
const (
	htClient      = 1
	htLeft        = 10
	htRight       = 11
	htTop         = 12
	htTopLeft     = 13
	htTopRight    = 14
	htBottom      = 15
	htBottomLeft  = 16
	htBottomRight = 17
)

// Header button actions.
const (
	actionClear = iota
	actionHide
	actionClose
)

// HeaderButton is one button in the header bar.
type HeaderButton struct {
	Action int
	Label  string
	Rect   Rect
}

// computeLayout places the header, conversation, input row and status bar in a client
// area of w x h pixels. scale is the DPI scale (1.0 = 96 DPI).
func computeLayout(w, h int, scale float64) Layout {
	p := scaled(padding, scale)
	g := scaled(gap, scale)
	grip := scaled(gripWidth, scale)
	hdr := scaled(headerHeight, scale)
	statusH := scaled(statusHeight, scale)
	rowH := scaled(rowHeight, scale)
	inH := scaled(inputHeight, scale)

	status := Rect{X: p, Y: h - grip - statusH, W: w - 2*p, H: statusH}
	rowY := status.Y - g - rowH
	inputY := rowY - g - inH
	convY := hdr + g
	convH := inputY - g - convY
	if convH < 0 {
		convH = 0
	}
	check := Rect{X: p, Y: rowY, W: scaled(checkWidth, scale), H: rowH}
	send := Rect{X: w - p - scaled(sendWidth, scale), Y: rowY, W: scaled(sendWidth, scale), H: rowH}
	fbX := check.X + check.W + g
	fbW := send.X - g - fbX
	if fbW < 0 {
		fbW = 0
	}
	return Layout{
		Header:   Rect{X: 0, Y: 0, W: w, H: hdr},
		Conv:     Rect{X: p, Y: convY, W: w - 2*p, H: convH},
		Input:    Rect{X: p, Y: inputY, W: w - 2*p, H: inH},
		Check:    check,
		Send:     send,
		Status:   status,
		Feedback: Rect{X: fbX, Y: rowY, W: fbW, H: rowH},
	}
}

// feedbackThumbs returns the 👍 and 👎 buttons at the right end of the feedback area, 👍
// first. The caller hides the label when the area is too narrow for it.
func feedbackThumbs(l Layout, scale float64) (up, down Rect) {
	bw := scaled(thumbWidth, scale)
	sp := scaled(buttonSpacing, scale)
	x := l.Feedback.X + l.Feedback.W - bw
	down = Rect{X: x, Y: l.Feedback.Y, W: bw, H: l.Feedback.H}
	up = Rect{X: x - bw - sp, Y: l.Feedback.Y, W: bw, H: l.Feedback.H}
	return up, down
}

// feedbackLabelRect is the text area left of the thumbs, or an empty rectangle when it
// would be too narrow to read.
func feedbackLabelRect(l Layout, scale float64) Rect {
	up, _ := feedbackThumbs(l, scale)
	r := Rect{X: l.Feedback.X, Y: l.Feedback.Y, W: up.X - scaled(gap, scale) - l.Feedback.X, H: l.Feedback.H}
	if r.W < scaled(feedbackLabelMin, scale) {
		return Rect{}
	}
	return r
}

// headerButtons returns the header buttons from right to left: close, hide, clear.
func headerButtons(w int, scale float64) []HeaderButton {
	hdr := scaled(headerHeight, scale)
	bw := scaled(buttonWidth, scale)
	cw := scaled(clearWidth, scale)
	sp := scaled(buttonSpacing, scale)
	closeX := w - bw - sp
	hideX := closeX - bw - sp
	clearX := hideX - cw - sp
	return []HeaderButton{
		{Action: actionClear, Label: "Limpiar", Rect: Rect{X: clearX, Y: sp, W: cw, H: hdr - 2*sp}},
		{Action: actionHide, Label: "–", Rect: Rect{X: hideX, Y: sp, W: bw, H: hdr - 2*sp}},
		{Action: actionClose, Label: "×", Rect: Rect{X: closeX, Y: sp, W: bw, H: hdr - 2*sp}},
	}
}

// hitHeaderButton returns the header button under (x, y), if any.
func hitHeaderButton(x, y, w int, scale float64) (HeaderButton, bool) {
	for _, b := range headerButtons(w, scale) {
		if b.Rect.Contains(x, y) {
			return b, true
		}
	}
	return HeaderButton{}, false
}

// hitEdge returns the hit-test code for a point (x, y) in a window of w x h pixels: the
// corners and edges resize, everything else is client area. grip is the band width.
func hitEdge(x, y, w, h, grip int) int {
	left := x < grip
	right := x >= w-grip
	top := y < grip
	bottom := y >= h-grip
	switch {
	case top && left:
		return htTopLeft
	case top && right:
		return htTopRight
	case bottom && left:
		return htBottomLeft
	case bottom && right:
		return htBottomRight
	case left:
		return htLeft
	case right:
		return htRight
	case top:
		return htTop
	case bottom:
		return htBottom
	}
	return htClient
}

// overlaps reports whether two rectangles share at least one pixel.
func overlaps(a, b Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// insideRect moves r, without resizing it, so that it lies within bounds where possible.
func insideRect(r, bounds Rect) Rect {
	if r.X+r.W > bounds.X+bounds.W {
		r.X = bounds.X + bounds.W - r.W
	}
	if r.Y+r.H > bounds.Y+bounds.H {
		r.Y = bounds.Y + bounds.H - r.H
	}
	if r.X < bounds.X {
		r.X = bounds.X
	}
	if r.Y < bounds.Y {
		r.Y = bounds.Y
	}
	return r
}

// clearOf returns r moved, if needed, so that it does not cover avoid (the WoW pixel
// strip) and lies within screen. It tries the places to the right of avoid, below it,
// left of it and above it, in that order, and keeps the first one that fits on screen.
// When nothing fits (a very small screen), it returns r kept inside the screen.
func clearOf(r, avoid, screen Rect) Rect {
	if !overlaps(r, avoid) {
		return insideRect(r, screen)
	}
	candidates := []Rect{
		{X: avoid.X + avoid.W, Y: r.Y, W: r.W, H: r.H},
		{X: r.X, Y: avoid.Y + avoid.H, W: r.W, H: r.H},
		{X: avoid.X - r.W, Y: r.Y, W: r.W, H: r.H},
		{X: r.X, Y: avoid.Y - r.H, W: r.W, H: r.H},
	}
	for _, c := range candidates {
		c = insideRect(c, screen)
		if !overlaps(c, avoid) {
			return c
		}
	}
	return insideRect(r, screen)
}

// defaultPlacement puts a w x h panel on the right side of screen, vertically centred,
// and then moves it clear of avoid.
func defaultPlacement(w, h int, screen, avoid Rect, margin int) Rect {
	r := Rect{X: screen.X + screen.W - w - margin, Y: screen.Y + (screen.H-h)/2, W: w, H: h}
	return clearOf(r, avoid, screen)
}
