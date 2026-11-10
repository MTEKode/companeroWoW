package ui

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/MTEKode/companeroWoW/app/internal/assistant"
	"github.com/MTEKode/companeroWoW/app/internal/config"
)

func TestRouteTipPanelModeOpensHiddenPanel(t *testing.T) {
	if got := routeTip(config.TipsPanel, false, false); got != (TipRoute{Conversation: true, ShowPanel: true}) {
		t.Errorf("panel, hidden, out of combat = %+v", got)
	}
	if got := routeTip(config.TipsPanel, true, false); got.ShowPanel || !got.Conversation {
		t.Errorf("panel, visible = %+v", got)
	}
	// In combat the tip waits in the conversation and the panel stays hidden, as before.
	if got := routeTip(config.TipsPanel, false, true); got.ShowPanel || !got.Conversation {
		t.Errorf("panel, hidden, in combat = %+v", got)
	}
	if got := routeTip(config.TipsPanel, false, true); got.Badge || got.Balloon || got.Defer {
		t.Errorf("panel mode must not badge or balloon: %+v", got)
	}
}

// The panel never opens by itself in bandeja mode; the tip is announced instead.
func TestRouteTipBandejaNeverOpensPanel(t *testing.T) {
	for _, visible := range []bool{false, true} {
		for _, inCombat := range []bool{false, true} {
			if got := routeTip(config.TipsBandeja, visible, inCombat); got.ShowPanel {
				t.Errorf("bandeja, visible %v, combat %v: ShowPanel = true", visible, inCombat)
			}
		}
	}
}

func TestRouteTipBandejaHiddenOutOfCombat(t *testing.T) {
	want := TipRoute{Conversation: true, Badge: true, Balloon: true}
	if got := routeTip(config.TipsBandeja, false, false); got != want {
		t.Errorf("bandeja, hidden: %+v, want %+v", got, want)
	}
}

// In combat the balloon waits for the end of the fight; the badge still counts the tip.
func TestRouteTipBandejaHiddenInCombatDefersBalloon(t *testing.T) {
	want := TipRoute{Conversation: true, Badge: true, Defer: true}
	if got := routeTip(config.TipsBandeja, false, true); got != want {
		t.Errorf("bandeja, hidden, combat: %+v, want %+v", got, want)
	}
}

// A visible panel shows the tip at once: no badge and no balloon.
func TestRouteTipBandejaVisibleShowsOnlyInConversation(t *testing.T) {
	want := TipRoute{Conversation: true}
	if got := routeTip(config.TipsBandeja, true, false); got != want {
		t.Errorf("bandeja, visible: %+v, want %+v", got, want)
	}
}

func TestRouteTipOffDropsEverything(t *testing.T) {
	for _, visible := range []bool{false, true} {
		for _, inCombat := range []bool{false, true} {
			if got := routeTip(config.TipsOff, visible, inCombat); got != (TipRoute{}) {
				t.Errorf("off, visible %v, combat %v: %+v", visible, inCombat, got)
			}
		}
	}
	if got := routeTip("", false, false); got != (TipRoute{}) {
		t.Errorf("unknown mode: %+v", got)
	}
}

func TestTipBadgeCountsAndRemembersFirstUnread(t *testing.T) {
	var b tipBadge
	b.clear()
	if _, ok := b.firstUnread(); ok || b.count() != 0 {
		t.Fatalf("cleared badge: count %d, unread %v", b.count(), ok)
	}
	b.add(4)
	b.add(7)
	b.add(9)
	if b.count() != 3 {
		t.Errorf("count = %d, want 3", b.count())
	}
	if idx, ok := b.firstUnread(); !ok || idx != 4 {
		t.Errorf("first unread = %d, %v; want 4, true", idx, ok)
	}
	b.clear()
	if b.count() != 0 {
		t.Errorf("after clear: count %d", b.count())
	}
	b.add(12)
	if idx, _ := b.firstUnread(); idx != 12 || b.count() != 1 {
		t.Errorf("new unread after clear: idx %d, count %d", idx, b.count())
	}
}

