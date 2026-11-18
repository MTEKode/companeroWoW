package theory

// The character sheet the addon exports (contract "CHARACTER SHEET + SPELL DATA"). Every field
// is optional: a missing value is zero (or nil for the offhand), and the engine says which
// numbers it could not use instead of guessing them.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Stats are the five primary stats.
type Stats struct {
	Str float64 `json:"str"`
	Agi float64 `json:"agi"`
	Sta float64 `json:"sta"`
	Int float64 `json:"int"`
	Spi float64 `json:"spi"`
}

// Melee is the melee block of the sheet. Percentages are as the client shows them (crit 6.2 = 6.2 %).
type Melee struct {
	AP          float64  `json:"ap"`
	APBase      float64  `json:"apBase"`
	APPos       float64  `json:"apPos"`
	APNeg       float64  `json:"apNeg"`
	Crit        float64  `json:"crit"`
	Hit         float64  `json:"hit"`
	Expertise   float64  `json:"expertise"`
	MHMin       float64  `json:"mhMin"`
	MHMax       float64  `json:"mhMax"`
	MHSpeed     float64  `json:"mhSpeed"`
	OHMin       *float64 `json:"ohMin"`
	OHMax       *float64 `json:"ohMax"`
	OHSpeed     *float64 `json:"ohSpeed"`
	MHSkill     float64  `json:"mhSkill"`
	OHSkill     *float64 `json:"ohSkill"`
	AttackSpeed float64  `json:"attackSpeed"`
}

// Defense is the defence block: dodge, parry and block in percent, and the defence skill.
type Defense struct {
	Dodge        float64 `json:"dodge"`
	Parry        float64 `json:"parry"`
	Block        float64 `json:"block"`
	BlockValue   float64 `json:"blockValue"`
	DefenseSkill float64 `json:"defenseSkill"`
}

// SheetTalent is one talent of a tree, with the rank the character has.
type SheetTalent struct {
	Name    string `json:"name"`
	Rank    int    `json:"rank"`
	MaxRank int    `json:"maxRank"`
	Row     int    `json:"row"`
	Col     int    `json:"col"`
	SpellID int    `json:"spellID"`
}

// SheetTree is one talent tree of the character.
type SheetTree struct {
	Name    string        `json:"name"`
	Points  int           `json:"points"`
	Talents []SheetTalent `json:"talents"`
}

// SheetSpell is one known spell with the client's tooltip text.
type SheetSpell struct {
	SpellID   int     `json:"spellID"`
	Name      string  `json:"name"`
	Rank      string  `json:"rank"`
	Known     bool    `json:"known"`
	Cost      float64 `json:"cost"`
	PowerType string  `json:"powerType"`
	CastTime  float64 `json:"castTime"`
	Cooldown  float64 `json:"cooldown"`
	Range     float64 `json:"range"`
	Tooltip   string  `json:"tooltip"`
}

// WeaponSkill is one weapon skill line of the character.
type WeaponSkill struct {
	Name string  `json:"name"`
	Rank float64 `json:"rank"`
	Max  float64 `json:"max"`
}

// Sheet is the character sheet.
type Sheet struct {
	ExportedAt     int64   `json:"exportedAt"`
	Level          int     `json:"level"`
	Class          string  `json:"class"`
	Race           string  `json:"race"`
	Stance         string  `json:"stance"`
	Base           Stats   `json:"base"`
	Total          Stats   `json:"total"`
	Health         float64 `json:"health"`
	HealthMax      float64 `json:"healthMax"`
	Power          float64 `json:"power"`
	PowerMax       float64 `json:"powerMax"`
	PowerType      string  `json:"powerType"`
	Armor          float64 `json:"armor"`
	ArmorEffective float64 `json:"armorEffective"`
	Melee          Melee   `json:"melee"`
	Defense        Defense `json:"defense"`
	Talents        struct {
		Tabs    []SheetTree `json:"tabs"`
		Unspent int         `json:"unspent"`
	} `json:"talents"`
	Spells       []SheetSpell  `json:"spells"`
	Buffs        []int         `json:"buffs"` // spell ids of the buffs the character has now (player auras)
	WeaponSkills []WeaponSkill `json:"weaponSkills"`
}

// ParseSheet decodes the sheet JSON. It fails only when the JSON is broken or the sheet has no
// level; everything else is optional.
func ParseSheet(raw []byte) (Sheet, error) {
	var s Sheet
	if err := json.Unmarshal(raw, &s); err != nil {
		return Sheet{}, fmt.Errorf("la hoja del personaje no es JSON válido: %w", err)
	}
	if s.Level <= 0 {
		return Sheet{}, errors.New("la hoja del personaje no trae el nivel")
	}
	s.Class = strings.ToUpper(strings.TrimSpace(s.Class))
	return s, nil
}

// SpentPoints is the points the sheet shows as spent in all trees.
func (s Sheet) SpentPoints() int {
	n := 0
	for _, t := range s.Talents.Tabs {
		for _, tal := range t.Talents {
			n += tal.Rank
		}
	}
	return n
}

// TalentRanks returns the sheet's talents as name → rank (talents with no rank are left out).
func (s Sheet) TalentRanks() map[string]int {
	out := map[string]int{}
	for _, t := range s.Talents.Tabs {
		for _, tal := range t.Talents {
			if tal.Rank > 0 {
				out[tal.Name] = tal.Rank
			}
		}
	}
	return out
}

// WeaponKind names the main-hand weapon family from the weapon skill lines ("Two-Handed Swords"
// → sword, two-handed). ok is false when the sheet has no weapon skill of a known family.
func (s Sheet) WeaponKind() (kind string, twoHanded bool, ok bool) {
	for _, w := range s.WeaponSkills {
		if kind, th, ok := KindOf(w.Name); ok {
			return kind, th, true
		}
	}
	return "", false, false
}

// KindOf names the weapon family of a weapon skill or item subclass name, in English ("One-Handed Axes"
// → axe). ok is false for a name it does not know.
func KindOf(name string) (kind string, twoHanded bool, ok bool) {
	n := strings.ToLower(name)
	th := strings.Contains(n, "two-handed")
	switch {
	case strings.Contains(n, "sword"):
		return "sword", th, true
	case strings.Contains(n, "axe"):
		return "axe", th, true
	case strings.Contains(n, "mace"):
		return "mace", th, true
	case strings.Contains(n, "polearm"):
		return "polearm", th, true
	case strings.Contains(n, "staff"):
		return "staff", th, true
	case strings.Contains(n, "dagger"):
		return "dagger", false, true
	case strings.Contains(n, "fist"):
		return "fist", false, true
	}
	return "", false, false
}
