//go:build windows

package ui

import (
	"errors"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// richEditClass is the window class of the RichEdit 5.0 control in Msftedit.dll.
const richEditClass = "RICHEDIT50W"

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

// charFormat returns the character format for a run style.
func (w *window) charFormat(s SpanStyle) charFormat2 {
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
		YHeight:     int32(pt * 20 * float64(w.dpi) / 96),
		CrTextColor: color,
		CrBackColor: back,
	}
	copy(cf.SzFaceName[:31], utf16Of(face))
	return cf
}

// appendRuns writes styled text at the end of the conversation and scrolls to it.
func (w *window) appendRuns(runs []Span) {
	for _, r := range runs {
		if r.Text == "" {
			continue
		}
		end := call(procSendMessageW, w.conv, wmGetTextLength, 0, 0)
		call(procSendMessageW, w.conv, emSetSel, end, end)
		cf := w.charFormat(r.Style)
		call(procSendMessageW, w.conv, emSetCharFormat, scfSelection, ptrOf(&cf))
		text := utf16Of(strings.ReplaceAll(r.Text, "\n", "\r"))
		call(procSendMessageW, w.conv, emReplaceSel, 0, ptrOf(&text[0]))
		runtime.KeepAlive(text)
	}
	end := call(procSendMessageW, w.conv, wmGetTextLength, 0, 0)
	call(procSendMessageW, w.conv, emSetSel, end, end)
	call(procSendMessageW, w.conv, emScrollCaret, 0, 0)
}

// renderAll rewrites the whole conversation from the entries, for example after the
// history is cleared or the DPI changes. The streamed answer keeps its place in it.
func (w *window) renderAll() {
	if w.conv == 0 {
		return
	}
	setWindowText(w.conv, "")
	for i, e := range w.entries {
		start := w.textLength()
		w.appendRuns(entryRuns(e))
		if i == w.streamIdx {
			w.streamAt = start
			w.streamLen = w.textLength() - start
		}
	}
}

// textLength is the number of UTF-16 units in the conversation.
func (w *window) textLength() int {
	return int(call(procSendMessageW, w.conv, wmGetTextLength, 0, 0))
}

// replaceRange replaces the n units at pos of the conversation with runs, and returns how many
// units the runs take. The text after the range stays where it is, so an entry written after a
// streamed answer (a tip) keeps its place. When the range ends the conversation, the view follows it.
func (w *window) replaceRange(pos, n int, runs []Span) int {
	atEnd := pos+n >= w.textLength()
	call(procSendMessageW, w.conv, emSetSel, uintptr(pos), uintptr(pos+n))
	empty := utf16Of("")
	call(procSendMessageW, w.conv, emReplaceSel, 0, ptrOf(&empty[0]))
	runtime.KeepAlive(empty)
	cur := pos
	for _, r := range runs {
		if r.Text == "" {
			continue
		}
		call(procSendMessageW, w.conv, emSetSel, uintptr(cur), uintptr(cur))
		cf := w.charFormat(r.Style)
		call(procSendMessageW, w.conv, emSetCharFormat, scfSelection, ptrOf(&cf))
		text := utf16Of(strings.ReplaceAll(r.Text, "\n", "\r"))
		before := w.textLength()
		call(procSendMessageW, w.conv, emReplaceSel, 0, ptrOf(&text[0]))
		runtime.KeepAlive(text)
		cur += w.textLength() - before
	}
	if atEnd {
		end := w.textLength()
		call(procSendMessageW, w.conv, emSetSel, uintptr(end), uintptr(end))
		call(procSendMessageW, w.conv, emScrollCaret, 0, 0)
	}
	return cur - pos
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
