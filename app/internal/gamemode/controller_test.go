package gamemode

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// fakeAsker answers by question text. A slow question waits until its context ends, the way a
// provider call does when a newer question cancels it.
type fakeAsker struct {
	reply   map[string]string
	fail    map[string]error
	slow    map[string]bool
	started chan string
}

func newFakeAsker() *fakeAsker {
	return &fakeAsker{reply: map[string]string{}, fail: map[string]error{}, slow: map[string]bool{}, started: make(chan string, 16)}
}

func (f *fakeAsker) Ask(ctx context.Context, question string, withScreenshot bool) (ai.Response, error) {
	if withScreenshot {
		return ai.Response{}, errors.New("the game frame never asks for a screenshot")
	}
	f.started <- question
	if f.slow[question] {
		<-ctx.Done()
		return ai.Response{}, ctx.Err()
	}
	if err := f.fail[question]; err != nil {
		return ai.Response{}, err
	}
	return ai.Response{Text: f.reply[question], Provider: "fake/model"}, nil
}

// fakeClip keeps every text the controller put on the clipboard, in order.
type fakeClip struct {
	mu    sync.Mutex
	texts []string
	err   error
}

func (c *fakeClip) SetText(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.texts = append(c.texts, text)
	return nil
}

func (c *fakeClip) last() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.texts) == 0 {
		return ""
	}
	return c.texts[len(c.texts)-1]
}

func (c *fakeClip) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.texts...)
}

// fakeSound records the cues it was asked to play.
type fakeSound struct {
	mu   sync.Mutex
	cues []Cue
}

func (s *fakeSound) Play(cue Cue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cues = append(s.cues, cue)
	return nil
}

func (s *fakeSound) played() []Cue {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Cue(nil), s.cues...)
}

type rig struct {
	asker *fakeAsker
	clip  *fakeClip
	sound *fakeSound
	ctl   *Controller
}

func newRig(t *testing.T) *rig {
	t.Helper()
	gate, err := NewGate("")
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{asker: newFakeAsker(), clip: &fakeClip{}, sound: &fakeSound{}}
	r.ctl = NewController(r.asker, r.clip, r.sound, gate,
		func() string { return "Ana" },
		func(err error) string { return "Error de prueba: " + err.Error() })
	r.ctl.logf = quietLog
	return r
}

func question(id uint64, text string) bridge.Question {
	return bridge.Question{ID: id, Text: text, MapID: 85}
}

func TestSubmitAnswersAndCopiesTheBlock(t *testing.T) {
	r := newRig(t)
	r.asker.reply["¿Dónde entrego?"] = "📍 /compa marca 51.2 38.7 Entregar\nFuente: wowforevertalents.com"
	if !r.ctl.Submit(question(4, "¿Dónde entrego?")) {
		t.Fatal("a new question must be accepted")
	}
	r.ctl.Wait()

	want := "[CWOW1 #4 fake/model]\n@marca 85 51.2 38.7 Entregar\n@fuente wowforevertalents.com\n[/CWOW1]"
	if got := r.clip.last(); got != want {
		t.Fatalf("clipboard\n got: %q\nwant: %q", got, want)
	}
	if cues := r.sound.played(); len(cues) != 1 || cues[0] != CueReady {
		t.Fatalf("cues %v, want one CueReady", cues)
	}
}

func TestSameIdIsAnsweredOnce(t *testing.T) {
	r := newRig(t)
	r.asker.reply["hola"] = "Hola."
	if !r.ctl.Submit(question(1, "hola")) {
		t.Fatal("first submit must be accepted")
	}
	if r.ctl.Submit(question(1, "hola")) {
		t.Fatal("the same question sent again must be ignored")
	}
	r.ctl.Wait()
	if n := len(r.clip.all()); n != 1 {
		t.Fatalf("clipboard written %d times, want 1", n)
	}
	if n := len(r.asker.started); n != 1 {
		t.Fatalf("asked %d times, want 1", n)
	}
}

