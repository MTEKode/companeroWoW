package gamedata

import (
	"fmt"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/session"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

const (
	hintSession = "Aún no hay sesiones guardadas. Con el puente conectado, la sesión empieza sola y se guarda al cerrar la app."
	// sessionListLimit is how many names a session line lists before "y N más".
	sessionListLimit = 3
	// sessionZoneLimit is the same for the zones line.
	sessionZoneLimit = 5
)

// SetSessions connects the running session tracker. Without it, SessionSummary reads only
// the last saved session.
func (g *Service) SetSessions(t *session.Tracker) {
	g.sessions = t
}

// SessionSummary describes the session in progress, or the last saved one when none is
// running: its times, XP, money, quests, zones and deaths. The field "texto" is a short
// Spanish summary for the chat.
func (g *Service) SessionSummary() (any, error) {
	var se store.Session
	active := false
	if g.sessions != nil {
		se, active = g.sessions.Current()
	}
	if !active {
		if g.store == nil {
			return unavailable(hintSession), nil
		}
		last, err := g.store.LastSession()
		if err != nil {
			return nil, fmt.Errorf("leer la última sesión: %w", err)
		}
		if last == nil {
			return unavailable(hintSession), nil
		}
		se = *last
	}

	end := se.EndedAt
	if active {
		end = time.Now()
	}
	minutes := 0
	if !end.IsZero() && end.After(se.StartedAt) {
		minutes = int(end.Sub(se.StartedAt).Minutes())
	}
	delta := se.MoneyEnd - se.MoneyStart
	completed := g.questTitles(se.QuestsCompleted)
	turned := g.questTitles(se.QuestsTurnedIn)
	zones := g.zoneLabels(se.ZoneMapIDs)

	fields := map[string]any{
		"enCurso":             active,
		"inicio":              se.StartedAt.Format(time.RFC3339),
		"duracionMin":         minutes,
		"nivelInicio":         se.StartLevel,
		"nivelFin":            se.EndLevel,
		"nivelesSubidos":      se.LevelsGained,
		"xpGanadoPct":         round1(se.XPGainedPct),
		"dineroCambio":        delta,
		"dineroCambioTexto":   signedMoneyText(delta),
		"misionesCompletadas": completed,
		"misionesEntregadas":  turned,
		"zonas":               zones,
		"muertes":             se.Deaths,
	}
	if !active && !se.EndedAt.IsZero() {
		fields["fin"] = se.EndedAt.Format(time.RFC3339)
	}
	fields["texto"] = sessionText(sessionLines{
		Active:       active,
		Start:        se.StartedAt,
		Minutes:      minutes,
		StartLevel:   se.StartLevel,
		EndLevel:     se.EndLevel,
		LevelsGained: se.LevelsGained,
		XPGainedPct:  se.XPGainedPct,
		MoneyDelta:   delta,
		Completed:    titlesOf(completed),
		TurnedIn:     titlesOf(turned),
		Zones:        zones,
		Deaths:       se.Deaths,
	})
	return available("sesion", se.StartedAt, fields), nil
}

// questTitles pairs quest ids with their titles: the knowledge store first, then the
// Questie export of the active character (it names the quests of the log).
func (g *Service) questTitles(ids []int64) []map[string]any {
	var qx questieExport
	if ch, _, ok := g.activeCharacter(); ok {
		qx = readQuestie(ch.data)
	}
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		title := ""
		if g.store != nil {
			if k, err := g.store.GetQuest(id); err == nil && k != nil {
				title = k.Title
			}
		}
		if title == "" {
			if q, ok := qx.quest(id); ok {
				title = q.Name
			}
		}
		if title == "" {
			title = fmt.Sprintf("misión %d", id)
		}
		out = append(out, map[string]any{"questId": id, "titulo": title})
	}
	return out
}

