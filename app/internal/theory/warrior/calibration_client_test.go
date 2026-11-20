package warrior

import (
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// These tests use the real level 25 sheet (testdata/sheet-warrior-l25-es.json, a Spanish client): the
// numbers the client itself gave, not the constants. The level 20 fixtures are hand-written and are not
// the reference for anything here.

func checkNamed(t *testing.T, checks []theory.Check, name string) theory.Check {
	t.Helper()
	for _, c := range checks {
		if c.Nombre == name {
			return c
		}
	}
	t.Fatalf("no check named %q in %+v", name, checks)
	return theory.Check{}
}

func TestAttackPowerBaseMatchesTheClientSheetAtLevel25(t *testing.T) {
	in := theory.Input{Sheet: sheetES(t), Data: siteData(t)}
	checks := New().Calibrate(in)
	ap := checkNamed(t, checks, "Poder de ataque base")
	if ap.Modelo != 201 || ap.Hoja != 201 || !ap.Ok {
		t.Fatalf("attack power check = %+v, want 201 against 201", ap)
	}
	if msg := theory.Mismatch(checks); msg != "" {
		t.Fatalf("the level 25 sheet must calibrate: %s", msg)
	}
}

func TestAttackPowerFormulaNeedsTheLevelTerm(t *testing.T) {
	// Without the level term the formula gives 2 × 73 − 20 = 126: the mismatch the real run reported.
	k := Table
	if got := k.APPerStrength*73 + k.APOffset; got == 201 {
		t.Fatal("the strength formula alone must not match the sheet (it does not: the level term is needed)")
	}
	if got := k.APPerLevel*25 + k.APPerStrength*73 + k.APOffset; got != 201 {
		t.Fatalf("3 × 25 + 2 × 73 − 20 = %v, want 201", got)
	}
}

func TestSheetAttackPowerIsTheBaseForWhatIfs(t *testing.T) {
	in := theory.Input{Sheet: sheetES(t), Data: siteData(t)}
	_, m, err := New().prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	if !near(m.ap, 201, 1e-9) {
		t.Fatalf("ap on the sheet's own build = %.2f, want the sheet's 201", m.ap)
	}
	in.Build.Delta = theory.Delta{Str: 10}
	_, m2, err := New().prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	if !near(m2.ap, 221, 1e-9) {
		t.Fatalf("ap with +10 strength = %.2f, want 221 (2 per point)", m2.ap)
	}
}

func TestZeroCritReadingUsesAgilityAndTalents(t *testing.T) {
	// The sheet reads 0 % crit with 52 agility and Cruelty 5. Agility/20 + 5 = 7.6: the model must not
	// take the 0 as the crit of the character.
	in := theory.Input{Sheet: sheetES(t), Data: siteData(t)}
	if in.Sheet.Melee.Crit != 0 || in.Sheet.Total.Agi != 52 {
		t.Fatalf("fixture changed: crit %v agi %v", in.Sheet.Melee.Crit, in.Sheet.Total.Agi)
	}
	_, m, err := New().prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	if !near(m.crit, 52.0/20+5, 1e-9) {
		t.Fatalf("crit = %.3f, want 7.6 (agility/20 + Cruelty 5)", m.crit)
	}
	noted := false
	for _, n := range m.notes {
		if strings.Contains(n, "0 %") {
			noted = true
		}
	}
	if !noted {
		t.Error("the model must say it did not use the sheet's 0 %")
	}
	c := checkNamed(t, New().Calibrate(in), "Crítico")
	if !c.Ok || !strings.Contains(c.Nota, "no se compara") {
		t.Errorf("a 0 %% crit reading must not be compared as a mismatch: %+v", c)
	}
}

func TestDeathWishAndSweepingStrikeIDsAreTheClientsPairs(t *testing.T) {
	// The Spanish client's talent entries: Deseo de la Muerte (Death Wish) is 12328 and Golpes de barrido
	// (Sweeping Strikes) is 12292. The ability table must agree with the talent table.
	for _, c := range []struct {
		id    int
		key   string
		slug  string
		label string
	}{
		{12328, "death_wish", "death-wish", "Deseo de la Muerte"},
		{12292, "sweeping_strikes", "sweeping-strikes", "Golpes de barrido"},
	} {
		if got, _ := abilityKeyOf(c.id); got != c.key {
			t.Errorf("%s (%d): ability key %q, want %q", c.label, c.id, got, c.key)
		}
		if got, _ := talentSlugOf(c.id); got != c.slug {
			t.Errorf("%s (%d): talent slug %q, want %q", c.label, c.id, got, c.slug)
		}
	}
}

func TestSheetTalentIDsResolveToTheTreeEntries(t *testing.T) {
	sh := sheetES(t)
	tree, err := NewTree(siteData(t).Talents)
	if err != nil {
		t.Fatal(err)
	}
	for _, tab := range sh.Talents.Tabs {
		for _, tal := range tab.Talents {
			if tal.SpellID != 12328 && tal.SpellID != 12292 {
				continue
			}
			slug, ok := talentSlugOf(tal.SpellID)
			if !ok {
				t.Fatalf("%s (%d) has no slug", tal.Name, tal.SpellID)
			}
			if _, ok := tree.Index(slug); !ok {
				t.Errorf("%s (%d): slug %s is not in the tree", tal.Name, tal.SpellID, slug)
			}
		}
	}
}

func TestDeathWishTalentGrantsTheAbilityKeyedByItsID(t *testing.T) {
	tree, err := NewTree(siteData(t).Talents)
	if err != nil {
		t.Fatal(err)
	}
	ranks := make([]int, len(tree.Defs))
	dw, ok := tree.Index("death-wish")
	if !ok {
		t.Fatal("death-wish not in the tree")
	}
	ranks[dw] = 1
	if !talentsOf(tree, ranks, "").grantDW {
		t.Fatal("Death Wish talent must grant the Death Wish ability")
	}
	// The id of the talent and the id of the ability it grants are the same pair.
	if key, _ := abilityKeyOf(12328); key != "death_wish" {
		t.Fatalf("12328 must be the Death Wish ability, got %q", key)
	}
}