func TestNewQuestionCancelsTheOldAndOnlyTheNewIsDelivered(t *testing.T) {
	r := newRig(t)
	r.asker.slow["primera"] = true
	r.asker.reply["segunda"] = "Respuesta a la segunda."

	r.ctl.Submit(question(1, "primera"))
	<-r.asker.started // the first question is in flight before the second arrives
	r.ctl.Submit(question(2, "segunda"))
	r.ctl.Wait()

	texts := r.clip.all()
	if len(texts) != 1 {
		t.Fatalf("clipboard written %d times, want only the newest answer: %q", len(texts), texts)
	}
	if !strings.HasPrefix(texts[0], "[CWOW1 #2 ") || strings.Contains(texts[0], "#1") {
		t.Fatalf("the wrong answer reached the clipboard: %q", texts[0])
	}
	if cues := r.sound.played(); len(cues) != 1 || cues[0] != CueReady {
		t.Fatalf("cues %v, want one CueReady for the newest answer", cues)
	}
}

func TestAnOlderAnswerNeverOverwritesANewerOne(t *testing.T) {
	r := newRig(t)
	// The first question finishes after the second has been delivered: it must be dropped.
	r.asker.slow["lenta"] = true
	r.asker.reply["rápida"] = "Rápida."
	r.ctl.Submit(question(1, "lenta"))
	<-r.asker.started
	r.ctl.Submit(question(2, "rápida"))
	r.ctl.Wait()
	r.ctl.Submit(question(3, "rápida")) // a repeat of the answered text with a new id
	r.ctl.Wait()

	for _, text := range r.clip.all() {
		if strings.Contains(text, "#1 ") {
			t.Fatalf("a replaced answer reached the clipboard: %q", text)
		}
	}
	if !strings.HasPrefix(r.clip.last(), "[CWOW1 #3 ") {
		t.Fatalf("the newest answer must win: %q", r.clip.last())
	}
}

func TestErrorGoesToTheClipboardWithTheErrorCue(t *testing.T) {
	r := newRig(t)
	r.asker.fail["x"] = errors.New("ningún proveedor de IA respondió")
	r.ctl.Submit(question(6, "x"))
	r.ctl.Wait()

	want := "[CWOW1 #6 app/error]\nError de prueba: ningún proveedor de IA respondió\n[/CWOW1]"
	if got := r.clip.last(); got != want {
		t.Fatalf("error block\n got: %q\nwant: %q", got, want)
	}
	if cues := r.sound.played(); len(cues) != 1 || cues[0] != CueError {
		t.Fatalf("cues %v, want one CueError", cues)
	}
}

func TestEmptyAnswerIsAnError(t *testing.T) {
	r := newRig(t)
	r.asker.reply["vacía"] = "   "
	r.ctl.Submit(question(7, "vacía"))
	r.ctl.Wait()
	if got := r.clip.last(); !strings.HasPrefix(got, "[CWOW1 #7 app/error]") {
		t.Fatalf("an empty answer must be an error block: %q", got)
	}
	if cues := r.sound.played(); len(cues) != 1 || cues[0] != CueError {
		t.Fatalf("cues %v, want CueError", cues)
	}
}

func TestClipboardFailureAnnouncesAnError(t *testing.T) {
	r := newRig(t)
	r.clip.err = errors.New("el portapapeles está ocupado")
	r.asker.reply["hola"] = "Hola."
	r.ctl.Submit(question(8, "hola"))
	r.ctl.Wait()
	if cues := r.sound.played(); len(cues) != 1 || cues[0] != CueError {
		t.Fatalf("a failed copy must not announce a ready answer: %v", cues)
	}
}

func TestQuestionsOfOtherCharactersAreNotDeduplicated(t *testing.T) {
	r := newRig(t)
	r.asker.reply["a"] = "A."
	r.ctl.Submit(question(1, "a"))
	r.ctl.Wait()
	r.ctl.key = func() string { return "Luis" }
	if !r.ctl.Submit(question(1, "a")) {
		t.Fatal("id 1 of another character is a new question")
	}
	r.ctl.Wait()
}

func TestParsedQuestionFlowsThroughTheController(t *testing.T) {
	// The msgType 15 body as the addon sends it, through the parser into the controller.
	r := newRig(t)
	body := []byte(`{"id":12,"text":"  ¿qué hago ahora?  ","at":1700000000,"mapID":85,"target":null}`)
	q, err := bridge.ParseQuestion(body)
	if err != nil {
		t.Fatal(err)
	}
	r.asker.reply["¿qué hago ahora?"] = "**Ve a Entregar**"
	r.ctl.Submit(q)
	r.ctl.Wait()
	if want := "[CWOW1 #12 fake/model]\n**Ve a Entregar**\n[/CWOW1]"; r.clip.last() != want {
		t.Fatalf("got %q", r.clip.last())
	}
}
