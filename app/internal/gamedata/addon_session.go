package gamedata

import (
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// The session the addon keeps (on-demand contract). In the on-demand mode the app keeps no
// session of its own: the addon's session block, sent on channel message 14 and saved in
// characters[key].session, is what ritmo_subida, resumen_sesion and contexto_jugador read.
//
// Shape (see the contract): startedAt, updatedAt, playedSec, level {start, now}, xpGained,
// xpPerHour, money {start, now}, questsCompleted [ids], questsAccepted [ids], kills, deaths,
// zones [map ids], timeSplit {...}, xpSamples [{t, xp, level}] (every 60 s, last 60) and
// previous [{startedAt, endedAt, playedSec, xpGained, levelStart, levelEnd, moneyDelta,
// questsCompleted (a count), kills, deaths}] (last five).
//
// xpGained is XP, not percent of a level. The app's session types hold percent of a level,
// so the addon's XP is turned into percent with the XP needed for the current level (the
// only maximum known). For sessions that spanned several levels that is an approximation,
// and the answers say "% de nivel" as before.

// addonSessionMsg is the session block of the addon: the live message 14 when it is newer
// than the saved one, otherwise characters[key].session. ok is false when neither exists.
func (g *Service) addonSessionMsg() (map[string]any, time.Time, bool) {
	ch, _, chOK := g.activeCharacter()
	var savedAt time.Time
	var saved map[string]any
	if chOK {
		saved = asMap(ch.data["session"])
		savedAt = ch.updatedAt
	}
	if live := g.LiveChannel(); live != nil {
		if it, ok := live.Data.Get(bridge.MsgSession); ok {
			if m := asMap(it.Value); len(m) > 0 && (saved == nil || it.At.After(savedAt)) {
				return m, it.At, true
			}
		}
	}
	if len(saved) > 0 {
		return saved, savedAt, true
	}
	return nil, time.Time{}, false
}

// addonXPMax is the XP needed for the current level, from the sheet or the saved character. It
// is 0 when unknown.
func (g *Service) addonXPMax() float64 {
	live, liveOK := g.Live()
	_, _, xpMax, _, _ := g.paceCharacter(live, liveOK)
	return xpMax
}

// addonSessions turns the addon's session block into the session types the answers use: the
// current session (open) and the previous ones (newest first), plus the rolling XP samples.
// ok is false when the addon has not sent a session.
func (g *Service) addonSessions() (current store.Session, previous []store.Session, samples []PaceSample, ok bool) {
	m, at, found := g.addonSessionMsg()
	if !found {
		return store.Session{}, nil, nil, false
	}
	xpMax := g.addonXPMax()
	current = addonCurrent(m, xpMax, at)
	for _, p := range asList(m["previous"]) {
		if pm := asMap(p); pm != nil {
			previous = append(previous, addonPrevious(pm, xpMax))
		}
	}
	samples = addonSamples(asList(m["xpSamples"]), xpMax)
	return current, previous, samples, true
}

// addonCurrent is the running session of the addon block.
func addonCurrent(m map[string]any, xpMax float64, at time.Time) store.Session {
	start := unixTime(numberOf(m["startedAt"]))
	levels := asMap(m["level"])
	money := asMap(m["money"])
	se := store.Session{
		StartedAt:       start,
		LastDataAt:      at,
		Open:            true,
		StartLevel:      int(numberOf(levels["start"])),
		EndLevel:        int(numberOf(levels["now"])),
		XPGainedPct:     percentOfLevel(numberOf(m["xpGained"]), xpMax),
		MoneyStart:      int64(numberOf(money["start"])),
		MoneyEnd:        int64(numberOf(money["now"])),
		QuestsCompleted: idsOf(m["questsCompleted"]),
		ZoneMapIDs:      intsOf(m["zones"]),
		Deaths:          int(numberOf(m["deaths"])),
	}
	se.LevelsGained = levelsGained(se.StartLevel, se.EndLevel)
	if updated := unixTime(numberOf(m["updatedAt"])); !updated.IsZero() {
		se.LastDataAt = updated
	}
	return se
}

// addonPrevious is one of the addon's last five sessions.
func addonPrevious(p map[string]any, xpMax float64) store.Session {
	levelStart := int(numberOf(p["levelStart"]))
	levelEnd := int(numberOf(p["levelEnd"]))
	money := int64(numberOf(p["moneyDelta"]))
	return store.Session{
		StartedAt:    unixTime(numberOf(p["startedAt"])),
		EndedAt:      unixTime(numberOf(p["endedAt"])),
		StartLevel:   levelStart,
		EndLevel:     levelEnd,
		LevelsGained: levelsGained(levelStart, levelEnd),
		XPGainedPct:  percentOfLevel(numberOf(p["xpGained"]), xpMax),
		MoneyStart:   0,
		MoneyEnd:     money,
		Deaths:       int(numberOf(p["deaths"])),
	}
}

// addonSamples turns the addon's XP samples into the rolling window of the pace. The addon sends
// xp as the CUMULATIVE XP gained in the session (its xpGained at time t, not the raw UnitXP), so
// the percent of a level is xp over the XP needed for the current level. It is 0 when that
// maximum is unknown. The samples keep the order they were sent in.
func addonSamples(list []any, xpMax float64) []PaceSample {
	if xpMax <= 0 {
		return nil
	}
	var out []PaceSample
	for _, v := range list {
		m := asMap(v)
		if m == nil {
			continue
		}
		at := unixTime(numberOf(m["t"]))
		if at.IsZero() {
			continue
		}
		out = append(out, PaceSample{
			At:     at,
			Level:  int(numberOf(m["level"])),
			CumPct: numberOf(m["xp"]) / xpMax * 100,
		})
	}
	return out
}

// percentOfLevel is XP as percent of the XP needed for the level; 0 when the maximum is unknown.
func percentOfLevel(xp, xpMax float64) float64 {
	if xpMax <= 0 {
		return 0
	}
	return xp / xpMax * 100
}

func levelsGained(start, end int) int {
	if end > start {
		return end - start
	}
	return 0
}

// idsOf reads a list of quest ids.
func idsOf(v any) []int64 {
	var out []int64
	for _, x := range asList(v) {
		if id := int64(numberOf(x)); id > 0 {
			out = append(out, id)
		}
	}
	return out
}

// intsOf reads a list of map ids.
func intsOf(v any) []int {
	var out []int
	for _, x := range asList(v) {
		if id := int(numberOf(x)); id > 0 {
			out = append(out, id)
		}
	}
	return out
}

// asList returns v as a list; a table with keys 1..n (already a list) or anything else gives nil.
func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// sessionFromAddon is the session the summary and the pace read in the on-demand mode: the
// running session of the addon, when it has sent one.
func (g *Service) sessionFromAddon() (store.Session, bool) {
	cur, _, _, ok := g.addonSessions()
	return cur, ok
}
