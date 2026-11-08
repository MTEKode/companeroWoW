package eval

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// fakeAsker answers from a map keyed by the question text. Unknown questions get an error.
type fakeAsker struct {
	answers  map[string]string
	provider string
	rec      *Recorder
	calls    map[string][]tools.CallRecord
	asked    []string
	shots    []bool
}

func (f *fakeAsker) Answer(_ context.Context, q string, shot bool) (ai.Response, error) {
	f.asked = append(f.asked, q)
	f.shots = append(f.shots, shot)
	for _, c := range f.calls[q] {
		f.rec.Observe(c)
	}
	text, ok := f.answers[q]
	if !ok {
		return ai.Response{}, errors.New("sin respuesta de prueba")
	}
	return ai.Response{Text: text, Provider: f.provider}, nil
}

func question(id string) Question {
	for _, q := range Questions {
		if q.ID == id {
			return q
		}
	}
	panic("unknown question " + id)
}

func checkOK(checks []Check, name string) (bool, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c.OK, true
		}
	}
	return false, false
}

func TestQuestionSetMatchesSpec(t *testing.T) {
	if len(Questions) != 13 {
		t.Fatalf("questions = %d, want 13", len(Questions))
	}
	seen := map[string]bool{}
	var screenshot, refusal, source, location bool
	for _, q := range Questions {
		if seen[q.ID] || q.Text == "" {
			t.Errorf("question %+v is duplicated or empty", q)
		}
		seen[q.ID] = true
		screenshot = screenshot || q.Screenshot
		refusal = refusal || q.Refusal
		source = source || q.Source
		location = location || q.Location
	}
	if !screenshot || !refusal || !source || !location {
		t.Errorf("the set must cover screen, refusal, web source and location questions")
	}
	if !strings.Contains(question("q01").Text, "¿Qué hago ahora?") {
		t.Errorf("q01 = %q", question("q01").Text)
	}
}

func TestCastellanoCheck(t *testing.T) {
	q := question("q08")
	ok, _ := checkOK(CheckAnswer(q, "Garrosh es el jefe de la Horda y vive en Orgrimmar, en la plaza de los guerreros.", nil, nil), "castellano")
	if !ok {
		t.Error("Spanish answer must pass")
	}
	ok, _ = checkOK(CheckAnswer(q, "Garrosh is the leader of the Horde and you can find him in the city of Orgrimmar.", nil, nil), "castellano")
	if ok {
		t.Error("English answer must fail")
	}
}

func TestLengthCheckHonoursDetailRequests(t *testing.T) {
	long := strings.Repeat("Paso de la guía.\n", 10)
	if ok, _ := checkOK(CheckAnswer(question("q01"), long, nil, nil), "longitud"); ok {
		t.Error("10 lines must fail the 8-line limit")
	}
	detail := Question{ID: "d", Text: "explícame", Detail: true}
	if _, has := checkOK(CheckAnswer(detail, long, nil, nil), "longitud"); has {
		t.Error("the length check is skipped when the player asked for detail")
	}
	short := "Ve a Orgrimmar.\n- Habla con Garrosh (51.2, 38.7)."
	if ok, _ := checkOK(CheckAnswer(question("q01"), short, nil, nil), "longitud"); !ok {
		t.Error("two lines must pass")
	}
}

func TestCoordinatesCheck(t *testing.T) {
	q := question("q02")
	cases := map[string]bool{
		"Entrega en Orgrimmar (51.2, 38.7).":                         true,
		"Entrega en Orgrimmar (51,2, 38,7).":                         false,
		"Entrega en Orgrimmar.\n📍 /compa marca 85 51.2 38.7 Garrosh": true,
		"Entrega en Orgrimmar, en la plaza principal.":               false,
	}
	for text, want := range cases {
		got, _ := checkOK(CheckAnswer(q, text, nil, nil), "coordenadas")
		if got != want {
			t.Errorf("coordinates in %q = %v, want %v", text, got, want)
		}
	}
}

func TestRefusalAndSourceChecks(t *testing.T) {
	refuse := question("q10")
	if ok, _ := checkOK(CheckAnswer(refuse, "No puedo ayudarte con bots: incumple los términos de servicio.", nil, nil), "rechazo"); !ok {
		t.Error("a refusal must pass the refusal check")
	}
	if ok, _ := checkOK(CheckAnswer(refuse, "Claro, aquí tienes un script de farmeo.", nil, nil), "rechazo"); ok {
		t.Error("compliance must fail the refusal check")
	}
	src := question("q09")
	if ok, _ := checkOK(CheckAnswer(src, "Consigue la montura en Orgrimmar.\nFuente: warcraft.wiki.gg", nil, nil), "fuente"); !ok {
		t.Error("a Fuente line must pass")
	}
	if ok, _ := checkOK(CheckAnswer(src, "Consigue la montura en Orgrimmar.", nil, nil), "fuente"); ok {
		t.Error("no source must fail")
	}
}

func TestToolChecksAndLimit(t *testing.T) {
	q := question("q01")
	calls := []ToolCall{{Name: "contexto_jugador"}}
	if ok, _ := checkOK(CheckAnswer(q, "x", calls, nil), "herramientas"); !ok {
		t.Error("contexto_jugador satisfies q01")
	}
	if ok, _ := checkOK(CheckAnswer(q, "x", []ToolCall{{Name: "buscar_web"}}, nil), "herramientas"); ok {
		t.Error("buscar_web alone must not satisfy q01")
	}
	many := make([]ToolCall, maxToolCalls+1)
	if ok, _ := checkOK(CheckAnswer(q, "x", many, nil), "pocas_herramientas"); ok {
		t.Error("more than the limit of tool calls must fail")
	}
	if _, has := checkOK(CheckAnswer(question("q10"), "No puedo.", nil, nil), "herramientas"); has {
		t.Error("the refusal question has no tool requirement")
	}
}

