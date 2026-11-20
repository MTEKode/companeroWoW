package warrior

// Tests for a Spanish client (esES): the sheet's names and tooltips are in Spanish, and the model must read
// the same character as it reads the English one. The fixtures are testdata/esES/ (a Spanish sheet with the
// same numbers as sheet-warrior-l20.json, and tooltip pairs English/Spanish with the same numbers).

import (
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// tooltipPair is one entry of testdata/esES/tooltips-warrior.json.
type tooltipPair struct {
	Clave     string `json:"clave"`
	HechizoID int    `json:"hechizoID"`
	Nivel     int    `json:"nivel"`
	Nombre    string `json:"nombre"`
	Ingles    string `json:"ingles"`
	Espanol   string `json:"espanol"`
}

func tooltipPairs(t *testing.T) []tooltipPair {
	t.Helper()
	var doc struct {
		Entradas []tooltipPair `json:"entradas"`
	}
	if err := json.Unmarshal(fixture(t, filepath.Join("esES", "tooltips-warrior.json")), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Entradas) == 0 {
		t.Fatal("no hay pares de tooltip")
	}
	return doc.Entradas
}

// spanishSheet is the level 20 Spanish sheet: the same numbers as the English fixture, with Spanish names.
func spanishSheet(t *testing.T) theory.Sheet {
	t.Helper()
	sh, err := theory.ParseSheet(fixture(t, filepath.Join("esES", "sheet-warrior-l20-esES.json")))
	if err != nil {
		t.Fatal(err)
	}
	return sh
}

// withTalents sets the talents of a sheet to the given names and ranks, keeping the tree layout of the sheet.
func withTalents(sh theory.Sheet, ranks map[string]int) theory.Sheet {
	for ti := range sh.Talents.Tabs {
		tab := &sh.Talents.Tabs[ti]
		tab.Points = 0
		for li := range tab.Talents {
			tal := &tab.Talents[li]
			tal.Rank = ranks[tal.Name]
			tab.Points += tal.Rank
		}
	}
	return sh
}

// sameReport compares the numbers two reports give: damage, kills and time to kill.
func sameReport(a, b theory.Report) bool {
	return a.DPS == b.DPS && a.KillsPerHour == b.KillsPerHour && a.TTK == b.TTK
}

// The English and the Spanish tooltip of an ability read the same fields: the same numbers, by position.
func TestSpanishTooltipsReadTheSameFieldsAsTheEnglishText(t *testing.T) {
	for _, p := range tooltipPairs(t) {
		if strings.HasPrefix(p.Clave, "talento:") {
			// Talent values come from the site's feed (English); the Spanish text is only checked for numbers.
			if !reflect.DeepEqual(theory.Numbers(p.Ingles), theory.Numbers(p.Espanol)) {
				t.Errorf("%s: los números no coinciden: inglés %v, español %v", p.Nombre, theory.Numbers(p.Ingles), theory.Numbers(p.Espanol))
			}
			continue
		}
		var en, es ability
		okEN := readEffects(p.Clave, p.Ingles, &en)
		okES := readTooltip(p.Clave, p.Espanol, &es)
		if !okEN || !okES {
			t.Errorf("%s: no se lee (inglés %v, español %v)", p.Nombre, okEN, okES)
			continue
		}
		if !reflect.DeepEqual(en, es) {
			t.Errorf("%s: campos distintos\ninglés  %+v\nespañol %+v", p.Nombre, en, es)
		}
	}
}

// The English site texts of every ability the model reads have the number of values its layout expects, for
// every rank: a layout that does not match the client text would be silently skipped.
func TestSiteTextsMatchTheLayoutForEveryRank(t *testing.T) {
	d := siteData(t)
	for key := range effectSpecs {
		i, ok := abilityIndex(key)
		if !ok {
			t.Fatalf("layout for unknown key %q", key)
		}
		ranks := d.Abilities[abilityNames[i]]
		if len(ranks) == 0 {
			continue // not in the site's table (for example Sweeping Strikes): nothing to check
		}
		want, _ := effectCount(key)
		for _, r := range ranks {
			if got := len(theory.Numbers(r.Text)); got != want {
				t.Errorf("%s rango %d: %d números, la disposición espera %d: %q", abilityNames[i], r.Rank, got, want, r.Text)
			}
		}
	}
}

// Ability numbers come from the Spanish tooltip of the sheet when the build is the sheet's build, and the
// sheet's talents are matched by spell id: the same character gives the same numbers in both languages.
func TestSpanishSheetGivesTheSameNumbersAsTheEnglishSheet(t *testing.T) {
	in := theory.Input{Data: siteData(t), Opt: theory.Options{Fights: 200}}
	talents := map[string]int{"Deflection": 5, "Improved Overpower": 2, "Cruelty": 5}

	en := in
	en.Sheet = withTalents(sheetL20(t), talents)
	es := in
	es.Sheet = withTalents(spanishSheet(t), map[string]int{"Desviación": 5, "Abrumar mejorado": 2, "Crueldad": 5})

	repEN, err := New().Evaluate(en)
	if err != nil {
		t.Fatal(err)
	}
	repES, err := New().Evaluate(es)
	if err != nil {
		t.Fatal(err)
	}
	if !sameReport(repEN, repES) {
		t.Fatalf("los números cambian con el idioma:\ninglés  %+v\nespañol %+v", repEN.DPS, repES.DPS)
	}
	for _, s := range repES.Supuestos {
		if strings.Contains(s, "sin identificar") {
			t.Errorf("con ids conocidos no debe quedar talento sin identificar: %s", s)
		}
	}
	// The abilities read from the Spanish tooltips report the sheet as their source.
	for _, h := range repES.Habilidades {
		if h.Nombre == "Heroic Strike" && h.Fuente != srcSheet {
			t.Errorf("Heroic Strike debe salir de la hoja, sale de %q", h.Fuente)
		}
	}
}

// A talent whose spell id is not known and whose name is Spanish is reported, not guessed.
func TestUnidentifiedTalentIsReportedNotGuessed(t *testing.T) {
	in := theory.Input{Data: siteData(t), Opt: theory.Options{Fights: 100}}
	sh := spanishSheet(t)
	sh.Talents.Tabs[0].Talents = append(sh.Talents.Tabs[0].Talents, theory.SheetTalent{
		Name: "Golpe heroico mejorado", Rank: 3, MaxRank: 3, Row: 1, Col: 2, SpellID: 0,
	})
	in.Sheet = sh
	rep, err := New().Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range rep.Supuestos {
		if strings.Contains(s, "sin identificar") && strings.Contains(s, "Golpe heroico mejorado") {
			found = true
		}
	}
	if !found {
		t.Fatalf("el talento sin identificar no aparece en los supuestos: %v", rep.Supuestos)
	}
}

// The Battle Shout attack power is not added twice: a sheet that already shows the buff gives none.
func TestBattleShoutIsNotCountedTwiceWhenTheSheetHasTheBuff(t *testing.T) {
	in := inputL20(t)
	in.Build.Rotation = []string{"battle_shout", "mortal_strike", "bloodthirst", "overpower", "rend", "hs"}
	plain, err := New().Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if !hasNote(plain.Supuestos, "Grito de batalla: +") {
		t.Fatalf("sin el buff, el grito de batalla debe sumar poder de ataque: %v", plain.Supuestos)
	}
	in.Sheet.Buffs = []int{6673}
	buffed, err := New().Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if !hasNote(buffed.Supuestos, "ya activo en la hoja") {
		t.Fatalf("con el buff en la hoja debe decirlo: %v", buffed.Supuestos)
	}
	for _, h := range buffed.Habilidades {
		if h.Nombre == "Battle Shout" {
			t.Fatalf("con el buff en la hoja no se lanza el grito: %+v", h)
		}
	}
}

// Sunder Armor, Revenge and Bloodrage are simulated when the rotation asks for them.
func TestNewAbilitiesAreSimulatedWhenInTheRotation(t *testing.T) {
	in := inputL20(t)
	in.Build.Rotation = []string{"sunder", "revenge", "bloodrage", "mortal_strike", "bloodthirst", "overpower", "rend", "hs"}
	rep, err := New().Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	uses := map[string]float64{}
	for _, h := range rep.Habilidades {
		uses[h.Nombre] = h.UsosPelea
	}
	for _, name := range []string{"Sunder Armor", "Revenge", "Bloodrage"} {
		if uses[name] <= 0 {
			t.Errorf("%s no se lanza (usos %v): %v", name, uses[name], uses)
		}
	}
}

// Two enemies pulled together: the fight ends when both are dead, so a kill counts for two and the scenario is
// stated in the assumptions.
func TestTwoTargetsEndWhenBothAreDead(t *testing.T) {
	one := inputL20(t)
	two := inputL20(t)
	two.Opt.Targets = 2
	repOne, err := New().Evaluate(one)
	if err != nil {
		t.Fatal(err)
	}
	repTwo, err := New().Evaluate(two)
	if err != nil {
		t.Fatal(err)
	}
	if !hasNote(repTwo.Supuestos, "dos enemigos juntos") {
		t.Fatalf("el escenario de dos enemigos debe decirse: %v", repTwo.Supuestos)
	}
	if repTwo.TTK.Mean <= repOne.TTK.Mean {
		t.Errorf("matar a dos tarda más que a uno: %.1f s frente a %.1f s", repTwo.TTK.Mean, repOne.TTK.Mean)
	}
	if repTwo.KillsPerHour.Mean <= 0 {
		t.Errorf("las muertes por hora con dos enemigos deben ser positivas")
	}
}

// The fight review matches the recorded spell ids; names only label the abilities.
func TestReviewMatchesSpellIDsNotNames(t *testing.T) {
	in := inputL20(t)
	in.Build.Rotation = []string{"slam", "rend", "hs"}
	rev := New().Review(in, theory.FightRecord{Segundos: 60, DanoHecho: 1000, Hechizos: []int{1464, 772, 78}, Habilidades: []string{"Aplastar"}})
	for _, n := range rev.SinUsar {
		if n == "Slam" || n == "Rend" {
			t.Errorf("%s figura sin usar aunque su id está en el registro", n)
		}
	}
}

// Weapons are compared by damage and speed. A weapon the model cannot compare says why.
func TestWeaponCandidatesAreComparedByDamageAndSpeed(t *testing.T) {
	in := inputL20(t)
	items := []theory.Item{
		// The fixture's warrior uses a two-handed sword: two-handed candidates are compared, the others are not.
		{Name: "Espada a dos manos mejor", EquipLoc: "INVTYPE_2HWEAPON", Subclass: "Two-Handed Swords", Weapon: &theory.Weapon{Min: 80, Max: 120, Speed: 3.4}},
		{Name: "Hacha a dos manos peor", EquipLoc: "INVTYPE_2HWEAPON", Subclass: "Two-Handed Axes", Weapon: &theory.Weapon{Min: 20, Max: 30, Speed: 3.6}},
		{Name: "Espada de una mano", EquipLoc: "INVTYPE_WEAPONMAINHAND", Subclass: "One-Handed Swords", Weapon: &theory.Weapon{Min: 60, Max: 80, Speed: 2.6}},
		{Name: "Mano izquierda", EquipLoc: "INVTYPE_WEAPONOFFHAND", Subclass: "One-Handed Swords", Weapon: &theory.Weapon{Min: 30, Max: 40, Speed: 2.0}},
	}
	rep, err := New().CompareItems(in, items)
	if err != nil {
		t.Fatal(err)
	}
	score := map[string]float64{}
	for _, it := range rep.Items {
		score[it.Name] = it.DeltaKPH
	}
	if score["Espada a dos manos mejor"] <= 0 {
		t.Errorf("una espada con más daño y casi la misma velocidad debe subir las muertes por hora: %v", score)
	}
	if score["Hacha a dos manos peor"] >= score["Espada a dos manos mejor"] {
		t.Errorf("el hacha peor debe puntuar menos que la espada: %v", score)
	}
	if _, ok := score["Espada de una mano"]; ok {
		t.Errorf("un arma de una mano frente a una de dos manos no se compara")
	}
	if !hasNote(rep.NoComparables, "una y dos manos") || !hasNote(rep.NoComparables, "mano izquierda") {
		t.Errorf("las armas no comparables deben decir por qué: %v", rep.NoComparables)
	}
}

// The weapon change keeps the range of the sheet's weapon in the simulation: the average and the spread move.
func TestWeaponDeltaMovesTheAverageAndTheSpread(t *testing.T) {
	sh := sheetL20(t)
	it := theory.Item{Name: "x", EquipLoc: "INVTYPE_2HWEAPON", Subclass: "Two-Handed Swords", Weapon: &theory.Weapon{Min: 50, Max: 70, Speed: 3.5}}
	_, m, err := New().prepare(theory.Input{Sheet: sh, Data: siteData(t)})
	if err != nil {
		t.Fatal(err)
	}
	d, _, why := weaponDelta(sh, it, m)
	if why != "" {
		t.Fatalf("no debería ser no comparable: %s", why)
	}
	// The current weapon is its own range: UnitDamage without the attack power it holds (184 × 3.5 / 14 = 46 of attack power, so 90 − 46 = 44 and 108 − 46 = 62).
	curLo, curHi := weaponRange(sh, m.k, sh.Melee.MHMin, sh.Melee.MHMax, sh.Melee.MHSpeed)
	curAvg := (curLo + curHi) / 2
	if math.Abs(d.MHAvg-(60-curAvg)) > 1e-9 {
		t.Errorf("el cambio de daño medio es %.2f, debe ser %.2f", d.MHAvg, 60-curAvg)
	}
	if math.Abs(d.MHSpread-(20-(curHi-curLo))) > 1e-9 {
		t.Errorf("el cambio de amplitud es %.2f", d.MHSpread)
	}
	// The model swings the candidate's own range: 50 to 70, average 60.
	_, m2, err := New().prepare(theory.Input{Sheet: sh, Data: siteData(t), Build: theory.Build{Delta: d}})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(m2.mhMin-50) > 1e-9 || math.Abs(m2.mhMax-70) > 1e-9 {
		t.Errorf("el arma del modelo va de %.1f a %.1f, debe ir de 50 a 70", m2.mhMin, m2.mhMax)
	}
}

func hasNote(list []string, part string) bool {
	for _, s := range list {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

// A stance change keeps only the rage Tactical Mastery retains; the parameter is read by the model.
func TestStanceSwitchesAreWiredAndCostRage(t *testing.T) {
	base := inputL20(t)
	base.Build.Rotation = []string{"execute", "mortal_strike", "bloodthirst", "overpower", "rend", "hs"}
	plain, err := New().Evaluate(base)
	if err != nil {
		t.Fatal(err)
	}
	with := inputL20(t)
	with.Build.Rotation = base.Build.Rotation
	with.Build.Params = map[string]float64{"cambios_postura": 3}
	switched, err := New().Evaluate(with)
	if err != nil {
		t.Fatal(err)
	}
	if !hasNote(switched.Supuestos, "Cambios de postura: 3") {
		t.Fatalf("los cambios de postura deben decirse en los supuestos: %v", switched.Supuestos)
	}
	if switched.DPS.Mean > plain.DPS.Mean+1e-9 {
		t.Errorf("cambiar de postura no puede subir el daño: %.2f frente a %.2f", switched.DPS.Mean, plain.DPS.Mean)
	}
}
