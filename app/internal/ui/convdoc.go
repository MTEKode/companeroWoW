package ui

// The conversation as text: each entry is a run of text in the rich edit, and the streamed answer
// in progress is one entry that is rewritten in place. convDoc knows where every entry sits, in
// the units the rich edit counts for EM_SETSEL and EM_REPLACESEL (one per UTF-16 code unit, a line
// break counts one). It makes no Win32 calls: each change returns the textEdit that the window
// applies to the rich edit, so the positions are decided here and tested without a window.

// cancelledText is what a cancelled answer leaves in the conversation.
const cancelledText = "(cancelada)"

// textEdit replaces the units [Start, End) of the conversation with Runs. An insertion has
// Start == End. Total is the length of the conversation before the edit.
type textEdit struct {
	Start, End int
	Runs       []Span
	Total      int
}

// units is how many units the edit leaves in the conversation.
func (e textEdit) units() int {
	return runsUnits(e.Runs)
}

// adjust moves a position of the conversation (a caret or the start of a selection) across the
// edit: before the edit it stays, after it it moves with the text, inside the replaced range it
// stays at the same offset from the start, within the new text.
func (e textEdit) adjust(p int) int {
	switch {
	case p <= e.Start:
		return p
	case p >= e.End:
		return p - (e.End - e.Start) + e.units()
	default:
		off := p - e.Start
		if n := e.units(); off > n {
			off = n
		}
		return e.Start + off
	}
}

// convDoc is the conversation shown in the window: the entries, the length of each one as it is
// rendered, and the entry of the streamed answer in progress (stream, -1 when none).
//
// The streamed answer is one entry from beginStream until finishStream or cancelStream. Each
// piece of its text rewrites that entry only; entries added meanwhile (tips, notices) go after it
// and keep their place, so the in-progress block never multiplies.
type convDoc struct {
	entries []entry
	units   []int // units[i] is the length of entries[i] as rendered
	stream  int   // the streamed answer in progress, -1 when none
}

func newConvDoc() *convDoc {
	return &convDoc{stream: -1}
}

// total is the length of the whole conversation in units.
func (d *convDoc) total() int {
	n := 0
	for _, u := range d.units {
		n += u
	}
	return n
}

// start is where entry i begins.
func (d *convDoc) start(i int) int {
	n := 0
	for j := 0; j < i; j++ {
		n += d.units[j]
	}
	return n
}

// setEntries replaces the whole conversation (the history loaded at start-up) and returns its
// text as runs, from the start.
func (d *convDoc) setEntries(list []entry) []Span {
	d.entries = list
	d.stream = -1
	return d.render()
}

// clear empties the conversation, for example after the history is cleared.
func (d *convDoc) clear() {
	d.entries = nil
	d.units = nil
	d.stream = -1
}

// render returns the text of the whole conversation as runs, and records the length of each entry.
func (d *convDoc) render() []Span {
	d.units = make([]int, len(d.entries))
	var out []Span
	for i, e := range d.entries {
		runs := entryRuns(e)
		d.units[i] = runsUnits(runs)
		out = append(out, runs...)
	}
	return out
}

// add appends an entry at the end of the conversation.
func (d *convDoc) add(e entry) textEdit {
	at := d.total()
	runs := entryRuns(e)
	d.entries = append(d.entries, e)
	d.units = append(d.units, runsUnits(runs))
	return textEdit{Start: at, End: at, Runs: runs, Total: at}
}

// replace rewrites entry i.
func (d *convDoc) replace(i int, e entry) textEdit {
	at := d.start(i)
	old := d.units[i]
	total := d.total()
	runs := entryRuns(e)
	d.entries[i] = e
	d.units[i] = runsUnits(runs)
	return textEdit{Start: at, End: at + old, Runs: runs, Total: total}
}

// beginStream adds the answer that is about to stream, still empty, and makes it the one in
// progress. at is its time, shown in its line.
func (d *convDoc) beginStream(at string) textEdit {
	ed := d.add(entry{Kind: entryAssistant, At: at})
	d.stream = len(d.entries) - 1
	return ed
}

// streamText adds text to the answer in progress; reset first voids the text it has (the answer
// started again). ok is false when there is nothing to show: no answer in progress, or no change.
func (d *convDoc) streamText(text string, reset bool) (ed textEdit, ok bool) {
	if d.stream < 0 || (text == "" && !reset) {
		return textEdit{}, false
	}
	e := d.entries[d.stream]
	if reset {
		e.Text = ""
	}
	e.Text += text
	return d.replace(d.stream, e), true
}

// finishStream replaces the answer in progress with its final entry. Without one in progress, the
// entry is added at the end.
func (d *convDoc) finishStream(e entry) textEdit {
	if d.stream < 0 || d.stream >= len(d.entries) {
		d.stream = -1
		return d.add(e)
	}
	i := d.stream
	d.stream = -1
	return d.replace(i, e)
}

// cancelStream turns the answer in progress into a note that it was cancelled. ok is false when
// no answer is in progress.
func (d *convDoc) cancelStream() (ed textEdit, ok bool) {
	if d.stream < 0 {
		return textEdit{}, false
	}
	return d.finishStream(entry{Kind: entryInfo, Text: cancelledText}), true
}
