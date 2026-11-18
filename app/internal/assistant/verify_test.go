package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// scriptStep is one provider answer of a scripted provider, with the tool calls it made.
type scriptStep struct {
	resp  ai.Response
	err   error
	calls []tools.CallRecord
}

// scriptAsker answers the requests in order. Each step's tool calls are fed to the captures,
// as the MCP server would while the provider works.
type scriptAsker struct {
	steps []scriptStep
	reqs  []ai.Request
	caps  *tools.Captures
}

func (s *scriptAsker) Ask(_ context.Context, req ai.Request) (ai.Response, error) {
	s.reqs = append(s.reqs, req)
	i := len(s.reqs) - 1
	if i >= len(s.steps) {
		return ai.Response{}, errors.New("sin guion para esta petición")
	}
	st := s.steps[i]
	if s.caps != nil {
		for _, c := range st.calls {
			s.caps.Observe(c)
		}
	}
	return st.resp, st.err
}

// toolOutput is a call record whose text is the given tool output.
func toolOutput(name, text string) tools.CallRecord {
	return tools.CallRecord{Name: name, Text: text}
}

// newVerifyAssistant builds an assistant over the script with captures and the given config.
func newVerifyAssistant(t *testing.T, steps []scriptStep, cfg *config.Config) (*Assistant, *scriptAsker) {
	t.Helper()
	caps := tools.NewCaptures()
	fake := &scriptAsker{steps: steps, caps: caps}
	a := newWithAsker(fake, nil, openStore(t), cfg)
	a.SetCaptures(caps)
	return a, fake
}

func verifiedCfg() *config.Config {
	c := config.Defaults()
	c.VerifyAnswers = true
	return &c
}

// groundedOutput is a tool output that holds the coordinates 51.3 and 38.5.
const groundedOutput = `{"x": 51.3, "y": 38.5, "zona": "Orgrimmar"}`

func TestGroundedAnswerIsShownAsItIs(t *testing.T) {
	steps := []scriptStep{{
		resp:  ai.Response{Text: "Entrega en Orgrimmar.\n📍 /compa marca 85 51.3 38.5 Entregar", Provider: "codex"},
		calls: []tools.CallRecord{toolOutput("guia_mision", groundedOutput)},
	}}
	a, fake := newVerifyAssistant(t, steps, verifiedCfg())
	resp, err := a.Ask(context.Background(), "¿Dónde entrego?", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.reqs) != 1 {
		t.Fatalf("a grounded answer must not be repaired: %d requests", len(fake.reqs))
	}
	if strings.Contains(resp.Text, verifyWarning) {
		t.Errorf("no footer for a grounded answer: %q", resp.Text)
	}
	if st := a.VerifyStats(); st.Answers != 1 || st.Violations != 0 || st.Repaired != 0 {
		t.Errorf("stats = %+v", st)
	}
}

func TestUnbackedAnswerIsRepairedOnceWithTheTools(t *testing.T) {
	steps := []scriptStep{
		{resp: ai.Response{Text: "Ve a (12.3, 45.6) y entrega.", Provider: "codex"}},
		{
			resp:  ai.Response{Text: "Ve a (51.3, 38.5) y entrega.", Provider: "codex"},
			calls: []tools.CallRecord{toolOutput("guia_mision", groundedOutput)},
		},
	}
	a, fake := newVerifyAssistant(t, steps, verifiedCfg())
	resp, err := a.Ask(context.Background(), "¿Dónde entrego?", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.reqs) != 2 {
		t.Fatalf("requests = %d, want the question and one repair", len(fake.reqs))
	}
	repair := fake.reqs[1].Prompt
	if !strings.HasPrefix(repair, "Corrige solo estos datos no respaldados") || !strings.Contains(repair, "(12.3, 45.6)") {
		t.Errorf("repair prompt = %q", repair)
	}
	if strings.Contains(repair, "(51.3, 38.5)") {
		t.Error("the repair must ask about the unbacked data, not repeat the grounded answer")
	}
	if !strings.Contains(resp.Text, "(51.3, 38.5)") || strings.Contains(resp.Text, verifyWarning) {
		t.Errorf("the repaired answer must be shown without footer: %q", resp.Text)
	}
	if st := a.VerifyStats(); st.Answers != 1 || st.Violations != 1 || st.Repaired != 1 || st.Flagged != 0 {
		t.Errorf("stats = %+v", st)
	}
}

