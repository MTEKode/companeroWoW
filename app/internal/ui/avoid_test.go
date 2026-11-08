package ui

import "testing"

var (
	testScreen = Rect{X: 0, Y: 0, W: 1920, H: 1080}
	// The WoW pixel strip: the top-left corner of the client area.
	testStrip = Rect{X: 0, Y: 0, W: 464, H: 16}
)

func TestClearOfLeavesNonOverlappingRect(t *testing.T) {
	r := Rect{X: 600, Y: 200, W: 420, H: 560}
	if got := clearOf(r, testStrip, testScreen); got != r {
		t.Errorf("clearOf moved a rect that did not overlap: %+v", got)
	}
}

func TestClearOfMovesRightOfStrip(t *testing.T) {
	r := Rect{X: 0, Y: 0, W: 420, H: 560}
	got := clearOf(r, testStrip, testScreen)
	if overlaps(got, testStrip) {
		t.Fatalf("result %+v still covers the strip", got)
	}
	if got.X != testStrip.X+testStrip.W || got.Y != 0 {
		t.Errorf("expected the place right of the strip, got %+v", got)
	}
}

func TestClearOfFallsBackBelowWhenRightDoesNotFit(t *testing.T) {
	screen := Rect{X: 0, Y: 0, W: 600, H: 1000}
	r := Rect{X: 0, Y: 0, W: 420, H: 560}
	got := clearOf(r, testStrip, screen)
	if overlaps(got, testStrip) {
		t.Fatalf("result %+v still covers the strip", got)
	}
	if got.Y != testStrip.Y+testStrip.H {
		t.Errorf("expected the place below the strip, got %+v", got)
	}
	if got.X+got.W > screen.W || got.Y+got.H > screen.H {
		t.Errorf("result %+v leaves the screen", got)
	}
}

func TestClearOfKeepsRectOnScreen(t *testing.T) {
	r := Rect{X: 1800, Y: -30, W: 420, H: 560}
	got := clearOf(r, Rect{X: 0, Y: 0, W: 10, H: 10}, testScreen)
	if got.X+got.W > testScreen.W || got.Y < 0 {
		t.Errorf("off-screen rect was not pulled inside: %+v", got)
	}
}

func TestClearOfWithStripOffset(t *testing.T) {
	// WoW client origin away from the screen corner: the panel must still stay clear.
	strip := Rect{X: 300, Y: 100, W: 464, H: 16}
	r := Rect{X: 250, Y: 90, W: 420, H: 560}
	got := clearOf(r, strip, testScreen)
	if overlaps(got, strip) {
		t.Errorf("result %+v covers the strip %+v", got, strip)
	}
}

func TestDefaultPlacementRightCentred(t *testing.T) {
	got := defaultPlacement(420, 560, testScreen, testStrip, 24)
	want := Rect{X: 1920 - 420 - 24, Y: (1080 - 560) / 2, W: 420, H: 560}
	if got != want {
		t.Errorf("defaultPlacement = %+v, want %+v", got, want)
	}
}

func TestDefaultPlacementAvoidsStripOnSmallScreen(t *testing.T) {
	screen := Rect{X: 0, Y: 0, W: 800, H: 600}
	got := defaultPlacement(420, 560, screen, testStrip, 24)
	if overlaps(got, testStrip) {
		t.Errorf("default placement %+v covers the strip", got)
	}
}
