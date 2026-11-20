package warrior

import (
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// A Spanish client's sheet names the talents and the spells in Spanish; ClientName maps the English name
// of the model to the sheet's name through the spell id, and leaves an unknown name alone.
func TestClientNameTranslatesByTheSpellIDOfTheSheet(t *testing.T) {
	sh := sheetES(t)
	want := ""
	for _, tab := range sh.Talents.Tabs {
		for _, tal := range tab.Talents {
			if talentIDs[tal.SpellID] == "deflection" && tal.Name != "" {
				want = tal.Name
			}
		}
	}
	if want == "" {
		t.Fatal("the Spanish sheet has no Deflection talent with a known id")
	}
	got, ok := ClientName("Deflection", sh)
	if !ok || got != want {
		t.Fatalf("ClientName(Deflection) = %q, %v; want %q", got, ok, want)
	}
	if got == "Deflection" {
		t.Fatalf("the name is still English: %q", got)
	}
	if spell, ok := ClientName("Heroic Strike", sh); ok && spell == "Heroic Strike" {
		t.Fatalf("Heroic Strike kept its English name: %q", spell)
	}
}

func TestClientNameFallsBackToEnglish(t *testing.T) {
	sh := sheetES(t)
	if name, ok := ClientName("No Such Talent", sh); ok || name != "" {
		t.Fatalf("unknown name = %q, %v; want no translation", name, ok)
	}
	if name, ok := ClientName("", sh); ok || name != "" {
		t.Fatalf("empty name = %q, %v", name, ok)
	}
	// A sheet without talents or spells translates nothing.
	if name, ok := ClientName("Deflection", theory.Sheet{}); ok {
		t.Fatalf("empty sheet translated to %q", name)
	}
}
