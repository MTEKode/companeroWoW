package warrior

// The warrior talent tree (wft:/tips/warrior.json, client-derived) and what each talent does in the
// model. The tree rules come from the feed: rank caps, the points a row needs in its tree, and the
// prerequisite talent. The effects are the numbers of the feed's rank 1 and maximum rank; the ranks
// in between are interpolated linearly (the feed gives only those two).

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

// fx is a talent value at rank 1 and at its maximum rank.
type fx struct{ r1, rmax float64 }

// talentFX are the numbers the model reads from each talent (see the comment of each). A talent that is
// not here has no simulated effect; the report lists it as «sin efecto modelado».
var talentFX = map[string]fx{
	"improved-heroic-strike":           {-1, -3}, // Heroic Strike rage cost (only when the cost comes from the table)
	"improved-execute":                 {-3, -5}, // Execute rage cost (same)
	"focused-rage":                     {-1, -3}, // rage cost of the offensive abilities (same)
	"deflection":                       {1, 5},   // parry chance, percent
	"anticipation":                     {4, 20},  // defence skill points
	"improved-rend":                    {12, 35}, // Rend bleed damage, percent (table source only)
	"improved-overpower":               {25, 50}, // Overpower crit chance, points
	"deep-wounds":                      {20, 60}, // bleed over 12 s, percent of the weapon average, per crit
	"impale":                           {10, 20}, // crit damage bonus, percent of the base multiplier
	"two-handed-weapon-specialization": {2, 6},   // melee damage with a two-handed weapon, percent
	"bloodthrill":                      {10, 20}, // chance Overpower opens after a Rend hit, percent
	"weaponmaster":                     {1, 5},   // axe and polearm: crit points
	"weaponmaster-armor":               {3, 15},  // mace and staff: armour ignored, percent
	"weaponmaster-extra":               {1, 5},   // sword: extra attack chance, percent
	"cruelty":                          {1, 5},   // melee crit points
	"improved-slam":                    {2.5, 5}, // Slam cooldown cut, seconds
	"improved-slam-cast":               {0.25, 0.5},
	"unbridled-wrath":                  {12, 60}, // chance for +1 rage per melee hit, percent
	"furious-precision":                {4, 10},  // offhand hit points
	"enrage":                           {2, 10},  // damage bonus after a hit, percent (30 % chance)
	"dual-wield-specialization":        {5, 25},  // offhand damage, percent
	"dual-wield-specialization-rage":   {10, 50}, // offhand rage, percent
	"flurry":                           {10, 30}, // attack speed after a crit, percent (3 swings)
	"improved-whirlwind":               {15, 30}, // Whirlwind damage, percent
	"improved-whirlwind-cd":            {1, 2},   // Whirlwind cooldown cut, seconds
	"anger-management":                 {1, 1},   // rage every 3 s in combat
	"improved-sunder-armor":            {-1, -3}, // Sunder Armor rage cost (only when the cost comes from the table)
	"improved-revenge":                 {20, 60}, // Revenge damage, percent
	"improved-bloodrage":               {25, 50}, // Bloodrage rage, percent
	"improved-berserker-rage":          {5, 10},  // rage Berserker Rage generates at once
	"improved-tactical-mastery":        {5, 15},  // rage kept when changing stance, on top of the base 10
}

// talentGrants are the talents that give the model an ability or a buff by themselves.
var talentGrants = map[string]bool{
	"mortal-strike": true, "bloodthirst": true, "death-wish": true, "sweeping-strikes": true,
}

// slugOf turns a talent name into the site's slug ("Two-Handed Weapon Specialization" → two-handed-weapon-specialization).
func slugOf(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

var prereqRe = regexp.MustCompile(`^(\d+) points? in (.+)$`)

// parsePrereq splits "3 points in Improved Rend" into 3 and the slug of Improved Rend.
func parsePrereq(text string) (points int, slug string) {
	m := prereqRe.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return 0, ""
	}
	points, _ = strconv.Atoi(m[1])
	return points, slugOf(m[2])
}

// Tree is the talent tree of a class, indexed for the optimiser.
type Tree struct {
	Defs    []theory.TalentDef
	index   map[string]int // slug → index
	prereqP []int          // points needed in the prerequisite talent (0 = none)
	prereqI []int          // index of the prerequisite talent (-1 = none)
	trees   []string       // tree names in order
}

