package warrior

// The model: a sheet and a build compiled into the numbers one fight reads. Talent effects are read
// relative to the sheet: the sheet's crit, parry and defence already include the talents the character
// has, so a build only adds the difference. Damage bonuses are not in the sheet and are applied in full.
//
// Identity is by spell id (ids.go): the sheet's spells and talents are matched by their ids, so the
// model reads the same character in any client language. Names only label the report.

import (
	"fmt"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// tacticalMasteryLevel is the level the Tactical Mastery ability is learned at (site: level 14).
const tacticalMasteryLevel = 14

// talentEffects are the talent numbers the fight reads, for one rank set.
type talentEffects struct {
	critPts     float64 // crit points from talents (cruelty; weaponmaster for axes and polearms)
	parryPts    float64
	defPts      float64
	hsCost      float64 // rage cost reductions
	exCost      float64
	focus       float64
	sunderCost  float64
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
	revengePct  float64 // Revenge damage, percent
	brPct       float64 // Bloodrage rage, percent
	brInstant   float64 // rage Berserker Rage generates at once
	tmRetain    float64 // rage kept on a stance change, on top of the base
	sweeping    bool
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
		sunderCost:  -v("improved-sunder-armor"),
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
		revengePct:  v("improved-revenge"),
		brPct:       v("improved-bloodrage"),
		brInstant:   v("improved-berserker-rage"),
		tmRetain:    v("improved-tactical-mastery"),
		sweeping:    t.has(ranks, "sweeping-strikes"),
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
	known      bool
	source     string
	cost       float64
	cd         float64
	cast       float64
	weapon     bool    // weapon damage plus Bonus
	bonus      float64 // flat damage
	apPct      float64 // Attack Power coefficient, percent
	mult       float64 // damage multiplier from talents (0 means 1)
	bleed      float64
	bleedSec   float64
	rageConv   float64 // Execute: extra damage per point of rage spent above the cost
	armorStack float64 // Sunder Armor: armour removed per stack
	maxStacks  float64
	dur        float64 // seconds: a buff's length, a window, or Sunder's stack length
	buffAP     float64 // Battle Shout: attack power given to the group
	rage       float64 // rage gained at once (Bloodrage, Berserker Rage)
	rage2      float64 // Bloodrage: rage gained over its length
	critBonus  float64 // Recklessness: critical strike chance, percent
	takenPct   float64 // Recklessness: damage taken increase, percent
}

// damageMult is the talent multiplier of an ability (1 when none).
func (a *ability) damageMult() float64 {
	if a.mult == 0 {
		return 1
	}
	return a.mult
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
	// bsAP is the attack power Battle Shout adds when the rotation has it. buffedBS is true when the sheet
	// already shows the buff: its attack power is in the sheet and is not added again.
	bsAP     float64
	buffedBS bool
	// retain is the rage kept when the stance changes (Tactical Mastery); switches is how many times the
	// build changes stance in one fight (the parameter cambios_postura, 0 by default).
	retain   float64
	switches int
	// targets is the number of enemies pulled together (1 or 2).
	targets int
	// mob
	elite    bool
	mobLevel int
	fixedMob bool
	// notes for the report
	notes []string
}

// compile turns the input into a model. The build is the talents the input gives by name, or the sheet's
// own talents when it gives none.
func compile(in theory.Input, tree *Tree, k Constants) (*model, error) {
	if in.Build.Talents == nil {
		return compileRanks(in, tree, k, nil)
	}
	buildRanks, err := tree.Ranks(in.Build.Talents)
	if err != nil {
		return nil, err
	}
	return compileRanks(in, tree, k, buildRanks)
}

// sheetTalentRanks reads the talents of the sheet into a rank slice, matching each by its spell id and
// by its English name only when the id is not known. The talents it cannot match are returned by name
// (with their id), so the report can say so instead of guessing.
func sheetTalentRanks(tree *Tree, sh theory.Sheet) ([]int, []string, error) {
	out := make([]int, len(tree.Defs))
	var unknown []string
	for _, tab := range sh.Talents.Tabs {
		for _, tal := range tab.Talents {
			if tal.Rank <= 0 {
				continue
			}
			i, ok := -1, false
			if slug, known := talentSlugOf(tal.SpellID); known {
				i, ok = tree.Index(slug)
			}
			if !ok {
				i, ok = tree.Index(tal.Name)
			}
			if !ok {
				unknown = append(unknown, fmt.Sprintf("%s (id %d)", tal.Name, tal.SpellID))
				continue
			}
			if tal.Rank > tree.Defs[i].Max {
				return nil, nil, fmt.Errorf("la hoja tiene %s en rango %d, por encima del máximo %d", tree.Defs[i].Name, tal.Rank, tree.Defs[i].Max)
			}
			out[i] = tal.Rank
		}
	}
	return out, unknown, nil
}

// compileRanks is compile for a rank slice; the optimiser calls it for each candidate build. A nil rank
// slice means the sheet's own talents.
func compileRanks(in theory.Input, tree *Tree, k Constants, buildRanks []int) (*model, error) {
	sh := in.Sheet
	if sh.Class != "" && sh.Class != "WARRIOR" {
		return nil, fmt.Errorf("la hoja es de %s, no de guerrero", sh.Class)
	}
	if sh.Melee.MHMax <= 0 || sh.Melee.MHSpeed <= 0 {
		return nil, fmt.Errorf("la hoja no trae el arma de la mano derecha (daño y velocidad)")
	}
	m := &model{k: k, tree: tree, level: sh.Level}
	sheetRanks, unknown, err := sheetTalentRanks(tree, sh)
	if err != nil {
		return nil, err
	}
	// The tree rules are checked only when every talent of the sheet is known: an unknown talent may hold
	// the points a row needs, and the check would then fail for a reason the report cannot show.
	if len(unknown) == 0 {
		if err := tree.Valid(sheetRanks); err != nil {
			return nil, fmt.Errorf("la hoja tiene talentos que no siguen las reglas del árbol: %v", err)
		}
	}
	if buildRanks == nil {
		buildRanks = sheetRanks
	} else if len(unknown) == 0 {
		if err := tree.Valid(buildRanks); err != nil {
			return nil, fmt.Errorf("build no válida: %v", err)
		}
	}
	if len(unknown) > 0 {
		m.notes = append(m.notes, "Talentos de la hoja sin identificar (su id de hechizo no está en la tabla, no se valoran): "+strings.Join(unknown, ", ")+".")
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
	// The average moves by MHAvg (both ends move by it) and the range by MHSpread (split between the ends).
	m.mhMin = sh.Melee.MHMin + d.MHAvg - d.MHSpread/2
	m.mhMax = sh.Melee.MHMax + d.MHAvg + d.MHSpread/2
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
	m.targets = in.Opt.Targets
	if m.targets != 2 {
		m.targets = 1
	}
	if v, ok := in.Build.Params["cambios_postura"]; ok && v > 0 {
		m.switches = int(v)
	}
	if m.level >= tacticalMasteryLevel {
		m.retain = k.TacticalRetain + m.tfx.tmRetain
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
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sheetSpells indexes the sheet's spells by rotation key, by spell id. A known spell wins over an
// unknown one with the same id.
func sheetSpells(sh theory.Sheet) map[string]theory.SheetSpell {
	out := map[string]theory.SheetSpell{}
	for _, sp := range sh.Spells {
		key, ok := abilityKeyOf(sp.SpellID)
		if !ok {
			continue
		}
		if prev, seen := out[key]; seen && prev.Known && !sp.Known {
			continue
		}
		out[key] = sp
	}
	return out
}

// buffedBattleShout reports whether the sheet shows the Battle Shout buff: its attack power is then
// already in the sheet and the rotation must not add it again.
func buffedBattleShout(sh theory.Sheet) bool {
	for _, id := range sh.Buffs {
		if key, ok := abilityKeyOf(id); ok && key == "battle_shout" {
			return true
		}
	}
	return false
}

// compileAbilities fills the ability table: the sheet's tooltip when the build is the sheet's build,
// the site's table otherwise, the constants when neither has the ability.
func (m *model) compileAbilities(in theory.Input, sameAsSheet bool, sheetFX talentEffects) {
	L := m.level
	spells := sheetSpells(in.Sheet)
	hasSheetSpells := len(in.Sheet.Spells) > 0
	m.buffedBS = buffedBattleShout(in.Sheet)
	for i, key := range abilityKeys {
		a := &m.abil[i]
		name := abilityNames[i]
		table := baseFromTable(name, key, in.Data, L, a)
		a.known = table
		fromSheet := false
		// The spell list of the sheet decides what is known when it has the spell.
		if sp, ok := spells[key]; ok && hasSheetSpells {
			a.known = sp.Known
			if sp.Known {
				a.cost, a.cd, a.cast = sp.Cost, sp.Cooldown, sp.CastTime
				if sameAsSheet && readTooltip(key, sp.Tooltip, a) {
					a.source = srcSheet
				} else {
					// The sheet's cost and cooldown include the sheet's talents: only the difference is applied.
					fromSheet = true
				}
			}
		}
		// Talent-granted abilities.
		switch key {
		case "mortal_strike":
			if m.tfx.grantMS {
				a.known = true
				if a.source != srcSheet && !table {
					a.source = srcTalent
					if t, ok := talentText(in.Data, "mortal-strike"); ok {
						readEffects(key, t, a)
					}
				}
			}
		case "bloodthirst":
			if m.tfx.grantBT {
				a.known = true
				if a.source != srcSheet && !table {
					a.source = srcTalent
					if t, ok := talentText(in.Data, "bloodthirst"); ok {
						readEffects(key, t, a)
					}
				}
			}
		case "death_wish":
			a.known = m.tfx.grantDW
			a.source = srcTalent
		case "sweeping_strikes":
			if m.tfx.sweeping {
				a.known = true
				if a.source == "" {
					a.source = srcTalent
				}
			}
		}
		if !a.known {
			continue
		}
		if a.source == "" {
			a.source = srcConstant
			if table {
				a.source = srcTable
			}
		}
		m.fallbacks(key, a)
		m.talentMods(key, a, fromSheet, sheetFX)
		if key == "battle_shout" && m.buffedBS {
			// The sheet's buff already counts; the rotation does not add it again.
			m.notes = append(m.notes, "Grito de batalla ya activo en la hoja: su poder de ataque ya cuenta y no se suma otra vez.")
			a.known = false
		}
	}
}

// fallbacks fills the numbers no source gave: the constants table, which holds the classic rules.
func (m *model) fallbacks(key string, a *ability) {
	k := m.k
	if a.cost == 0 && key != "death_wish" {
		a.cost = k.FallbackCost[key]
	}
	switch key {
	case "rend":
		if a.bleed == 0 {
			a.bleed = k.RendFallback
			a.bleedSec = 12
			a.source = srcConstant
		}
	case "hs":
		if a.bonus == 0 && !a.weapon {
			a.bonus = k.HSFallback
			a.source = srcConstant
		}
	case "execute":
		if a.bonus == 0 {
			a.bonus = 600
			a.source = srcConstant
		}
	case "overpower":
		if a.cd == 0 {
			a.cd = 5
		}
	case "whirlwind":
		a.weapon = true
		if a.cd == 0 {
			a.cd = k.WhirlCD
		}
	case "mortal_strike":
		a.weapon = true
		if a.cd == 0 {
			a.cd = k.MortalCD
		}
	case "bloodthirst":
		if a.cd == 0 {
			a.cd = k.BloodthirstCD
		}
	case "slam":
		if a.cd == 0 {
			a.cd = k.SlamCD
			a.cast = k.SlamCast
		}
	case "death_wish":
		a.cd = k.DeathWishCD
	case "sunder":
		if a.armorStack == 0 {
			a.armorStack, a.maxStacks, a.dur = k.SunderStack, k.SunderMaxStacks, k.SunderLen
			a.source = srcConstant
		}
	case "revenge":
		if a.bonus == 0 {
			a.bonus, a.apPct = (k.RevengeMin+k.RevengeMax)/2, k.RevengeAP
			a.source = srcConstant
		}
		if a.cd == 0 {
			a.cd = k.RevengeWindow
		}
	case "battle_shout":
		if a.buffAP == 0 {
			a.buffAP, a.dur = k.BattleShoutAP, k.BattleShoutLen
			a.source = srcConstant
		}
	case "bloodrage":
		if a.rage == 0 {
			a.rage, a.rage2, a.dur = 10, 10, k.BloodrageLen
		}
		if a.cd == 0 {
			a.cd = k.BloodrageCD
		}
	case "berserker_rage":
		a.rage = m.tfx.brInstant
		if a.cd == 0 {
			a.cd = k.BerserkerCD
		}
		if a.dur == 0 {
			a.dur = k.BerserkerLen
		}
	case "victory_rush":
		if a.bonus == 0 {
			a.bonus, a.apPct = 1, k.VictoryAP
		}
		if a.dur == 0 {
			a.dur = k.VictoryWindow
		}
		if a.cd == 0 {
			a.cd = k.VictoryCD
		}
	case "recklessness":
		if a.cd == 0 {
			a.cd = k.RecklessCD
		}
		if a.dur == 0 {
			a.dur = k.RecklessLen
		}
		a.critBonus, a.takenPct = k.RecklessCrit, k.RecklessTake
	case "sweeping_strikes":
		if a.cost == 0 {
			a.cost = k.SweepCost
		}
		if a.cd == 0 {
			a.cd = k.SweepCD
		}
	}
}

// talentMods applies the talents to a number that does not already include them. A cost or cooldown read
// from the sheet includes the sheet's talents, so only the difference between the build and the sheet
// applies (ref); a number from the site's table or the constants carries none, so the full effect applies.
func (m *model) talentMods(key string, a *ability, fromSheet bool, sheetFX talentEffects) {
	if a.source == srcSheet {
		return
	}
	ref := talentEffects{}
	if fromSheet {
		ref = sheetFX
	}
	t := m.tfx
	switch key {
	case "hs":
		a.cost = max0(a.cost - (t.hsCost - ref.hsCost))
	case "execute":
		a.cost = max0(a.cost - (t.exCost - ref.exCost))
	case "rend":
		a.bleed *= 1 + t.rendPct/100
	case "whirlwind":
		a.cd = max0(a.cd - (t.wwCut - ref.wwCut))
		a.mult = a.damageMult() * (1 + t.wwDmg/100)
	case "slam":
		a.cd = max0(a.cd - (t.slamCut - ref.slamCut))
		a.cast = max0(a.cast - (t.slamCast - ref.slamCast))
	case "sunder":
		a.cost = max0(a.cost - (t.sunderCost - ref.sunderCost))
	case "revenge":
		a.mult = a.damageMult() * (1 + t.revengePct/100)
	case "bloodrage":
		a.rage *= 1 + t.brPct/100
		a.rage2 *= 1 + t.brPct/100
	}
	switch key {
	case "mortal_strike", "overpower", "cleave", "slam", "rend", "bloodthirst", "whirlwind", "revenge":
		a.cost = max0(a.cost - (t.focus - ref.focus))
	}
}

// talentText returns the client text of the talent's first rank, by slug.
func talentText(d theory.Data, slug string) (string, bool) {
	for _, t := range d.Talents {
		if t.Slug == slug || slugOf(t.Name) == slug {
			if txt, ok := t.RankText[1]; ok && txt != "" {
				return txt, true
			}
			return "", false
		}
	}
	return "", false
}

// compileRotation turns the priority names into ability indices. Abilities the character does not have
// are left out and named in the notes.
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
		if i == kBattleShout && m.buffedBS {
			continue // the sheet's buff counts already (see compileAbilities)
		}
		if i == kBattleShout && m.abil[i].known {
			// Battle Shout's attack power counts when the rotation has it (see fight: it lasts the whole fight).
			m.bsAP = m.abil[i].buffAP
		}
		if !m.abil[i].known {
			m.notes = append(m.notes, fmt.Sprintf("%s no está en la rotación: el personaje no la tiene", abilityNames[i]))
			continue
		}
		m.order = append(m.order, i)
	}
	return nil
}
