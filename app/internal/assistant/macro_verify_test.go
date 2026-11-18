package assistant

import (
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/eval"
)

// The macro rule: a name a /compa macro command puts in the macro must be in the evidence (the sheet
// or the bags that the tools returned). A chunked macro is checked once all its parts are in the answer.
func TestMacroGroundingNeedsSpellsInTheEvidence(t *testing.T) {
	sheet := `{"hechizos":[{"nombre":"Abrumar"},{"nombre":"Ejecutar"}]}`
	ev := eval.EvidenceFor([]string{sheet}, "")

	ok := "⌨️ /compa macro crear Tanque|INV_MISC_QUESTIONMARK|#showtooltip Abrumar\\n/cast [stance:2] Abrumar; Ejecutar"
	if v, bad := macroGrounding(ok, ev); bad {
		t.Fatalf("grounded macro flagged: %+v", v)
	}

	invented := "/compa macro crear Vieja|INV_MISC_QUESTIONMARK|/cast [stance:2] Overpower\\n/cast Heroic Strike"
	v, bad := macroGrounding(invented, ev)
	if !bad || v.Check != "macro" || v.Claims != 2 {
		t.Fatalf("invented names: bad=%v %+v", bad, v)
	}
	if !strings.Contains(v.Detail, "Overpower") || !strings.Contains(v.Detail, "Heroic Strike") {
		t.Errorf("detail = %q", v.Detail)
	}
	if len(groundingOf(invented, ev)) == 0 {
		t.Error("groundingOf must include the macro rule")
	}
}

func TestMacroGroundingReadsChunkedMacros(t *testing.T) {
	ev := eval.EvidenceFor([]string{`{"hechizos":[{"nombre":"Abrumar"}]}`}, "")
	parts := "/compa macro parte 1/2 Trozo|INV_X|/cast [stance:2] Overpower\\n" +
		"\n/compa macro parte 2/2 /cast Abrumar"
	if _, bad := macroGrounding(parts, ev); !bad {
		t.Fatal("complete chunked macro with an English name must be flagged")
	}
	half := "/compa macro parte 1/2 Trozo|INV_X|/cast [stance:2] Overpower"
	if _, bad := macroGrounding(half, ev); bad {
		t.Error("an incomplete chunked macro is not checked yet")
	}
}

func TestMacroGroundingIgnoresAnswersWithoutMacros(t *testing.T) {
	ev := eval.EvidenceFor(nil, "")
	if _, bad := macroGrounding("Ve a Elwynn (45.2, 67.1).", ev); bad {
		t.Error("no macro, nothing to check")
	}
}