// NewTree builds the tree from the feed's talents. It fails when a talent has no row or no maximum rank,
// or a prerequisite names a talent the feed does not have.
func NewTree(defs []theory.TalentDef) (*Tree, error) {
	t := &Tree{Defs: defs, index: map[string]int{}}
	for i, d := range defs {
		if d.Max <= 0 || d.Row <= 0 {
			return nil, fmt.Errorf("talento %q sin fila o sin rangos en la hoja de talentos", d.Name)
		}
		slug := d.Slug
		if slug == "" {
			slug = slugOf(d.Name)
		}
		t.index[slug] = i
		if !contains(t.trees, d.Tree) {
			t.trees = append(t.trees, d.Tree)
		}
	}
	t.prereqP = make([]int, len(defs))
	t.prereqI = make([]int, len(defs))
	for i, d := range defs {
		t.prereqI[i] = -1
		if d.Prereq == "" {
			continue
		}
		p, s := parsePrereq(d.Prereq)
		if s == "" {
			return nil, fmt.Errorf("requisito de %q no entendido: %q", d.Name, d.Prereq)
		}
		j, ok := t.index[s]
		if !ok {
			return nil, fmt.Errorf("el requisito de %q (%s) no está en la hoja de talentos", d.Name, s)
		}
		t.prereqP[i], t.prereqI[i] = p, j
	}
	return t, nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Slug returns the slug of talent i.
func (t *Tree) Slug(i int) string {
	if t.Defs[i].Slug != "" {
		return t.Defs[i].Slug
	}
	return slugOf(t.Defs[i].Name)
}

// Index returns the index of the talent with that name or slug.
func (t *Tree) Index(name string) (int, bool) {
	i, ok := t.index[slugOf(name)]
	return i, ok
}

// Ranks turns a name → rank map into a rank slice indexed like Defs. Unknown names are an error.
func (t *Tree) Ranks(m map[string]int) ([]int, error) {
	out := make([]int, len(t.Defs))
	for name, r := range m {
		i, ok := t.Index(name)
		if !ok {
			return nil, fmt.Errorf("talento desconocido: %q", name)
		}
		if r < 0 || r > t.Defs[i].Max {
			return nil, fmt.Errorf("%s: rango %d fuera de 0–%d", t.Defs[i].Name, r, t.Defs[i].Max)
		}
		out[i] = r
	}
	return out, nil
}

// Named returns the rank slice as name → rank, leaving out the zeros.
func (t *Tree) Named(ranks []int) map[string]int {
	out := map[string]int{}
	for i, r := range ranks {
		if r > 0 {
			out[t.Defs[i].Name] = r
		}
	}
	return out
}

// TreePoints returns the points spent in each tree of a rank slice, by tree name.
func (t *Tree) TreePoints(ranks []int) map[string]int {
	out := map[string]int{}
	for i, r := range ranks {
		out[t.Defs[i].Tree] += r
	}
	return out
}

// Open reports whether one more point can go to talent i: the rank is below the maximum, the tree has
// the points the row needs and the prerequisite talent has its points.
func (t *Tree) Open(ranks []int, i int) bool {
	d := t.Defs[i]
	if ranks[i] >= d.Max {
		return false
	}
	if t.treeTotal(ranks, d.Tree) < d.Need {
		return false
	}
	if j := t.prereqI[i]; j >= 0 && ranks[j] < t.prereqP[i] {
		return false
	}
	return true
}

// Valid reports whether a rank slice follows the tree rules: every talent within its maximum and every
// spent talent opened by the points spent before it. It checks the whole build, not one point.
func (t *Tree) Valid(ranks []int) error {
	for i, d := range t.Defs {
		if ranks[i] < 0 || ranks[i] > d.Max {
			return fmt.Errorf("%s: rango %d fuera de 0–%d", d.Name, ranks[i], d.Max)
		}
		if ranks[i] == 0 {
			continue
		}
		if t.treeTotal(ranks, d.Tree) < d.Need {
			return fmt.Errorf("%s necesita %d puntos en %s", d.Name, d.Need, d.Tree)
		}
		if j := t.prereqI[i]; j >= 0 && ranks[j] < t.prereqP[i] {
			return fmt.Errorf("%s necesita %d puntos en %s", d.Name, t.prereqP[i], t.Defs[j].Name)
		}
	}
	return nil
}

// treeTotal is the points spent in one tree.
func (t *Tree) treeTotal(ranks []int, tree string) int {
	n := 0
	for i, d := range t.Defs {
		if d.Tree == tree {
			n += ranks[i]
		}
	}
	return n
}

// Modelled returns the names of the spent talents that the model reads, and those it does not.
func (t *Tree) Modelled(ranks []int) (used, unused []string) {
	for i, r := range ranks {
		if r == 0 {
			continue
		}
		s := t.Slug(i)
		if _, ok := talentFX[s]; ok || talentGrants[s] {
			used = append(used, t.Defs[i].Name)
		} else {
			unused = append(unused, t.Defs[i].Name)
		}
	}
	sort.Strings(used)
	sort.Strings(unused)
	return used, unused
}

// value returns the effect of talent slug at rank r (0 when the talent is not spent). Ranks between 1 and
// the maximum are interpolated; the maximum comes from the feed.
func (t *Tree) value(ranks []int, slug string) float64 {
	i, ok := t.index[slug]
	if !ok || ranks[i] == 0 {
		return 0
	}
	f, ok := talentFX[slug]
	if !ok {
		return 0
	}
	max := t.Defs[i].Max
	if max <= 1 {
		return f.r1
	}
	r := float64(ranks[i])
	return f.r1 + (r-1)*(f.rmax-f.r1)/float64(max-1)
}

// has reports whether talent slug is spent.
func (t *Tree) has(ranks []int, slug string) bool {
	i, ok := t.index[slug]
	return ok && ranks[i] > 0
}
