package warrior

// Names in the player's language. The model and its reports name the talents and the abilities in English
// (the site's names: "Improved Tactical Mastery", "Heroic Strike"), but a Spanish client shows the sheet's
// names ("Maestría táctica mejorada", "Golpe heroico"). ClientName translates an English name back through
// its spell id: talentIDs and abilityIDs give the ids of a talent slug or of an ability key, and the sheet
// carries the name the client shows for each id. A name with no id in the sheet stays English.

import "github.com/MTEKode/companeroWoW/app/internal/theory"

// ClientName returns the sheet's name of the talent or the ability whose English name is english. ok is
// false when the sheet has no talent or spell with an id of that name (the caller keeps the English name).
func ClientName(english string, sheet theory.Sheet) (string, bool) {
	if english == "" {
		return "", false
	}
	if name, ok := talentSheetName(slugOf(english), sheet); ok {
		return name, true
	}
	for i, n := range abilityNames {
		if n == english {
			return abilitySheetName(abilityKeys[i], sheet)
		}
	}
	return "", false
}

// talentSheetName returns the name of the sheet's talent whose spell id belongs to the talent slug.
func talentSheetName(slug string, sheet theory.Sheet) (string, bool) {
	if slug == "" {
		return "", false
	}
	for _, tab := range sheet.Talents.Tabs {
		for _, tal := range tab.Talents {
			if tal.Name != "" && talentIDs[tal.SpellID] == slug {
				return tal.Name, true
			}
		}
	}
	return "", false
}

// abilitySheetName returns the name of the sheet's spell whose id belongs to the rotation key.
func abilitySheetName(key string, sheet theory.Sheet) (string, bool) {
	for _, sp := range sheet.Spells {
		if sp.Name != "" && abilityIDs[sp.SpellID] == key {
			return sp.Name, true
		}
	}
	return "", false
}
