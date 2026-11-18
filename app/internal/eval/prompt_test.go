package eval

import (
	"context"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
)

// promptAsker answers every question with the same text and reports the prompt stats it is given,
// one set per question in order.
type promptAsker struct {
	text  string
	stats []PromptStats
	n     int
	last  PromptStats
}

func (p *promptAsker) Answer(context.Context, string, bool) (ai.Response, error) {
	if p.n < len(p.stats) {
		p.last = p.stats[p.n]
	}
	p.n++
	return ai.Response{Text: p.text, Provider: "codex"}, nil
}

func (p *promptAsker) LastPromptStats() PromptStats { return p.last }

func TestCheckPromptPassesWhenIntentAndNeededPartsHold(t *testing.T) {
	q := Question{ID: "t", Text: "¿Qué hago ahora?", Intent: "misiones", Needs: []string{"ubicacion", "misionRastreada"}}
	st := PromptStats{Intent: "misiones", Keys: []string{"ubicacion", "misionRastreada"}, PresentKeys: []string{"ubicacion", "misionRastreada"}}
	for _, c := range CheckPrompt(q, st) {
		if !c.OK {
			t.Errorf("%s failed: %s", c.Name, c.Detail)
		}
	}
}

func TestCheckPromptFailsOnAnotherIntent(t *testing.T) {
	q := Question{ID: "t", Text: "¿Por qué he muerto?", Intent: "combate"}
	checks := CheckPrompt(q, PromptStats{Intent: "viaje"})
	if ok, found := checkOK(checks, "intencion"); !found || ok {
		t.Fatalf("a question classed as viaje must fail the intencion check: %+v", checks)
	}
}

func TestCheckPromptFailsWhenAHeldPartIsLeftOut(t *testing.T) {
	q := Question{ID: "t", Text: "x", Needs: []string{"ubicacion", "dinero"}}
	// The game holds both parts; the prompt carries only the location.
	st := PromptStats{Intent: "viaje", Keys: []string{"ubicacion"}, PresentKeys: []string{"ubicacion", "dinero"}}
	checks := CheckPrompt(q, st)
	ok, found := checkOK(checks, "contexto_necesario")
	if !found || ok {
		t.Fatalf("a held part left out of the prompt must fail: %+v", checks)
	}
	if !strings.Contains(checkDetail(checks, "contexto_necesario"), "dinero") {
		t.Errorf("the detail must name the part: %+v", checks)
	}
	// The game does not hold the gold: its absence is not a failure.
	st.PresentKeys = []string{"ubicacion"}
	if ok, _ := checkOK(CheckPrompt(q, st), "contexto_necesario"); !ok {
		t.Errorf("a part the game does not hold must not fail the check")
	}
}

func checkDetail(checks []Check, name string) string {
	for _, c := range checks {
		if c.Name == name {
			return c.Detail
		}
	}
	return ""
}

func TestRunReportsContextBeforeAndAfterPerQuestion(t *testing.T) {
	asker := &promptAsker{
		text: "Ve a Villa del Lago (66.5, 77.5).",
		stats: []PromptStats{
			{Intent: "misiones", Chars: 700, BaselineChars: 1300, Keys: []string{"ubicacion"}, PresentKeys: []string{"ubicacion"}},
			{Intent: "lore", Chars: 120, BaselineChars: 1300},
		},
	}
	qs := []Question{
		{ID: "a", Text: "¿Qué hago ahora?", Intent: "misiones", Needs: []string{"ubicacion"}},
		{ID: "b", Text: "¿Quién es Garrosh?", Intent: "lore"},
	}
	rep := Run(context.Background(), asker, nil, qs, "", "v")
	if rep.CharsBefore != 2600 || rep.CharsAfter != 820 {
		t.Fatalf("totals before %d, after %d; want 2600 and 820", rep.CharsBefore, rep.CharsAfter)
	}
	first := rep.Records[0]
	if first.Intent != "misiones" || first.CharsBefore != 1300 || first.CharsAfter != 700 {
		t.Errorf("record of a: %+v", first)
	}
	for _, r := range rep.Records {
		for _, name := range []string{"intencion"} {
			if ok, found := checkOK(r.Checks, name); !found || !ok {
				t.Errorf("%s: %s check missing or failed: %+v", r.ID, name, r.Checks)
			}
		}
	}
	md := Markdown(rep)
	for _, want := range []string{"## Contexto por pregunta", "| a | misiones | 1300 | 700 | 46 % |", "**Total**", "≈ 650 tokens"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

func TestRunWithoutPromptReporterHasNoContextChecks(t *testing.T) {
	asker := fakeReporter{ctx: ""}
	rep := Run(context.Background(), asker, nil, []Question{{ID: "q", Text: "¿qué?", Intent: "misiones"}}, "", "v")
	if _, found := checkOK(rep.Records[0].Checks, "intencion"); found {
		t.Errorf("an asker without prompt stats gets no intent check: %+v", rep.Records[0].Checks)
	}
	if rep.CharsBefore != 0 || rep.CharsAfter != 0 {
		t.Errorf("no prompt stats, no totals: %d %d", rep.CharsBefore, rep.CharsAfter)
	}
	if strings.Contains(Markdown(rep), "## Contexto por pregunta") {
		t.Errorf("the context table is written only when the run reported prompts")
	}
}
