package warrior

// Ability numbers. The source of each number is kept with it, in this order:
//
//  1. the client's tooltip of the spell on the character sheet, read by position (effects.go): exact,
//     language-independent, and it already includes the character's talents, so the talent modifiers
//     are not applied again. It is used only when the build is the sheet's own build;
//  2. the site's ability table (wft:/abilities/warrior/): the rank whose level the character has, read
//     in English with the same layout, with the talent modifiers applied on top;
//  3. the constants table (constants.go), when neither exists.

import (
	"regexp"
	"strconv"

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
	kSunder
	kRevenge
	kBattleShout
	kBloodrage
	kBerserkerRage
	kVictoryRush
	kRecklessness
	kSweeping
	nAbil
)

var abilityKeys = [nAbil]string{
	"hs", "rend", "overpower", "execute", "mortal_strike", "bloodthirst", "slam", "whirlwind", "cleave", "death_wish",
	"sunder", "revenge", "battle_shout", "bloodrage", "berserker_rage", "victory_rush", "recklessness", "sweeping_strikes",
}

// abilityNames are the English names: the site's table key and the report's label (the tools show the
// Spanish names through the explanation, the numbers keep these keys).
var abilityNames = [nAbil]string{
	"Heroic Strike", "Rend", "Overpower", "Execute", "Mortal Strike", "Bloodthirst", "Slam", "Whirlwind", "Cleave", "Death Wish",
	"Sunder Armor", "Revenge", "Battle Shout", "Bloodrage", "Berserker Rage", "Victory Rush", "Recklessness", "Sweeping Strikes",
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

// reMeta reads the cost, cooldown and cast time of the site's "10 Rage · Melee Range · Instant · 6 sec
// cooldown" line. The site is English on every client, so this text is never localised.
var (
	reCost = regexp.MustCompile(`(\d+) Rage`)
	reCD   = regexp.MustCompile(`(\d+(?:\.\d+)?) (sec|min) cooldown`)
	reCast = regexp.MustCompile(`(\d+(?:\.\d+)?) sec cast`)
)

// parseMeta reads the site's cost, cooldown and cast time into a.
func parseMeta(meta string, a *ability) {
	if m := reCost.FindStringSubmatch(meta); m != nil {
		a.cost, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reCD.FindStringSubmatch(meta); m != nil {
		a.cd, _ = strconv.ParseFloat(m[1], 64)
		if m[2] == "min" {
			a.cd *= 60
		}
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

// baseFromTable fills a from the site's ability table for the rank the level allows. The English text is
// read by the same layout as the sheet's tooltip; a text that does not match leaves the numbers unset.
func baseFromTable(name, key string, data theory.Data, level int, a *ability) bool {
	ranks, ok := data.Abilities[name]
	if !ok {
		return false
	}
	r, ok := ParseRank(ranks, level)
	if !ok {
		return false
	}
	parseMeta(r.Cost, a)
	readEffects(key, r.Text, a)
	a.source = srcTable
	return true
}

// Sources of an ability number, as the report names them.
const (
	srcSheet    = "hoja"
	srcTable    = "tabla"
	srcTalent   = "talento"
	srcConstant = "constante"
)
