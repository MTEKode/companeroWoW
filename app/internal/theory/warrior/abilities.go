package warrior

// Ability numbers. The source of each number is kept with it, in this order:
//
//  1. the client's tooltip of the spell on the character sheet (exact; it already includes the
//     character's talents, so the talent modifiers are not applied again, and it is used only
//     when the build is the sheet's own build);
//  2. the site's ability table (wft:/abilities/warrior/): the rank whose level the character has,
//     with the talent modifiers applied on top;
//  3. the constants table (constants.go), when neither exists.

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// Ability keys, in the order the simulator stores them. The keys are the names the rotation uses.
const (
	kHS = iota
	kRend
	kOverpower
	kExecute
	kMortal
	kBloodthirst
	kSlam
	kWhirlwind
	kCleave
	kDeathWish
	nAbil
)

var abilityKeys = [nAbil]string{
	"hs", "rend", "overpower", "execute", "mortal_strike", "bloodthirst", "slam", "whirlwind", "cleave", "death_wish",
}

var abilityNames = [nAbil]string{
	"Heroic Strike", "Rend", "Overpower", "Execute", "Mortal Strike", "Bloodthirst", "Slam", "Whirlwind", "Cleave", "Death Wish",
}

// abilityIndex maps a rotation key to its index.
func abilityIndex(key string) (int, bool) {
	for i, k := range abilityKeys {
		if k == key {
			return i, true
		}
	}
	return 0, false
}

// Keys returns the rotation keys the model knows, in the order of the abilities.
func Keys() []string {
	out := make([]string, nAbil)
	copy(out, abilityKeys[:])
	return out
}

// Name returns the English name of a rotation key ("" when unknown); the tools translate it.
func Name(key string) string {
	if i, ok := abilityIndex(key); ok {
		return abilityNames[i]
	}
	return key
}

// Regular expressions of the client text. They accept the wording of the site and of the tooltip.
var (
	reIncrease = regexp.MustCompile(`increases melee damage by (\d+)`)
	reWeapon   = regexp.MustCompile(`weapon damage plus (\d+)`)
	reBleed    = regexp.MustCompile(`bleed for (\d+) damage over (\d+) sec`)
	reCausing  = regexp.MustCompile(`causing (\d+) damage`)
	reAP       = regexp.MustCompile(`(\d+)% of your Attack Power plus (\d+)`)
	reCost     = regexp.MustCompile(`(\d+) Rage`)
	reCD       = regexp.MustCompile(`(\d+(?:\.\d+)?) sec cooldown`)
	reCast     = regexp.MustCompile(`(\d+(?:\.\d+)?) sec cast`)
)

// parseText reads the numbers of an ability's client text into a. It fills only what the text says.
func parseText(text string, a *ability) {
	t := strings.ToLower(text)
	if m := reBleed.FindStringSubmatch(t); m != nil {
		a.bleed, _ = strconv.ParseFloat(m[1], 64)
		a.bleedSec, _ = strconv.ParseFloat(m[2], 64)
	}
	if m := reIncrease.FindStringSubmatch(t); m != nil {
		a.bonus, _ = strconv.ParseFloat(m[1], 64)
		a.weapon = false
	}
	if m := reWeapon.FindStringSubmatch(t); m != nil {
		a.bonus, _ = strconv.ParseFloat(m[1], 64)
		a.weapon = true
	}
	if m := reCausing.FindStringSubmatch(t); m != nil {
		a.bonus, _ = strconv.ParseFloat(m[1], 64)
		a.weapon = false
	}
	if m := reAP.FindStringSubmatch(t); m != nil {
		a.apPct, _ = strconv.ParseFloat(m[1], 64)
		a.bonus, _ = strconv.ParseFloat(m[2], 64)
	}
}

// parseMeta reads the cost, cooldown and cast time of a "10 Rage · Melee Range · Instant · 6 sec cooldown" line.
func parseMeta(meta string, a *ability) {
	if m := reCost.FindStringSubmatch(meta); m != nil {
		a.cost, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reCD.FindStringSubmatch(meta); m != nil {
		a.cd, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reCast.FindStringSubmatch(meta); m != nil {
		a.cast, _ = strconv.ParseFloat(m[1], 64)
	}
}

// ParseRank picks the rank the character has: the highest one whose level is at most the character's.
// ok is false when the character has not learned the ability.
func ParseRank(ranks []theory.AbilityRank, level int) (theory.AbilityRank, bool) {
	var best theory.AbilityRank
	ok := false
	for _, r := range ranks {
		if r.Level <= level && (!ok || r.Rank > best.Rank) {
			best, ok = r, true
		}
	}
	return best, ok
}

// rankMeta is the cost and cooldown the class table gives for a rank.
func rankMeta(r theory.AbilityRank) string {
	return r.Cost + " · " + r.Text
}

// baseFromTable fills a from the site's ability table for the rank the level allows.
func baseFromTable(name string, data theory.Data, level int, a *ability) bool {
	ranks, ok := data.Abilities[name]
	if !ok {
		return false
	}
	r, ok := ParseRank(ranks, level)
	if !ok {
		return false
	}
	parseMeta(r.Cost, a)
	parseText(r.Text, a)
	a.source = "tabla"
	return true
}

// Sources of an ability number, as the report names them.
const (
	srcSheet    = "hoja"
	srcTable    = "tabla"
	srcTalent   = "talento"
	srcConstant = "constante"
)
