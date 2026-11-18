package warrior

// The warrior implementation of theory.Class: evaluation, stat weights, item comparison, the talent
// search, the rotation comparison, calibration and the fight review. Every number comes from the
// simulation or from the constants table; the language model never sees a number it did not get here.

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// Defaults of the calculations.
const (
	defaultFights  = 400
	searchFights   = 40
	reviewFights   = 200
	defaultSeed    = 20261118
	bootstrapRound = 200
	calibTolerance = 3.0 // percent
	talentBeam     = 5
	talentPoints   = 10
)

// Warrior is the warrior model. The zero value uses Table.
type Warrior struct {
	K Constants
}

// New returns the warrior model with the constants table.
func New() Warrior { return Warrior{K: Table} }

// Key is the class token.
func (Warrior) Key() string { return "WARRIOR" }

// Name is the Spanish name.
func (Warrior) Name() string { return "Guerrero" }

func (w Warrior) constants() Constants {
	if w.K.Attack.ArmorK == 0 {
		return Table
	}
	return w.K
}

// options fills the defaults of the simulation settings.
func options(o theory.Options, fights int) theory.Options {
	if o.Fights <= 0 {
		o.Fights = fights
	}
	if o.Seed == 0 {
		o.Seed = defaultSeed
	}
	return o
}

// prepare builds the tree and the model of an input.
func (w Warrior) prepare(in theory.Input) (*Tree, *model, error) {
	if len(in.Data.Talents) == 0 {
		return nil, nil, fmt.Errorf("no tengo el árbol de talentos del guerrero (wowforevertalents.com)")
	}
	tree, err := NewTree(in.Data.Talents)
	if err != nil {
		return nil, nil, err
	}
	m, err := compile(in, tree, w.constants())
	if err != nil {
		return nil, nil, err
	}
	return tree, m, nil
}

// runFights simulates the fights of a model. The mob level is drawn from the leveling window (level±2)
// unless the options fix it; the draws come from the seed, so the series is the same every time.
func (m *model) runFights(opt theory.Options) []fightRec {
	recs := make([]fightRec, opt.Fights)
	for i := range recs {
		// Each fight has its own stream, so the same fight index draws the same mob level and rolls
		// in every build compared with the same seed (paired comparisons).
		rng := theory.NewRand(opt.Seed + uint64(i)*0x9e3779b97f4a7c15)
		lvl := m.mobLevel
		if !m.fixedMob {
			lvl = m.level + rng.IntN(5) - 2
		}
		if lvl < 1 {
			lvl = 1
		}
		recs[i] = m.fight(rng, m.k.Mob, lvl, m.elite)
	}
	return recs
}

// cycle is the time one fight costs, with what follows it: eating and resetting after a kill, the
// death penalty after a death.
func (m *model) cycle(r fightRec) (kill float64, cyc float64) {
	if r.kill {
		d := m.descanso(r)
		return float64(max(r.kills, 1)), r.dur + d
	}
	if r.died {
		return 0, r.dur + m.k.DeathPenalty
	}
	return 0, r.dur
}

// descanso is the seconds to eat back the health a kill cost, plus the reset.
func (m *model) descanso(r fightRec) float64 {
	rate := m.k.FoodFraction * m.hpMax / m.k.FoodSeconds
	return r.taken/rate + m.k.Reset
}

// kph is kills per hour over the fights chosen by idx (all when idx is nil).
func (m *model) kph(recs []fightRec, idx []int) float64 {
	var k, c float64
	n := len(recs)
	pick := func(i int) {
		kill, cyc := m.cycle(recs[i])
		k += kill
		c += cyc
	}
	if idx == nil {
		for i := range recs {
			pick(i)
		}
	} else {
		n = len(idx)
		for _, i := range idx {
			pick(i)
		}
	}
	if c <= 0 || n == 0 {
		return 0
	}
	return 3600 * k / c
}

// kphCI is the kills per hour with its 95 % interval, from a bootstrap over the fights.
func (m *model) kphCI(recs []fightRec, seed uint64) theory.CI {
	n := len(recs)
	mean := m.kph(recs, nil)
	if n < 2 {
		return theory.CI{Mean: mean, Low: mean, High: mean}
	}
	rng := theory.NewRand(seed ^ 0xb007)
	samples := make([]float64, bootstrapRound)
	idx := make([]int, n)
	for b := range samples {
		for j := range idx {
			idx[j] = rng.IntN(n)
		}
		samples[b] = m.kph(recs, idx)
	}
	sort.Float64s(samples)
	lo := samples[int(0.025*float64(bootstrapRound))]
	hi := samples[int(0.975*float64(bootstrapRound))-1]
	return theory.CI{Mean: mean, Low: lo, High: hi}
}

