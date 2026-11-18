package warrior

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

func TestEvaluateIsDeterministicForASeed(t *testing.T) {
	in := inputL20(t)
	in.Opt = theory.Options{Fights: 150, Seed: 99}
	a, err := New().Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := New().Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("the same seed gave different reports")
	}
	in.Opt.Seed = 100
	c, _ := New().Evaluate(in)
	if reflect.DeepEqual(a.DPS, c.DPS) {
		t.Fatal("a different seed gave the same DPS interval")
	}
}

func TestEvaluateSanityOnTheLevel20Sheet(t *testing.T) {
	in := inputL20(t)
	in.Opt = theory.Options{Fights: 300}
	rep, err := New().Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.DPS.Mean < 10 || rep.DPS.Mean > 120 {
		t.Errorf("DPS %.1f outside a plausible range for level 20", rep.DPS.Mean)
	}
	if rep.TTK.Mean < 5 || rep.TTK.Mean > 120 {
		t.Errorf("time to kill %.1f s outside a plausible range", rep.TTK.Mean)
	}
	if rep.KillsPerHour.Mean <= 0 {
		t.Errorf("kills per hour = %.1f", rep.KillsPerHour.Mean)
	}
	if rep.DPS.Low > rep.DPS.Mean || rep.DPS.High < rep.DPS.Mean {
		t.Errorf("interval %+v does not contain the mean", rep.DPS)
	}
	if len(rep.Ira) != rageSamples {
		t.Errorf("rage curve has %d points", len(rep.Ira))
	}
	if rep.Ira[0].Media != 0 {
		t.Errorf("rage at the start = %v, want 0", rep.Ira[0].Media)
	}
	names := map[string]bool{}
	for _, h := range rep.Habilidades {
		names[h.Nombre] = true
	}
	for _, want := range []string{"Heroic Strike", "Rend", "Overpower"} {
		if !names[want] {
			t.Errorf("ability %s missing from the breakdown", want)
		}
	}
}

func TestSheetTooltipsWinOnTheSheetBuild(t *testing.T) {
	in := inputL20(t)
	_, m, err := New().prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	if m.abil[kHS].source != srcSheet || m.abil[kHS].cost != 12 {
		t.Errorf("Heroic Strike = %+v, want the tooltip cost 12 (talents already counted)", m.abil[kHS])
	}
	if m.abil[kRend].bleed != 61 || m.abil[kRend].bleedSec != 15 {
		t.Errorf("Rend = %+v, want the tooltip's 61 over 15 s", m.abil[kRend])
	}
	if m.abil[kRend].source != srcSheet {
		t.Errorf("Rend source = %s", m.abil[kRend].source)
	}
}

func TestChangedBuildUsesTheTableAndTalentModifiers(t *testing.T) {
	in := inputL20(t)
	in.Build.Talents = map[string]int{"Improved Heroic Strike": 1}
	_, m, err := New().prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	// Without the sheet's talents the Heroic Strike cost is the table's 15 minus the 1 point rank.
	if m.abil[kHS].source == srcSheet {
		t.Errorf("a changed build must not use the sheet tooltip")
	}
	if m.abil[kHS].cost != 14 {
		t.Errorf("Heroic Strike cost = %v, want 14 (15 − 1)", m.abil[kHS].cost)
	}
}

func TestTalentsAreNotCountedTwiceInTheSheetCrit(t *testing.T) {
	in := inputL20(t)
	_, m, err := New().prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	if !near(m.crit, 5.6, 1e-9) {
		t.Fatalf("crit on the sheet's own build = %.3f, want the sheet's 5.6 (cruelty already in it)", m.crit)
	}
	in.Build.Talents = map[string]int{"Improved Heroic Strike": 3, "Improved Rend": 3, "Improved Overpower": 2, "Cruelty": 5}
	_, m2, err := New().prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	if !near(m2.crit, 5.6+2, 1e-9) {
		t.Fatalf("crit with cruelty 5 = %.3f, want 7.6 (two more points than the sheet's rank 3)", m2.crit)
	}
}

func TestCalibrationMatchesTheSheet(t *testing.T) {
	in := inputL20(t)
	checks := New().Calibrate(in)
	if msg := theory.Mismatch(checks); msg != "" {
		t.Fatalf("the fixture sheet must calibrate: %s", msg)
	}
	var ap *theory.Check
	for i := range checks {
		if checks[i].Nombre == "Poder de ataque base" {
			ap = &checks[i]
		}
	}
	if ap == nil || ap.Modelo != 100 || ap.Hoja != 100 {
		t.Fatalf("attack power check = %+v", ap)
	}
}

func TestCalibrationDetectsAMismatch(t *testing.T) {
	in := inputL20(t)
	in.Sheet.Melee.APBase = 180 // the sheet says 180 where 2 × 60 − 20 gives 100
	msg := theory.Mismatch(New().Calibrate(in))
	if !strings.HasPrefix(msg, "modelo desajustado: Poder de ataque base") {
		t.Fatalf("mismatch message = %q", msg)
	}
	in = inputL20(t)
	in.Sheet.Melee.APBase = 101 // 1 %: inside the 3 % tolerance
	if msg := theory.Mismatch(New().Calibrate(in)); msg != "" {
		t.Fatalf("a 1 %% difference must not be reported: %s", msg)
	}
}

func TestCalibrationReportsWhatItCannotCheck(t *testing.T) {
	in := inputL20(t)
	found := false
	for _, c := range New().Calibrate(in) {
		if strings.Contains(c.Nota, "no se compara") {
			found = true
			if !c.Ok {
				t.Error("an unchecked number must not count as a mismatch")
			}
		}
	}
	if !found {
		t.Fatal("the white damage check must say it is not compared")
	}
}

func TestClassNeedsTalentData(t *testing.T) {
	in := inputL20(t)
	in.Data.Talents = nil
	if _, err := New().Evaluate(in); err == nil {
		t.Fatal("no talent tree must be an error")
	}
}

func TestFightCapEndsLongFights(t *testing.T) {
	in := inputL20(t)
	in.Opt = theory.Options{Fights: 20, Seed: 5, MobLevel: 40}
	rep, err := New().Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Peleas != 20 {
		t.Fatalf("fights = %d", rep.Peleas)
	}
}
