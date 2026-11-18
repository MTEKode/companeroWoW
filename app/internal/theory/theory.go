// Package theory is the deterministic theorycraft engine. It computes numbers (DPS, rage, stat
// weights, item and talent values) from the character sheet and the game's formulas, by simulating
// fights with a fixed seed. The language model explains these numbers; it never computes them.
//
// A class plugs in by implementing Class. The types here are class-agnostic; the numbers that belong
// to one class live in its package (see theory/warrior).
package theory

import "fmt"

// Class is the model of one playable class.
type Class interface {
	// Key is the class token the sheet uses ("WARRIOR").
	Key() string
	// Name is the Spanish name of the class.
	Name() string
	// Evaluate simulates leveling fights for the build and returns the report.
	Evaluate(in Input) (Report, error)
	// Weights returns the stat weights at the current gear (finite differences over the simulation).
	Weights(in Input) (WeightsReport, error)
	// CompareItems scores candidate items by simulated kills per hour, not by item level.
	CompareItems(in Input, items []Item) (ItemsReport, error)
	// OptimizeTalents searches the talent points still to come and returns the best builds.
	OptimizeTalents(in Input, opt TalentSearch) (TalentPlan, error)
	// OptimizeRotation compares priority lists built from the known abilities.
	OptimizeRotation(in Input, opt RotationSearch) (RotationPlan, error)
	// Calibrate compares the model's derived numbers with the sheet's.
	Calibrate(in Input) []Check
	// Review compares a recorded fight with the expected numbers of the model.
	Review(in Input, f FightRecord) FightReview
}

// Data is what the class model needs from the site: the talent tree and the ability ranks (the
// wowforevertalents.com feeds, client-derived). The tools fill it; tests use the saved fixtures.
type Data struct {
	Talents   []TalentDef
	Abilities map[string][]AbilityRank // by ability name, as the site's class page lists it
}

// TalentDef is one talent of the tree.
type TalentDef struct {
	Slug     string
	Name     string
	Tree     string
	Row      int
	Max      int
	Need     int    // points in the tree before the talent opens
	Prereq   string // "3 points in Improved Rend", or ""
	Status   string
	RankText map[int]string // rank → client text (the feed gives rank 1 and the maximum)
}

// AbilityRank is one rank of a trainer ability: the level it unlocks, its cost and its client text.
type AbilityRank struct {
	Rank  int
	Level int
	Cost  string
	Text  string
}

// Delta is a change to the character's numbers, used by finite differences and item comparisons.
// Stats are the primary stats; the rest are the sheet's own units (percentage points, skill points).
type Delta struct {
	Str, Agi, Sta  float64
	AP             float64
	Crit, Hit      float64 // percentage points
	Skill          float64 // weapon skill points
	Expertise      float64
	Armor          float64
	Dodge, Parry   float64 // percentage points
	MHAvg, MHSpeed float64 // weapon average damage and speed changes
}

// Add returns the sum of two deltas.
func (d Delta) Add(o Delta) Delta {
	return Delta{
		Str: d.Str + o.Str, Agi: d.Agi + o.Agi, Sta: d.Sta + o.Sta, AP: d.AP + o.AP,
		Crit: d.Crit + o.Crit, Hit: d.Hit + o.Hit, Skill: d.Skill + o.Skill, Expertise: d.Expertise + o.Expertise,
		Armor: d.Armor + o.Armor, Dodge: d.Dodge + o.Dodge, Parry: d.Parry + o.Parry,
		MHAvg: d.MHAvg + o.MHAvg, MHSpeed: d.MHSpeed + o.MHSpeed,
	}
}

// Item is a candidate the comparison scores. Stats use the client's stat keys (ITEM_MOD_*_SHORT).
type Item struct {
	Name      string
	ItemID    int64
	EquipLoc  string
	ItemLevel int
	Slot      string // the sheet slot it would replace, when known
	Stats     map[string]float64
	Replaces  map[string]float64 // stats of the item it replaces (empty for an empty slot)
	Source    string
}

// Options are the simulation settings shared by every calculation.
type Options struct {
	Fights   int    // fights per evaluation; 0 uses the class default
	Seed     uint64 // the seed; 0 uses the class default
	MobLevel int    // a fixed mob level; 0 draws it from the leveling window (level-2 to level+2)
	Elite    bool
}

// Build is one configuration to evaluate: talent ranks by name, the rotation (class ability keys in
// priority order), class parameters and a change to the sheet's numbers.
type Build struct {
	Talents  map[string]int
	Rotation []string
	Params   map[string]float64
	Delta    Delta
}

// Input is everything one calculation reads.
type Input struct {
	Sheet Sheet
	Data  Data
	Build Build
	Opt   Options
}

// CI is a mean with its 95 % confidence interval.
type CI struct {
	Mean float64 `json:"media"`
	Low  float64 `json:"bajo"`
	High float64 `json:"alto"`
}

// Report is the result of Evaluate.
type Report struct {
	Clase        string         `json:"clase"`
	Nivel        int            `json:"nivel"`
	Peleas       int            `json:"peleas"`
	DPS          CI             `json:"dps"`
	KillsPerHour CI             `json:"killsPorHora"`
	TTK          CI             `json:"tiempoParaMatarSeg"`
	Muerte       float64        `json:"fraccionMuertes"`
	Descanso     CI             `json:"descansoPorMuerteSeg"` // eating and resetting after a kill
	Ira          []RagePoint    `json:"ira,omitempty"`
	Habilidades  []AbilityStat  `json:"habilidades"`
	Supuestos    []string       `json:"supuestos"`
	Calibracion  []Check        `json:"calibracion,omitempty"`
	Rotacion     []string       `json:"rotacion,omitempty"`
	Talentos     map[string]int `json:"talentos,omitempty"`
}

