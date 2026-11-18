package theory

// Classic-era combat mechanics, written once for every class. The numbers they read come in
// as parameters (AttackTables, RageTable, MobTables) that each class fills from its own constants
// table, so every constant has one place and one documented source.

import (
	"math"
	"math/rand/v2"
	"sort"
	"strings"
)

// AttackTables are the constants of the melee attack table. Percentages are percentage points.
type AttackTables struct {
	MissBase      float64 // miss chance against a target of the same level
	MissSlopeLow  float64 // extra miss per point of defence over skill, up to MissBreakPts points
	MissBreakPts  float64 // points of the gap where the slope changes
	MissSlopeHigh float64 // extra miss per point beyond MissBreakPts
	DualWieldMiss float64 // extra miss for an offhand swing (dual wield)
	DefPerPoint   float64 // avoidance (dodge, parry) and crit change per point of defence over skill
	CritPerPoint  float64 // crit lost per point of defence over skill (crit gained when skill is higher)
	GlanceBase    float64 // glancing chance at a gap of zero points
	GlancePerPt   float64 // extra glancing chance per point of gap
	GlanceMax     float64 // glancing chance cap
	GlanceMult    float64 // damage multiplier of a glancing blow
	ArmorK        float64 // armour formula constant (400)
	ArmorPerLevel float64 // armour formula per attacker level (85)
}

// Probs are the outcome chances of one attack, in percent, in the order the roll checks them.
type Probs struct {
	Miss, Dodge, Parry, Glance, Crit float64
}

// Roll is one attack outcome.
type Roll int

// The outcomes of an attack.
const (
	RollHit Roll = iota
	RollCrit
	RollMiss
	RollDodge
	RollParry
	RollGlance
)

// Attack returns the outcome chances of an attack by attackerSkill with critStat against a defender
// with defenseSkill and the given avoidance. white is true for auto attacks: glancing blows apply to
// them only. offhand adds the dual wield penalty to the miss chance.
func (t AttackTables) Attack(attackerSkill, defenseSkill, critStat, hit, defDodge, defParry, expertise float64, white, offhand bool) Probs {
	d := defenseSkill - attackerSkill
	miss := t.MissBase
	if d > 0 {
		if d <= t.MissBreakPts {
			miss = t.MissBase + t.MissSlopeLow*d
		} else {
			miss = t.MissBase + t.MissSlopeLow*t.MissBreakPts + t.MissSlopeHigh*(d-t.MissBreakPts)
		}
	}
	if offhand {
		miss += t.DualWieldMiss
	}
	miss = math.Max(0, miss-hit)
	avoid := func(base float64) float64 {
		v := base - expertise*0.25
		if d > 0 {
			v += t.DefPerPoint * d
		}
		return math.Max(0, v)
	}
	p := Probs{Miss: miss, Dodge: avoid(defDodge), Parry: avoid(defParry)}
	if white && d > 0 {
		p.Glance = math.Min(t.GlanceMax, t.GlanceBase+t.GlancePerPt*d)
	}
	p.Crit = math.Max(0, critStat-t.CritPerPoint*d)
	return p
}

// Pick maps one uniform draw u in [0,100) to an outcome, checking the chances in order.
func (p Probs) Pick(u float64) Roll {
	switch {
	case u < p.Miss:
		return RollMiss
	case u < p.Miss+p.Dodge:
		return RollDodge
	case u < p.Miss+p.Dodge+p.Parry:
		return RollParry
	case u < p.Miss+p.Dodge+p.Parry+p.Glance:
		return RollGlance
	case u < p.Miss+p.Dodge+p.Parry+p.Glance+p.Crit:
		return RollCrit
	}
	return RollHit
}

// Mitigation is the share of damage armour removes from an attacker of attackerLevel (the formula
// armour/(armour + 400 + 85 × attacker level)). Levels above 59 use the same formula.
func (t AttackTables) Mitigation(armor float64, attackerLevel int) float64 {
	if armor <= 0 {
		return 0
	}
	return armor / (armor + t.ArmorK + t.ArmorPerLevel*float64(attackerLevel))
}

// RageTable converts damage into rage: rage = k × damage / conversion(level). The conversion values are
// interpolated linearly between the table's levels.
type RageTable struct {
	Levels      []float64
	Conversions []float64
	FromDealt   float64 // k for damage dealt (7.5)
	FromTaken   float64 // k for damage taken (2.5)
}

