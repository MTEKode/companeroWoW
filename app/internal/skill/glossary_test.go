package skill

import (
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/glossary"
)

// The skill's zone table and the glossary package must name the same places: every English
// name of the glossary, with its Spanish name when confirmed, appears in the body.
func TestSkillBodyListsEveryGlossaryEntry(t *testing.T) {
	body := Body()
	for _, e := range glossary.Entries {
		if !strings.Contains(body, e.EN) {
			t.Errorf("SKILL.md does not mention %q", e.EN)
		}
		if e.ES != "" && !strings.Contains(body, e.ES) {
			t.Errorf("SKILL.md does not mention the Spanish name %q of %q", e.ES, e.EN)
		}
	}
}

func TestSkillCarriesTheGroundingRules(t *testing.T) {
	body := Body()
	for _, want := range []string{
		"debe aparecer en las herramientas o en el contexto del jugador",
		"no tengo su ubicación",
		"(dato general, sin confirmar en Forever)",
		"en cursiva",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("SKILL.md lacks the grounding rule %q", want)
		}
	}
}

func TestMCPInstructionsKeepTheGroundingLine(t *testing.T) {
	if !strings.Contains(MCPInstructions(), "Nombres y coordenadas solo de las herramientas") {
		t.Error("the condensed MCP instructions must keep the grounding line")
	}
}
