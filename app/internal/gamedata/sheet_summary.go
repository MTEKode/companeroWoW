package gamedata

import (
	"time"
)

// What the context says about the character sheet and about the age of the data. The sheet can come from
// three places (the store, the SavedVariables table and the live channel); the summary names the one that
// holds it, and the age is the newest of the sources (see newestData).

// Sources of the sheet, as the context names them.
const (
	sheetFromStore = "tienda"
	sheetFromSV    = "savedvariables"
	sheetFromLive  = "en vivo"
)

// sheetSummary is the character sheet in a few fields: class, race, level, the talent points spent and
// where the sheet came from. Without a sheet in any source it says so (disponible false).
func (g *Service) sheetSummary(ch charEntry) map[string]any {
	sheet := asMap(ch.data["sheet"])
	if sheet == nil {
		return map[string]any{"disponible": false}
	}
	char := asMap(ch.data["character"])
	out := map[string]any{"disponible": true, "fuente": g.sheetSource(ch)}
	class := str(sheet["class"])
	if class == "" && char != nil {
		class = str(char["classFile"])
	}
	if class != "" {
		out["clase"] = class
	}
	if race := str(sheet["race"]); race != "" {
		out["raza"] = race
	}
	level := numberOf(sheet["level"])
	if level <= 0 && char != nil {
		level = numberOf(char["level"])
	}
	if level > 0 {
		out["nivel"] = level
	}
	if talents := asMap(sheet["talents"]); talents != nil {
		points := 0.0
		for _, tab := range listOf(talents["tabs"]) {
			points += numberOf(asMap(tab)["points"])
		}
		out["puntosTalento"] = points
	}
	at := unixTime(numberOf(sheet["exportedAt"]))
	if at.IsZero() {
		at = ch.updatedAt
	}
	if !at.IsZero() {
		out["haceSeg"] = int64(time.Since(at).Seconds())
	}
	return out
}

// sheetSource names where the sheet of the character comes from: the store (the channel and the outbox
// of schema 2), the SavedVariables table, or the live channel alone.
func (g *Service) sheetSource(ch charEntry) string {
	if st, ok := g.storedState(ch.key); ok {
		if _, has := st.Sections["sheet"]; has {
			return sheetFromStore
		}
	}
	if sv := g.savedVars(); sv != nil {
		chars := asMap(sv.Raw["characters"])
		if asMap(asMap(chars[ch.key])["sheet"]) != nil {
			return sheetFromSV
		}
	}
	return sheetFromLive
}

// newestData returns the time of the newest data the reads hold and its source: the SavedVariables file
// (its modification time), the character state of the store and the live channel. The zero time means
// none of them has data.
func (g *Service) newestData() (time.Time, string) {
	var at time.Time
	from := ""
	consider := func(t time.Time, name string) {
		if !t.IsZero() && t.After(at) {
			at, from = t, name
		}
	}
	if sv := g.savedVars(); sv != nil {
		consider(sv.ModTime, sheetFromSV)
	}
	if ch, _, ok := g.activeCharacter(); ok {
		if st, stored := g.storedState(ch.key); stored {
			consider(st.Latest, sheetFromStore)
		}
	}
	if live, ok := g.Live(); ok {
		consider(live.ReadAt, sheetFromLive)
	}
	return at, from
}
