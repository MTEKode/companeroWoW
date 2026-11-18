package warrior

// The model: a sheet and a build compiled into the numbers one fight reads. Talent effects are read
// relative to the sheet: the sheet's crit, parry and defence already include the talents the character
// has, so a build only adds the difference. Damage bonuses are not in the sheet and are applied in full.

import (
	"fmt"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// talentEffects are the talent numbers the fight reads, for one rank set.
type talentEffects struct {
	critPts     float64 // crit points from talents (cruelty; weaponmaster for axes and polearms)
	parryPts    float64
	defPts      float64
	hsCost      float64 // rage cost reductions
	exCost      float64
	focus       float64
	owCrit      float64
	impale      float64 // crit damage bonus, percent of the base multiplier
	h2          float64 // two-handed damage, percent
	dwDmg       float64 // offhand damage, percent
	dwRage      float64 // offhand rage, percent
	unbridled   float64 // chance, percent
	bloodthrill float64 // chance, percent
	deepWounds  float64 // percent of the weapon average, per crit
	flurry      float64 // attack speed, percent
	enrage      float64 // damage, percent
	wmArmor     float64 // armour ignored by a mace or staff, percent
	wmExtra     float64 // extra attack chance with a sword, percent
	furious     float64 // offhand hit points
	anger       bool
	rendPct     float64 // Rend damage, percent
	slamCut     float64 // seconds
	slamCast    float64 // seconds
	wwCut       float64 // seconds
	wwDmg       float64 // percent
	grantMS     bool
	grantBT     bool
	grantDW     bool
}

// talentsOf reads the talent effects of a rank set for a weapon family.
func talentsOf(t *Tree, ranks []int, kind string) talentEffects {
	v := func(slug string) float64 { return t.value(ranks, slug) }
	e := talentEffects{
		critPts:     v("cruelty"),
		parryPts:    v("deflection"),
		defPts:      v("anticipation"),
		hsCost:      -v("improved-heroic-strike"),
		exCost:      -v("improved-execute"),
		focus:       -v("focused-rage"),
		owCrit:      v("improved-overpower"),
		impale:      v("impale"),
		h2:          v("two-handed-weapon-specialization"),
		dwDmg:       v("dual-wield-specialization"),
		dwRage:      v("dual-wield-specialization-rage"),
		unbridled:   v("unbridled-wrath"),
		bloodthrill: v("bloodthrill"),
		deepWounds:  v("deep-wounds"),
		flurry:      v("flurry"),
		enrage:      v("enrage"),
		furious:     v("furious-precision"),
		anger:       t.has(ranks, "anger-management"),
		rendPct:     v("improved-rend"),
		slamCut:     v("improved-slam"),
		slamCast:    v("improved-slam-cast"),
		wwCut:       v("improved-whirlwind-cd"),
		wwDmg:       v("improved-whirlwind"),
		grantMS:     t.has(ranks, "mortal-strike"),
		grantBT:     t.has(ranks, "bloodthirst"),
		grantDW:     t.has(ranks, "death-wish"),
	}
	switch kind {
	case "axe", "polearm":
		e.critPts += v("weaponmaster")
	case "mace", "staff":
		e.wmArmor = v("weaponmaster-armor")
	case "sword":
		e.wmExtra = v("weaponmaster-extra")
	}
	return e
}

// ability is one ability as the fight uses it (see abilities.go for the sources).
type ability struct {
	known    bool
	source   string
	cost     float64
	cd       float64
	cast     float64
	weapon   bool    // weapon damage plus Bonus
	bonus    float64 // flat damage
	apPct    float64 // Attack Power coefficient, percent
	bleed    float64
	bleedSec float64
}

// model is a compiled sheet and build.
type model struct {
	k     Constants
	tree  *Tree
	level int
	kind  string
	twoH  bool
	ranks []int
	tfx   talentEffects
	// character numbers, effective for the build
	ap, crit, hit, skill, expertise float64
	mhMin, mhMax, mhSpeed           float64
	ohOn                            bool
	ohMin, ohMax, ohSpeed           float64
	dodge, parry, defSkill, block   float64
	blockValue, armor, hpMax        float64
	// abilities and the rotation
	abil    [nAbil]ability
	order   []int
	hsAbove float64
	// mob
	elite    bool
	mobLevel int
	fixedMob bool
	// notes for the report
	notes []string
}

// compile turns the input into a model. sheetRanks are the sheet's own talent ranks.
func compile(in theory.Input, tree *Tree, k Constants) (*model, error) {
	talents := in.Build.Talents
	if talents == nil {
		talents = in.Sheet.TalentRanks() // no build given: the character's own talents
	}
	buildRanks, err := tree.Ranks(talents)
	if err != nil {
		return nil, err
	}
	return compileRanks(in, tree, k, buildRanks)
}

// compileRanks is compile for a rank slice; the optimiser calls it for each candidate build.
func compileRanks(in theory.Input, tree *Tree, k Constants, buildRanks []int) (*model, error) {
	sh := in.Sheet
	if sh.Class != "" && sh.Class != "WARRIOR" {
		return nil, fmt.Errorf("la hoja es de %s, no de guerrero", sh.Class)
	}
	if sh.Melee.MHMax <= 0 || sh.Melee.MHSpeed <= 0 {
		return nil, fmt.Errorf("la hoja no trae el arma de la mano derecha (daño y velocidad)")
	}
	m := &model{k: k, tree: tree, level: sh.Level}
	sheetRanks, err := tree.Ranks(sh.TalentRanks())
	if err != nil {
		return nil, fmt.Errorf("la hoja tiene talentos que no están en la hoja de talentos: %v", err)
	}
	if err := tree.Valid(buildRanks); err != nil {
		return nil, fmt.Errorf("build no válida: %v", err)
	}
	kind, twoH, ok := sh.WeaponKind()
	if !ok {
		kind = ""
	}
	m.kind, m.twoH = kind, twoH
	m.ranks = buildRanks
	m.tfx = talentsOf(tree, buildRanks, kind)
	sheetFX := talentsOf(tree, sheetRanks, kind)
	d := in.Build.Delta
	same := sameRanks(sheetRanks, buildRanks)

	// Talent differences against the sheet (the sheet already has its talents).
	dCrit := m.tfx.critPts - sheetFX.critPts
	dParry := m.tfx.parryPts - sheetFX.parryPts
	dDef := m.tfx.defPts - sheetFX.defPts

	agi := sh.Total.Agi + d.Agi
	m.ap = sh.Melee.AP + d.AP + k.APPerStrength*d.Str
	m.crit = sh.Melee.Crit + d.Crit + dCrit + (agi-sh.Total.Agi)/k.AgiPerCrit + k.CritBaseTalent
	m.hit = sh.Melee.Hit + d.Hit
	m.skill = sh.Melee.MHSkill + d.Skill
	if m.skill <= 0 {
		m.skill = k.DefPerSkill * float64(sh.Level)
	}
	m.expertise = sh.Melee.Expertise + d.Expertise
	m.mhMin = sh.Melee.MHMin + d.MHAvg/2
	m.mhMax = sh.Melee.MHMax + d.MHAvg/2
	m.mhSpeed = sh.Melee.MHSpeed + d.MHSpeed
	if sh.Melee.OHMin != nil && sh.Melee.OHMax != nil && sh.Melee.OHSpeed != nil && *sh.Melee.OHSpeed > 0 {
		m.ohOn = true
		m.ohMin, m.ohMax, m.ohSpeed = *sh.Melee.OHMin, *sh.Melee.OHMax, *sh.Melee.OHSpeed
	}
	m.dodge = sh.Defense.Dodge + d.Dodge
	m.parry = sh.Defense.Parry + d.Parry + dParry
	m.defSkill = sh.Defense.DefenseSkill + dDef
	if m.defSkill <= 0 {
		m.defSkill = k.DefPerSkill * float64(sh.Level)
	}
	m.block = sh.Defense.Block
	m.blockValue = sh.Defense.BlockValue
	m.armor = sh.ArmorEffective + d.Armor
	if m.armor <= 0 {
		m.armor = sh.Armor + d.Armor
	}
	m.hpMax = sh.HealthMax + d.Sta*k.HPPerStamina
	if m.hpMax <= 0 {
		return nil, fmt.Errorf("la hoja no trae la vida máxima")
	}
	m.compileAbilities(in, same, sheetFX)
	if err := m.compileRotation(in.Build.Rotation); err != nil {
		return nil, err
	}
	if v, ok := in.Build.Params["hs_sobre"]; ok {
		m.hsAbove = v
	}
	m.elite = in.Opt.Elite
	if in.Opt.MobLevel > 0 {
		m.mobLevel = in.Opt.MobLevel
		m.fixedMob = true
	}
	return m, nil
}

// sameRanks reports whether two rank sets are the same build.
func sameRanks(a, b []int) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// compileAbilities fills the ability table: the sheet's tooltip when the build is the sheet's build,
// the site's table otherwise, the constants when neither has the ability.
func (m *model) compileAbilities(in theory.Input, sameAsSheet bool, sheetFX talentEffects) {
	k := m.k
	L := m.level
	for i, key := range abilityKeys {
		a := &m.abil[i]
		fromSheet := false
		name := abilityNames[i]
		table := baseFromTable(name, in.Data, L, a)
		if table {
			a.known = true
		}
		// The spell list of the sheet decides what is known when it has one.
		if sp, ok := in.Sheet.SpellByName(name); ok && len(in.Sheet.Spells) > 0 {
			a.known = sp.Known
			if sp.Known && sameAsSheet {
				a.source = srcSheet
				a.cost, a.cd, a.cast = sp.Cost, sp.Cooldown, sp.CastTime
				parseText(sp.Tooltip, a)
			} else if sp.Known {
				a.cost, a.cd = sp.Cost, sp.Cooldown
				fromSheet = true
			}
		}
		if !table && a.source != srcSheet {
			a.source = srcConstant
		} else if a.source == "" {
			a.source = srcTable
		}
		// Talent-granted abilities.
		switch key {
		case "mortal_strike":
			if m.tfx.grantMS {
				a.known = true
				if a.cost == 0 {
					a.cost = k.FallbackCost[key]
				}
				if a.cd == 0 {
					a.cd = k.MortalCD
				}
				a.weapon = true
				if t, ok := talentText(in.Data, "Mortal Strike"); ok {
					parseText(t, a)
				}
				if a.source == srcConstant {
					a.source = srcTalent
				}
			} else if !table {
				a.known = false
			}
		case "bloodthirst":
			if m.tfx.grantBT {
				a.known = true
				if a.cost == 0 {
					a.cost = k.FallbackCost[key]
				}
				if a.cd == 0 {
					a.cd = k.BloodthirstCD
				}
				if t, ok := talentText(in.Data, "Bloodthirst"); ok {
					parseText(t, a)
				}
				a.source = srcTalent
			}
		case "death_wish":
			a.known = m.tfx.grantDW
			a.cd = k.DeathWishCD
			a.source = srcTalent
		}
		if !a.known && key != "death_wish" {
			continue
		}
		// Fallbacks for numbers no source gave.
		if a.cost == 0 && key != "death_wish" {
			a.cost = k.FallbackCost[key]
		}
		if key == "rend" && a.bleed == 0 {
			a.bleed = k.RendFallback
			a.bleedSec = 12
			a.source = srcConstant
		}
		if key == "hs" && a.bonus == 0 && !a.weapon {
			a.bonus = k.HSFallback
			a.source = srcConstant
		}
		if key == "whirlwind" {
			a.weapon = true
			if a.cd == 0 {
				a.cd = k.WhirlCD
			}
		}
		if key == "slam" && a.cd == 0 {
			a.cd = k.SlamCD
			a.cast = k.SlamCast
		}
		// Talent modifiers. A cost or cooldown read from the sheet already includes the sheet's talents,
		// so only the difference between the build and the sheet is applied to it (ref). A number from
		// the site's table carries no talents, so the full effect applies.
		if a.source != srcSheet {
			ref := talentEffects{}
			if fromSheet {
				ref = sheetFX
			}
			switch key {
			case "hs":
				a.cost = max0(a.cost - (m.tfx.hsCost - ref.hsCost))
			case "execute":
				a.cost = max0(a.cost - (m.tfx.exCost - ref.exCost))
			case "rend":
				a.bleed *= 1 + m.tfx.rendPct/100
			case "whirlwind":
				a.cd = max0(a.cd - (m.tfx.wwCut - ref.wwCut))
				a.bonus *= 1 + m.tfx.wwDmg/100
			case "slam":
				a.cd = max0(a.cd - (m.tfx.slamCut - ref.slamCut))
				a.cast = max0(a.cast - (m.tfx.slamCast - ref.slamCast))
			}
			if key == "mortal_strike" || key == "overpower" || key == "cleave" || key == "slam" || key == "rend" {
				a.cost = max0(a.cost - (m.tfx.focus - ref.focus))
			}
		}
		if key == "execute" {
			a.bonus = firstPositive(a.bonus, 600)
		}
		if key == "execute" && a.bonus == 0 {
			a.source = srcConstant
		}
		if key == "overpower" && a.cd == 0 {
			a.cd = 5
		}
	}
}

func firstPositive(v, def float64) float64 {
	if v > 0 {
		return v
	}
	return def
}

// talentText returns the client text of the talent's first rank.
func talentText(d theory.Data, name string) (string, bool) {
	for _, t := range d.Talents {
		if strings.EqualFold(t.Name, name) {
			if txt, ok := t.RankText[1]; ok && txt != "" {
				return txt, true
			}
			return "", false
		}
	}
	return "", false
}

// compileRotation turns the priority names into ability indices. Abilities the character does not
// have are left out and named in the notes.
func (m *model) compileRotation(list []string) error {
	if len(list) == 0 {
		list = []string{"execute", "mortal_strike", "bloodthirst", "death_wish", "overpower", "slam", "rend", "cleave", "hs"}
	}
	seen := map[int]bool{}
	for _, key := range list {
		i, ok := abilityIndex(key)
		if !ok {
			return fmt.Errorf("habilidad desconocida en la rotación: %q", key)
		}
		if seen[i] {
			continue
		}
		seen[i] = true
		if !m.abil[i].known {
			m.notes = append(m.notes, fmt.Sprintf("%s no está en la rotación: el personaje no la tiene", abilityNames[i]))
			continue
		}
		m.order = append(m.order, i)
	}
	return nil
}
