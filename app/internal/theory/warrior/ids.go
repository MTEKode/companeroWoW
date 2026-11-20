package warrior

// Spell ids of the warrior abilities and talents. Everything the model reads from the character
// sheet is matched by spell id, never by name: the client's names and tooltips are in the player's
// language (a Spanish client says "Desgarrar" where the site says "Rend"), while the spell id is the
// same in every locale.
//
// Where the ids come from:
//
//   - The rotation abilities were checked against the sheet fixture (testdata/sheet-warrior-l20.json
//     carries 78 Heroic Strike, 772 Rend, 7384 Overpower, 845 Cleave, 1464 Slam, 6673 Battle Shout,
//     2457 Battle Stance, 100 Charge, 1715 Hamstring). The other ranks of those abilities and the
//     other abilities of the table are the Classic-era ids of the same spells.
//   - The talents are the Classic-era ids of the same talents, for the ranks the model reads. The
//     site (wowforevertalents.com) does not publish spell ids: its ability pages link ids only for
//     armour and weapon proficiencies, and its talent pages not at all. The addon exports the id of
//     every talent entry it reads (Sheet.lua, entrySpell), so one live sheet shows the ids the client
//     uses. A talent whose id is not in this table is matched by its English name when the sheet has
//     an English name, and otherwise it is reported as «talento sin identificar» (never guessed).
//
// To add an id: add it to the table below with its ability or talent. The tests check that every id
// maps to one key and that the fixture ids keep their keys.

// abilityIDs maps a spell id to its rotation key (see abilityKeys in abilities.go). Each rank of an
// ability has its own id; all of them map to the same key.
var abilityIDs = map[int]string{
	// Heroic Strike
	78: "hs", 284: "hs", 285: "hs", 1608: "hs", 11564: "hs", 11565: "hs", 11566: "hs", 11567: "hs", 25286: "hs", 29707: "hs", 30324: "hs",
	// Rend
	772: "rend", 6546: "rend", 6547: "rend", 6548: "rend", 11572: "rend", 11573: "rend", 11574: "rend",
	// Overpower
	7384: "overpower", 7887: "overpower", 11584: "overpower", 11585: "overpower",
	// Execute
	5308: "execute", 20658: "execute", 20660: "execute", 20661: "execute", 20662: "execute", 25234: "execute", 25236: "execute",
	// Mortal Strike
	12294: "mortal_strike", 21551: "mortal_strike", 21552: "mortal_strike", 21553: "mortal_strike", 25248: "mortal_strike", 30330: "mortal_strike",
	// Bloodthirst
	23881: "bloodthirst", 23892: "bloodthirst", 23893: "bloodthirst", 23894: "bloodthirst", 25251: "bloodthirst", 30335: "bloodthirst",
	// Slam
	1464: "slam", 8820: "slam", 11604: "slam", 11605: "slam", 25242: "slam",
	// Whirlwind
	1680: "whirlwind",
	// Cleave
	845: "cleave", 7369: "cleave", 11608: "cleave", 11609: "cleave", 20569: "cleave", 25231: "cleave",
	// Death Wish and Sweeping Strikes: the client's pair (Deseo de la Muerte = 12328, Golpes de barrido =
	// 12292, Spanish-client sheet; see talentIDs). This table had them the other way round.
	12328: "death_wish",
	12292: "sweeping_strikes",
	// Sunder Armor
	7386: "sunder", 7405: "sunder", 8380: "sunder", 11596: "sunder", 11597: "sunder",
	// Revenge
	6572: "revenge", 6574: "revenge", 7379: "revenge", 11600: "revenge", 11601: "revenge", 25288: "revenge",
	// Battle Shout
	6673: "battle_shout", 5242: "battle_shout", 6192: "battle_shout", 11549: "battle_shout", 11550: "battle_shout", 11551: "battle_shout", 25289: "battle_shout",
	// Bloodrage, Berserker Rage, Victory Rush, Recklessness, Sweeping Strikes
	2687: "bloodrage", 18499: "berserker_rage", 34428: "victory_rush", 1719: "recklessness",
}

