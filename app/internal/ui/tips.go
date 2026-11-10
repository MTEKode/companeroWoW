package ui

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/MTEKode/companeroWoW/app/internal/assistant"
	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// TipRoute says what happens to one proactive tip, decided by the tips mode and by whether
// the panel is shown and the player is in combat.
type TipRoute struct {
	// Conversation appends the tip to the conversation. Only a tip that is dropped (mode
	// off) leaves it out.
	Conversation bool
	// ShowPanel shows the panel without taking the focus (panel mode, when it is hidden).
	ShowPanel bool
	// Badge counts the tip as unread on the tray icon (bandeja mode, panel hidden).
	Badge bool
	// Balloon shows the tray balloon now (bandeja mode, panel hidden, out of combat).
	Balloon bool
	// Defer keeps the balloon for when combat ends (bandeja mode, panel hidden, in combat).
	Defer bool
}

// routeTip returns what to do with a tip. The panel is never opened by a tip in bandeja
// mode; in panel mode it is opened as before, except in combat.
func routeTip(mode string, visible, inCombat bool) TipRoute {
	switch mode {
	case config.TipsPanel:
		return TipRoute{Conversation: true, ShowPanel: !visible && !inCombat}
	case config.TipsBandeja:
		if visible {
			// The player has the panel open and sees the tip arrive: nothing to announce.
			return TipRoute{Conversation: true}
		}
		return TipRoute{
			Conversation: true,
			Badge:        true,
			Balloon:      !inCombat,
			Defer:        inCombat,
		}
	}
	return TipRoute{}
}

// Limits of the tray balloon. Shell_NotifyIcon takes 63 characters of title and 255 of text
// (less the terminator); the text is kept under the 200 characters the design asks for.
const (
	balloonTitleMax = 60
	balloonTextMax  = 200
)

// balloonText is the title and the text of the tray balloon for a tip. Line breaks become
// spaces and both are cut to their limits, with an ellipsis when cut.
func balloonText(t assistant.Tip) (title, text string) {
	title = clipRunes(oneLine(t.Title), balloonTitleMax)
	if title == "" {
		title = "Consejo"
	}
	text = clipRunes(oneLine(t.Text), balloonTextMax)
	return title, text
}

// oneLine joins the lines of s with spaces and trims the ends.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\r", "\n")), " ")
}

// clipRunes shortens s to n characters, ending in "…" when it is cut.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// tipBadge counts the tips that arrived while the panel was closed. The UI thread owns it;
// the tray icon keeps a copy for the status goroutine.
type tipBadge struct {
	n     int
	first int // index into the conversation entries of the first unread tip
}

// add counts one unread tip, whose conversation entry is at index entry.
func (b *tipBadge) add(entry int) {
	if b.n == 0 {
		b.first = entry
	}
	b.n++
}

// clear marks everything as read.
func (b *tipBadge) clear() {
	b.n = 0
	b.first = -1
}

// count is the number of unread tips.
func (b tipBadge) count() int { return b.n }

// firstUnread returns the entry index of the first unread tip; ok is false when none is.
func (b tipBadge) firstUnread() (int, bool) {
	return b.first, b.n > 0
}

// trayTipWithBadge is the tray tooltip while tips are unread: the number of them replaces
// the status line, which the next status refresh brings back once the badge is cleared.
// Without unread tips it is the usual trayTip.
func trayTipWithBadge(s Status, unread int) string {
	if unread <= 0 {
		return trayTip(s)
	}
	word := "consejos nuevos"
	if unread == 1 {
		word = "consejo nuevo"
	}
	return fmt.Sprintf("Compañero WoW · %d %s", unread, word)
}

// unreadOffset is the character offset, in the conversation text, where entry idx starts. It
// counts the UTF-16 units of the entries before it, as the rich edit does.
func unreadOffset(entries []entry, idx int) int {
	off := 0
	for i := 0; i < idx && i < len(entries); i++ {
		off += runsUnits(entryRuns(entries[i]))
	}
	return off
}

// runsUnits is the length of the text of the spans in UTF-16 units.
func runsUnits(runs []Span) int {
	n := 0
	for _, r := range runs {
		n += len(utf16.Encode([]rune(r.Text)))
	}
	return n
}
