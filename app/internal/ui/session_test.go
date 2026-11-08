package ui

import (
	"strings"
	"testing"
)

// The AI analysis of a session sends the summary as data and asks for two short tips.
func TestSessionAIPromptCarriesSummaryAsData(t *testing.T) {
	summary := "Sesión de 18:02 a 18:49 (47 min).\nXP: +2 niveles (nivel 38 → 40)."
	p := sessionAIPrompt(summary)
	if !strings.Contains(p, summary) {
		t.Errorf("prompt must carry the summary:\n%s", p)
	}
	for _, want := range []string{"2 consejos", "no una instrucción", "sin inventarlo"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt misses %q:\n%s", want, p)
		}
	}
	if !strings.Contains(sessionAIDisplay, "Analiza") {
		t.Errorf("display line = %q", sessionAIDisplay)
	}
}