// diffCI is the paired difference of kills per hour between two runs of the same seed.
func (m *model) diffCI(a, b []fightRec, seed uint64) theory.CI {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	mean := m.kph(b[:n], nil) - m.kph(a[:n], nil)
	if n < 2 {
		return theory.CI{Mean: mean, Low: mean, High: mean}
	}
	rng := theory.NewRand(seed ^ 0xd1ff)
	samples := make([]float64, bootstrapRound)
	idx := make([]int, n)
	for s := range samples {
		for j := range idx {
			idx[j] = rng.IntN(n)
		}
		samples[s] = m.kph(b[:n], idx) - m.kph(a[:n], idx)
	}
	sort.Float64s(samples)
	return theory.CI{
		Mean: mean,
		Low:  samples[int(0.025*float64(bootstrapRound))],
		High: samples[int(0.975*float64(bootstrapRound))-1],
	}
}

// dpsCI is the damage per second of the fights with its 95 % interval.
func dpsCI(recs []fightRec) theory.CI {
	v := make([]float64, 0, len(recs))
	for _, r := range recs {
		if r.dur > 0 {
			v = append(v, r.dealt/r.dur)
		}
	}
	return theory.CI95(v)
}

// summarize turns fights into the report.
func (m *model) summarize(recs []fightRec, opt theory.Options) theory.Report {
	rep := theory.Report{Clase: "WARRIOR", Nivel: m.level, Peleas: len(recs)}
	rep.DPS = dpsCI(recs)
	rep.KillsPerHour = m.kphCI(recs, opt.Seed)
	var ttk, descanso, muertes []float64
	var rage [rageSamples]float64
	var dealt, casts [nSlots]float64
	var totalDealt float64
	for _, r := range recs {
		if r.kill {
			ttk = append(ttk, r.dur)
			descanso = append(descanso, m.descanso(r))
		}
		if r.died {
			muertes = append(muertes, 1)
		} else {
			muertes = append(muertes, 0)
		}
		for i := range rage {
			rage[i] += r.rage[i]
		}
		for i := range dealt {
			dealt[i] += r.dealtBy[i]
		}
		for i := range casts {
			if i < nAbil {
				casts[i] += float64(r.casts[i])
			}
		}
		totalDealt += r.dealt
	}
	rep.TTK = theory.CI95(ttk)
	rep.Descanso = theory.CI95(descanso)
	rep.Muerte = theory.CI95(muertes).Mean
	for i := range rage {
		rep.Ira = append(rep.Ira, theory.RagePoint{
			Segundo: float64(i) * rageEvery,
			Media:   round1(rage[i] / float64(len(recs))),
		})
	}
	n := float64(len(recs))
	for i := 0; i < nSlots; i++ {
		var uses float64
		var label, source string
		switch {
		case i < nAbil:
			if !m.abil[i].known {
				continue
			}
			uses = casts[i] / n
			label = abilityNames[i]
			source = m.abil[i].source
		case i == slotAutoMH:
			label, source = "Golpe automático (mano derecha)", "modelo"
		case i == slotAutoOH:
			if !m.ohOn {
				continue
			}
			label, source = "Golpe automático (mano izquierda)", "modelo"
		case i == slotDeepWounds:
			if m.tfx.deepWounds == 0 {
				continue
			}
			label, source = "Golpes profundos (sangrado)", "talento"
		}
		if dealt[i] == 0 && uses == 0 {
			continue
		}
		st := theory.AbilityStat{Nombre: label, UsosPelea: round2(uses), Fuente: source}
		if totalDealt > 0 {
			st.DanoPorcen = round1(100 * dealt[i] / totalDealt)
		}
		if i < nAbil && casts[i] > 0 {
			st.DanoUso = round1(dealt[i] / casts[i])
		}
		rep.Habilidades = append(rep.Habilidades, st)
	}
	sort.SliceStable(rep.Habilidades, func(a, b int) bool {
		return rep.Habilidades[a].DanoPorcen > rep.Habilidades[b].DanoPorcen
	})
	rep.Supuestos = m.assumptions(opt)
	rep.Rotacion = m.orderNames()
	rep.Talentos = m.tree.Named(m.ranks)
	return rep
}

