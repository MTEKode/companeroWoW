package ui

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// The conversation model is platform-neutral. The rich edit is simulated here by a buffer of
// UTF-16 units: every textEdit must splice it so that it equals the rendered conversation.

// docText is the whole conversation as the rich edit holds it: line breaks are one unit each.
func docText(d *convDoc) string {
	var b strings.Builder
	for _, e := range d.entries {
		for _, r := range entryRuns(e) {
			b.WriteString(strings.ReplaceAll(r.Text, "\n", "\r"))
		}
	}
	return b.String()
}

// splice applies ed to the simulated buffer and checks the edit against the model.
func splice(t *testing.T, buf []uint16, d *convDoc, ed textEdit) []uint16 {
	t.Helper()
	if ed.Total != len(buf) {
		t.Fatalf("edit made at total %d, the buffer holds %d units", ed.Total, len(buf))
	}
	var ins []uint16
	for _, r := range ed.Runs {
		ins = append(ins, utf16.Encode([]rune(strings.ReplaceAll(r.Text, "\n", "\r")))...)
	}
	if ed.Start < 0 || ed.End < ed.Start || ed.End > len(buf) {
		t.Fatalf("edit [%d,%d) outside the buffer of %d units", ed.Start, ed.End, len(buf))
	}
	out := append(append(append([]uint16{}, buf[:ed.Start]...), ins...), buf[ed.End:]...)
	if got, want := string(utf16.Decode(out)), docText(d); got != want {
		t.Fatalf("buffer after the edit:\n%q\nwant the conversation:\n%q", got, want)
	}
	return out
}

func TestStreamedAnswerIsOneBlock(t *testing.T) {
	d := newConvDoc()
	var buf []uint16
	buf = splice(t, buf, d, d.add(entry{Kind: entryUser, Text: "¿Qué hago?"}))

	buf = splice(t, buf, d, d.beginStream("13:03"))
	for _, piece := range []string{"Escribe", " /compa", " sync\n", "en el juego", " para", " que pueda."} {
		ed, ok := d.streamText(piece, false)
		if !ok {
			t.Fatalf("delta %q: no edit", piece)
		}
		buf = splice(t, buf, d, ed)
	}

	answers := 0
	for _, e := range d.entries {
		if e.Kind == entryAssistant {
			answers++
			if want := "Escribe /compa sync\nen el juego para que pueda."; e.Text != want {
				t.Errorf("answer text = %q, want %q", e.Text, want)
			}
		}
	}
	if answers != 1 {
		t.Fatalf("%d answer entries after six deltas, want one", answers)
	}
}

func TestFinalAnswerReplacesTheStreamedBlock(t *testing.T) {
	d := newConvDoc()
	var buf []uint16
	buf = splice(t, buf, d, d.add(entry{Kind: entryUser, Text: "Hola"}))
	buf = splice(t, buf, d, d.beginStream("13:03"))
	ed, _ := d.streamText("Escribe /com", false)
	buf = splice(t, buf, d, ed)

	final := entry{Kind: entryAssistant, Text: "Escribe /compa sync.", Provider: "antigravity:gemini-4-hydrogen-high",
		At: "13:04", Corrected: true}
	buf = splice(t, buf, d, d.finishStream(final))

	if _, ok := d.streamText("más", false); ok {
		t.Error("a finished answer must take no more text")
	}
	last := d.entries[len(d.entries)-1]
	if last.Text != final.Text || last.Provider != final.Provider {
		t.Fatalf("last entry = %+v, want the final answer", last)
	}
	if n := len(d.entries); n != 2 {
		t.Fatalf("%d entries, want the question and one answer", n)
	}
	if rendered := docText(d); !strings.Contains(rendered, "(corregida)") || strings.Contains(rendered, "Escribe /com\r") {
		t.Errorf("rendered answer lacks the correction mark, or keeps the partial text:\n%q", rendered)
	}
}

func TestNoticeInsertedWhileStreamingStaysAfterTheBlock(t *testing.T) {
	d := newConvDoc()
	var buf []uint16
	buf = splice(t, buf, d, d.add(entry{Kind: entryUser, Text: "Pregunta"}))
	buf = splice(t, buf, d, d.beginStream("13:03"))
	ed, _ := d.streamText("Hola", false)
	buf = splice(t, buf, d, ed)

	// A tip arrives while the answer is still streaming: it goes after the block.
	buf = splice(t, buf, d, d.add(entry{Kind: entryTip, Title: "Mapa 🚀", Text: "Mira el mapa."}))
	ed, _ = d.streamText(" mundo", false)
	buf = splice(t, buf, d, ed)
	buf = splice(t, buf, d, d.add(entry{Kind: entryInfo, Text: "Sincronizado."}))
	ed, _ = d.streamText("!", false)
	buf = splice(t, buf, d, ed)

	buf = splice(t, buf, d, d.finishStream(entry{Kind: entryAssistant, Text: "Hola mundo!", At: "13:03"}))

	var kinds []entryKind
	for _, e := range d.entries {
		kinds = append(kinds, e.Kind)
	}
	want := []entryKind{entryUser, entryAssistant, entryTip, entryInfo}
	if len(kinds) != len(want) {
		t.Fatalf("entries = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("entries = %v, want %v", kinds, want)
		}
	}
	if d.entries[1].Text != "Hola mundo!" {
		t.Errorf("answer = %q", d.entries[1].Text)
	}
}