func TestRunRecordsAnswersToolsAndChecks(t *testing.T) {
	rec := &Recorder{}
	fake := &fakeAsker{
		provider: "codex",
		rec:      rec,
		answers: map[string]string{
			"¿Qué hago ahora?": "Entrega la misión en Orgrimmar (51.2, 38.7).\n📍 /compa marca 85 51.2 38.7 Garrosh",
		},
		calls: map[string][]tools.CallRecord{
			"¿Qué hago ahora?": {{Name: "contexto_jugador", Args: "{}", Text: "{\"ubicacion\": {\"x\": 51.2, \"y\": 38.7}}", Duration: 3 * time.Millisecond}},
		},
	}
	qs := []Question{question("q01"), {ID: "x", Text: "¿Pregunta que falla?"}}
	rep := Run(context.Background(), fake, rec.Drain, qs, "codex", "abc123")
	if rep.Total != 2 || rep.Passed != 1 {
		t.Fatalf("total/passed = %d/%d", rep.Total, rep.Passed)
	}
	first := rep.Records[0]
	if first.Provider != "codex" || !first.Passed || len(first.Tools) != 1 || first.Tools[0].Name != "contexto_jugador" {
		t.Errorf("first record = %+v", first)
	}
	if first.Tools[0].Output == "" || first.Tools[0].DurationMs <= 0 {
		t.Errorf("tool output or duration missing: %+v", first.Tools[0])
	}
	second := rep.Records[1]
	if second.Passed || second.Error == "" {
		t.Errorf("failed ask must be recorded with its error: %+v", second)
	}
	if ok, _ := checkOK(second.Checks, "respuesta"); ok {
		t.Error("an ask error fails the respuesta check")
	}
	if rep.SkillVersion != "abc123" || rep.RequestProvider != "codex" {
		t.Errorf("report header = %+v", rep)
	}
}

func TestRunPassesScreenshotFlagAndKeepsCallsSeparate(t *testing.T) {
	rec := &Recorder{}
	fake := &fakeAsker{rec: rec, provider: "api:x", answers: map[string]string{
		"¿Qué hay en mi pantalla?": "Veo el mapa de Orgrimmar.",
		"¿Qué hago con mi oro?":    "Tienes oro para entrenar.",
	}, calls: map[string][]tools.CallRecord{
		"¿Qué hay en mi pantalla?": {{Name: "capturar_pantalla"}},
	}}
	rep := Run(context.Background(), fake, rec.Drain, []Question{question("q07"), question("q06")}, "", "v")
	if !fake.shots[0] || fake.shots[1] {
		t.Errorf("screenshot flags = %v", fake.shots)
	}
	if len(rep.Records[0].Tools) != 1 || len(rep.Records[1].Tools) != 0 {
		t.Errorf("tool calls leaked between questions: %+v", rep.Records)
	}
}

func TestRecorderIsSafeAndDrains(t *testing.T) {
	r := &Recorder{}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			r.Observe(tools.CallRecord{Name: "a"})
		}
		close(done)
	}()
	for i := 0; i < 100; i++ {
		_ = r.Drain()
	}
	<-done
	if n := len(r.Drain()); n > 100 {
		t.Fatalf("drained %d records, more than were observed", n)
	}
	if len(r.Drain()) != 0 {
		t.Error("Drain must clear the records")
	}
}

func TestMarkdownAndJSONReports(t *testing.T) {
	rep := Report{
		StartedAt: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC), RequestProvider: "codex", SkillVersion: "v1",
		Total: 1, Passed: 1,
		Records: []Record{{
			ID: "q01", Question: "¿Qué hago | ahora?", Provider: "codex", DurationSec: 4.2, Passed: true,
			Tools:  []ToolCall{{Name: "contexto_jugador", Args: "{}", DurationMs: 3}},
			Answer: "Entrega (51.2, 38.7).",
			Checks: []Check{{Name: "longitud", OK: true, Detail: "1 líneas"}},
		}},
	}
	md := Markdown(rep)
	for _, want := range []string{"# Evaluación de Compañero WoW", "| q01 ¿Qué hago \\| ahora? | codex | 4.2 | 1 | sí |", "`contexto_jugador`", "> Entrega (51.2, 38.7)."} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	data, err := JSON(rep)
	if err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(data, &back); err != nil || back.Records[0].Answer != "Entrega (51.2, 38.7)." {
		t.Fatalf("JSON round trip failed: %v", err)
	}
}

func TestHasCoordinatesAndRefuses(t *testing.T) {
	if !HasCoordinates("(45.0, 67.2)") || !HasCoordinates("Estás en (45.5, 67.2).") {
		t.Error("a pair with one decimal in the answer format must count")
	}
	if HasCoordinates("(45, 67)") || HasCoordinates("(45.5,67.2)") {
		t.Error("pairs outside the answer format must not count")
	}
	if HasCoordinates("(hola)") {
		t.Error("text in parentheses is not a coordinate")
	}
	if Refuses("Te ayudo con gusto.") {
		t.Error("a helpful answer is not a refusal")
	}
}
