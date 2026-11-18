package warrior

import (
	"strconv"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
)

func warriorTree(t *testing.T) *Tree {
	t.Helper()
	tree, err := NewTree(siteData(t).Talents)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestTreeFromTheFeed(t *testing.T) {
	tree := warriorTree(t)
	if len(tree.Defs) != 53 {
		t.Fatalf("talents = %d, want 53", len(tree.Defs))
	}
	if len(tree.trees) != 3 {
		t.Fatalf("trees = %v", tree.trees)
	}
	mortal, ok := tree.Index("Mortal Strike")
	if !ok {
		t.Fatal("Mortal Strike not in the tree")
	}
	if tree.prereqI[mortal] < 0 || tree.Defs[tree.prereqI[mortal]].Name != "Sweeping Strikes" || tree.prereqP[mortal] != 1 {
		t.Fatalf("Mortal Strike prerequisite = %d %d", tree.prereqI[mortal], tree.prereqP[mortal])
	}
}

func TestRowRuleNeedsPointsInTheTree(t *testing.T) {
	tree := warriorTree(t)
	ranks := make([]int, len(tree.Defs))
	row2, _ := tree.Index("Improved Overpower") // row 2: needs 5 points in Arms
	if tree.Open(ranks, row2) {
		t.Fatal("row 2 must be closed with no points")
	}
	deflection, _ := tree.Index("Deflection")
	ranks[deflection] = 5
	if !tree.Open(ranks, row2) {
		t.Fatal("row 2 must open with 5 points in the tree")
	}
}

func TestValidRejectsPrerequisiteAndRowBreaks(t *testing.T) {
	tree := warriorTree(t)
	ranks := make([]int, len(tree.Defs))
	mortal, _ := tree.Index("Mortal Strike")
	ranks[mortal] = 1
	if err := tree.Valid(ranks); err == nil {
		t.Fatal("Mortal Strike without Sweeping Strikes must be invalid")
	}
	ranks = make([]int, len(tree.Defs))
	imp, _ := tree.Index("Improved Overpower")
	ranks[imp] = 1
	if err := tree.Valid(ranks); err == nil {
		t.Fatal("a row 2 talent with no tree points must be invalid")
	}
	ranks = make([]int, len(tree.Defs))
	dfl, _ := tree.Index("Deflection")
	ranks[dfl] = 6
	if err := tree.Valid(ranks); err == nil {
		t.Fatal("a rank above the maximum must be invalid")
	}
}

func TestSheetBuildIsValid(t *testing.T) {
	tree := warriorTree(t)
	sh := sheetL20(t)
	ranks, err := tree.Ranks(sh.TalentRanks())
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Valid(ranks); err != nil {
		t.Fatalf("the sheet's own build is not valid: %v", err)
	}
	if got := tree.TreePoints(ranks); got["Arms"] != 8 || got["Fury"] != 3 {
		t.Fatalf("tree points = %v", got)
	}
}

// Every number the model reads from a talent must appear in the client text of that talent: the rank 1
// text carries the first value and the maximum rank's text the last one. The model's numbers come from
// the feed, so a mismatch means the table is wrong.
func TestTalentNumbersAppearInTheClientText(t *testing.T) {
	tree := warriorTree(t)
	feedOf := map[string]string{
		"weaponmaster-armor":             "weaponmaster",
		"weaponmaster-extra":             "weaponmaster",
		"improved-slam-cast":             "improved-slam",
		"improved-whirlwind-cd":          "improved-whirlwind",
		"dual-wield-specialization-rage": "dual-wield-specialization",
	}
	for key, fx := range talentFX {
		slug := key
		if s, ok := feedOf[key]; ok {
			slug = s
		}
		i, ok := tree.index[slug]
		if !ok {
			t.Errorf("%s: no talent %q in the feed", key, slug)
			continue
		}
		d := tree.Defs[i]
		first := d.RankText[1]
		last := d.RankText[d.Max]
		if d.Max == 1 {
			last = first
		}
		for _, v := range []struct {
			val  float64
			text string
		}{{fx.r1, first}, {fx.rmax, last}} {
			num := strconv.FormatFloat(absf(v.val), 'f', -1, 64)
			if !strings.Contains(v.text, num) {
				t.Errorf("%s: %s not in %q", key, num, v.text)
			}
		}
	}
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestMappingOfTalentNamesToSlugs(t *testing.T) {
	cases := map[string]string{
		"Two-Handed Weapon Specialization": "two-handed-weapon-specialization",
		"Improved Heroic Strike":           "improved-heroic-strike",
		"Weaponmaster":                     "weaponmaster",
	}
	for name, want := range cases {
		if got := slugOf(name); got != want {
			t.Errorf("slugOf(%q) = %q, want %q", name, got, want)
		}
	}
	p, s := parsePrereq("3 points in Improved Rend")
	if p != 3 || s != "improved-rend" {
		t.Fatalf("parsePrereq = %d %q", p, s)
	}
}

func TestNewTreeRejectsUnknownPrerequisite(t *testing.T) {
	_, err := NewTree([]theory.TalentDef{{Name: "X", Tree: "Arms", Row: 1, Max: 1, Prereq: "1 point in Nothing"}})
	if err == nil {
		t.Fatal("a prerequisite that is not in the tree must fail")
	}
}