func TestStillUnbackedAfterRepairGetsAFooterAndOnlyOneRepair(t *testing.T) {
	steps := []scriptStep{
		{resp: ai.Response{Text: "Ve a (12.3, 45.6).", Provider: "codex"}},
		{resp: ai.Response{Text: "Ve a (99.9, 1.1).", Provider: "codex"}},
		{resp: ai.Response{Text: "nunca se pide", Provider: "codex"}},
	}
	a, fake := newVerifyAssistant(t, steps, verifiedCfg())
	resp, err := a.Ask(context.Background(), "¿Dónde?", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.reqs) != 2 {
		t.Fatalf("requests = %d, want at most one repair", len(fake.reqs))
	}
	if !strings.Contains(resp.Text, verifyWarning) {
		t.Fatalf("an unbacked answer must carry the warning: %q", resp.Text)
	}
	// The repaired text had as many unbacked claims as the first, so the first is kept and
	// the footer names what it claims.
	if !strings.Contains(resp.Text, "(12.3, 45.6)") {
		t.Errorf("footer answer = %q", resp.Text)
	}
	if st := a.VerifyStats(); st.Violations != 1 || st.Repaired != 0 || st.Flagged != 1 || st.Claims < 1 {
		t.Errorf("stats = %+v", st)
	}
}

func TestRepairUsesTheBetterAnswerWhenItHasFewerClaims(t *testing.T) {
	steps := []scriptStep{
		{resp: ai.Response{Text: "Ve a (12.3, 45.6) y a (70.1, 80.2).", Provider: "codex"}},
		{resp: ai.Response{Text: "Ve a (70.1, 80.2).", Provider: "codex"}},
	}
	a, _ := newVerifyAssistant(t, steps, verifiedCfg())
	resp, err := a.Ask(context.Background(), "¿Dónde?", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resp.Text, "(12.3, 45.6)") {
		t.Errorf("the repaired answer had fewer unbacked claims and must be shown: %q", resp.Text)
	}
	if !strings.Contains(resp.Text, verifyWarning) || !strings.Contains(resp.Text, "(70.1, 80.2)") {
		t.Errorf("the still-unbacked repaired answer must carry a footer naming its claim: %q", resp.Text)
	}
}

func TestFailedRepairKeepsTheOriginalWithFooter(t *testing.T) {
	steps := []scriptStep{
		{resp: ai.Response{Text: "Ve a (12.3, 45.6).", Provider: "codex"}},
		{err: errors.New("tiempo agotado")},
	}
	a, _ := newVerifyAssistant(t, steps, verifiedCfg())
	resp, err := a.Ask(context.Background(), "¿Dónde?", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.Text, "Ve a (12.3, 45.6).") || !strings.Contains(resp.Text, verifyWarning) {
		t.Errorf("answer after a failed repair = %q", resp.Text)
	}
}

func TestNoRepairWhenTheDeadlineIsClose(t *testing.T) {
	steps := []scriptStep{
		{resp: ai.Response{Text: "Ve a (12.3, 45.6).", Provider: "codex"}},
		{resp: ai.Response{Text: "no debería llegar", Provider: "codex"}},
	}
	a, fake := newVerifyAssistant(t, steps, verifiedCfg())
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer cancel()
	resp, err := a.Ask(ctx, "¿Dónde?", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.reqs) != 1 {
		t.Errorf("requests = %d; a repair must not start with %v left", len(fake.reqs), 5*time.Second)
	}
	if !strings.Contains(resp.Text, verifyWarning) {
		t.Errorf("the answer must still carry the footer: %q", resp.Text)
	}
}