// zoneLabels names the map ids of the session in the order they were visited.
func (g *Service) zoneLabels(ids []int) []string {
	_, sv, _ := g.activeCharacter()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		name, err := g.zoneName(id, sv)
		if err != nil || name == "" {
			name = fmt.Sprintf("mapa %d", id)
		}
		out = append(out, name)
	}
	return out
}

// titlesOf reads the titles of a questTitles list.
func titlesOf(list []map[string]any) []string {
	out := make([]string, 0, len(list))
	for _, m := range list {
		out = append(out, str(m["titulo"]))
	}
	return out
}

// sessionLines is what the summary text needs: plain values, so the text is testable.
type sessionLines struct {
	Active       bool
	Start        time.Time
	Minutes      int
	StartLevel   int
	EndLevel     int
	LevelsGained int
	XPGainedPct  float64
	MoneyDelta   int64
	Completed    []string
	TurnedIn     []string
	Zones        []string
	Deaths       int
}

// sessionText writes the short Spanish summary, one line per topic.
func sessionText(s sessionLines) string {
	clock := s.Start.Local().Format("15:04")
	var lines []string
	if s.Active {
		lines = append(lines, fmt.Sprintf("Sesión en curso desde las %s (%d min).", clock, s.Minutes))
	} else {
		end := s.Start.Add(time.Duration(s.Minutes) * time.Minute).Local().Format("15:04")
		lines = append(lines, fmt.Sprintf("Sesión de %s a %s (%d min).", clock, end, s.Minutes))
	}

	var xp []string
	if s.LevelsGained > 0 {
		xp = append(xp, fmt.Sprintf("+%d %s (nivel %d → %d)", s.LevelsGained, plural(s.LevelsGained, "nivel", "niveles"), s.StartLevel, s.EndLevel))
	}
	if pct := int(s.XPGainedPct + 0.5); pct > 0 {
		xp = append(xp, fmt.Sprintf("+%d %% de nivel", pct))
	}
	if len(xp) == 0 {
		lines = append(lines, "XP: sin cambios.")
	} else {
		lines = append(lines, "XP: "+strings.Join(xp, " y ")+".")
	}

	switch {
	case s.MoneyDelta > 0:
		lines = append(lines, "Oro: +"+moneyText(s.MoneyDelta)+".")
	case s.MoneyDelta < 0:
		lines = append(lines, "Oro: -"+moneyText(-s.MoneyDelta)+".")
	default:
		lines = append(lines, "Oro: sin cambios.")
	}

	if len(s.Completed) == 0 && len(s.TurnedIn) == 0 {
		lines = append(lines, "Misiones: ninguna.")
	} else {
		var parts []string
		if len(s.Completed) > 0 {
			parts = append(parts, fmt.Sprintf("completadas %d (%s)", len(s.Completed), listText(s.Completed, sessionListLimit)))
		}
		if len(s.TurnedIn) > 0 {
			parts = append(parts, fmt.Sprintf("entregadas %d (%s)", len(s.TurnedIn), listText(s.TurnedIn, sessionListLimit)))
		}
		lines = append(lines, "Misiones: "+strings.Join(parts, ". ")+".")
	}

	if len(s.Zones) > 0 {
		lines = append(lines, "Zonas: "+listText(s.Zones, sessionZoneLimit)+".")
	}
	if s.Deaths > 0 {
		lines = append(lines, fmt.Sprintf("Muertes: %d.", s.Deaths))
	} else {
		lines = append(lines, "Muertes: ninguna.")
	}
	return strings.Join(lines, "\n")
}

// listText joins up to limit names with commas and "y N más" for the rest.
func listText(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s y %d más", strings.Join(items[:limit], ", "), len(items)-limit)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// signedMoneyText is a money change with its sign: "+1 oro" or "-20 plata".
func signedMoneyText(copper int64) string {
	switch {
	case copper > 0:
		return "+" + moneyText(copper)
	case copper < 0:
		return "-" + moneyText(-copper)
	}
	return "0 cobre"
}
