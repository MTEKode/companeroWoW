package skill

import (
	"regexp"
	"strings"
	"testing"
)

// The macro playbook: crear_macro and revisar_macros, the /compa macro commands, and warrior examples
// that use placeholders (the tool writes the player's own spell names), never English names.
func TestMacroPlaybookIsInTheSkill(t *testing.T) {
	md := Markdown()
	for _, want := range []string{
		"`crear_macro`", "`revisar_macros`", "(u) \"crea una macro\"", "(v) \"revisa mis macros\"",
		"## Comando de macros", "🧩 /compa macro crear", "/compa macro parte", "/compa macro deshacer",
		"Fuera de combate. /compa macro deshacer vuelve atrás.", "{overpower}", "{charge}", "{intercept}",
		"{execute}", "{heroic_strike}",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("SKILL.md lacks %q", want)
		}
	}
}

// englishSpell matches a whole English spell name (the Spanish word "Interceptar" is not one).
var englishSpell = regexp.MustCompile(`\b(Overpower|Heroic Strike|Charge|Intercept|Execute|Battle Stance)\b`)

func TestMacroExamplesHaveNoEnglishSpellNames(t *testing.T) {
	if m := englishSpell.FindString(Markdown()); m != "" {
		t.Errorf("SKILL.md names %q in English: the examples must use placeholders", m)
	}
}

func TestMacroBodyLineIsDocumented(t *testing.T) {
	if !strings.Contains(MCPInstructions(), "crear_macro") {
		t.Error("the condensed MCP instructions must mention crear_macro")
	}
}