func TestTrayTipWithBadge(t *testing.T) {
	s := Status{Bridge: "connected", Level: 12, Zone: "Ventormenta"}
	if got := trayTipWithBadge(s, 0); got != trayTip(s) {
		t.Errorf("no badge = %q, want the status tip", got)
	}
	if got := trayTipWithBadge(s, 1); got != "Compañero WoW · 1 consejo nuevo" {
		t.Errorf("one tip = %q", got)
	}
	if got := trayTipWithBadge(s, 3); got != "Compañero WoW · 3 consejos nuevos" {
		t.Errorf("three tips = %q", got)
	}
}

func TestBalloonTextIsCut(t *testing.T) {
	long := assistant.Tip{Title: strings.Repeat("T", 200), Text: strings.Repeat("ñ", 500)}
	title, text := balloonText(long)
	if n := len([]rune(text)); n > balloonTextMax {
		t.Errorf("text has %d runes, max %d", n, balloonTextMax)
	}
	if n := len([]rune(title)); n > balloonTitleMax {
		t.Errorf("title has %d runes, max %d", n, balloonTitleMax)
	}
	if !strings.HasSuffix(text, "…") {
		t.Errorf("cut text lacks ellipsis: %q", text[len(text)-5:])
	}
}

func TestBalloonTextJoinsLinesAndKeepsTitle(t *testing.T) {
	title, text := balloonText(assistant.Tip{
		Title: "Subida de nivel",
		Text:  "Ya puedes entrenar en Ventormenta.\r\n📍 /compa marca 45 60 Entrenador",
	})
	if title != "Subida de nivel" {
		t.Errorf("title = %q", title)
	}
	if strings.ContainsAny(text, "\r\n") {
		t.Errorf("text kept line breaks: %q", text)
	}
	if !strings.Contains(text, "📍 /compa marca 45 60 Entrenador") {
		t.Errorf("text lost the mark line: %q", text)
	}
	if title, _ := balloonText(assistant.Tip{}); title != "Consejo" {
		t.Errorf("empty title = %q, want Consejo", title)
	}
}

func TestUnreadOffsetCountsUTF16Units(t *testing.T) {
	entries := []entry{
		{Kind: entryUser, Text: "hola"},
		{Kind: entryTip, Title: "💡 emoji", Text: "x"},
	}
	want := runsUnits(entryRuns(entries[0]))
	if got := unreadOffset(entries, 1); got != want {
		t.Errorf("offset of entry 1 = %d, want %d", got, want)
	}
	if got := unreadOffset(entries, 0); got != 0 {
		t.Errorf("offset of entry 0 = %d, want 0", got)
	}
	// The count matches the text the rich edit holds: the entry's own text in UTF-16 units.
	var text strings.Builder
	for _, sp := range entryRuns(entries[0]) {
		text.WriteString(sp.Text)
	}
	if got := len(utf16.Encode([]rune(text.String()))); got != want {
		t.Errorf("runsUnits = %d, utf16 of the text = %d", want, got)
	}
}

// The badge icon: 40-byte header, colour rows and the mask, with the red dot in the top-right
// corner and a transparent top-left corner.
func TestBadgeIconImageLayout(t *testing.T) {
	const size = 16
	img := badgeIconImage(size)
	if want := 40 + size*size*4 + 4*size; len(img) != want {
		t.Fatalf("length %d, want %d", len(img), want)
	}
	if binary.LittleEndian.Uint32(img[0:]) != 40 || binary.LittleEndian.Uint32(img[4:]) != size {
		t.Fatal("header size or width is wrong")
	}
	if binary.LittleEndian.Uint32(img[8:]) != 2*size {
		t.Fatal("height must be twice the size (colour and mask)")
	}
	if binary.LittleEndian.Uint16(img[14:]) != 32 {
		t.Fatal("bits per pixel must be 32")
	}
	pix := func(x, y int) (r, g, b, a byte) {
		row := size - 1 - y // bottom row first
		o := 40 + (row*size+x)*4
		return img[o+2], img[o+1], img[o+0], img[o+3]
	}
	if _, _, _, a := pix(0, 0); a != 0 {
		t.Errorf("top-left corner alpha = %d, want 0", a)
	}
	if r, g, b, a := pix(size-3, 2); a != 255 || r < 200 || g > 120 || b > 120 {
		t.Errorf("dot pixel = %d,%d,%d alpha %d, want red and opaque", r, g, b, a)
	}
	if _, _, _, a := pix(size/2, size/2); a != 255 {
		t.Errorf("centre alpha = %d, want opaque", a)
	}
}