func TestCancelTurnsTheBlockIntoACancelledNote(t *testing.T) {
	d := newConvDoc()
	var buf []uint16
	buf = splice(t, buf, d, d.add(entry{Kind: entryUser, Text: "Pregunta"}))
	buf = splice(t, buf, d, d.beginStream("13:03"))
	ed, _ := d.streamText("Parcial", false)
	buf = splice(t, buf, d, ed)

	ed, ok := d.cancelStream()
	if !ok {
		t.Fatal("cancel with an answer in progress must edit it")
	}
	buf = splice(t, buf, d, ed)

	if last := d.entries[len(d.entries)-1]; last.Kind != entryInfo || last.Text != "(cancelada)" {
		t.Fatalf("last entry = %+v, want the cancelled note", last)
	}
	if rendered := docText(d); strings.Contains(rendered, "Parcial") || !strings.Contains(rendered, "(cancelada)") {
		t.Errorf("rendered after cancel = %q", rendered)
	}
	if _, ok := d.cancelStream(); ok {
		t.Error("a second cancel has nothing to edit")
	}
	if _, ok := d.streamText("tarde", false); ok {
		t.Error("a cancelled answer takes no more text")
	}
}

func TestRestartVoidsTheStreamedText(t *testing.T) {
	d := newConvDoc()
	var buf []uint16
	buf = splice(t, buf, d, d.beginStream("13:03"))
	ed, _ := d.streamText("abc", false)
	buf = splice(t, buf, d, ed)
	ed, ok := d.streamText("xy", true)
	if !ok {
		t.Fatal("a restart is an edit")
	}
	buf = splice(t, buf, d, ed)
	if got := d.entries[0].Text; got != "xy" {
		t.Errorf("text after restart = %q, want \"xy\"", got)
	}
	if _, ok := d.streamText("", false); ok {
		t.Error("an empty delta changes nothing")
	}
}

func TestHistoryLoadedThenStreamedKeepsPositions(t *testing.T) {
	d := newConvDoc()
	runs := d.setEntries([]entry{
		{Kind: entryUser, Text: "Hola 😀\nqué tal"},
		{Kind: entryAssistant, Text: "Bien.\n\nGracias.", Provider: "codex", At: "10:00"},
		{Kind: entryError, Text: "Fallo"},
	})
	var buf []uint16
	for _, r := range runs {
		buf = append(buf, utf16.Encode([]rune(strings.ReplaceAll(r.Text, "\n", "\r")))...)
	}
	if got, want := string(utf16.Decode(buf)), docText(d); got != want {
		t.Fatalf("history render = %q, want %q", got, want)
	}
	buf = splice(t, buf, d, d.beginStream("10:01"))
	ed, _ := d.streamText("Aquí 🚀", false)
	buf = splice(t, buf, d, ed)
	buf = splice(t, buf, d, d.add(entry{Kind: entryInfo, Text: "nota"}))
	ed, _ = d.streamText("\nlínea", false)
	buf = splice(t, buf, d, ed)
	buf = splice(t, buf, d, d.finishStream(entry{Kind: entryAssistant, Text: "Final.", At: "10:01"}))

	d.clear()
	if d.total() != 0 || len(d.entries) != 0 || d.stream != -1 {
		t.Fatalf("after clear: total %d, %d entries, stream %d", d.total(), len(d.entries), d.stream)
	}
}

func TestAdjustKeepsTheUsersPlaceAcrossAnEdit(t *testing.T) {
	ed := textEdit{Start: 10, End: 20, Runs: []Span{{Text: "abcde"}}} // 20 units become 5
	cases := []struct{ p, want int }{
		{5, 5},   // before the edit: unchanged
		{10, 10}, // at its start
		{15, 15}, // inside the replaced text: same offset, within the new text
		{20, 15}, // at its end: moves with the text
		{30, 25}, // after it
	}
	for _, c := range cases {
		if got := ed.adjust(c.p); got != c.want {
			t.Errorf("adjust(%d) = %d, want %d", c.p, got, c.want)
		}
	}
	ins := textEdit{Start: 4, End: 4, Runs: []Span{{Text: "xy"}}}
	if got := ins.adjust(4); got != 4 {
		t.Errorf("caret at an insertion point stays before it: %d", got)
	}
	if got := ins.adjust(6); got != 8 {
		t.Errorf("after an insertion: %d, want 8", got)
	}
}

func TestUnitsCountLineBreaksAndSurrogatesAsTheControlDoes(t *testing.T) {
	if got := runsUnits([]Span{{Text: "a\n"}, {Text: "💡"}}); got != 4 {
		t.Errorf("units = %d, want 4 (a, line break, two for the emoji)", got)
	}
}