func (m *model) orderNames() []string {
	out := make([]string, 0, len(m.order))
	for _, i := range m.order {
		out = append(out, abilityKeys[i])
	}
	return out
}

// assumptions are the lines the report states about the model, so the explanation can repeat them.
func (m *model) assumptions(opt theory.Options) []string {
	lv := fmt.Sprintf("nivel %d ±2", m.level)
	if m.fixedMob {
		lv = fmt.Sprintf("nivel %d", m.mobLevel)
	}
	scenario := "Escenario: un enemigo"
	if m.targets == 2 {
		scenario = "Escenario: dos enemigos juntos (el segundo también te ataca; Barrido, Torbellino y Hendedura le pegan)"
	}
	out := []string{
		scenario + ".",
		"Enemigo " + lv + " normal (defensa 5 × nivel, armadura 25 × nivel, vida 50 + 25 × nivel, golpe medio 5 + nivel cada 2 s): aproximación del modelo.",
		"Pelea desde vida llena y 0 de ira, hasta que mueren los enemigos, tú mueres o pasan 300 s; una sola pelea a la vez.",
		"Tras morir, 60 s de penalización; tras matar, comer para recuperar la vida y 10 s de reinicio (aproximaciones).",
		"Ira: 7,5 × daño causado y 2,5 × daño recibido, divididos por la conversión del nivel (classic; algunas fuentes dan 15 en lugar de 7,5).",
	}
	var src []string
	for i := 0; i < nAbil; i++ {
		if m.abil[i].known {
			src = append(src, abilityNames[i]+": "+m.abil[i].source)
		}
	}
	out = append(out, "Números de habilidades (hoja > tabla del sitio > constantes): "+strings.Join(src, ", ")+".")
	if m.bsAP > 0 {
		out = append(out, fmt.Sprintf("Grito de batalla: +%.0f poder de ataque mientras dura (rotación); la hoja no lo tiene.", m.bsAP))
	}
	if m.buffedBS {
		out = append(out, "Grito de batalla ya activo en la hoja: su poder de ataque ya está en la hoja y no se suma.")
	}
	if m.abil[kSunder].known && m.order != nil && containsIdx(m.order, kSunder) {
		out = append(out, "Sunder Armor en la rotación: reduce la armadura del enemigo, sin otras reducciones del grupo (supuesto de tanque o de un solo guerrero).")
	}
	if m.switches > 0 {
		out = append(out, fmt.Sprintf("Cambios de postura: %d por pelea; la ira se queda en %.0f al cambiar (Táctica).", m.switches, m.retain))
	}
	used, unused := m.tree.Modelled(m.ranks)
	if len(unused) > 0 {
		out = append(out, "Talentos sin efecto en el modelo (no se valoran): "+strings.Join(unused, ", ")+".")
	}
	out = append(out, m.notes...)
	_ = used
	return out
}

// containsIdx reports whether an ability index is in an order list.
func containsIdx(list []int, i int) bool {
	for _, x := range list {
		if x == i {
			return true
		}
	}
	return false
}

// Evaluate simulates the build and returns the report.
func (w Warrior) Evaluate(in theory.Input) (theory.Report, error) {
	_, m, err := w.prepare(in)
	if err != nil {
		return theory.Report{}, err
	}
	opt := options(in.Opt, defaultFights)
	recs := m.runFights(opt)
	rep := m.summarize(recs, opt)
	rep.Calibracion = w.calibrate(in, m)
	return rep, nil
}

// Calibrate compares the model's derived numbers with the sheet.
func (w Warrior) Calibrate(in theory.Input) []theory.Check {
	tree, err := NewTree(in.Data.Talents)
	if err != nil {
		return []theory.Check{{Nombre: "talentos", Ok: true, Nota: "sin árbol de talentos: no se calibra"}}
	}
	m, err := compile(in, tree, w.constants())
	if err != nil {
		return []theory.Check{{Nombre: "modelo", Ok: true, Nota: err.Error()}}
	}
	return w.calibrate(in, m)
}

