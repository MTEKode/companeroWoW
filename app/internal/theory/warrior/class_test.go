package warrior

import (
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// quick is the option set for tests: enough fights for stable ordering, fast to run.
var quick = theory.Options{Fights: 200, Seed: 20261118}

func TestStatWeightsSanityOnTheLevel20Sheet(t *testing.T) {
	in := inputL20(t)
	in.Opt = quick
	w, err := New().Weights(in)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]theory.StatWeight{}
	for _, p := range w.Pesos {
		byName[p.Stat] = p
	}
	// Strength gives 2 attack power per point, agility only crit at a 20-per-point rate: at 5.6 % crit
	// strength must be worth far more than agility.
	if byName["Fuerza"].DeltaKPH <= byName["Agilidad"].DeltaKPH {
		t.Errorf("strength %.3f kills/h per point must beat agility %.3f", byName["Fuerza"].DeltaKPH, byName["Agilidad"].DeltaKPH)
	}
	if byName["Poder de ataque"].DeltaDPS <= 0 || byName["Crítico"].DeltaDPS <= 0 {
		t.Errorf("attack power and crit must add damage: %+v %+v", byName["Poder de ataque"], byName["Crítico"])
	}
	if byName["Velocidad del arma"].DeltaDPS <= 0 {
		t.Errorf("a faster weapon must add damage: %+v", byName["Velocidad del arma"])
	}
	if byName["Poder de ataque"].RelativoAP != 1 {
		t.Errorf("attack power is the reference: %+v", byName["Poder de ataque"])
	}
	if w.Ruido == "" {
		t.Error("the weights must state their noise")
	}
}

func TestItemsAreOrderedBySimulationNotItemLevel(t *testing.T) {
	in := inputL20(t)
	in.Opt = quick
	items := []theory.Item{
		{Name: "Hacha de mucho nivel", ItemLevel: 40, EquipLoc: "INVTYPE_CHEST", Stats: map[string]float64{"ITEM_MOD_STAMINA_SHORT": 40}},
		{Name: "Peto de poder", ItemLevel: 20, EquipLoc: "INVTYPE_CHEST", Stats: map[string]float64{"ITEM_MOD_ATTACK_POWER_SHORT": 40}},
		{Name: "Peto menor", ItemLevel: 20, EquipLoc: "INVTYPE_CHEST", Stats: map[string]float64{"ITEM_MOD_ATTACK_POWER_SHORT": 20}},
		{Name: "Anillo con prisa", ItemLevel: 20, EquipLoc: "INVTYPE_FINGER", Stats: map[string]float64{"ITEM_MOD_HASTE_RATING_SHORT": 10}},
		{Name: "Espada", ItemLevel: 30, EquipLoc: "INVTYPE_WEAPON", Stats: map[string]float64{"ITEM_MOD_STRENGTH_SHORT": 10}},
	}
	rep, err := New().CompareItems(in, items)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, it := range rep.Items {
		pos[it.Name] = i
	}
	if pos["Peto de poder"] != 0 || pos["Peto menor"] <= pos["Peto de poder"] {
		t.Fatalf("order = %+v", rep.Items)
	}
	if pos["Peto de poder"] >= pos["Hacha de mucho nivel"] {
		t.Errorf("40 attack power (ilvl 20) must beat 40 stamina (ilvl 40): %+v", rep.Items)
	}
	for _, it := range rep.Items {
		if it.Name == "Anillo con prisa" && len(it.NoModelado) == 0 {
			t.Error("haste is not modelled: the score must say so")
		}
	}
	if len(rep.NoComparables) != 1 || !strings.Contains(rep.NoComparables[0], "Espada") {
		t.Errorf("weapons without damage data must be named as not comparable: %v", rep.NoComparables)
	}
}

func TestItemReplacesTheCurrentPiece(t *testing.T) {
	in := inputL20(t)
	in.Opt = quick
	same := theory.Item{Name: "Igual", EquipLoc: "INVTYPE_CHEST",
		Stats:    map[string]float64{"ITEM_MOD_ATTACK_POWER_SHORT": 30},
		Replaces: map[string]float64{"ITEM_MOD_ATTACK_POWER_SHORT": 30}}
	rep, err := New().CompareItems(in, []theory.Item{same})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Items) != 1 || rep.Items[0].DeltaKPH != 0 || rep.Items[0].DeltaDPS != 0 {
		t.Fatalf("an item identical to the one it replaces must change nothing: %+v", rep.Items)
	}
}