// RagePoint is the average rage at one time of the fight.
type RagePoint struct {
	Segundo float64 `json:"segundo"`
	Media   float64 `json:"media"`
}

// AbilityStat is one ability's share of the damage.
type AbilityStat struct {
	Nombre     string  `json:"nombre"`
	UsosPelea  float64 `json:"usosPorPelea"`
	DanoPorcen float64 `json:"porcentajeDano"`
	DanoUso    float64 `json:"danoPorUso"`
	Fuente     string  `json:"fuente"` // "hoja" (the client tooltip) or "constante" (the model's table)
}

// WeightsReport is the output of Weights.
type WeightsReport struct {
	Nivel     int          `json:"nivel"`
	Peleas    int          `json:"peleas"`
	DPSBase   float64      `json:"dpsBase"`
	KPHBase   float64      `json:"killsPorHoraBase"`
	Pesos     []StatWeight `json:"pesos"`
	Supuestos []string     `json:"supuestos"`
	Ruido     string       `json:"ruido,omitempty"`
}

// StatWeight is the change in DPS and kills per hour for one unit of a stat, and its ratio to AP.
type StatWeight struct {
	Stat       string  `json:"stat"`
	Unidad     string  `json:"unidad"`
	DeltaDPS   float64 `json:"deltaDPSPorUnidad"`
	DeltaKPH   float64 `json:"deltaKPHPorUnidad"`
	RelativoAP float64 `json:"relativoAP"` // weight as a fraction of AP's weight, in kills per hour
}

// ItemsReport ranks the candidates, best first.
type ItemsReport struct {
	KPHBase       float64     `json:"killsPorHoraBase"`
	Items         []ItemScore `json:"items"`
	NoComparables []string    `json:"noComparables,omitempty"`
	Supuestos     []string    `json:"supuestos"`
}

// ItemScore is the simulated change one item brings.
type ItemScore struct {
	Name       string   `json:"nombre"`
	ItemID     int64    `json:"itemID,omitempty"`
	Slot       string   `json:"ranura,omitempty"`
	DeltaDPS   float64  `json:"deltaDPS"`
	DeltaKPH   float64  `json:"deltaKPH"`
	DeltaKPHCI CI       `json:"deltaKPHIC"`
	NoModelado []string `json:"statsNoModelados,omitempty"`
	Fuente     string   `json:"fuente,omitempty"`
}

// TalentSearch configures OptimizeTalents.
type TalentSearch struct {
	Points int // points to add (one per level), default 10
	Beam   int // states kept per step, default 5
	Opts   Options
}

// TalentPlan is the output of OptimizeTalents.
type TalentPlan struct {
	Nivel     int            `json:"nivel"`
	Puntos    int            `json:"puntosAñadidos"`
	Actual    Report         `json:"actual"` // the build as the sheet has it
	Top       []TalentOption `json:"opciones"`
	Evaluadas int            `json:"evaluaciones"`
	Supuestos []string       `json:"supuestos"`
}

// TalentOption is one ranked build: the order of the points added and the numbers it gives.
type TalentOption struct {
	Orden     []string       `json:"ordenPuntos"`
	Arboles   map[string]int `json:"arboles"`
	Resultado Report         `json:"resultado"`
}

// RotationSearch configures OptimizeRotation.
type RotationSearch struct {
	Opts Options
}

// RotationPlan is the output of OptimizeRotation, best first.
type RotationPlan struct {
	Opciones  []RotationOption `json:"opciones"`
	Supuestos []string         `json:"supuestos"`
}

// RotationOption is one priority list with its numbers.
type RotationOption struct {
	Prioridad  []string           `json:"prioridad"`
	Parametros map[string]float64 `json:"parametros,omitempty"`
	Resultado  Report             `json:"resultado"`
}

// Check is one calibration comparison: the model against the sheet.
type Check struct {
	Nombre string  `json:"nombre"`
	Modelo float64 `json:"modelo"`
	Hoja   float64 `json:"hoja"`
	Desvio float64 `json:"desvioPorcen"` // relative difference, percent
	Ok     bool    `json:"ok"`
	Nota   string  `json:"nota,omitempty"`
}

// Mismatch returns the sentence the tools show when a calibration check fails, or "" when they match.
func Mismatch(checks []Check) string {
	var parts []string
	for _, c := range checks {
		if c.Ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: modelo %.1f, hoja %.1f (%+.1f %%)", c.Nombre, c.Modelo, c.Hoja, c.Desvio))
	}
	if len(parts) == 0 {
		return ""
	}
	out := "modelo desajustado: "
	for i, p := range parts {
		if i > 0 {
			out += "; "
		}
		out += p
	}
	return out
}

// FightRecord is one recorded fight, as the combat record gives it.
type FightRecord struct {
	Segundos    float64
	Muerte      bool
	DanoHecho   float64
	NivelMob    int
	Habilidades []string // ability keys seen in the fight
}

// FightReview is the comparison of a recorded fight with the model.
type FightReview struct {
	DPSReal     float64  `json:"dpsReal"`
	DPSEsperado CI       `json:"dpsEsperado"`
	Brecha      float64  `json:"brechaPorcen"` // actual against expected, percent (negative = below)
	SinUsar     []string `json:"habilidadesSinUsar,omitempty"`
	Supuestos   []string `json:"supuestos,omitempty"`
}