// calibrate checks what the sheet can check: attack power from strength, crit from agility and the sheet's
// talents, and attack speed. Damage per swing has no client reference in the sheet, so it is not compared.
func (w Warrior) calibrate(in theory.Input, m *model) []theory.Check {
	k := w.constants()
	sh := in.Sheet
	var out []theory.Check
	check := func(name string, model, sheet float64, note string) {
		c := theory.Check{Nombre: name, Modelo: round2(model), Hoja: round2(sheet), Nota: note}
		if sheet == 0 {
			c.Ok = true
			c.Nota = "sin dato en la hoja"
			out = append(out, c)
			return
		}
		c.Desvio = round1(100 * (model - sheet) / math.Abs(sheet))
		c.Ok = math.Abs(c.Desvio) <= calibTolerance
		out = append(out, c)
	}
	check("Poder de ataque base", k.APPerStrength*sh.Base.Str+k.APOffset, sh.Melee.APBase,
		"2 × fuerza base − 20 (classic, guerrero)")
	sheetTalents, _, err := sheetTalentRanks(m.tree, sh)
	critTalents := 0.0
	if err == nil {
		critTalents = talentsOf(m.tree, sheetTalents, m.kind).critPts
	}
	check("Crítico", sh.Total.Agi/k.AgiPerCrit+critTalents+k.CritBaseTalent, sh.Melee.Crit,
		"agilidad total / 20 + talentos (aprox.: la pendiente agilidad→crítico queda por verificar)")
	check("Velocidad de ataque", sh.Melee.MHSpeed, sh.Melee.AttackSpeed,
		"velocidad base del arma frente a la de la hoja (la hoja incluye aceleraciones activas)")
	out = append(out, theory.Check{Nombre: "Daño de golpe blanco", Ok: true,
		Nota: "la hoja no trae el daño por golpe: no se compara"})
	return out
}

// Weights returns the stat weights: the change of kills per hour and damage per second for one unit of
// each stat, measured by the same seed with and without the change.
func (w Warrior) Weights(in theory.Input) (theory.WeightsReport, error) {
	_, m, err := w.prepare(in)
	if err != nil {
		return theory.WeightsReport{}, err
	}
	opt := options(in.Opt, defaultFights)
	base := m.runFights(opt)
	baseDPS := dpsCI(base).Mean
	baseKPH := m.kph(base, nil)
	rep := theory.WeightsReport{Nivel: m.level, Peleas: len(base), DPSBase: round1(baseDPS), KPHBase: round1(baseKPH)}
	rep.Supuestos = m.assumptions(opt)
	type probe struct {
		stat, unit string
		step       float64
		d          theory.Delta
	}
	probes := []probe{
		{"Fuerza", "1 de fuerza", 10, theory.Delta{Str: 10}},
		{"Agilidad", "10 de agilidad", 10, theory.Delta{Agi: 10}},
		{"Poder de ataque", "10 de poder de ataque", 10, theory.Delta{AP: 10}},
		{"Crítico", "1 % de crítico", 1, theory.Delta{Crit: 1}},
		{"Precisión", "1 % de precisión", 1, theory.Delta{Hit: 1}},
		{"Habilidad con arma", "5 de habilidad con arma", 5, theory.Delta{Skill: 5}},
		{"Daño medio del arma", "5 de daño medio del arma", 5, theory.Delta{MHAvg: 5}},
		{"Velocidad del arma", "0,1 s más rápida", 1, theory.Delta{MHSpeed: -0.1}},
		{"Aguante", "10 de aguante", 10, theory.Delta{Sta: 10}},
		{"Armadura", "100 de armadura", 100, theory.Delta{Armor: 100}},
	}
	type res struct {
		dps, kph float64
	}
	results := make([]res, len(probes))
	parallel(len(probes), func(i int) {
		in2 := in
		in2.Build.Delta = in.Build.Delta.Add(probes[i].d)
		_, m2, err := w.prepare(in2)
		if err != nil {
			return
		}
		recs := m2.runFights(opt)
		results[i] = res{dps: dpsCI(recs).Mean - baseDPS, kph: m2.kph(recs, nil) - baseKPH}
	})
	var apKPH float64
	for i, p := range probes {
		r := results[i]
		sw := theory.StatWeight{
			Stat:     p.stat,
			Unidad:   p.unit,
			DeltaDPS: round2(r.dps / p.step),
			DeltaKPH: round2(r.kph / p.step),
		}
		if p.stat == "Poder de ataque" {
			apKPH = sw.DeltaKPH
		}
		rep.Pesos = append(rep.Pesos, sw)
	}
	for i := range rep.Pesos {
		if apKPH != 0 {
			rep.Pesos[i].RelativoAP = round2(rep.Pesos[i].DeltaKPH / apKPH)
		}
	}
	ci := m.kphCI(base, opt.Seed)
	noise := (ci.High - ci.Low) / 2
	rep.Ruido = fmt.Sprintf("El margen de error de los puntos de muerte por hora es ±%.1f (%d peleas): diferencias menores no son fiables.", noise, len(base))
	return rep, nil
}