func TestVerifierOffShowsTheAnswerAsIs(t *testing.T) {
	c := config.Defaults()
	c.VerifyAnswers = false
	steps := []scriptStep{{resp: ai.Response{Text: "Ve a (12.3, 45.6).", Provider: "codex"}}}
	a, fake := newVerifyAssistant(t, steps, &c)
	resp, err := a.Ask(context.Background(), "¿Dónde?", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.reqs) != 1 || resp.Text != "Ve a (12.3, 45.6)." {
		t.Errorf("with verifyAnswers off the answer is untouched: %d requests, %q", len(fake.reqs), resp.Text)
	}
	if st := a.VerifyStats(); st.Answers != 0 {
		t.Errorf("no answer may be counted with the verifier off: %+v", st)
	}
}

func TestQuotedNamesAndBarCommandsAreGrounded(t *testing.T) {
	// A quoted name that no tool gave is a violation; a short quoted word is style.
	steps := []scriptStep{
		{resp: ai.Response{Text: "Habla con «Garrosh Hellscream» (anota «Ya»).", Provider: "codex"}},
		{resp: ai.Response{Text: "Habla con «Thrall» (anota «Ya»).", Provider: "codex"}, calls: []tools.CallRecord{toolOutput("npcs_en_mapa", `{"nombre": "Thrall"}`)}},
	}
	a, fake := newVerifyAssistant(t, steps, verifiedCfg())
	resp, err := a.Ask(context.Background(), "¿Quién?", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.reqs) != 2 || !strings.Contains(fake.reqs[1].Prompt, "Garrosh Hellscream") {
		t.Fatalf("a quoted name with no source must start a repair: %d requests", len(fake.reqs))
	}
	correction := strings.Split(fake.reqs[1].Prompt, "Pregunta original")[0]
	if strings.Contains(correction, "Ya") {
		t.Error("a short quoted word must not be checked")
	}
	if !strings.Contains(resp.Text, "Thrall") {
		t.Errorf("the repaired answer = %q", resp.Text)
	}

	// A barras command with a spell id that no tool listed is a violation.
	steps = []scriptStep{
		{resp: ai.Response{Text: "⌨️ /compa barras aplicar 1:585", Provider: "codex"}},
		{resp: ai.Response{Text: "⌨️ /compa barras aplicar 1:585", Provider: "codex"}, calls: []tools.CallRecord{toolOutput("consejo_barras", `{"hechizo": {"id": 585, "nombre": "Dolor"}}`)}},
	}
	a, fake = newVerifyAssistant(t, steps, verifiedCfg())
	if _, err := a.Ask(context.Background(), "¿Barras?", false); err != nil {
		t.Fatal(err)
	}
	if len(fake.reqs) != 2 {
		t.Errorf("a bar command with a spell id not listed must be repaired: %d requests", len(fake.reqs))
	}
}

func TestAnswerWithoutClaimsNeedsNoRepair(t *testing.T) {
	steps := []scriptStep{{resp: ai.Response{Text: "Hola, ¿qué quieres saber?", Provider: "codex"}}}
	a, fake := newVerifyAssistant(t, steps, verifiedCfg())
	if _, err := a.Ask(context.Background(), "hola", false); err != nil {
		t.Fatal(err)
	}
	if len(fake.reqs) != 1 || a.VerifyStats().Violations != 0 {
		t.Errorf("plain answers pass without a repair: %d requests, %+v", len(fake.reqs), a.VerifyStats())
	}
}

func TestPlayerContextCountsAsEvidence(t *testing.T) {
	// The fixture character stands at (51.2, 38.7): the player context sent with the question
	// backs that place even though no tool was called.
	st := openStore(t)
	w := bridge.NewSVWatcher(fixtureLua, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll fixture: %v", err)
	}
	g := gamedata.NewService(nil, w, st)
	caps := tools.NewCaptures()
	fake := &scriptAsker{steps: []scriptStep{{resp: ai.Response{Text: "Estás en (51.2, 38.7).", Provider: "codex"}}}, caps: caps}
	a := newWithAsker(fake, g, st, verifiedCfg())
	a.SetCaptures(caps)
	resp, err := a.Ask(context.Background(), "¿Dónde estoy?", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.reqs) != 1 || strings.Contains(resp.Text, verifyWarning) {
		t.Fatalf("a place that the context holds must pass: %d requests, %q", len(fake.reqs), resp.Text)
	}
	if !strings.Contains(fake.reqs[0].System, "Contexto del jugador") {
		t.Fatalf("the context block must be in the prompt: %s", fake.reqs[0].System)
	}

	// The same place in an answer that the context does not hold is a violation.
	fake2 := &scriptAsker{steps: []scriptStep{
		{resp: ai.Response{Text: "Estás en (90.5, 10.1).", Provider: "codex"}},
		{resp: ai.Response{Text: "Estás en (51.2, 38.7).", Provider: "codex"}},
	}, caps: caps}
	a2 := newWithAsker(fake2, g, st, verifiedCfg())
	a2.SetCaptures(caps)
	if _, err := a2.Ask(context.Background(), "¿Dónde estoy?", false); err != nil {
		t.Fatal(err)
	}
	if len(fake2.reqs) != 2 {
		t.Errorf("an invented place must start a repair: %d requests", len(fake2.reqs))
	}
}

