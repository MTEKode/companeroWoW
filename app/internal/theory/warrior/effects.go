package warrior

// The numbers of an ability's text, by position. A client text (the sheet's tooltip in the player's
// language, or the site's English text) is read as the ordered list of its numbers (theory.Numbers),
// and each position has a meaning given here. The order is the one of the English text of the site
// (wowforevertalents.com, client-derived; the abilities fixture checks it), and translations keep it:
// «causing weapon damage plus 5» and «causando daño de arma más 5» both give [5].
//
// A text whose count of numbers is not the one of its entry is not read: the model then uses the
// site's table or its constants and says so (the source of the number is the report's «fuente»).

import (
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// effectSpec is one ability's text layout: how many numbers the text has, and what each one sets.
type effectSpec struct {
	n     int
	apply func(v []float64, a *ability)
}

// weaponPlus sets a weapon-damage ability: weapon damage plus v.
func weaponPlus(v float64, a *ability) {
	a.bonus = v
	a.weapon = true
}

// effectSpecs by rotation key. The comment gives the English text of the first rank the fixture has
// and the numbers it contains, in order.
var effectSpecs = map[string]effectSpec{
	// "increases melee damage by 11 and causes a high amount of threat" → [11]
	"hs": {1, func(v []float64, a *ability) { a.bonus = v[0] }},
	// "Wounds the target causing them to bleed for 15 damage over 9 sec" → [15 9]
	"rend": {2, func(v []float64, a *ability) { a.bleed, a.bleedSec = v[0], v[1] }},
	// "causing weapon damage plus 5. Only useable after the target dodges" → [5]
	"overpower": {1, func(v []float64, a *ability) { weaponPlus(v[0], a) }},
	// "causing 125 damage and converting each extra point of rage into 3 additional damage. … 20% or less health" → [125 3 20]
	"execute": {3, func(v []float64, a *ability) { a.bonus, a.rageConv = v[0], v[1] }},
	// "deals weapon damage plus 110 … reducing the effectiveness of any healing by 50% for 10 sec" → [110 50 10]
	"mortal_strike": {3, func(v []float64, a *ability) { weaponPlus(v[0], a) }},
	// "damage equal to 45% of your Attack Power plus 37 … movement speed by 10% for 10 sec" → [45 37 10 10]
	"bloodthirst": {4, func(v []float64, a *ability) { a.apPct, a.bonus = v[0], v[1] }},
	// "causing weapon damage plus 16" → [16]
	"slam": {1, func(v []float64, a *ability) { weaponPlus(v[0], a) }},
	// "up to 4 enemies within 8 yards, causing weapon damage from both melee weapons" → [4 8]
	"whirlwind": {2, func(v []float64, a *ability) { a.weapon = true }},
	// "does your weapon damage plus 5 to the target and a second nearby enemy" → [5]
	"cleave": {1, func(v []float64, a *ability) { weaponPlus(v[0], a) }},
	// "reducing it by 90 per Sunder Armor … up to 5 times. Lasts 30 sec" → [90 5 30]
	"sunder": {3, func(v []float64, a *ability) { a.armorStack, a.maxStacks, a.dur = v[0], v[1], v[2] }},
	// "(20 + 22.5% of Attack Power) to (24 + 22.5% of Attack Power) damage" → [20 22.5 24 22.5]
	"revenge": {4, func(v []float64, a *ability) { a.bonus, a.apPct = (v[0]+v[2])/2, v[1] }},
	// "within 20 yards by 12. Lasts 3 min" → [20 12 3]
	"battle_shout": {3, func(v []float64, a *ability) { a.buffAP, a.dur = v[1], v[2]*60 }},
	// "Generates 10 rage at the cost of health, and then generates an additional 10 rage over 10 sec" → [10 10 10]
	"bloodrage": {3, func(v []float64, a *ability) { a.rage, a.rage2, a.dur = v[0], v[1], v[2] }},
	// "… generating extra rage when taking damage. Lasts 10 sec" → [10]
	"berserker_rage": {1, func(v []float64, a *ability) { a.dur = v[0] }},
	// "causing (1 + 15% of Attack Power) damage and healing you for 10% … within 20 sec" → [1 15 10 20]
	"victory_rush": {4, func(v []float64, a *ability) { a.bonus, a.apPct, a.dur = v[0], v[1], v[3] }},
	// "100% increased critical strike chance … next 15 sec … all damage taken is increased by 20%" → [100 15 20]
	"recklessness": {3, func(v []float64, a *ability) { a.critBonus, a.dur, a.takenPct = v[0], v[1], v[2] }},
}

// effectCount is the number of numbers the text of an ability has (0 when the model does not read it).
func effectCount(key string) (int, bool) {
	s, ok := effectSpecs[key]
	return s.n, ok
}

// readEffects reads a client text into a when its numbers match the layout of the ability. It returns
// false, and changes nothing, when the ability has no layout or the count differs.
func readEffects(key, text string, a *ability) bool {
	s, ok := effectSpecs[key]
	if !ok {
		return false
	}
	v := theory.Numbers(text)
	if len(v) != s.n {
		return false
	}
	s.apply(v, a)
	return true
}

// readTooltip reads the sheet's tooltip of a spell, which is in the player's language. The numbers
// are read by position; the count must match the English layout, or nothing is read.
func readTooltip(key, text string, a *ability) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	return readEffects(key, text, a)
}