// CompareItems scores each candidate: the simulated change of kills per hour, with its 95 % interval.
// Armour and jewellery change the stats; a main-hand weapon changes the damage range and the speed.
func (w Warrior) CompareItems(in theory.Input, items []theory.Item) (theory.ItemsReport, error) {
	_, m, err := w.prepare(in)
	if err != nil {
		return theory.ItemsReport{}, err
	}
	opt := options(in.Opt, defaultFights)
	base := m.runFights(opt)
	rep := theory.ItemsReport{KPHBase: round1(m.kph(base, nil)), Supuestos: m.assumptions(opt)}
	scores := make([]theory.ItemScore, len(items))
	ok := make([]bool, len(items))
	reasons := make([]string, len(items))
	parallel(len(items), func(i int) {
		it := items[i]
		var add, sub theory.Delta
		var unmodelled []string
		if it.Weapon != nil || isWeaponLoc(it.EquipLoc) {
			d, notes, why := weaponDelta(in.Sheet, it, m)
			if why != "" {
				reasons[i] = why
				return
			}
			add, unmodelled = d, notes
		} else {
			add, unmodelled = theory.DeltaFromItem(it.Stats)
			sub, _ = theory.DeltaFromItem(it.Replaces)
		}
		in2 := in
		in2.Build.Delta = in.Build.Delta.Add(add).Add(negate(sub))
		_, m2, err := w.prepare(in2)
		if err != nil {
			reasons[i] = err.Error()
			return
		}
		recs := m2.runFights(opt)
		scores[i] = theory.ItemScore{
			Name:       it.Name,
			ItemID:     it.ItemID,
			Slot:       it.Slot,
			DeltaDPS:   round2(dpsCI(recs).Mean - dpsCI(base).Mean),
			DeltaKPH:   round2(m2.kph(recs, nil) - m.kph(base, nil)),
			DeltaKPHCI: m.diffCI(base, recs, opt.Seed),
			NoModelado: unmodelled,
			Fuente:     it.Source,
		}
		ok[i] = true
	})
	for i, it := range items {
		if !ok[i] {
			why := reasons[i]
			if why == "" {
				why = "no tengo daño ni velocidad de este arma (o no es comparable)"
			}
			rep.NoComparables = append(rep.NoComparables, it.Name+": "+why)
			continue
		}
		rep.Items = append(rep.Items, scores[i])
	}
	sort.SliceStable(rep.Items, func(a, b int) bool {
		if rep.Items[a].DeltaKPH != rep.Items[b].DeltaKPH {
			return rep.Items[a].DeltaKPH > rep.Items[b].DeltaKPH
		}
		return rep.Items[a].Name < rep.Items[b].Name
	})
	return rep, nil
}

// weaponDelta is the change a main-hand weapon makes to the sheet: its average damage, its damage range and
// its speed against the weapon the sheet has. notes name what the change leaves out; why is set when the
// weapon cannot be compared (offhand, ranged, one hand against two, no damage).
func weaponDelta(sh theory.Sheet, it theory.Item, m *model) (d theory.Delta, notes []string, why string) {
	if it.Weapon == nil || it.Weapon.Speed <= 0 || it.Weapon.Max <= 0 {
		return d, nil, "no tengo daño ni velocidad de este arma (o no es comparable)"
	}
	switch it.EquipLoc {
	case "INVTYPE_WEAPONOFFHAND", "INVTYPE_HOLDABLE", "INVTYPE_SHIELD":
		return d, nil, "va en la mano izquierda: su daño no está modelado"
	case "INVTYPE_RANGED", "INVTYPE_RANGEDRIGHT", "INVTYPE_THROWN":
		return d, nil, "es un arma a distancia: no cambia el cuerpo a cuerpo"
	case "INVTYPE_2HWEAPON", "INVTYPE_WEAPON", "INVTYPE_WEAPONMAINHAND":
	default:
		return d, nil, "no es un arma de la mano derecha"
	}
	twoH := it.EquipLoc == "INVTYPE_2HWEAPON"
	if twoH != m.twoH {
		return d, nil, "cambia entre una y dos manos: no modelado"
	}
	if it.Subclass != "" {
		if kind, _, ok := theory.KindOf(it.Subclass); ok && kind != m.kind {
			notes = append(notes, "cambia de familia de arma: los talentos de arma se evalúan con la familia actual")
		}
	}
	curMin, curMax := sh.Melee.MHMin, sh.Melee.MHMax
	d.MHAvg = it.Weapon.Average() - (curMin+curMax)/2
	d.MHSpread = (it.Weapon.Max - it.Weapon.Min) - (curMax - curMin)
	d.MHSpeed = it.Weapon.Speed - sh.Melee.MHSpeed
	return d, notes, ""
}

