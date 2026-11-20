package warrior

import (
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// sheetES is the sheet of a level 25 warrior on a Spanish client (testdata/sheet-warrior-l25-es.json), as
// the store hands it to the engine: its talents carry the client's names and the spell ids of this client.
// Its names are in Spanish, so every talent must be matched by its spell id.
func sheetES(t *testing.T) theory.Sheet {
	t.Helper()
	sh, err := theory.ParseSheet(fixture(t, "sheet-warrior-l25-es.json"))
	if err != nil {
		t.Fatal(err)
	}
	return sh
}

func TestSpanishClientSheetTalentsAreAllIdentified(t *testing.T) {
	sh := sheetES(t)
	tree, err := NewTree(siteData(t).Talents)
	if err != nil {
		t.Fatal(err)
	}
	ranks, unknown, err := sheetTalentRanks(tree, sh)
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 0 {
		t.Fatalf("talentos sin identificar: %v", unknown)
	}
	want := map[string]int{
		"cruelty": 5, "improved-overpower": 2, "improved-tactical-mastery": 2,
		"improved-rend": 3, "deflection": 2,
	}
	for slug, rank := range want {
		i, ok := tree.Index(slug)
		if !ok {
			t.Fatalf("%s no está en el árbol", slug)
		}
		if ranks[i] != rank {
			t.Errorf("%s = %d, want %d", slug, ranks[i], rank)
		}
	}
	// Every talent of the sheet has a slug: the sheet lists 53 entries of the 53 of the site.
	seen := map[string]bool{}
	for _, tab := range sh.Talents.Tabs {
		for _, tal := range tab.Talents {
			slug, ok := talentSlugOf(tal.SpellID)
			if !ok {
				t.Errorf("%s (id %d) no tiene slug", tal.Name, tal.SpellID)
				continue
			}
			if seen[slug] {
				t.Errorf("%s (id %d): el slug %s ya está en otro talento", tal.Name, tal.SpellID, slug)
			}
			seen[slug] = true
		}
	}
	if len(seen) != 53 {
		t.Errorf("slugs = %d, want 53", len(seen))
	}
}

func TestSpanishClientSheetOptimizerOrdersPoints(t *testing.T) {
	in := theory.Input{Sheet: sheetES(t), Data: siteData(t)}
	plan, err := New().OptimizeTalents(in, theory.TalentSearch{Points: 10, Beam: 3, Opts: theory.Options{Fights: 30, Seed: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Puntos != 10 || len(plan.Top) == 0 {
		t.Fatalf("plan = %d points, %d options", plan.Puntos, len(plan.Top))
	}
	for _, opt := range plan.Top {
		if len(opt.Orden) != 10 {
			t.Fatalf("order %v has %d steps, want 10", opt.Orden, len(opt.Orden))
		}
	}
}
