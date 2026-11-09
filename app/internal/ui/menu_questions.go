package ui

import (
	"log"
	"sync"
)

// The header menu items that send the assistant a ready question. Each one runs through
// RegisterMenuAction; the panel sets the sink when it starts, and the item hands it the question.

// Labels of the question items, in menu order, and the questions they send.
const (
	menuRotationLabel = "Revisar rotación"
	menuMistakesLabel = "¿Qué hice mal?"
	menuBarsLabel     = "Colocar barras"

	questionRotation = "Revisa mi rotación"
	questionMistakes = "¿Qué estoy haciendo mal en los combates?"
	questionBars     = "Colócame las barras"
)

var questionSink struct {
	sync.Mutex
	send func(question string) bool
}

var registerQuestionsOnce sync.Once

// setQuestionSink sets the function that asks the assistant a question. It returns false when
// the panel cannot take it now (for example, a question is in flight).
func setQuestionSink(send func(question string) bool) {
	questionSink.Lock()
	defer questionSink.Unlock()
	questionSink.send = send
}

// sendQuestion hands the question to the panel. It reports whether the panel took it.
func sendQuestion(question string) bool {
	questionSink.Lock()
	send := questionSink.send
	questionSink.Unlock()
	return send != nil && send(question)
}

// RegisterQuestionActions adds the three assistant items to the header menu, before Salir.
// It registers them once; later calls do nothing.
func RegisterQuestionActions() {
	registerQuestionsOnce.Do(func() {
		items := []struct{ label, question string }{
			{menuRotationLabel, questionRotation},
			{menuMistakesLabel, questionMistakes},
			{menuBarsLabel, questionBars},
		}
		for _, it := range items {
			question := it.question
			RegisterMenuAction(it.label, func() {
				if !sendQuestion(question) {
					log.Printf("ui: la pregunta del menú no se pudo enviar: %q", question)
				}
			})
		}
	})
}
