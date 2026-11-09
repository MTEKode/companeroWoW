package ui

import (
	"strings"
	"testing"
)

func TestTrayTipShowsStatus(t *testing.T) {
	got := trayTip(Status{Bridge: "connected", Level: 12, Zone: "Ventormenta"})
	if got != "Compañero WoW · ● WoW conectado · nivel 12 · Ventormenta" {
		t.Errorf("trayTip = %q", got)
	}
	if got := trayTip(Status{Bridge: "no_wow"}); !strings.Contains(got, "WoW no detectado") {
		t.Errorf("trayTip without WoW = %q", got)
	}
}

func TestTrayTipIsCut(t *testing.T) {
	long := Status{Bridge: "connected", Level: 60, Zone: strings.Repeat("ñ", 300)}
	got := []rune(trayTip(long))
	if len(got) > trayTipMax {
		t.Errorf("tip has %d runes, max %d", len(got), trayTipMax)
	}
}

func TestOpacityAlpha(t *testing.T) {
	cases := map[int]uint8{60: 153, 90: 229, 100: 255, 10: 153, 500: 255}
	for pct, want := range cases {
		if got := opacityAlpha(pct); got != want {
			t.Errorf("opacityAlpha(%d) = %d, want %d", pct, got, want)
		}
	}
}