// talentIDs maps a talent entry's spell id to the talent's slug (the site's slug, see slugOf).
var talentIDs = map[int]string{
	// Deep Wounds (rank 1 to 3)
	12162: "deep-wounds", 12850: "deep-wounds", 12868: "deep-wounds",
	// Flurry (rank 1 to 3 of the site's three ranks)
	12966: "flurry", 12967: "flurry", 12968: "flurry",
	// Cruelty
	12320: "cruelty", 12852: "cruelty", 12853: "cruelty", 12854: "cruelty", 12855: "cruelty",
	// Deflection
	16462: "deflection", 16463: "deflection", 16464: "deflection", 16465: "deflection", 16466: "deflection",
	// Improved Overpower
	12963: "improved-overpower", 12964: "improved-overpower",
	// Impale
	16493: "impale", 16494: "impale",
	// Two-Handed Weapon Specialization
	12163: "two-handed-weapon-specialization", 12711: "two-handed-weapon-specialization", 12712: "two-handed-weapon-specialization",
	// Talents that are also the spell they grant
	12294: "mortal-strike", 23881: "bloodthirst",
	// The ids of this client's talent entries (the addon's Sheet.lua, definitionID.spellID), checked
	// against a Spanish-client sheet: the name the client gives each entry, its tree, row and max rank,
	// and the site's talent of the same tree, row and max rank. The client's pair wins over the older
	// table: 12328 is Deseo de la Muerte (Death Wish) and 12292 is Golpes de barrido (Sweeping Strikes).
	12328: "death-wish", 12292: "sweeping-strikes", // Deseo de la Muerte, Golpes de barrido
	// abilityIDs uses the same pair. That is an inference: as with Mortal Strike and Bloodthirst above, the
	// spell a talent grants is taken to have the talent's id; no sheet fixture shows the granted spell.
	12319: "flurry",                 // Aluvión
	12317: "enrage",                 // Enfurecer
	12321: "booming-voice",          // Voz retumbante
	12322: "unbridled-wrath",        // Cólera desbocada
	20500: "improved-berserker-rage", // Ira rabiosa mejorada
	20502: "improved-execute",       // Ejecutar mejorado
	20504: "improved-intercept",     // Intercepción mejorada
	23584: "dual-wield-specialization", // Especialización en doble empuñadura
	12323: "piercing-howl",          // Aullido penetrante
	12289: "improved-hamstring",     // Seccionar mejorado
	1290261: "weaponmaster",         // Maestro de armas
	1310222: "spearing-strike",      // Lanzazo
	12834: "deep-wounds",            // Heridas profundas
	12296: "anger-management",       // Control de furia
	12295: "improved-tactical-mastery", // Maestría táctica mejorada
	12290: "improved-overpower",     // Abrumar mejorado
	12285: "improved-charge",        // Embestir mejorado
	12282: "improved-heroic-strike", // Golpe heroico mejorado
	12286: "improved-rend",          // Desgarrar mejorado
	16487: "blood-craze",            // Manía sangrienta
	1323963: "furious-precision",    // Precisión furiosa
	1323964: "lingering-rage",       // Ira persistente
	1323967: "gore-drinker",         // Bebedor de entrañas
	12962: "iron-will",              // Voluntad férrea
	12298: "shield-specialization",  // Especialización en escudos
	12297: "anticipation",           // Anticipación
	12301: "improved-bloodrage",     // Sanguinario mejorado
	12308: "improved-sunder-armor",  // Hender armadura mejorado
	12311: "improved-shield-bash",   // Azote de escudo mejorado
	12312: "improved-shield-wall",   // Muro de escudo mejorado
	12313: "improved-disarm",        // Desarmar mejorado
	12287: "improved-thunder-clap",  // Trueno mejorado
	12797: "improved-revenge",       // Revancha mejorada
	12792: "defiance",               // Desafío
	12809: "concussion-blow",        // Golpe de concusión
	12862: "improved-slam",          // Embate mejorado
	12975: "last-stand",             // Última posición
	1310315: "improved-whirlwind",   // Torbellino mejorado
	1310316: "master-of-defense",    // Maestro de defensa
	1310317: "vanguard",             // Vanguardia
	1329555: "reckless-swings",      // Golpes temerarios
	23922: "shield-slam",            // Embate con escudo
	29787: "focused-rage",           // Ira enfocada
	16538: "bastion",                // Bastión
	1289682: "bloodthrill",          // Fragor de la sangre
}

// abilityKeyOf returns the rotation key of a spell id.
func abilityKeyOf(spellID int) (string, bool) {
	k, ok := abilityIDs[spellID]
	return k, ok
}

// talentSlugOf returns the talent slug of a spell id.
func talentSlugOf(spellID int) (string, bool) {
	s, ok := talentIDs[spellID]
	return s, ok
}
