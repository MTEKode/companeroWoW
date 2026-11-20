//go:build windows

package ui

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

const (
	emGetFirstVisibleLine = 0x00CE
	emLineScroll          = 0x00B6
)

var procCoUninitialize = modOle32.NewProc("CoUninitialize")

// newTestConversation creates a visible RichEdit like the conversation's, and destroys it at the
// end of the test.
func newTestConversation(t *testing.T) uintptr {
	t.Helper()
	// The control belongs to this thread: SendMessage to it from another thread would wait for a
	// message loop that never runs here.
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	// The app initializes COM on its UI thread; the control expects the same at teardown.
	call(procCoInitializeEx, 0, coinitApartmentThreaded)
	lib := utf16Of("Msftedit.dll")
	if call(procLoadLibraryW, ptrOf(&lib[0])) == 0 {
		t.Skipf("no Msftedit.dll: %v", windows.GetLastError())
	}
	style := uintptr(wsPopup | wsVisible | wsVScroll | esMultiline | esAutoVScroll | esReadOnly | esNoHideSel)
	cls := utf16Of(richEditClass)
	h := call(procCreateWindowExW, 0, ptrOf(&cls[0]), 0, style, 0, 0, 320, 140, 0, 0, 0, 0)
	runtime.KeepAlive(cls)
	if h == 0 {
		t.Fatal("no RichEdit control")
	}
	call(procSendMessageW, h, emSetReadOnly, 1, 0)
	t.Cleanup(func() {
		call(procDestroyWindow, h)
		call(procCoUninitialize)
	})
	return h
}

// controlText is the text the control shows, with line breaks as CRLF.
func controlText(h uintptr) string {
	buf := make([]uint16, 1<<16)
	n := call(procSendMessageW, h, 0x000D /* WM_GETTEXT */, uintptr(len(buf)), ptrOf(&buf[0]))
	runtime.KeepAlive(buf)
	return windows.UTF16ToString(buf[:n])
}

// The model's positions must match the control's: after every edit the control shows exactly the
// conversation the model renders. This is what the length the control reports for itself would
// get wrong, since it counts each line break twice.
func TestConversationEditsMatchTheRichEdit(t *testing.T) {
	h := newTestConversation(t)
	d := newConvDoc()
	apply := func(step string, ed textEdit) {
		t.Helper()
		applyEditTo(h, 96, ed, false)
		got := strings.ReplaceAll(controlText(h), "\r\n", "\r")
		if want := docText(d); got != want {
			t.Fatalf("%s: the control shows\n%q\nwant\n%q", step, got, want)
		}
	}

	apply("question", d.add(entry{Kind: entryUser, Text: "¿Qué hago?\nNo sé."}))
	apply("stream starts", d.beginStream("13:03"))
	for i := 0; i < 30; i++ {
		piece := fmt.Sprintf("palabra%d ", i)
		if i%10 == 9 {
			piece += "\n"
		}
		ed, ok := d.streamText(piece, false)
		if !ok {
			t.Fatalf("delta %d: no edit", i)
		}
		apply(fmt.Sprintf("delta %d", i), ed)
	}
	if n := strings.Count(controlText(h), "Compañero"); n != 1 {
		t.Fatalf("%d answer blocks after 30 deltas, want one", n)
	}

	apply("tip mid-stream", d.add(entry{Kind: entryTip, Title: "Mapa", Text: "Mira el mapa."}))
	ed, _ := d.streamText("fin", false)
	apply("delta after the tip", ed)

	final := entry{Kind: entryAssistant, Text: "Respuesta final.", Provider: "antigravity:gemini-4-hydrogen-high",
		At: "13:04", Corrected: true}
	apply("final answer", d.finishStream(final))
	if n := strings.Count(controlText(h), "Compañero"); n != 1 {
		t.Fatalf("%d answer blocks after the final answer, want one", n)
	}
	if sa, sb := selectionOf(h); sa != d.total() || sb != d.total() {
		t.Errorf("caret at (%d,%d), want the end %d: the view follows the answer", sa, sb, d.total())
	}

	apply("second question", d.add(entry{Kind: entryUser, Text: "otra"}))
	apply("second stream", d.beginStream("13:05"))
	ed, _ = d.streamText("Parcial", false)
	apply("partial", ed)
	ed, _ = d.cancelStream()
	apply("cancelled", ed)
	if text := controlText(h); strings.Contains(text, "Parcial") || !strings.Contains(text, "(cancelada)") {
		t.Errorf("after cancel the control shows %q", text)
	}
}

// A reader who scrolls up while the answer streams keeps their place and their selection. The
// answer's start follows the user's question, as it does in the window; the reader moves after it.
func TestStreamingKeepsTheReadersPlace(t *testing.T) {
	h := newTestConversation(t)
	d := newConvDoc()
	for i := 0; i < 25; i++ {
		applyEditTo(h, 96, d.add(entry{Kind: entryUser, Text: fmt.Sprintf("pregunta %d\nrespuesta corta", i)}), true)
	}
	applyEditTo(h, 96, d.beginStream("13:03"), true)
	ed, _ := d.streamText("comienzo\n", false)
	applyEditTo(h, 96, ed, false)

	// The reader goes to the top, then down a few lines: the caret stays where they put it.
	setSel(h, 0, 0)
	call(procSendMessageW, h, emScrollCaret, 0, 0)
	call(procSendMessageW, h, emLineScroll, 0, 3)
	firstLine := call(procSendMessageW, h, emGetFirstVisibleLine, 0, 0)
	if firstLine == 0 {
		t.Fatal("the conversation does not scroll in the test window; the test proves nothing")
	}

	for i := 0; i < 20; i++ {
		ed, _ := d.streamText(fmt.Sprintf("trozo %d\n", i), false)
		applyEditTo(h, 96, ed, false)
	}
	if got := call(procSendMessageW, h, emGetFirstVisibleLine, 0, 0); got != firstLine {
		t.Errorf("the view moved from line %d to %d while the answer streamed", firstLine, got)
	}
	if sa, sb := selectionOf(h); sa != 0 || sb != 0 {
		t.Errorf("the reader's selection moved to (%d,%d), want (0,0)", sa, sb)
	}
}
