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
// history is cleared or the DPI changes.
func (w *window) renderAll() {
	if w.conv == 0 {
		return
	}
	setWindowText(w.conv, "")
	for _, e := range w.entries {
		w.appendRuns(entryRuns(e))
	}
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
