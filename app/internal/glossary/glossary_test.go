package glossary

import (
	"strings"
	"testing"
)

func TestAnnotateAddsSpanishNameAfterEnglishName(t *testing.T) {
	cases := map[string]string{
		"Mount trainer in Stormwind.":            "Mount trainer in Stormwind (Ventormenta).",
		"Head north from Stormwind City.":        "Head north from Stormwind City (Ventormenta).",
		"Between The Barrens and Ashenvale.":     "Between The Barrens (Los Baldíos) and Ashenvale (Vallefresno).",
		"Thunder Bluff and Undercity":            "Thunder Bluff (Cima del Trueno) and Undercity (Entrañas)",
		"Redridge Mountains, Elwynn Forest":      "Redridge Mountains (Montañas Crestagrana), Elwynn Forest (Bosque de Elwynn)",
		"Nothing to translate here.":             "Nothing to translate here.",
		"Orgrimmar and Durotar stay as they are": "Orgrimmar and Durotar stay as they are",
	}
	for in, want := range cases {
		if got := Annotate(in); got != want {
			t.Errorf("Annotate(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestAnnotateIsIdempotent(t *testing.T) {
	once := Annotate("Ironforge is near Darkshore.")
	if twice := Annotate(once); twice != once {
		t.Errorf("second pass changed the text:\n%q\n%q", once, twice)
	}
	if !strings.Contains(once, "Forjaz") || !strings.Contains(once, "Costa Oscura") {
		t.Errorf("annotations missing: %q", once)
	}
}

func TestAnnotateRespectsWordBoundaries(t *testing.T) {
	// "Wetlandsy" is not the zone; "Stormwindow" is not Stormwind.
	for _, in := range []string{"Wetlandsy", "Stormwindow", "undercity lowercase"} {
		if got := Annotate(in); got != in {
			t.Errorf("Annotate(%q) = %q, want no change", in, got)
		}
	}
}

func TestAnnotateLongestNameWins(t *testing.T) {
	got := Annotate("Tirisfal Glades")
	if got != "Tirisfal Glades (Claros de Tirisfal)" {
		t.Errorf("got %q", got)
	}
}

func TestEntriesWithoutSpanishNameAreNotAnnotated(t *testing.T) {
	in := "Loch Modan is east of Dun Morogh."
	if got := Annotate(in); got != in {
		t.Errorf("unconfirmed names must stay as they are: %q", got)
	}
	if Spanish("Loch Modan") != "" {
		t.Error("Loch Modan has no confirmed Spanish name yet")
	}
}

func TestSpanishLookup(t *testing.T) {
	if got := Spanish("Stormwind"); got != "Ventormenta" {
		t.Errorf("Spanish(Stormwind) = %q", got)
	}
	if got := Spanish("Atlantis"); got != "" {
		t.Errorf("unknown names have no translation: %q", got)
	}
}

func TestEveryEntryHasTheCanonicalCount(t *testing.T) {
	if len(Entries) != 23 {
		t.Errorf("glossary has %d entries, want 23", len(Entries))
	}
}
