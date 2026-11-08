package eval

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

func evWith(text string) Evidence {
	return Evidence{Output: text, Calls: []ToolCall{{Name: "guia_mision", Output: text}}}
}

func TestGroundingPassesWhenTheToolOutputHoldsThePair(t *testing.T) {
	out := "{\n  \"nombre\": \"Magistrado Solomon\",\n  \"x\": 18.7,\n  \"y\": 46.0\n}"
	c := groundingCheck("Entrega en Villa del Lago (18.7, 46.0).", evWith(out))
	if !c.OK {
		t.Fatalf("a pair in the output must pass: %+v", c)
	}
}

func TestGroundingFailsForAnInventedPair(t *testing.T) {
	// The second run's hallucinations: (39.9, 48.5) was in no tool output.
	out := "{\"zona\": \"Bosque de Elwynn\", \"x\": 41.5, \"y\": 54.7}"
	c := groundingCheck("Herborista en Bosque de Elwynn, coordenadas (39.9, 48.5).", evWith(out))
	if c.OK {
		t.Fatalf("an invented pair must fail: %+v", c)
	}
	if !strings.Contains(c.Detail, "(39.9, 48.5)") {
		t.Errorf("the detail must name the invented pair: %q", c.Detail)
	}
}

func TestGroundingFailsForAnInventedMarkCommand(t *testing.T) {
	out := `{"x": 41.5, "y": 54.7}`
	if c := groundingCheck("📍 /compa marca 39.9 48.5 Herborista Pomeroy", evWith(out)); c.OK {
		t.Errorf("a mark command with coordinates that no tool holds must fail: %+v", c)
	}
	if c := groundingCheck("📍 /compa marca 41.5 54.7 Cofre", evWith(out)); !c.OK {
		t.Errorf("a grounded mark command must pass: %+v", c)
	}
}

func TestGroundingReadsTheMapIDFormOfTheMarkCommand(t *testing.T) {
	out := `{"mapId": 1453, "x": 53.2, "y": 81.4}`
	if c := groundingCheck("📍 /compa marca 1453 53.2 81.4 Georgio Bolero", evWith(out)); !c.OK {
		t.Errorf("the mapID form must read x and y after the map: %+v", c)
	}
	if c := groundingCheck("📍 /compa marca 1453 53.9 81.4 Georgio Bolero", evWith(out)); c.OK {
		t.Errorf("a changed x must fail: %+v", c)
	}
}

func TestGroundingToleranceIsTwoTenths(t *testing.T) {
	out := `{"x": 46.0, "y": 18.7}`
	if c := groundingCheck("(46.2, 18.5)", evWith(out)); !c.OK {
		t.Errorf("0.2 away must pass: %+v", c)
	}
	if c := groundingCheck("(46.3, 18.7)", evWith(out)); c.OK {
		t.Errorf("0.3 away must fail: %+v", c)
	}
}

func TestGroundingAcceptsZeroToOneCoordinatesOnTheMapScale(t *testing.T) {
	out := `{"x": 0.415, "y": 0.547}`
	if c := groundingCheck("(41.5, 54.7)", evWith(out)); !c.OK {
		t.Errorf("0..1 values are read on the 0..100 scale too: %+v", c)
	}
}

func TestGroundingNeedsBothValuesNearEachOther(t *testing.T) {
	far := `{"x": 41.5, "relleno": "` + strings.Repeat("a", 400) + `", "y": 54.7}`
	if c := groundingCheck("(41.5, 54.7)", evWith(far)); c.OK {
		t.Errorf("x and y far apart are not one place: %+v", c)
	}
}

func TestGroundingWithoutCoordinatesPasses(t *testing.T) {
	c := groundingCheck("Habla con el entrenador de tu zona.", evWith(""))
	if !c.OK || c.Detail != "sin coordenadas que comprobar" {
		t.Errorf("an answer without coordinates has nothing to check: %+v", c)
	}
	if c := groundingCheck("/compa marca borrar", evWith("")); !c.OK {
		t.Errorf("removing a mark is not a place: %+v", c)
	}
}

func TestGroundingUsesThePlayerContextToo(t *testing.T) {
	ev := Evidence{Injected: `{"ubicacion":{"x":66.5,"y":77.5}}`}
	if c := groundingCheck("Estás en (66.5, 77.5).", ev); !c.OK {
		t.Errorf("the pre-injected context is evidence: %+v", c)
	}
}