// isWeaponLoc reports whether an equip location is a weapon.
func isWeaponLoc(loc string) bool {
	return strings.Contains(loc, "WEAPON") || strings.Contains(loc, "RANGED")
}

// negate is the opposite change: what the replaced item gave up.
func negate(d theory.Delta) theory.Delta {
	return theory.Delta{
		Str: -d.Str, Agi: -d.Agi, Sta: -d.Sta, AP: -d.AP, Crit: -d.Crit, Hit: -d.Hit, Skill: -d.Skill,
		Expertise: -d.Expertise, Armor: -d.Armor, Dodge: -d.Dodge, Parry: -d.Parry,
		MHAvg: -d.MHAvg, MHSpeed: -d.MHSpeed, MHSpread: -d.MHSpread,
	}
}

// OptimizeTalents searches the points the character still has to spend, plus the next ones, one point
// at a time. Each step keeps the best states by kills per hour (beam search) over the legal points.
func (w Warrior) OptimizeTalents(in theory.Input, opt theory.TalentSearch) (theory.TalentPlan, error) {
	if opt.Opts.Targets > 0 {
		in.Opt.Targets = opt.Opts.Targets
	}
	tree, m0, err := w.prepare(in)
	if err != nil {
		return theory.TalentPlan{}, err
	}
	k := w.constants()
	if opt.Points <= 0 {
		opt.Points = talentPoints
	}
	if opt.Beam <= 0 {
		opt.Beam = talentBeam
	}
	full := options(opt.Opts, defaultFights)
	search := full
	if opt.Opts.Fights <= 0 {
		search.Fights = searchFights
	}
	start, _, err := sheetTalentRanks(tree, in.Sheet)
	if err != nil {
		return theory.TalentPlan{}, err
	}
	steps := in.Sheet.Talents.Unspent + opt.Points
	if steps < 0 {
		steps = opt.Points
	}
	actual := m0.summarize(m0.runFights(full), full)
	actual.Calibracion = w.calibrate(in, m0)

	type state struct {
		ranks []int
		order []string
		kph   float64
	}
	evals := 0
	eval := func(ranks []int) float64 {
		in2 := in
		in2.Build = theory.Build{Talents: tree.Named(ranks), Rotation: in.Build.Rotation, Params: in.Build.Params, Delta: in.Build.Delta}
		m, err := compileRanks(in2, tree, k, ranks)
		if err != nil {
			return -1
		}
		return m.kph(m.runFights(search), nil)
	}
	beam := []state{{ranks: start}}
	for step := 0; step < steps; step++ {
		seen := map[string]bool{}
		var children []state
		for _, s := range beam {
			for _, i := range tree.Candidates(s.ranks) {
				next := append([]int(nil), s.ranks...)
				next[i]++
				key := fmt.Sprint(next)
				if seen[key] {
					continue
				}
				seen[key] = true
				label := fmt.Sprintf("%s %d/%d", tree.Defs[i].Name, next[i], tree.Defs[i].Max)
				children = append(children, state{ranks: next, order: append(append([]string(nil), s.order...), label)})
			}
		}
		if len(children) == 0 {
			break
		}
		parallel(len(children), func(i int) {
			children[i].kph = eval(children[i].ranks)
		})
		evals += len(children)
		sort.SliceStable(children, func(a, b int) bool {
			if children[a].kph != children[b].kph {
				return children[a].kph > children[b].kph
			}
			return fmt.Sprint(children[a].ranks) < fmt.Sprint(children[b].ranks)
		})
		if len(children) > opt.Beam {
			children = children[:opt.Beam]
		}
		beam = children
	}
	plan := theory.TalentPlan{
		Nivel:     in.Sheet.Level,
		Puntos:    steps,
		Actual:    actual,
		Evaluadas: evals,
		Supuestos: []string{
			fmt.Sprintf("Búsqueda por haz (%d estados por paso) sobre los talentos con efecto en el modelo; los sin efecto no se valoran.", opt.Beam),
			"Los talentos gastados en la hoja se mantienen; el orden es el de los puntos que quedan y los de los próximos niveles.",
			"Cada candidato se evalúa con " + fmt.Sprint(search.Fights) + " peleas y la misma semilla; las opciones finales con " + fmt.Sprint(full.Fights) + ".",
		},
	}
	// The final options are evaluated with the full settings.
	finals := make([]theory.TalentOption, len(beam))
	parallel(len(beam), func(i int) {
		s := beam[i]
		in2 := in
		in2.Build = theory.Build{Talents: tree.Named(s.ranks), Rotation: in.Build.Rotation, Params: in.Build.Params, Delta: in.Build.Delta}
		m, err := compileRanks(in2, tree, k, s.ranks)
		if err != nil {
			return
		}
		rep := m.summarize(m.runFights(full), full)
		finals[i] = theory.TalentOption{Orden: s.order, Arboles: tree.TreePoints(s.ranks), Resultado: rep}
	})
	sort.SliceStable(finals, func(a, b int) bool {
		return finals[a].Resultado.KillsPerHour.Mean > finals[b].Resultado.KillsPerHour.Mean
	})
	if len(finals) > 3 {
		finals = finals[:3] // the three best builds, as the player asked
	}
	plan.Top = finals
	return plan, nil
}