// Conversion returns the rage conversion value at a level.
func (r RageTable) Conversion(level int) float64 {
	l := float64(level)
	n := len(r.Levels)
	if n == 0 {
		return 1
	}
	if l <= r.Levels[0] {
		return r.Conversions[0]
	}
	for i := 1; i < n; i++ {
		if l <= r.Levels[i] {
			f := (l - r.Levels[i-1]) / (r.Levels[i] - r.Levels[i-1])
			return r.Conversions[i-1] + f*(r.Conversions[i]-r.Conversions[i-1])
		}
	}
	return r.Conversions[n-1]
}

// RageDealt is the rage from dealing damage at a level.
func (r RageTable) RageDealt(damage float64, level int) float64 {
	return r.FromDealt * damage / r.Conversion(level)
}

// RageTaken is the rage from taking damage at a level.
func (r RageTable) RageTaken(damage float64, level int) float64 {
	return r.FromTaken * damage / r.Conversion(level)
}

// MobTables are the mob model's constants (documented approximations, see each class's table).
type MobTables struct {
	DefensePerLevel float64 // defence skill = level × this
	HPBase          float64
	HPPerLevel      float64
	ArmorPerLevel   float64
	DamageBase      float64 // average damage per hit = base + per level × level
	DamagePerLevel  float64
	Swing           float64 // seconds between mob attacks
	Crit            float64 // percent
	Dodge           float64 // percent, before the defence difference
	EliteHP         float64 // multipliers for an elite
	EliteDamage     float64
}

// Mob is one enemy at a level.
type Mob struct {
	Level    int
	Skill    float64 // weapon and defence skill
	Armor    float64
	HP       float64
	DamageAv float64
	Swing    float64
	Crit     float64
	Dodge    float64
}

// MobAt builds the model of a mob of level l.
func (m MobTables) MobAt(l int, elite bool) Mob {
	lv := float64(l)
	mob := Mob{
		Level:    l,
		Skill:    m.DefensePerLevel * lv,
		Armor:    m.ArmorPerLevel * lv,
		HP:       m.HPBase + m.HPPerLevel*lv,
		DamageAv: m.DamageBase + m.DamagePerLevel*lv,
		Swing:    m.Swing,
		Crit:     m.Crit,
		Dodge:    m.Dodge,
	}
	if elite {
		mob.HP *= m.EliteHP
		mob.DamageAv *= m.EliteDamage
	}
	return mob
}

// NewRand returns the deterministic generator of a fight series. The same seed gives the same numbers.
func NewRand(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
}

// Stat keys of the client's item stats (GetItemStats) and what they mean for a melee character.
var (
	statToDelta = map[string]func(*Delta, float64){
		"ITEM_MOD_STRENGTH_SHORT":     func(d *Delta, v float64) { d.Str += v },
		"ITEM_MOD_AGILITY_SHORT":      func(d *Delta, v float64) { d.Agi += v },
		"ITEM_MOD_STAMINA_SHORT":      func(d *Delta, v float64) { d.Sta += v },
		"ITEM_MOD_ATTACK_POWER_SHORT": func(d *Delta, v float64) { d.AP += v },
	}
	// Stats that do not change a melee warrior's numbers: they are skipped silently.
	statIgnored = []string{"INTELLECT", "SPIRIT", "SPELL_POWER", "RANGED_ATTACK_POWER", "MANA_REGENERATION", "RESISTANCE"}
)

// DeltaFromItem turns an item's stats into a Delta. Stats the model cannot convert (ratings, haste,
// damage per second) are returned by name, so the caller can say the score leaves them out.
func DeltaFromItem(stats map[string]float64) (Delta, []string) {
	var d Delta
	var left []string
	for key, v := range stats {
		if f, ok := statToDelta[key]; ok {
			f(&d, v)
			continue
		}
		if ignoredStat(key) {
			continue
		}
		left = append(left, key)
	}
	sort.Strings(left)
	return d, left
}

func ignoredStat(key string) bool {
	for _, p := range statIgnored {
		if strings.Contains(key, p) {
			return true
		}
	}
	return false
}

// CI95 returns the mean and its 95 % interval of samples (normal approximation). With fewer than two
// samples the interval is the mean itself.
func CI95(samples []float64) CI {
	n := len(samples)
	if n == 0 {
		return CI{}
	}
	sum := 0.0
	for _, v := range samples {
		sum += v
	}
	mean := sum / float64(n)
	if n < 2 {
		return CI{Mean: mean, Low: mean, High: mean}
	}
	ss := 0.0
	for _, v := range samples {
		ss += (v - mean) * (v - mean)
	}
	half := 1.96 * math.Sqrt(ss/float64(n-1)) / math.Sqrt(float64(n))
	return CI{Mean: mean, Low: mean - half, High: mean + half}
}