func TestTalentOptimizerRespectsTheTreeAndCountsPoints(t *testing.T) {
	in := inputL20(t)
	plan, err := New().OptimizeTalents(in, theory.TalentSearch{Points: 3, Beam: 3, Opts: theory.Options{Fights: 30, Seed: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Puntos != 3 {
		t.Fatalf("points added = %d, want 3", plan.Puntos)
	}
	if len(plan.Top) == 0 || len(plan.Top) > 3 {
		t.Fatalf("options = %d", len(plan.Top))
	}
	tree := warriorTree(t)
	for _, opt := range plan.Top {
		if len(opt.Orden) != 3 {
			t.Fatalf("order %v has %d steps, want 3", opt.Orden, len(opt.Orden))
		}
		ranks, _ := tree.Ranks(in.Sheet.TalentRanks())
		for _, step := range opt.Orden {
			name := step[:strings.LastIndex(step, " ")]
			i, ok := tree.Index(name)
			if !ok {
				t.Fatalf("step %q names no talent", step)
			}
			if !tree.Open(ranks, i) {
				t.Fatalf("step %q is not open in order %v", step, opt.Orden)
			}
			ranks[i]++
		}
		if err := tree.Valid(ranks); err != nil {
			t.Fatalf("option %v breaks the tree: %v", opt.Orden, err)
		}
	}
	for i := 1; i < len(plan.Top); i++ {
		if plan.Top[i].Resultado.KillsPerHour.Mean > plan.Top[i-1].Resultado.KillsPerHour.Mean {
			t.Fatal("options are not sorted by kills per hour")
		}
	}
}

func TestTalentOptimizerOnlyPicksModelledTalents(t *testing.T) {
	in := inputL20(t)
	plan, err := New().OptimizeTalents(in, theory.TalentSearch{Points: 2, Beam: 2, Opts: theory.Options{Fights: 20, Seed: 3}})
	if err != nil {
		t.Fatal(err)
	}
	tree := warriorTree(t)
	for _, opt := range plan.Top {
		for _, step := range opt.Orden {
			name := step[:strings.LastIndex(step, " ")]
			i, _ := tree.Index(name)
			if !tree.modelled(i) && !tree.unlocksModelled(i) {
				t.Errorf("%s has no effect in the model and was picked", name)
			}
		}
	}
}

func TestTalentOptimizerIsFastEnough(t *testing.T) {
	if testing.Short() {
		t.Skip("performance check skipped with -short")
	}
	in := inputL20(t)
	start := time.Now()
	plan, err := New().OptimizeTalents(in, theory.TalentSearch{})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	t.Logf("talent search: %d points, %d evaluations, %s", plan.Puntos, plan.Evaluadas, elapsed)
	if elapsed > 3*time.Second {
		t.Errorf("talent search took %s, want under 3 s", elapsed)
	}
}

func TestRotationOptimizerRanksBestFirst(t *testing.T) {
	in := inputL20(t)
	plan, err := New().OptimizeRotation(in, theory.RotationSearch{Opts: quick})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Opciones) < 3 {
		t.Fatalf("rotations = %d", len(plan.Opciones))
	}
	for i := 1; i < len(plan.Opciones); i++ {
		if plan.Opciones[i].Resultado.KillsPerHour.Mean > plan.Opciones[i-1].Resultado.KillsPerHour.Mean {
			t.Fatal("rotations are not sorted by kills per hour")
		}
	}
	for _, o := range plan.Opciones {
		for _, k := range o.Prioridad {
			if k == "mortal_strike" || k == "bloodthirst" {
				t.Errorf("rotation %v uses %s, which the character does not have", o.Prioridad, k)
			}
		}
	}
}

func TestReviewNamesUnusedAbilities(t *testing.T) {
	in := inputL20(t)
	in.Opt = quick
	rev := New().Review(in, theory.FightRecord{Segundos: 20, DanoHecho: 400, NivelMob: 20, Habilidades: []string{"rend"}})
	if rev.DPSReal != 20 {
		t.Fatalf("real DPS = %v", rev.DPSReal)
	}
	if rev.DPSEsperado.Mean <= 0 {
		t.Fatalf("expected DPS = %+v", rev.DPSEsperado)
	}
	unused := strings.Join(rev.SinUsar, ",")
	if !strings.Contains(unused, "Overpower") || strings.Contains(unused, "Rend") {
		t.Errorf("unused abilities = %q", unused)
	}
}

func TestFightsWithTheSameSeedArePaired(t *testing.T) {
	in := inputL20(t)
	in.Opt = theory.Options{Fights: 50, Seed: 11, MobLevel: 20}
	_, m, err := New().prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	a := m.runFights(in.Opt)
	b := m.runFights(in.Opt)
	for i := range a {
		if a[i].dur != b[i].dur || a[i].dealt != b[i].dealt {
			t.Fatalf("fight %d differs between two runs with the same seed", i)
		}
	}
}
