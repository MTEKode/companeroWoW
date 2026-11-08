package eval

import (
	"context"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
)

func checkNamed(checks []Check, name string) (Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func TestNoiseFailsSyncWhenAddonDataIsFresh(t *testing.T) {
	calls := []ToolCall{{Name: "contexto_jugador", Output: `{"available": true, "source": "sv", "datos": {"svHaceSeg": 42}}`}}
	c := NoiseCheck("Escribe /compa sync en el juego para actualizar.", calls, "")
	if c.OK {
		t.Errorf("sync with fresh data must be noise: %+v", c)
	}
}

func TestNoiseAllowsSyncWhenTheAddonFlagsStaleData(t *testing.T) {
	calls := []ToolCall{{Name: "contexto_jugador", Output: `{"available": true, "avisos": ["Los datos de misiones o profesiones del addon tienen más de 10 minutos: pide /compa sync en el juego."]}`}}
	if c := NoiseCheck("Usa /compa sync antes de buscar la misión.", calls, ""); !c.OK {
		t.Errorf("a flagged stale quest log justifies the sync line: %+v", c)
	}
}

func TestNoiseFailsWindowModeWhenBridgeIsConnected(t *testing.T) {
	calls := []ToolCall{{Name: "estado_conexion", Output: `{"puenteConectado": true, "available": true}`}}
	if c := NoiseCheck("Usa modo ventana sin bordes.", calls, ""); c.OK {
		t.Errorf("window mode with the bridge connected must be noise: %+v", c)
	}
	if c := NoiseCheck("Usa modo ventana sin bordes.", nil, `{"puenteConectado":true}`); c.OK {
		t.Errorf("the injected context counts as evidence too: %+v", c)
	}
}

func TestNoiseFailsSyncWhenThePlayerContextCarriesTheData(t *testing.T) {
	injected := `{"ubicacion":{"zona":"Orgrimmar"},"personaje":{"nivel":18}}`
	if c := NoiseCheck("Escribe /compa sync antes de seguir.", nil, injected); c.OK {
		t.Errorf("sync with the context already holding the data must be noise: %+v", c)
	}
}

func TestNoiseAllowsWindowModeWhenBridgeIsDisconnected(t *testing.T) {
	calls := []ToolCall{{Name: "contexto_jugador", Output: `{"puenteConectado": false, "avisos": ["No sé la posición exacta: abre WoW en modo ventana o sin bordes."]}`}}
	if c := NoiseCheck("No sé la posición exacta: usa modo ventana sin bordes.", calls, ""); !c.OK {
		t.Errorf("window mode is useful without the bridge: %+v", c)
	}
}

func TestNoiseNoAdviceNoFailure(t *testing.T) {
	calls := []ToolCall{{Name: "guia_mision", Output: `{"available": true}`}}
	if c := NoiseCheck("Ve a Villa del Lago (66.5, 77.5).", calls, ""); !c.OK {
		t.Errorf("plain answer must pass: %+v", c)
	}
}

func TestLatencyIsInformational(t *testing.T) {
	slow := LatencyCheck(45)
	if slow.OK || !slow.Info {
		t.Errorf("45 s must warn: %+v", slow)
	}
	if fast := LatencyCheck(12); !fast.OK {
		t.Errorf("12 s must pass: %+v", fast)
	}
	if !allOK([]Check{{Name: "respuesta", OK: true}, slow}) {
		t.Error("an informational warning must not fail the question")
	}
}

func TestLengthLimitsFiveLinesAndFiveHundredChars(t *testing.T) {
	q := Question{ID: "x", Text: "¿qué hago?"}
	six := strings.Repeat("Línea corta de la respuesta.\n", 6)
	if c, _ := checkNamed(CheckAnswer(q, six, nil, nil), "longitud"); c.OK {
		t.Errorf("six lines must fail: %+v", c)
	}
	long := strings.Repeat("palabra ", 70) // 560 characters, three lines
	long = strings.Join([]string{long, long, long}, "\n")
	if c, _ := checkNamed(CheckAnswer(q, long, nil, nil), "longitud"); c.OK {
		t.Errorf("more than 500 characters must fail: %+v", c)
	}
	ok := "Ve a Villa del Lago (66.5, 77.5).\n- Entrega la misión.\n📍 /compa marca 66.5 77.5 Villa"
	if c, _ := checkNamed(CheckAnswer(q, ok, nil, nil), "longitud"); !c.OK {
		t.Errorf("short answer must pass: %+v", c)
	}
	detail := Question{ID: "y", Text: "explícame", Detail: true}
	if _, found := checkNamed(CheckAnswer(detail, long, nil, nil), "longitud"); found {
		t.Error("detail questions have no length check")
	}
}

func TestToolBudgetIsFourOrSixForWebResearch(t *testing.T) {
	five := make([]ToolCall, 5)
	for i := range five {
		five[i] = ToolCall{Name: "obtener_misiones"}
	}
	if c, _ := checkNamed(CheckAnswer(Question{ID: "a", Text: "x"}, "ok", five, nil), "pocas_herramientas"); c.OK {
		t.Errorf("five calls for a normal question must fail: %+v", c)
	}
	web := append(append([]ToolCall{}, five...), ToolCall{Name: "buscar_web"})
	if c, _ := checkNamed(CheckAnswer(Question{ID: "b", Text: "x"}, "ok", web, nil), "pocas_herramientas"); !c.OK {
		t.Errorf("six calls with a web search must pass: %+v", c)
	}
	seven := append(append([]ToolCall{}, web...), ToolCall{Name: "leer_pagina"})
	if c, _ := checkNamed(CheckAnswer(Question{ID: "c", Text: "x"}, "ok", seven, nil), "pocas_herramientas"); c.OK {
		t.Errorf("seven calls must fail even for web research: %+v", c)
	}
}

// fakeReporter answers with a fixed text and reports a fixed player context.
type fakeReporter struct {
	ctx string
}

func (f fakeReporter) Answer(context.Context, string, bool) (ai.Response, error) {
	return ai.Response{Text: "Ve a Villa del Lago (66.5, 77.5) y entrega la misión en la zona de la costa.", Provider: "codex"}, nil
}

func (f fakeReporter) LastContext() string { return f.ctx }

func TestRunAddsNoiseAndLatencyChecksAndReadsInjectedContext(t *testing.T) {
	asker := fakeReporter{ctx: `{"puenteConectado":true,"ubicacion":{"x":66.5,"y":77.5}}`}
	rep := Run(context.Background(), asker, nil, []Question{{ID: "q", Text: "¿dónde?", Location: true}}, "", "v")
	rec := rep.Records[0]
	if _, ok := checkNamed(rec.Checks, "ruido"); !ok {
		t.Fatalf("ruido check missing: %+v", rec.Checks)
	}
	if _, ok := checkNamed(rec.Checks, "latencia"); !ok {
		t.Fatalf("latencia check missing: %+v", rec.Checks)
	}
	if !rec.Passed {
		t.Errorf("a plain answer must pass: %+v", rec.Checks)
	}
}
