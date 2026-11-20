package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// spanishSheetForNames is the level 25 sheet of a Spanish client (testdata/sheet-warrior-l25-es.json).
func spanishSheetForNames(t *testing.T) theory.Sheet {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sheet-warrior-l25-es.json"))
	if err != nil {
		t.Fatal(err)
	}
	sh, err := theory.ParseSheet(raw)
	if err != nil {
		t.Fatal(err)
	}
	return sh
}

// The answers name the talents and the abilities as the client shows them: the sheet's name of the same
// spell id. English stays for a name the sheet does not have.
func TestAnswersUseTheSheetNamesOfTheClient(t *testing.T) {
	sh := spanishSheetForNames(t)
	tr := namesOf(sh)
	deflection := tr("Deflection")
	if deflection == "Deflection" || deflection == "" {
		t.Fatalf("Deflection = %q: the Spanish client name is missing", deflection)
	}
	if got := tr("No Such Talent"); got != "No Such Talent" {
		t.Fatalf("unknown name = %q, want the English name back", got)
	}
	if got := localLabel("Deflection 2/5", tr); got != deflection+" 2/5" {
		t.Fatalf("label = %q, want %q", got, deflection+" 2/5")
	}
	rep := theory.Report{Talentos: map[string]int{"Deflection": 2, "No Such Talent": 1}, Rotacion: []string{"Rend", "Heroic Strike"}}
	loc := localReport(rep, tr)
	if loc.Talentos[deflection] != 2 || loc.Talentos["No Such Talent"] != 1 {
		t.Fatalf("talents = %v", loc.Talentos)
	}
	if strings.Contains(strings.Join(loc.Rotacion, "|"), "Heroic Strike") {
		t.Fatalf("rotation = %v: Heroic Strike must be in the client's name", loc.Rotacion)
	}
	if rep.Talentos["Deflection"] != 2 || rep.Rotacion[0] != "Rend" {
		t.Fatal("localReport changed the original report")
	}
	plan := theory.TalentPlan{
		Actual: theory.Report{Talentos: map[string]int{"Deflection": 3}},
		Top:    []theory.TalentOption{{Orden: []string{"Deflection 1/5"}, Arboles: map[string]int{"Armas": 1}}},
	}
	view := talentPlanView(plan, tr)
	if view.TalentosActual[deflection] != 3 {
		t.Fatalf("talentosActuales = %v", view.TalentosActual)
	}
	if len(view.Opciones) != 1 || view.Opciones[0].Orden[0] != deflection+" 1/5" {
		t.Fatalf("ordenPuntos = %v", view.Opciones)
	}
}