// Candidates lists the talents a point can go to: the open ones that have an effect in the model, plus
// those that open one of them (their prerequisite talent).
func (t *Tree) Candidates(ranks []int) []int {
	var out []int
	for i := range t.Defs {
		if !t.Open(ranks, i) {
			continue
		}
		if t.modelled(i) || t.unlocksModelled(i) {
			out = append(out, i)
		}
	}
	return out
}

func (t *Tree) modelled(i int) bool {
	s := t.Slug(i)
	_, ok := talentFX[s]
	return ok || talentGrants[s]
}

// unlocksModelled reports whether talent i is the prerequisite of a talent with an effect in the model.
func (t *Tree) unlocksModelled(i int) bool {
	for j := range t.Defs {
		if t.prereqI[j] == i && t.modelled(j) {
			return true
		}
		if t.prereqI[j] == i && t.unlocksModelled(j) {
			return true
		}
	}
	return false
}

// OptimizeRotation compares the priority lists the character can cast, with the Heroic Strike thresholds.
// The lists are fixed templates; abilities the character does not have are removed by the model.
func (w Warrior) OptimizeRotation(in theory.Input, opt theory.RotationSearch) (theory.RotationPlan, error) {
	if opt.Opts.Targets > 0 {
		in.Opt.Targets = opt.Opts.Targets
	}
	_, _, err := w.prepare(in)
	if err != nil {
		return theory.RotationPlan{}, err
	}
	full := options(opt.Opts, defaultFights)
	full.Targets = in.Opt.Targets
	templates := [][]string{
		{"mortal_strike", "bloodthirst", "overpower", "rend", "hs"},
		{"bloodthirst", "mortal_strike", "overpower", "rend", "hs"},
		{"overpower", "mortal_strike", "bloodthirst", "rend", "hs"},
		{"mortal_strike", "overpower", "rend", "hs"},
		{"execute", "mortal_strike", "bloodthirst", "overpower", "rend", "hs"},
		{"death_wish", "bloodthirst", "mortal_strike", "overpower", "rend", "hs"},
		{"mortal_strike", "bloodthirst", "slam", "overpower", "cleave", "hs"},
		{"whirlwind", "mortal_strike", "bloodthirst", "overpower", "rend", "hs"},
		{"mortal_strike", "bloodthirst", "overpower", "rend"},
		{"battle_shout", "mortal_strike", "bloodthirst", "overpower", "rend", "hs"},
		{"sunder", "mortal_strike", "bloodthirst", "overpower", "rend", "hs"},
	}
	if full.Targets == 2 {
		templates = append(templates,
			[]string{"sweeping_strikes", "whirlwind", "mortal_strike", "bloodthirst", "overpower", "cleave", "hs"},
			[]string{"sweeping_strikes", "mortal_strike", "bloodthirst", "overpower", "cleave", "hs"},
		)
	}
	thresholds := []float64{15, 30, 45, 60, 80}
	type cand struct {
		order []string
		hs    float64
	}
	var cands []cand
	for _, t := range templates {
		hasHS := false
		for _, k := range t {
			if k == "hs" {
				hasHS = true
			}
		}
		if !hasHS {
			cands = append(cands, cand{order: t})
			continue
		}
		for _, th := range thresholds {
			cands = append(cands, cand{order: t, hs: th})
		}
	}
	type out struct {
		opt  theory.RotationOption
		kph  float64
		note string
	}
	results := make([]out, len(cands))
	parallel(len(cands), func(i int) {
		c := cands[i]
		in2 := in
		in2.Build = theory.Build{Talents: in.Build.Talents, Rotation: c.order, Params: map[string]float64{"hs_sobre": c.hs}, Delta: in.Build.Delta}
		in2.Opt = in.Opt
		_, m, err := w.prepare(in2)
		if err != nil {
			results[i] = out{kph: -1, note: err.Error()}
			return
		}
		rep := m.summarize(m.runFights(full), full)
		params := map[string]float64{}
		if c.hs > 0 {
			params["golpe_heroico_con_ira"] = c.hs
		}
		results[i] = out{kph: rep.KillsPerHour.Mean, opt: theory.RotationOption{Prioridad: rep.Rotacion, Parametros: params, Resultado: rep}}
	})
	sort.SliceStable(results, func(a, b int) bool { return results[a].kph > results[b].kph })
	plan := theory.RotationPlan{Supuestos: []string{
		"Cada lista se evalúa con las mismas peleas (misma semilla) y el mismo talento.",
		"El Grito de batalla dura toda la pelea (se lanza fuera de combate, su ira no entra en ella); Sunder Armor cuesta ira y quita armadura al enemigo; el Barrido solo cuenta con dos enemigos.",
		"El golpe heroico se encola con el umbral de ira indicado; el resto sigue el orden de la lista.",
	}}
	seen := map[string]bool{}
	for _, r := range results {
		if r.kph < 0 {
			continue
		}
		key := strings.Join(r.opt.Prioridad, ">") + fmt.Sprint(r.opt.Parametros)
		if seen[key] {
			continue
		}
		seen[key] = true
		plan.Opciones = append(plan.Opciones, r.opt)
		if len(plan.Opciones) == 5 {
			break
		}
	}
	return plan, nil
}

