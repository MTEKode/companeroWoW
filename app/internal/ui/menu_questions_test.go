package ui

import (
	"sync"
	"testing"
)

func TestRegisterQuestionActionsSendsTheQuestion(t *testing.T) {
	resetMenuActions()
	t.Cleanup(resetMenuActions)
	registerQuestionsOnce = sync.Once{}
	RegisterQuestionActions()
	RegisterQuestionActions() // a second call must not add the items again

	var sent []string
	setQuestionSink(func(q string) bool {
		sent = append(sent, q)
		return true
	})
	t.Cleanup(func() { setQuestionSink(nil) })

	want := map[string]string{
		menuRotationLabel: questionRotation,
		menuMistakesLabel: questionMistakes,
		menuBarsLabel:     questionBars,
	}
	acts := menuActions()
	if len(acts) != len(want) {
		t.Fatalf("registered %d items, want %d", len(acts), len(want))
	}
	for _, a := range acts {
		if want[a.Label] == "" {
			t.Errorf("unexpected item %q", a.Label)
		}
		a.Run()
	}
	if len(sent) != 3 || sent[0] != questionRotation || sent[1] != questionMistakes || sent[2] != questionBars {
		t.Errorf("sent = %q", sent)
	}
}

func TestQuestionWithoutSinkDoesNothing(t *testing.T) {
	setQuestionSink(nil)
	if sendQuestion("hola") {
		t.Error("no panel must not take the question")
	}
}
