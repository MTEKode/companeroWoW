package warrior

// The fight: an event-driven simulation of one leveling fight, from full health and zero rage, against one
// mob or against two mobs pulled together (Options.Targets). The events are the swings of both hands, the
// mobs' swings, the damage over time, the rage from time (anger management and Bloodrage), the buffs and
// cooldowns, the stance changes the build asks for and the global cooldown. Every random draw comes from one
// generator seeded per evaluation, so the same inputs give the same numbers.

import (
	"math"
	"math/rand/v2"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// Samples of the rage curve: every 5 s, for 60 s.
const (
	rageEvery   = 5.0
	rageSamples = 12
	eps         = 1e-9
	never       = math.MaxFloat64
	maxMobs     = 2
)

// Damage slots: the abilities, then the auto attacks and the Deep Wounds bleed.
const (
	slotAutoMH = nAbil + iota
	slotAutoOH
	slotDeepWounds
	nSlots
)

// fightRec is what one fight leaves behind.
type fightRec struct {
	dur      float64
	kill     bool
	kills    int // mobs killed (the whole pull when kill is set)
	died     bool
	dealt    float64
	taken    float64
	mobLevel int
	dealtBy  [nSlots]float64
	casts    [nAbil]int
	rage     [rageSamples]float64
}

// sim is the state of one fight.
type sim struct {
	m    *model
	rng  *rand.Rand
	mob  theory.Mob
	t    float64
	hp   float64
	rage float64

	nMobs   int
	mobHP   [maxMobs]float64
	mobDead [maxMobs]bool
	nextMob [maxMobs]float64

	gcdAt      float64
	cdAt       [nAbil]float64
	hsQueued   bool
	nextMH     float64
	nextOH     float64
	flurryLeft int
	rendEnd    float64
	rendNext   float64
	rendTick   float64
	dwEnd      float64
	dwNext     float64
	dwTick     float64
	owEnd      float64 // Overpower is open until this time
	enrageEnd  float64
	enrageVal  float64
	deathEnd   float64 // Death Wish buff until this time
	angerNext  float64
	sampleIdx  int

	bsEnd       float64 // Battle Shout until this time
	revengeEnd  float64 // Revenge is usable until this time (after a block, dodge or parry)
	berserkEnd  float64 // Berserker Rage: rage from damage taken is multiplied until this time
	recklessEnd float64
	sunderN     float64 // Sunder Armor stacks on the first mob
	sunderEnd   float64
	brLeft      float64 // Bloodrage rage still to come over its length
	brNext      float64
	brPer       float64 // rage per second of Bloodrage's rage over time
	vrEnd       float64 // Victory Rush is usable until this time (after a kill)
	sweepLeft   int     // Sweeping Strikes: melee attacks that also hit the second mob
	stanceLeft  int
	stanceNext  float64

	rec fightRec
}

// fight runs one fight at a mob level and returns its record.
func (m *model) fight(rng *rand.Rand, mt theory.MobTables, mobLevel int, elite bool) fightRec {
	s := &sim{m: m, rng: rng}
	s.mob = mt.MobAt(mobLevel, elite)
	s.rec.mobLevel = mobLevel
	s.hp = m.hpMax
	s.nMobs = m.targets
	if s.nMobs < 1 || s.nMobs > maxMobs {
		s.nMobs = 1
	}
	for i := 0; i < s.nMobs; i++ {
		s.mobHP[i] = s.mob.HP
		s.nextMob[i] = m.k.Mob.Swing * 0.5
	}
	s.nextMH = 0
	s.nextOH = never
	if m.ohOn {
		s.nextOH = 0
	}
	s.rendEnd, s.rendNext = -1, never
	s.dwEnd, s.dwNext = -1, never
	s.owEnd, s.enrageEnd, s.deathEnd = -1, -1, -1
	s.bsEnd, s.revengeEnd, s.berserkEnd, s.recklessEnd, s.sunderEnd, s.vrEnd = -1, -1, -1, -1, -1, -1
	if m.bsAP > 0 {
		// Battle Shout is cast out of combat and lasts 3 min, longer than a leveling fight: it counts for the
		// whole fight and its rage is not spent in it.
		s.bsEnd = never
	}
	s.brNext = never
	s.angerNext = never
	if m.tfx.anger {
		s.angerNext = 3
	}
	s.stanceLeft = m.switches
	s.stanceNext = m.k.StanceEvery
	s.sweepLeft = 0
	s.run()
	s.rec.dur = s.t
	s.rec.dealt = 0
	for _, v := range s.rec.dealtBy {
		s.rec.dealt += v
	}
	return s.rec
}

// run advances the fight event by event until the pull dies, the player dies or the cap is reached.
func (s *sim) run() {
	cap := s.m.k.FightCap
	for {
		next := s.nextEvent()
		if next >= cap {
			s.t = cap
			s.sample()
			return
		}
		s.t = next
		s.sample()
		s.due()
		s.decide()
		if s.allDead() {
			s.rec.kill = true
			s.rec.kills = s.nMobs
			return
		}
		if s.hp <= 0 {
			s.rec.died = true
			s.rec.kills = s.killed()
			return
		}
	}
}

// killed counts the mobs of the pull that are dead.
func (s *sim) killed() int {
	n := 0
	for i := 0; i < s.nMobs; i++ {
		if s.mobDead[i] {
			n++
		}
	}
	return n
}

// allDead reports whether the whole pull is dead.
func (s *sim) allDead() bool {
	return s.killed() == s.nMobs
}

// target is the mob the single-target attacks and the auto attacks hit: the first one alive.
func (s *sim) target() int {
	for i := 0; i < s.nMobs; i++ {
		if !s.mobDead[i] {
			return i
		}
	}
	return 0
}

// later returns the time x when it is still in the future, and never otherwise. Buff ends are events, so
// the decisions run when a buff ends or a cooldown ends.
func (s *sim) later(x float64, n float64) float64 {
	if x > s.t+eps && x < n {
		return x
	}
	return n
}

// nextEvent is the earliest scheduled time after the current one.
func (s *sim) nextEvent() float64 {
	n := math.Min(s.nextMH, s.nextOH)
	for i := 0; i < s.nMobs; i++ {
		if !s.mobDead[i] {
			n = math.Min(n, s.nextMob[i])
		}
	}
	n = math.Min(n, s.rendNext)
	n = math.Min(n, s.dwNext)
	n = math.Min(n, s.angerNext)
	n = math.Min(n, s.brNext)
	if s.stanceLeft > 0 {
		n = math.Min(n, s.stanceNext)
	}
	if s.gcdAt > s.t+eps {
		n = math.Min(n, s.gcdAt)
	}
	if s.owEnd > s.t+eps {
		n = math.Min(n, s.owEnd)
	}
	n = s.later(s.bsEnd, n)
	n = s.later(s.revengeEnd, n)
	n = s.later(s.sunderEnd, n)
	n = s.later(s.vrEnd, n)
	for i := 0; i < nAbil; i++ {
		if s.m.abil[i].known && s.cdAt[i] > s.t+eps {
			n = math.Min(n, s.cdAt[i])
		}
	}
	if s.sampleIdx < rageSamples {
		n = math.Min(n, float64(s.sampleIdx)*rageEvery)
	}
	if n <= s.t+eps {
		n = s.t + 0.05 // guard: nothing is due, so move on a little
	}
	return n
}

// sample records the rage at each 5 s mark the fight has passed.
func (s *sim) sample() {
	for s.sampleIdx < rageSamples && s.t+eps >= float64(s.sampleIdx)*rageEvery {
		s.rec.rage[s.sampleIdx] = s.rage
		s.sampleIdx++
	}
}

// due runs the events scheduled at the current time, in a fixed order.
func (s *sim) due() {
	if s.nextMH <= s.t+eps {
		s.mhSwing()
	}
	if s.nextOH <= s.t+eps {
		s.ohSwing()
	}
	for i := 0; i < s.nMobs; i++ {
		if !s.mobDead[i] && s.nextMob[i] <= s.t+eps {
			s.mobSwing(i)
		}
	}
	if s.rendNext <= s.t+eps {
		s.rendTicks()
	}
	if s.dwNext <= s.t+eps {
		s.dwTicks()
	}
	if s.angerNext <= s.t+eps {
		s.gainRage(1)
		s.angerNext += 3
	}
	if s.brNext <= s.t+eps {
		s.bloodrageTick()
	}
	if s.stanceLeft > 0 && s.stanceNext <= s.t+eps {
		// A stance change loses the rage above what Tactical Mastery keeps.
		s.rage = math.Min(s.rage, s.m.retain)
		s.stanceLeft--
		s.stanceNext += s.m.k.StanceEvery
	}
}

// ap is the attack power now: the sheet's and the rotation's Battle Shout while it lasts.
func (s *sim) ap() float64 {
	if s.t < s.bsEnd {
		return s.m.ap + s.m.bsAP
	}
	return s.m.ap
}

// mitigation is the share of a player hit that the mob's armour removes. Sunder Armor removes armour from
// the first mob; a mace or staff ignores part of what is left.
func (s *sim) mitigation(target int) float64 {
	armor := s.mob.Armor
	if target == 0 {
		armor -= s.sunderN * s.m.abil[kSunder].armorStack
	}
	armor = math.Max(0, armor) * (1 - s.m.tfx.wmArmor/100)
	return s.m.k.Attack.Mitigation(armor, s.m.level)
}

// gainRage adds rage, capped at 100.
func (s *sim) gainRage(v float64) {
	s.rage = math.Min(100, s.rage+v)
}

// weaponDamage is one roll of a weapon's damage plus its Attack Power share (AP/14 × speed).
func (s *sim) weaponDamage(min, max, speed float64) float64 {
	return min + s.rng.Float64()*(max-min) + s.ap()/s.m.k.WeaponAPDiv*speed
}

// playerMult is the damage multiplier of the talents and buffs that apply to a hit.
func (s *sim) playerMult(offhand bool) float64 {
	mult := 1.0
	if s.m.twoH {
		mult += s.m.tfx.h2 / 100
	}
	if offhand {
		mult += s.m.tfx.dwDmg / 100
	}
	if s.t < s.deathEnd {
		mult += s.m.k.DeathWishDmg / 100
	}
	if s.t < s.enrageEnd {
		mult += s.enrageVal / 100
	}
	return mult
}

// critMult is the critical multiplier: 2, plus Impale's bonus.
func (s *sim) critMult() float64 {
	return 2 + s.m.tfx.impale/100
}

// strike resolves one player attack against a mob: the attack table, the damage and the effects of the
// hit. It returns the damage dealt (0 for a miss, dodge or parry). A dodge opens Overpower. An Overpower
// cannot be avoided, so its misses, dodges and parries are removed first. Recklessness adds crit chance.
func (s *sim) strike(base float64, white, offhand, overpower bool, extraCrit float64, target int) float64 {
	m := s.m
	if s.t < s.recklessEnd {
		extraCrit += m.abil[kRecklessness].critBonus
	}
	probs := m.k.Attack.Attack(m.skill, s.mob.Skill, m.crit+extraCrit, m.hit, s.mob.Dodge, 0, m.expertise, white, offhand)
	if offhand {
		probs.Miss = max0(probs.Miss - m.tfx.furious)
	}
	if overpower {
		probs.Miss, probs.Dodge, probs.Parry, probs.Glance = 0, 0, 0, 0
	}
	roll := probs.Pick(s.rng.Float64() * 100)
	switch roll {
	case theory.RollMiss:
		return 0
	case theory.RollDodge, theory.RollParry:
		s.owEnd = s.t + m.k.OverpowerWin
		return 0
	}
	dmg := base * s.playerMult(offhand)
	crit := false
	switch roll {
	case theory.RollGlance:
		dmg *= m.k.Attack.GlanceMult
	case theory.RollCrit:
		dmg *= s.critMult()
		crit = true
	}
	dmg *= 1 - s.mitigation(target)
	s.mobHP[target] -= dmg
	s.rec.dealt += dmg
	s.landed(dmg, crit, offhand, overpower)
	s.checkDeath(target)
	return dmg
}

// checkDeath marks a mob dead when its health is gone. The first kill opens Victory Rush.
func (s *sim) checkDeath(target int) {
	if s.mobDead[target] || s.mobHP[target] > 0 {
		return
	}
	s.mobDead[target] = true
	if s.killed() == 1 && s.m.abil[kVictoryRush].known {
		s.vrEnd = s.t + s.m.abil[kVictoryRush].dur
	}
}

// landed applies the rage and proc effects of a hit that dealt damage.
func (s *sim) landed(dmg float64, crit, offhand, overpower bool) {
	rage := s.m.k.Rage.RageDealt(dmg, s.m.level)
	if offhand {
		rage *= 1 + s.m.tfx.dwRage/100
	}
	s.gainRage(rage)
	if s.rng.Float64()*100 < s.m.tfx.unbridled {
		s.gainRage(1)
	}
	if crit && !overpower {
		if s.m.tfx.flurry > 0 {
			s.flurryLeft = s.m.k.FlurryLen
		}
		if s.m.tfx.deepWounds > 0 {
			s.dwEnd = s.t + 12
			s.dwNext = s.t + s.m.k.RendTickSec
			s.dwTick = s.m.tfx.deepWounds / 100 * (s.m.mhMin + s.m.mhMax) / 2 / 4
		}
	}
	if !offhand && !overpower && s.m.tfx.bloodthrill > 0 && s.t < s.rendEnd {
		if s.rng.Float64()*100 < s.m.tfx.bloodthrill {
			s.owEnd = s.t + s.m.k.OverpowerWin
		}
	}
}

// sweep is the extra hit of Sweeping Strikes on the second mob, when two mobs are pulled. It spends one of
// the five charges of the buff.
func (s *sim) sweep(base float64) {
	if s.sweepLeft <= 0 || s.nMobs < 2 || s.mobDead[1] {
		return
	}
	s.sweepLeft--
	if dealt := s.strike(base*s.m.k.SweepDamage, false, false, false, 0, 1); dealt > 0 {
		s.rec.dealtBy[kSweeping] += dealt
	}
}

// mhSwing is the main-hand swing. A queued Heroic Strike replaces it. A sword may add an extra attack.
func (s *sim) mhSwing() {
	m := s.m
	base := s.weaponDamage(m.mhMin, m.mhMax, m.mhSpeed)
	tgt := s.target()
	if s.hsQueued {
		s.hsQueued = false
		hs := base + m.abil[kHS].bonus*(1+ifThen(m.twoH, m.tfx.h2, 0)/100)
		if dealt := s.strike(hs, false, false, false, 0, tgt); dealt > 0 {
			s.rec.dealtBy[kHS] += dealt
			s.rec.casts[kHS]++
			s.sweep(hs)
		}
	} else if dealt := s.strike(base, true, false, false, 0, tgt); dealt > 0 {
		s.rec.dealtBy[slotAutoMH] += dealt
		if s.rng.Float64()*100 < m.tfx.wmExtra {
			s.mobHP[tgt] -= dealt
			s.rec.dealt += dealt
			s.rec.dealtBy[slotAutoMH] += dealt
			s.checkDeath(tgt)
		}
		s.sweep(base)
	}
	s.nextMH = s.t + s.swingInterval(m.mhSpeed)
}

// ohSwing is the offhand swing: half damage, with the dual wield penalty on misses.
func (s *sim) ohSwing() {
	m := s.m
	base := s.weaponDamage(m.ohMin, m.ohMax, m.ohSpeed) * m.k.OffhandMult
	if dealt := s.strike(base, true, true, false, 0, s.target()); dealt > 0 {
		s.rec.dealtBy[slotAutoOH] += dealt
	}
	s.nextOH = s.t + s.swingInterval(m.ohSpeed)
}

// swingInterval is the time to the next swing: the speed, shortened by flurry while it lasts.
func (s *sim) swingInterval(speed float64) float64 {
	if s.flurryLeft > 0 {
		s.flurryLeft--
		return speed / (1 + s.m.tfx.flurry/100)
	}
	return speed
}

// mobSwing is mob i's attack on the player: the table, a block, the armour and the Death Wish penalty.
// A block, dodge or parry opens Revenge.
func (s *sim) mobSwing(i int) {
	m := s.m
	probs := m.k.Attack.Attack(s.mob.Skill, m.defSkill, s.mob.Crit, 0, m.dodge, m.parry, 0, false, false)
	roll := probs.Pick(s.rng.Float64() * 100)
	switch roll {
	case theory.RollParry:
		s.revengeEnd = s.t + m.k.RevengeWindow
		s.nextMob[i] = s.t + s.mob.Swing*(1-m.k.ParryHaste)
		return
	case theory.RollMiss:
		s.nextMob[i] = s.t + s.mob.Swing
		return
	case theory.RollDodge:
		s.revengeEnd = s.t + m.k.RevengeWindow
		s.nextMob[i] = s.t + s.mob.Swing
		return
	}
	dmg := s.mob.DamageAv * (1 + (s.rng.Float64()*2-1)*m.k.MobVariance)
	if roll == theory.RollCrit {
		dmg *= m.k.MobCritMult
	}
	if s.t < s.deathEnd {
		dmg *= 1 + m.k.DeathWishTake/100
	}
	if s.t < s.recklessEnd {
		dmg *= 1 + m.abil[kRecklessness].takenPct/100
	}
	if m.block > 0 && s.rng.Float64()*100 < m.block {
		dmg = math.Max(0, dmg-m.blockValue)
		s.revengeEnd = s.t + m.k.RevengeWindow
	}
	dmg *= 1 - m.k.Attack.Mitigation(m.armor, s.mob.Level)
	s.hp -= dmg
	s.rec.taken += dmg
	rage := m.k.Rage.RageTaken(dmg, m.level)
	if s.t < s.berserkEnd {
		rage *= m.k.BerserkerTake
	}
	s.gainRage(rage)
	if m.tfx.enrage > 0 && s.rng.Float64() < 0.3 {
		s.enrageEnd = s.t + m.k.EnrageLen
		s.enrageVal = m.tfx.enrage
	}
	s.nextMob[i] = s.t + s.mob.Swing
}

// rendTicks is one Rend tick on the first mob. Bleeds ignore armour (classic).
func (s *sim) rendTicks() {
	if s.t > s.rendEnd+eps || s.mobDead[0] {
		s.rendNext = never
		return
	}
	s.mobHP[0] -= s.rendTick
	s.rec.dealt += s.rendTick
	s.rec.dealtBy[kRend] += s.rendTick
	s.checkDeath(0)
	s.rendNext += s.m.k.RendTickSec
	if s.rendNext > s.rendEnd+eps {
		s.rendNext = never
	}
}

// dwTicks is one Deep Wounds tick on the first mob.
func (s *sim) dwTicks() {
	if s.t > s.dwEnd+eps || s.mobDead[0] {
		s.dwNext = never
		return
	}
	s.mobHP[0] -= s.dwTick
	s.rec.dealt += s.dwTick
	s.rec.dealtBy[slotDeepWounds] += s.dwTick
	s.checkDeath(0)
	s.dwNext += s.m.k.RendTickSec
	if s.dwNext > s.dwEnd+eps {
		s.dwNext = never
	}
}

// bloodrageTick gives one second of Bloodrage's rage over time.
func (s *sim) bloodrageTick() {
	if s.brLeft <= eps {
		s.brNext = never
		return
	}
	per := math.Min(s.brPer, s.brLeft)
	s.gainRage(per)
	s.brLeft -= per
	s.brNext += 1
	if s.brLeft <= eps {
		s.brNext = never
	}
}

// decide walks the priority order. Heroic Strike is queued (off the global cooldown) when its rage
// threshold is met; the first other ability that can be cast starts the global cooldown.
func (s *sim) decide() {
	m := s.m
	for _, i := range m.order {
		if i == kHS {
			if !s.hsQueued && s.rage >= math.Max(m.abil[kHS].cost, m.hsAbove) {
				s.hsQueued = true
				s.rage -= m.abil[kHS].cost
			}
			continue
		}
		if s.t < s.gcdAt-eps {
			continue
		}
		if s.castable(i) {
			s.cast(i)
			return
		}
	}
}

// castable reports whether ability i can be cast now.
func (s *sim) castable(i int) bool {
	a := &s.m.abil[i]
	if !a.known || s.t < s.cdAt[i]-eps {
		return false
	}
	// A cost of zero is always payable: Victory Rush, Bloodrage and Berserker Rage cost no rage.
	if s.rage < a.cost {
		return false
	}
	switch i {
	case kOverpower:
		return s.t < s.owEnd
	case kExecute:
		return s.mobHP[s.target()]/s.mob.HP <= s.m.k.ExecuteBelow
	case kRend:
		return s.t >= s.rendEnd
	case kBattleShout:
		return false // cast out of combat (see fight)
	case kRevenge:
		return s.t < s.revengeEnd
	case kVictoryRush:
		return s.t < s.vrEnd
	case kSweeping:
		return s.m.targets == 2 && s.sweepLeft <= 0 && !s.mobDead[1]
	case kSunder:
		return true
	case kBloodrage, kBerserkerRage, kRecklessness, kDeathWish:
		return true
	}
	return true
}

// cast spends the rage, starts the cooldown and resolves the ability.
func (s *sim) cast(i int) {
	m := s.m
	a := &m.abil[i]
	if i == kBloodrage {
		s.hp -= m.k.BloodrageHPCost * m.hpMax
	}
	s.rage -= a.cost
	if a.cd > 0 {
		s.cdAt[i] = s.t + a.cd
	}
	s.gcdAt = s.t + math.Max(m.k.GCD, a.cast)
	tgt := s.target()
	switch i {
	case kRend:
		s.rendEnd = s.t + a.bleedSec
		s.rendTick = a.bleed / math.Max(1, a.bleedSec/m.k.RendTickSec)
		s.rendNext = s.t + m.k.RendTickSec
		s.rec.casts[i]++
		return
	case kDeathWish:
		s.deathEnd = s.t + m.k.DeathWishLen
		s.rec.casts[i]++
		return
	case kBloodrage:
		s.gainRage(a.rage)
		s.brLeft, s.brPer, s.brNext = a.rage2, a.rage2/math.Max(1, a.dur), s.t+1
		s.rec.casts[i]++
		return
	case kBerserkerRage:
		s.gainRage(a.rage)
		s.berserkEnd = s.t + a.dur
		s.rec.casts[i]++
		return
	case kRecklessness:
		s.recklessEnd = s.t + a.dur
		s.rec.casts[i]++
		return
	case kSweeping:
		s.sweepLeft = m.k.SweepCharges
		s.rec.casts[i]++
		return
	case kVictoryRush:
		s.vrEnd = -1
	case kSunder:
		// Sunder Armor is a melee attack: it lands for weapon damage and stacks the armour reduction on a hit.
		base := s.weaponDamage(m.mhMin, m.mhMax, m.mhSpeed)
		if dealt := s.strike(base, false, false, false, 0, tgt); dealt > 0 {
			s.sunderN = math.Min(a.maxStacks, s.sunderN+1)
			s.sunderEnd = s.t + a.dur
			s.rec.dealtBy[i] += dealt
			s.rec.casts[i]++
		}
		return
	}
	// Abilities that hit the mob with a damage value: weapon damage plus a bonus, or Attack Power.
	var base float64
	if a.weapon {
		base = s.weaponDamage(m.mhMin, m.mhMax, m.mhSpeed)*(1+ifThen(m.twoH, m.tfx.h2, 0)/100) + a.bonus
	} else {
		base = a.bonus + s.ap()*a.apPct/100
	}
	base *= a.damageMult()
	if i == kExecute && a.rageConv > 0 {
		// Execute converts all the rage that is left into damage.
		base += s.rage * a.rageConv
		s.rage = 0
	}
	overpower := i == kOverpower
	extra := 0.0
	if overpower {
		extra = m.tfx.owCrit
	}
	if dealt := s.strike(base, false, false, overpower, extra, tgt); dealt > 0 {
		s.rec.dealtBy[i] += dealt
		s.rec.casts[i]++
		if a.weapon {
			s.sweep(base)
		}
	}
	if (i == kCleave || i == kWhirlwind) && m.targets == 2 {
		// Cleave and Whirlwind also hit the other mob, with their own roll.
		other := 1 - tgt
		if other < s.nMobs && !s.mobDead[other] {
			if dealt := s.strike(base, false, false, false, 0, other); dealt > 0 {
				s.rec.dealtBy[i] += dealt
			}
		}
	}
	if overpower {
		s.owEnd = -1
	}
}

func ifThen(c bool, a, b float64) float64 {
	if c {
		return a
	}
	return b
}

func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}