func TestProfileReachesEveryPromptAndTheTone(t *testing.T) {
	steps := []scriptStep{{resp: ai.Response{Text: "Vale.", Provider: "codex"}}}
	cfg := verifiedCfg()
	cfg.Profile = config.Profile{Goals: []string{config.GoalLevel, config.GoalGold}, Spec: "DPS", Tone: config.ToneDetailed, Hardcore: true}
	a, fake := newVerifyAssistant(t, steps, cfg)
	if _, err := a.Ask(context.Background(), "¿Qué hago?", false); err != nil {
		t.Fatal(err)
	}
	sys := fake.reqs[0].System
	for _, want := range []string{"Perfil del jugador:", "subir de nivel rápido, oro", "rol: DPS", "hardcore: sí", "hasta 8 líneas"} {
		if !strings.Contains(sys, want) {
			t.Errorf("the system prompt lacks %q:\n%s", want, sys)
		}
	}

	// A change in the settings reaches the next question without a restart.
	a.SetProfile(config.Profile{Tone: config.ToneBrief})
	steps2 := []scriptStep{{resp: ai.Response{Text: "Vale.", Provider: "codex"}}}
	fake.steps, fake.reqs = steps2, nil
	if _, err := a.Ask(context.Background(), "otra", false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fake.reqs[0].System, "Perfil del jugador") {
		t.Errorf("an empty profile must add no line: %s", fake.reqs[0].System)
	}
	if got := a.Profile(); got.Tone != config.ToneBrief {
		t.Errorf("Profile() = %+v", got)
	}
}

func TestProfileBlockAsksForBriefAnswersByDefault(t *testing.T) {
	cfg := verifiedCfg()
	cfg.Profile = config.Profile{Goals: []string{config.GoalDungeons}, Tone: config.ToneBrief}
	a, _ := newVerifyAssistant(t, nil, cfg)
	block := a.profileBlock()
	if !strings.Contains(block, "mazmorras") || !strings.Contains(block, "como mucho 5 líneas") {
		t.Errorf("block = %q", block)
	}
	if a2 := newWithAsker(&scriptAsker{}, nil, openStore(t), nil); a2.profileBlock() != "" {
		t.Error("no profile, no block")
	}
}

func TestVerifiedStatsAreJSONReadable(t *testing.T) {
	a, _ := newVerifyAssistant(t, nil, verifiedCfg())
	b, err := json.Marshal(a.VerifyStats())
	if err != nil || !strings.Contains(string(b), "respuestas") {
		t.Errorf("stats JSON = %s, %v", b, err)
	}
}

func TestTipKeepsTheWarningLineWithinItsLimit(t *testing.T) {
	long := "Primera línea.\nSegunda línea.\nTercera línea.\nCuarta línea."
	withFooter := long + "\n" + verifyWarning + "coordenadas sin respaldo: (1.0, 2.0)"
	got := limitTipLines(withFooter)
	lines := strings.Split(got, "\n")
	if len(lines) != maxTipLines || !strings.HasPrefix(lines[len(lines)-1], verifyWarning) {
		t.Errorf("tip = %q: the warning must be the last of %d lines", got, maxTipLines)
	}
	if plain := limitTipLines(long); strings.Contains(plain, verifyWarning) || len(strings.Split(plain, "\n")) != maxTipLines {
		t.Errorf("a tip without warning is limited as before: %q", plain)
	}
}
