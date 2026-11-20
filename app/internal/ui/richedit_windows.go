//go:build windows

package ui

import (
	"errors"
	"log"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// richEditClass is the window class of the RichEdit 5.0 control in Msftedit.dll.
const richEditClass = "RICHEDIT50W"

const (
	emGetSel       = 0x00B0
	emGetScrollPos = wmUser + 221 // POINT in pixels: where the view is
	emSetScrollPos = wmUser + 222
)

// createConversation creates the read-only RichEdit that shows the conversation. It
// reports link clicks (EN_LINK) to its parent.
func createConversation(parent, inst uintptr) (uintptr, error) {
	style := uintptr(wsChild | wsVisible | wsVScroll | esMultiline | esAutoVScroll | esReadOnly | esNoHideSel)
	cls := utf16Of(richEditClass)
	h := call(procCreateWindowExW, 0, ptrOf(&cls[0]), 0, style, 0, 0, 0, 0, parent, 0, inst, 0)
	runtime.KeepAlive(cls)
	if h == 0 {
		return 0, errors.New("no pude crear el control de texto RichEdit")
	}
	call(procSendMessageW, h, emSetEventMask, 0, enmLink)
	call(procSendMessageW, h, emSetBkgndColor, 0, uintptr(colConv))
	call(procSendMessageW, h, emSetReadOnly, 1, 0)
	return h, nil
}

// charFormatFor returns the character format for a run style at the given DPI.
func charFormatFor(s SpanStyle, dpi uint32) charFormat2 {
	pt := 10.0
	face := "Segoe UI"
	color := colText
	back := colConv
	var effects uint32
	if s&StyleMeta != 0 {
		pt = 8.5
		color = colMeta
	}
	if s&StyleGold != 0 {
		color = colHeading
	}
	if s&StyleError != 0 {
		color = colError
	}
	if s&StyleCode != 0 {
		face = "Consolas"
	}
	if s&StyleBold != 0 {
		effects |= cfeBold
	}
	if s&StyleLink != 0 {
		color = colLink
		effects |= cfeLink | cfeUnderline
	}
	if s&StyleTip != 0 {
		back = colTipBG
	}
	if s&StyleMark != 0 {
		back = colTipBG
		color = colHeading
	}
	cf := charFormat2{
		CbSize:      uint32(unsafe.Sizeof(charFormat2{})),
		DwMask:      cfmSize | cfmColor | cfmFace | cfmBackColor | cfmBold | cfmUnderline | cfmLink,
		DwEffects:   effects,
		YHeight:     int32(pt * 20 * float64(dpi) / 96),
		CrTextColor: color,
		CrBackColor: back,
	}
	copy(cf.SzFaceName[:31], utf16Of(face))
	return cf
}

// applyEdit applies one edit of the conversation (see convDoc) to the rich edit. See applyEditTo
// for follow.
func (w *window) applyEdit(ed textEdit, follow bool) {
	applyEditTo(w.conv, w.dpi, ed, follow)
}

// applyEditTo applies ed to the rich edit conv. The user keeps their place: a selection moves with
// the text, and the view stays where it is. The view follows the text when follow is set (the
// user's own question and its answer's start) or when the caret is at the end of the conversation.
func applyEditTo(conv uintptr, dpi uint32, ed textEdit, follow bool) {
	if conv == 0 {
		return
	}
	sa, sb := selectionOf(conv)
	follow = follow || (sa == sb && sa == ed.Total)
	var scroll [2]int32
	if !follow {
		call(procSendMessageW, conv, emGetScrollPos, 0, ptrOf(&scroll[0]))
	}
	replaceUnits(conv, dpi, ed.Start, ed.End, ed.Runs)
	if follow {
		end := ed.Total - (ed.End - ed.Start) + ed.units()
		setSel(conv, end, end)
		call(procSendMessageW, conv, emScrollCaret, 0, 0)
		return
	}
	setSel(conv, ed.adjust(sa), ed.adjust(sb))
	call(procSendMessageW, conv, emSetScrollPos, 0, ptrOf(&scroll[0]))
}

// replaceUnits replaces the units [start, end) of the rich edit with runs, styled one by one. The
// positions are the ones the rich edit counts for EM_SETSEL; the text it returns for its length
// is not (it counts each line break twice), so the caller's positions are never read back from it.
func replaceUnits(conv uintptr, dpi uint32, start, end int, runs []Span) {
	if end > start {
		setSel(conv, start, end)
		empty := utf16Of("")
		call(procSendMessageW, conv, emReplaceSel, 0, ptrOf(&empty[0]))
		runtime.KeepAlive(empty)
	}
	cur := start
	for _, r := range runs {
		if r.Text == "" {
			continue
		}
		setSel(conv, cur, cur)
		cf := charFormatFor(r.Style, dpi)
		call(procSendMessageW, conv, emSetCharFormat, scfSelection, ptrOf(&cf))
		text := utf16Of(strings.ReplaceAll(r.Text, "\n", "\r"))
		call(procSendMessageW, conv, emReplaceSel, 0, ptrOf(&text[0]))
		runtime.KeepAlive(text)
		cur += runsUnits([]Span{r})
	}
	if cur != start {
		if got, _ := selectionOf(conv); got != cur {
			log.Printf("ui: el texto del chat no cuadra con sus posiciones: %d, se esperaba %d", got, cur)
		}
	}
}

// renderAll rewrites the whole conversation from the entries, for example after the
// history is cleared or the DPI changes. The streamed answer stays in progress.
func (w *window) renderAll() {
	if w.conv == 0 {
		return
	}
	setWindowText(w.conv, "")
	replaceUnits(w.conv, w.dpi, 0, 0, w.doc.render())
	setSel(w.conv, w.doc.total(), w.doc.total())
	call(procSendMessageW, w.conv, emScrollCaret, 0, 0)
}

// setSel selects the units [a, b) of the conversation; a == b is the caret.
func setSel(conv uintptr, a, b int) {
	call(procSendMessageW, conv, emSetSel, uintptr(a), uintptr(b))
}

// selectionOf returns the selection of the conversation, in the units setSel uses.
func selectionOf(conv uintptr) (int, int) {
	var sel [2]uint32
	call(procSendMessageW, conv, emGetSel, ptrOf(&sel[0]), ptrOf(&sel[1]))
	return int(sel[0]), int(sel[1])
}

// linkText returns the text of a link range, which is the URL itself.
func (w *window) linkText(r charRange) string {
	n := int(r.Max - r.Min)
	if n <= 0 || n > 4096 {
		return ""
	}
	buf := make([]uint16, n+1)
	tr := textRange{Range: r, Text: &buf[0]}
	got := int(call(procSendMessageW, w.conv, emGetTextRange, 0, ptrOf(&tr)))
	if got <= 0 || got > n {
		return ""
	}
	return windows.UTF16ToString(buf[:got])
}