// Review compares a recorded fight with the expected damage per second at the same level and mob level.
func (w Warrior) Review(in theory.Input, f theory.FightRecord) theory.FightReview {
	rev := theory.FightReview{}
	if f.Segundos > 0 {
		rev.DPSReal = round1(f.DanoHecho / f.Segundos)
	}
	in2 := in
	if f.NivelMob > 0 {
		in2.Opt.MobLevel = f.NivelMob
	}
	opt := options(in2.Opt, reviewFights)
	_, m, err := w.prepare(in2)
	if err != nil {
		rev.Supuestos = []string{err.Error()}
		return rev
	}
	recs := m.runFights(opt)
	rev.DPSEsperado = dpsCI(recs)
	if rev.DPSEsperado.Mean > 0 {
		rev.Brecha = round1(100 * (rev.DPSReal - rev.DPSEsperado.Mean) / rev.DPSEsperado.Mean)
	}
	// The record's spell ids are the match; its names only label the abilities that were not used.
	used := map[string]bool{}
	for _, id := range f.Hechizos {
		if key, ok := abilityKeyOf(id); ok {
			used[key] = true
		}
	}
	for _, h := range f.Habilidades {
		for i, n := range abilityNames {
			if strings.EqualFold(n, h) {
				used[abilityKeys[i]] = true
			}
		}
	}
	for _, i := range m.order {
		if !used[abilityKeys[i]] && i != kHS {
			rev.SinUsar = append(rev.SinUsar, abilityNames[i])
		}
	}
	rev.Supuestos = m.assumptions(opt)
	return rev
}

// parallel runs f(0..n-1) on the available CPUs. Each call writes its own slot, so the result does not
// depend on the order.
func parallel(n int, f func(i int)) {
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > n {
		workers = n
	}
	var wg sync.WaitGroup
	jobs := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				f(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }
