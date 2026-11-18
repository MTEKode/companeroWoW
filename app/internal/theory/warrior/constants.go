// Package warrior is the theorycraft model of the Warrior class in WoW: Forever. Every number the
// model uses is in this file, with its source. The sources are:
//
//   - "tooltip": the client's own text, read from the character sheet (exact, includes talents).
//   - "wft:<page>": wowforevertalents.com, client-derived (talent tree feed, ability ranks).
//   - "classic": a Classic-era rule the community documents (vanilla-wow-archive, Wowhead guides).
//   - "aprox.": a documented approximation the model needs and no source gives exactly. These are
//     the first numbers to check when the calibration reports a mismatch.
//
// Talent effects are in talents.go with the same convention; the talent numbers come from the
// tooltip feed (wft:/tips/warrior.json).
package warrior

import "github.com/MTEKode/companeroWoW/app/internal/theory"

// Constants is the table the warrior model reads.
type Constants struct {
	// Melee attack table (theory.AttackTables).
	Attack theory.AttackTables
	// Rage conversion and generation (theory.RageTable), plus rage from talents and timers.
	Rage theory.RageTable
	// Mob model (theory.MobTables).
	Mob theory.MobTables

	// Stats (warrior rules).
	APPerStrength float64 // attack power per point of strength: 2 (classic)
	APOffset      float64 // attack power offset: -20 (classic, warrior)
	AgiPerCrit    float64 // agility per 1 % crit: 20 (aprox., pending check against the sheet)
	HPPerStamina  float64 // health per point of stamina: 10 (aprox.)
	OffhandMult   float64 // offhand damage multiplier: 0.5 (classic dual wield)
	WeaponAPDiv   float64 // weapon speed normalisation: 14 (classic, AP/14 × speed)
	DefPerSkill   float64 // defence skill per level: 5 (classic)

	// Time (seconds).
	GCD            float64 // global cooldown: 1.5 (classic)
	Reset          float64 // walk to the next mob and loot (aprox.)
	DeathPenalty   float64 // release, run back and repair after a death (aprox.)
	FightCap       float64 // a fight that lasts longer counts as not killed (aprox.)
	FoodFraction   float64 // share of max health a food restores (aprox.)
	FoodSeconds    float64 // seconds the food takes to restore it (aprox.)
	ParryHaste     float64 // share of the mob's remaining swing removed by a parry (classic: 0.4)
	RendTickSec    float64 // Rend and Deep Wounds tick every 3 s (classic)
	OverpowerWin   float64 // Overpower is usable 5 s after the target dodges (tooltip: 5 sec)
	ExecuteBelow   float64 // Execute works below 20 % health (tooltip)
	BleedsArmor    bool    // bleeds ignore armour (classic)
	MobCritMult    float64 // mob critical damage multiplier: 2 (classic)
	MobVariance    float64 // ±15 % spread of mob hits (aprox.)
	CritBaseTalent float64 // no base crit for the warrior in the model (aprox., 0)

	// Ability fallbacks used only when the client text is not available (the sheet and the class
	// page both missing). Each one is the classic rule; the tooltip wins when present.
	SunderStack     float64 // armour removed per Sunder Armor stack: 90 (rank 1 of the site)
	SunderMaxStacks float64 // 5 (site text)
	SunderLen       float64 // 30 s (site text)
	RevengeMin      float64 // Revenge damage, rank 1 of the site: 20 + 22.5 % AP
	RevengeAP       float64 // 22.5 (site text)
	RevengeMax      float64 // 24
	RevengeWindow   float64 // seconds after a block, dodge or parry that Revenge is usable: 5 (aprox.)
	BattleShoutAP   float64 // attack power of Battle Shout, rank 1: 12 (site text)
	BattleShoutLen  float64 // 180 s (site text: lasts 3 min)
	BloodrageHPCost float64 // share of base health Bloodrage costs: 0.2 (site text)
	BloodrageCD     float64 // 60 s (site text)
	BloodrageLen    float64 // the rage over time lasts 10 s (site text)
	BerserkerCD     float64 // 30 s (site text)
	BerserkerLen    float64 // 10 s (site text)
	BerserkerTake   float64 // rage from damage taken during Berserker Rage is multiplied by this: 2 (aprox.)
	VictoryAP       float64 // Victory Rush: 15 % of AP (site text)
	VictoryCD       float64 // 30 s (site text)
	VictoryWindow   float64 // usable 20 s after a non-trivial kill (site text)
	RecklessCD      float64 // 1800 s (site text: 30 min)
	RecklessLen     float64 // 15 s (site text)
	RecklessCrit    float64 // +100 % critical strike chance (site text)
	RecklessTake    float64 // +20 % damage taken (site text)
	SweepCD         float64 // 30 s (classic, aprox.)
	SweepCost       float64 // 30 rage (classic, aprox.)
	SweepCharges    int     // the next 5 melee attacks (feed text)
	TacticalRetain  float64 // rage kept when changing stance, before talents: 10 (feed text)
	StanceEvery     float64 // seconds between two stance changes when the build asks for them (aprox.)
	SweepDamage     float64 // the extra target takes this share of the swing (aprox., classic: 100 %)
	DeathWishCD     float64 // 180 s (classic)
	DeathWishLen    float64 // 30 s (tooltip of the talent)
	DeathWishDmg    float64 // +20 % physical damage (tooltip of the talent)
	DeathWishTake   float64 // +5 % damage taken (tooltip of the talent)
	EnrageLen       float64 // 12 s (tooltip of the talent)
	FlurryLen       int     // next 3 swings (tooltip of the talent)
	BloodthirstCD   float64 // 6 s (tooltip of the ability)
	MortalCD        float64 // 6 s (tooltip of the ability)
	SlamCD          float64 // 15 s (tooltip of the ability)
	SlamCast        float64 // 1.5 s (tooltip of the ability)
	WhirlCD         float64 // 10 s (tooltip of the ability)
	RendFallback    float64 // total Rend damage when there is no text: aprox.
	HSFallback      float64 // Heroic Strike bonus when there is no text: aprox.
	FallbackCost    map[string]float64
}

// Sources returns the lines of the formula table for the report, in reading order.
func Sources() []string {
	return []string{
		"Fallo de ataque: 5 % base; +0,1 %/punto de defensa sobre habilidad hasta 10 puntos; +0,4 %/punto después (classic: vanilla-wow-archive «Hit»; 8 % a +3 niveles con 300 de habilidad, foro Blizzard).",
		"Penalización de mano izquierda: +19 % de fallo (classic dual wield).",
		"Esquiva/parada y crítico: +0,04 %/punto de defensa sobre habilidad; el crítico baja 0,04 %/punto (Wowhead, Icy Veins beta).",
		"Golpe glanceante: 10 % + 2 %/punto (máx. 40 %), daño ×0,75 (aprox.).",
		"Armadura: armadura / (armadura + 400 + 85 × nivel atacante) (petición del proyecto).",
		"Ira por daño causado: 7,5 × daño / conversión(nivel); por daño recibido: 2,5 × daño / conversión(nivel) (classic; el 15 en lugar de 7,5 aparece en algunas fuentes).",
		"Conversión de ira por nivel: 10→37,4; 20→72,4; 30→109,3; 40→147,9; 50→188,3; 60→230,6; 70→274,7 (wiki y foro Blizzard).",
		"Golpe blanco: arma (mín–máx) + AP/14 × velocidad; mano izquierda ×0,5 (classic).",
		"AP del guerrero: 2 × fuerza − 20 (classic).",
		"Enemigos (aprox.): defensa 5 × nivel; vida 50 + 25 × nivel; armadura 25 × nivel; golpe medio 5 + nivel; golpe cada 2 s; crítico 5 %.",
		"Descanso tras un combate: comida que restaura 40 % de la vida en 27 s (aprox.) + 10 s de reinicio (aprox.); tras la muerte 60 s (aprox.).",
		"Grito de batalla: se lanza fuera de combate y dura 3 min (sitio); cuenta toda la pelea y no suma si la hoja ya lo muestra.",
		"Dos enemigos juntos (aprox.): cada uno tira sus golpes y te ataca; la pelea acaba cuando mueren los dos; Barrido, Torbellino y Hendedura pegan a los dos.",
	}
}

// Table is the constant table of the warrior model.
var Table = Constants{
	Attack: theory.AttackTables{
		MissBase:      5,
		MissSlopeLow:  0.1,
		MissBreakPts:  10,
		MissSlopeHigh: 0.4,
		DualWieldMiss: 19,
		DefPerPoint:   0.04,
		CritPerPoint:  0.04,
		GlanceBase:    10,
		GlancePerPt:   2,
		GlanceMax:     40,
		GlanceMult:    0.75,
		ArmorK:        400,
		ArmorPerLevel: 85,
	},
	Rage: theory.RageTable{
		Levels:      []float64{10, 20, 30, 40, 50, 60, 70},
		Conversions: []float64{37.4, 72.4, 109.3, 147.9, 188.3, 230.6, 274.7},
		FromDealt:   7.5,
		FromTaken:   2.5,
	},
	Mob: theory.MobTables{
		DefensePerLevel: 5,
		HPBase:          50,
		HPPerLevel:      25,
		ArmorPerLevel:   25,
		DamageBase:      5,
		DamagePerLevel:  1,
		Swing:           2.0,
		Crit:            5,
		Dodge:           5,
		EliteHP:         2.5,
		EliteDamage:     1.5,
	},
	APPerStrength: 2,
	APOffset:      -20,
	AgiPerCrit:    20,
	HPPerStamina:  10,
	OffhandMult:   0.5,
	WeaponAPDiv:   14,
	DefPerSkill:   5,

	GCD:            1.5,
	Reset:          10,
	DeathPenalty:   60,
	FightCap:       300,
	FoodFraction:   0.4,
	FoodSeconds:    27,
	ParryHaste:     0.4,
	RendTickSec:    3,
	OverpowerWin:   5,
	ExecuteBelow:   0.2,
	BleedsArmor:    false,
	MobCritMult:    2,
	MobVariance:    0.15,
	CritBaseTalent: 0,

	SunderStack:     90,
	SunderMaxStacks: 5,
	SunderLen:       30,
	RevengeMin:      20,
	RevengeAP:       22.5,
	RevengeMax:      24,
	RevengeWindow:   5,
	BattleShoutAP:   12,
	BattleShoutLen:  180,
	BloodrageHPCost: 0.2,
	BloodrageCD:     60,
	BloodrageLen:    10,
	BerserkerCD:     30,
	BerserkerLen:    10,
	BerserkerTake:   2,
	VictoryAP:       15,
	VictoryCD:       30,
	VictoryWindow:   20,
	RecklessCD:      1800,
	RecklessLen:     15,
	RecklessCrit:    100,
	RecklessTake:    20,
	SweepCD:         30,
	SweepCost:       30,
	SweepCharges:    5,
	TacticalRetain:  10,
	StanceEvery:     60,
	SweepDamage:     1,

	DeathWishCD:   180,
	DeathWishLen:  30,
	DeathWishDmg:  20,
	DeathWishTake: 5,
	EnrageLen:     12,
	FlurryLen:     3,
	BloodthirstCD: 6,
	MortalCD:      6,
	SlamCD:        15,
	SlamCast:      1.5,
	WhirlCD:       10,
	RendFallback:  60,
	HSFallback:    40,
	FallbackCost: map[string]float64{
		"hs": 15, "rend": 10, "overpower": 5, "execute": 15, "mortal_strike": 30,
		"bloodthirst": 30, "slam": 15, "whirlwind": 25, "cleave": 20,
		"sunder": 15, "revenge": 5, "battle_shout": 10, "bloodrage": 0, "berserker_rage": 0,
		"victory_rush": 0, "recklessness": 0, "sweeping_strikes": 30,
	},
}