func TestGroundingIsInTheAnswerChecks(t *testing.T) {
	q := Question{ID: "q", Text: "¿dónde?", Location: true}
	checks := CheckAnswer(q, "Ve a (39.9, 48.5).", nil, nil)
	c, ok := checkNamed(checks, "grounding")
	if !ok || c.OK {
		t.Errorf("CheckAnswer must include the grounding check and fail the invented pair: %+v", checks)
	}
}

func TestToolCreditForInjectedContextAndScreenshot(t *testing.T) {
	q06 := question("q06") // ¿Qué hago con mi oro? accepts obtener_dinero or contexto_jugador
	if c, _ := checkNamed(checkAnswer(q06, "Ahorra tu oro.", Evidence{Injected: `{"dinero":{"oro":12}}`}, nil), "herramientas"); !c.OK {
		t.Errorf("pre-injected player context must count as contexto_jugador: %+v", c)
	}
	if c, _ := checkNamed(checkAnswer(q06, "Ahorra tu oro.", Evidence{}, nil), "herramientas"); c.OK {
		t.Errorf("no context and no call must fail: %+v", c)
	}
	q07 := question("q07")
	if c, _ := checkNamed(checkAnswer(q07, "Veo la ventana de entrenamiento.", Evidence{Screenshot: true}, nil), "herramientas"); !c.OK {
		t.Errorf("an attached screenshot must count as capturar_pantalla: %+v", c)
	}
	if c, _ := checkNamed(checkAnswer(q07, "Veo la ventana de entrenamiento.", Evidence{}, nil), "herramientas"); c.OK {
		t.Errorf("no screenshot and no call must fail: %+v", c)
	}
}

func TestNoisePreciseWindowAndSyncPatterns(t *testing.T) {
	bridge := []ToolCall{{Name: "estado_conexion", Output: `{"puenteConectado": true, "available": true}`}}
	if c := NoiseCheck("Abierta la ventana de entrenamiento de Cocina (51/150).", bridge, ""); !c.OK {
		t.Errorf("'ventana de entrenamiento' is not window-mode advice: %+v", c)
	}
	if c := NoiseCheck("Usa la ventana sin bordes.", bridge, ""); c.OK {
		t.Errorf("'ventana sin bordes' with the bridge connected must be noise: %+v", c)
	}
	if c := NoiseCheck("Pon el juego en modo ventana.", bridge, ""); c.OK {
		t.Errorf("'modo ventana' with the bridge connected must be noise: %+v", c)
	}
	fresh := `{"available": true, "source": "sv", "datos": {"svHaceSeg": 42}}`
	if c := NoiseCheck("Escribe /compa sync.", []ToolCall{{Name: "obtener_misiones", Output: fresh}}, ""); c.OK {
		t.Errorf("'/compa sync' with fresh data must be noise: %+v", c)
	}
}

func TestLengthCountsOnlyNonEmptyLines(t *testing.T) {
	q := Question{ID: "x", Text: "¿qué hago?"}
	five := "Primera línea.\n\nSegunda línea.\n\nTercera línea.\n\nCuarta línea.\n\nQuinta línea."
	if c, _ := checkNamed(CheckAnswer(q, five, nil, nil), "longitud"); !c.OK {
		t.Errorf("five text lines with blank lines between them must pass: %+v", c)
	}
	six := "Primera.\n\nSegunda.\n\nTercera.\n\nCuarta.\n\nQuinta.\n\nSexta."
	if c, _ := checkNamed(CheckAnswer(q, six, nil, nil), "longitud"); c.OK {
		t.Errorf("six text lines must fail: %+v", c)
	}
}

func TestRunGroundsCoordinatesInTheFullToolOutput(t *testing.T) {
	// The coordinates sit past the 400 characters kept in the report: the check must read
	// the full output, not the clipped one.
	rec := &Recorder{}
	padding := strings.Repeat("relleno ", 80)
	full := padding + `{"nombre":"Herborista Pomeroy","x":39.9,"y":48.5}`
	fake := &fakeAsker{
		provider: "codex",
		rec:      rec,
		answers: map[string]string{
			"¿Dónde está el instructor de Herboristería?": "Herborista Pomeroy en (39.9, 48.5).",
		},
		calls: map[string][]tools.CallRecord{
			"¿Dónde está el instructor de Herboristería?": {{Name: "npcs_en_mapa", Text: full, Duration: time.Millisecond}},
		},
	}
	rep := Run(context.Background(), fake, rec.Drain, []Question{question("q05")}, "codex", "v")
	c, ok := checkNamed(rep.Records[0].Checks, "grounding")
	if !ok || !c.OK {
		t.Errorf("a pair from the full output must be grounded: %+v", c)
	}
}
