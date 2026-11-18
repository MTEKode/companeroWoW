package theory

import (
	"math"
	"strings"
	"testing"
)

// Reference values of the Classic-era formulas. The attack table values are the ones the community
// documents: 5 % miss against a target of the same level, 0.1 % per point of defence over skill up
// to 10 points, then 0.4 % per point; a Blizzard forum post gives 8 % against a target three levels up
// with 300 weapon skill (15 points of gap).
var classicAttack = AttackTables{
	MissBase: 5, MissSlopeLow: 0.1, MissBreakPts: 10, MissSlopeHigh: 0.4,
	DualWieldMiss: 19, DefPerPoint: 0.04, CritPerPoint: 0.04,
	GlanceBase: 10, GlancePerPt: 2, GlanceMax: 40, GlanceMult: 0.75,
	ArmorK: 400, ArmorPerLevel: 85,
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestMissChanceReferences(t *testing.T) {
	cases := []struct {
		name          string
		attacker, def float64
		want          float64
	}{
		{"same level", 100, 100, 5},
		{"three levels up, 300 skill", 300, 315, 8},
		{"two points up", 100, 102, 5.2},
		{"above the break point", 100, 120, 6 + 0.4*10},
		{"skill above defence", 120, 100, 5},
	}
	for _, c := range cases {
		p := classicAttack.Attack(c.attacker, c.def, 0, 0, 0, 0, 0, false, false)
		if !near(p.Miss, c.want, 1e-9) {
			t.Errorf("%s: miss %.3f, want %.3f", c.name, p.Miss, c.want)
		}
	}
}

func TestHitReducesMissButNotBelowZero(t *testing.T) {
	p := classicAttack.Attack(100, 100, 0, 3, 0, 0, 0, false, false)
	if p.Miss != 2 {
		t.Fatalf("miss with 3 hit = %.2f, want 2", p.Miss)
	}
	p = classicAttack.Attack(100, 100, 0, 9, 0, 0, 0, false, false)
	if p.Miss != 0 {
		t.Fatalf("miss with 9 hit = %.2f, want 0", p.Miss)
	}
}

func TestDualWieldPenaltyOnOffhand(t *testing.T) {
	p := classicAttack.Attack(100, 100, 0, 0, 0, 0, 0, true, true)
	if p.Miss != 24 {
		t.Fatalf("offhand miss = %.1f, want 24 (5 + 19)", p.Miss)
	}
}

func TestGlancingOnlyForWhiteHitsAgainstHigherDefence(t *testing.T) {
	white := classicAttack.Attack(100, 102, 5, 0, 0, 0, 0, true, false)
	if white.Glance != 14 { // 10 + 2 × 2 points
		t.Fatalf("glance = %.1f, want 14", white.Glance)
	}
	capped := classicAttack.Attack(100, 115, 5, 0, 0, 0, 0, true, false)
	if capped.Glance != 40 { // 10 + 2 × 15 reaches the cap
		t.Fatalf("capped glance = %.1f, want 40", capped.Glance)
	}
	yellow := classicAttack.Attack(100, 102, 5, 0, 0, 0, 0, false, false)
	if yellow.Glance != 0 {
		t.Fatalf("yellow attacks glance = %.1f, want 0", yellow.Glance)
	}
	level := classicAttack.Attack(100, 100, 5, 0, 0, 0, 0, true, false)
	if level.Glance != 0 {
		t.Fatalf("glance at equal skill = %.1f, want 0", level.Glance)
	}
}

func TestCritLosesPerPointOfDefence(t *testing.T) {
	p := classicAttack.Attack(100, 115, 10, 0, 0, 0, 0, false, false)
	if !near(p.Crit, 9.4, 1e-9) { // 10 − 0.04 × 15
		t.Fatalf("crit = %.3f, want 9.4", p.Crit)
	}
}

func TestPickFollowsTheOrderOfChances(t *testing.T) {
	p := Probs{Miss: 5, Dodge: 5, Parry: 5, Glance: 5, Crit: 5}
	cases := map[float64]Roll{2: RollMiss, 7: RollDodge, 12: RollParry, 17: RollGlance, 22: RollCrit, 60: RollHit}
	for u, want := range cases {
		if got := p.Pick(u); got != want {
			t.Errorf("Pick(%v) = %v, want %v", u, got, want)
		}
	}
}

func TestMitigationFormula(t *testing.T) {
	// 620 armour against a level 20 attacker: 620 / (620 + 400 + 85 × 20).
	got := classicAttack.Mitigation(620, 20)
	if !near(got, 620.0/2720.0, 1e-12) {
		t.Fatalf("mitigation = %.6f", got)
	}
	if classicAttack.Mitigation(0, 20) != 0 {
		t.Fatal("no armour must mean no mitigation")
	}
}

func TestRageConversionAndGeneration(t *testing.T) {
	r := RageTable{
		Levels:      []float64{10, 20, 30, 40, 50, 60, 70},
		Conversions: []float64{37.4, 72.4, 109.3, 147.9, 188.3, 230.6, 274.7},
		FromDealt:   7.5, FromTaken: 2.5,
	}
	if got := r.Conversion(20); got != 72.4 {
		t.Fatalf("conversion at 20 = %v", got)
	}
	if got := r.Conversion(25); !near(got, (72.4+109.3)/2, 1e-9) {
		t.Fatalf("conversion at 25 = %v, want the midpoint of 20 and 30", got)
	}
	if got := r.RageDealt(100, 20); !near(got, 7.5*100/72.4, 1e-9) {
		t.Fatalf("rage dealt = %v", got)
	}
	if got := r.RageTaken(100, 20); !near(got, 2.5*100/72.4, 1e-9) {
		t.Fatalf("rage taken = %v", got)
	}
}

func TestMobModelAtLevel(t *testing.T) {
	m := MobTables{DefensePerLevel: 5, HPBase: 50, HPPerLevel: 25, ArmorPerLevel: 25,
		DamageBase: 5, DamagePerLevel: 1, Swing: 2, Crit: 5, Dodge: 5, EliteHP: 2.5, EliteDamage: 1.5}
	normal := m.MobAt(20, false)
	if normal.HP != 550 || normal.Armor != 500 || normal.Skill != 100 || normal.DamageAv != 25 {
		t.Fatalf("normal mob at 20 = %+v", normal)
	}
	elite := m.MobAt(20, true)
	if elite.HP != 1375 || elite.DamageAv != 37.5 {
		t.Fatalf("elite mob at 20 = %+v", elite)
	}
}

func TestCI95(t *testing.T) {
	ci := CI95([]float64{1, 2, 3})
	if ci.Mean != 2 || ci.Low >= 2 || ci.High <= 2 {
		t.Fatalf("CI95 = %+v", ci)
	}
	one := CI95([]float64{5})
	if one.Low != 5 || one.High != 5 {
		t.Fatalf("single sample interval = %+v", one)
	}
	if (CI95(nil) != CI{}) {
		t.Fatal("no samples must give a zero interval")
	}
}

func TestDeltaFromItemSeparatesUnmodelledStats(t *testing.T) {
	d, left := DeltaFromItem(map[string]float64{
		"ITEM_MOD_STRENGTH_SHORT":     12,
		"ITEM_MOD_ATTACK_POWER_SHORT": 20,
		"ITEM_MOD_INTELLECT_SHORT":    5,
		"ITEM_MOD_HASTE_RATING_SHORT": 8,
		"ITEM_MOD_CRIT_RATING_SHORT":  4,
	})
	if d.Str != 12 || d.AP != 20 {
		t.Fatalf("delta = %+v", d)
	}
	want := "ITEM_MOD_CRIT_RATING_SHORT,ITEM_MOD_HASTE_RATING_SHORT"
	if got := strings.Join(left, ","); got != want {
		t.Fatalf("unmodelled = %q, want %q", got, want)
	}
}

func TestRandIsDeterministicForASeed(t *testing.T) {
	a, b := NewRand(42), NewRand(42)
	for i := 0; i < 100; i++ {
		if a.Float64() != b.Float64() {
			t.Fatal("same seed, different sequence")
		}
	}
	if NewRand(42).Float64() == NewRand(43).Float64() {
		t.Fatal("different seeds gave the same first draw")
	}
}

func TestParseSheetNeedsLevelAndValidJSON(t *testing.T) {
	if _, err := ParseSheet([]byte(`{"class":"WARRIOR"}`)); err == nil {
		t.Fatal("a sheet without level must fail")
	}
	if _, err := ParseSheet([]byte(`{"level":20,`)); err == nil {
		t.Fatal("broken JSON must fail")
	}
	sh, err := ParseSheet([]byte(`{"level":20,"class":"warrior","melee":{"ohMin":null,"mhSpeed":2.6}}`))
	if err != nil {
		t.Fatal(err)
	}
	if sh.Class != "WARRIOR" || sh.Melee.OHMin != nil {
		t.Fatalf("sheet = %+v", sh)
	}
}

func TestWeaponKindFromSkillLines(t *testing.T) {
	sh := Sheet{WeaponSkills: []WeaponSkill{{Name: "Defense"}, {Name: "Two-Handed Swords"}}}
	kind, twoHanded, ok := sh.WeaponKind()
	if !ok || kind != "sword" || !twoHanded {
		t.Fatalf("kind = %q two-handed=%v ok=%v", kind, twoHanded, ok)
	}
	if _, _, ok := (Sheet{}).WeaponKind(); ok {
		t.Fatal("no skill lines must mean no weapon kind")
	}
}

func TestMismatchOnlyNamesFailedChecks(t *testing.T) {
	checks := []Check{
		{Nombre: "Crítico", Modelo: 5.6, Hoja: 5.6, Ok: true},
		{Nombre: "Poder de ataque base", Modelo: 100, Hoja: 180, Desvio: -44.4, Ok: false},
	}
	msg := Mismatch(checks)
	if !strings.HasPrefix(msg, "modelo desajustado: Poder de ataque base") || strings.Contains(msg, "Crítico") {
		t.Fatalf("mismatch = %q", msg)
	}
	if Mismatch(checks[:1]) != "" {
		t.Fatal("matching checks must give no message")
	}
}
