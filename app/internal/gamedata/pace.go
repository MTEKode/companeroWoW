package gamedata

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// Leveling pace (ritmo_subida): how fast the character gains level, how long the next level
// takes and where the time goes. The XP comes from the session tracker (the last three
// sessions and the current one) and from a rolling window of live samples kept here; the
// split between combat, travel and standing still is estimated from the live flags and from
// the position between samples. Everything is an estimate from what the bridge reports.

const (
	// paceWindow is the rolling window of the pace.
	paceWindow = 30 * time.Minute
	// paceEvery is the least time between two kept samples.
	paceEvery = 5 * time.Second
	// paceMaxGap caps the time one interval adds to the split, so a bridge gap does not count
	// as hours of standing still.
	paceMaxGap = 30 * time.Second
	// paceMoved is the distance in map units (0..1) that counts as travel between two samples.
	paceMoved = 0.002
	// paceMinRate is how much time a rate needs before it is used: shorter windows are noise.
	paceMinRate = 10 * time.Minute
	// paceRecent is how many earlier sessions the pace reads (the last three, skipping open ones).
	paceRecent = 3
)

// PaceSample is one kept reading of the live state.
type PaceSample struct {
	At       time.Time
	Level    int
	XPPct    float64 // 0..100 of the current level
	CumPct   float64 // percentage points of a level gained since the sampler started; never falls
	InCombat bool
	Mounted  bool
	Resting  bool
	MapID    int
	X, Y     float64
}

// PaceSampler keeps the live samples of the last paceWindow (plus one older sample as the
// baseline of the window). Observe is called from the reader's poll goroutine.
type PaceSampler struct {
	now func() time.Time

	mu      sync.Mutex
	samples []PaceSample
	cum     float64
	last    PaceSample
	have    bool
}

// NewPaceSampler returns a sampler that reads time from now (nil means time.Now).
func NewPaceSampler(now func() time.Time) *PaceSampler {
	if now == nil {
		now = time.Now
	}
	return &PaceSampler{now: now}
}

// Observe feeds one valid live state. It is the callback for bridge.Reader.Subscribe.
func (p *PaceSampler) Observe(st bridge.LiveState) {
	if p == nil || st.Level == 0 {
		return
	}
	at := st.ReadAt
	if at.IsZero() {
		at = p.now()
	}
	p.record(PaceSample{
		At:       at,
		Level:    int(st.Level),
		XPPct:    float64(st.XPPct),
		InCombat: st.InCombat,
		Mounted:  st.Mounted,
		Resting:  st.Resting,
		MapID:    int(st.MapID),
		X:        st.X,
		Y:        st.Y,
	})
}

// record keeps a sample when enough time has passed since the last one, and drops samples
// that are older than the window, except the newest of them (the baseline).
func (p *PaceSampler) record(s PaceSample) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.have && s.At.Sub(p.last.At) < paceEvery {
		return
	}
	if p.have {
		p.cum += gainedBetween(p.last, s)
	}
	s.CumPct = p.cum
	p.last, p.have = s, true
	p.samples = append(p.samples, s)
	p.trimLocked(s.At)
}

func (p *PaceSampler) trimLocked(now time.Time) {
	cut := now.Add(-paceWindow)
	i := 0
	for i+1 < len(p.samples) && !p.samples[i+1].At.After(cut) {
		i++
	}
	if i > 0 {
		p.samples = append([]PaceSample(nil), p.samples[i:]...)
	}
}

// Window returns the kept samples of the rolling window ending now, oldest first, with the
// baseline sample before the window start when there is one.
func (p *PaceSampler) Window() []PaceSample {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]PaceSample, len(p.samples))
	copy(out, p.samples)
	return out
}

// gainedBetween is the percentage points of a level that the step from a to b adds. A level
// change counts the rest of the old level, the full levels in between and the new percentage,
// as the session tracker does. A drop (a reading glitch) adds nothing.
func gainedBetween(a, b PaceSample) float64 {
	if b.Level <= 0 || a.Level <= 0 || b.XPPct > 100 || a.XPPct > 100 {
		return 0
	}
	switch {
	case b.Level > a.Level:
		levels := float64(b.Level - a.Level)
		return (100 - a.XPPct) + (levels-1)*100 + b.XPPct
	case b.Level == a.Level && b.XPPct > a.XPPct:
		return b.XPPct - a.XPPct
	}
	return 0
}

// PaceWindow is what the samples of one window say: the XP gained, the time covered and the
// split of that time.
type PaceWindow struct {
	Start, End time.Time
	GainedPct  float64
	Combat     time.Duration
	Travel     time.Duration
	Idle       time.Duration
	Samples    int
}

// Duration is the time the window covers.
func (w PaceWindow) Duration() time.Duration { return w.End.Sub(w.Start) }

// paceFromSamples computes the window that ends at now from the samples (oldest first). The
// baseline is the newest sample at or before the window start, or the oldest sample when all
// of them are inside the window.
func paceFromSamples(samples []PaceSample, now time.Time) PaceWindow {
	var w PaceWindow
	start := now.Add(-paceWindow)
	// The window ends at the newest sample not after now.
	end := -1
	for i, s := range samples {
		if !s.At.After(now) {
			end = i
		}
	}
	if end < 0 {
		return w
	}
	// The baseline is the newest sample at or before the window start (or the first one).
	base := 0
	for i := 0; i <= end && !samples[i].At.After(start); i++ {
		base = i
	}
	inWin := samples[base : end+1]
	w.Start, w.End = inWin[0].At, inWin[len(inWin)-1].At
	w.GainedPct = inWin[len(inWin)-1].CumPct - inWin[0].CumPct
	w.Samples = len(inWin)
	for i := 1; i < len(inWin); i++ {
		prev, cur := inWin[i-1], inWin[i]
		dt := cur.At.Sub(prev.At)
		if dt <= 0 {
			continue
		}
		if dt > paceMaxGap {
			dt = paceMaxGap
		}
		switch {
		case cur.InCombat:
			w.Combat += dt
		case cur.Mounted || moved(prev, cur):
			w.Travel += dt
		default:
			w.Idle += dt
		}
	}
	return w
}

// moved reports whether the character changed position between two samples on the same map.
func moved(a, b PaceSample) bool {
	if a.MapID != b.MapID {
		return true
	}
	return math.Hypot(a.X-b.X, a.Y-b.Y) > paceMoved
}

// sessionRate is the percentage points per hour of a session that lasted at least paceMinRate.
type sessionRate struct {
	Start    time.Time
	Hours    float64
	Pct      float64
	PctHour  float64
	Open     bool
	Duration time.Duration
}

func rateOf(start, end time.Time, gained float64) (sessionRate, bool) {
	d := end.Sub(start)
	if d < paceMinRate {
		return sessionRate{}, false
	}
	h := d.Hours()
	return sessionRate{Start: start, Hours: h, Pct: gained, PctHour: gained / h, Duration: d}, true
}

// LevelingPace answers ritmo_subida. target, when above the current level, adds the estimated
// time to reach it. The fields are present when the data allows them; the texto field is a
// short Spanish summary for the chat.
func (g *Service) LevelingPace(target int) (any, error) {
	now := time.Now()
	live, liveOK := g.Live()
	level, xpPct, xpMax, source, at := g.paceCharacter(live, liveOK)
	if level == 0 {
		return unavailable(HintSync), nil
	}
	in := paceInput{
		Now: now, Level: level, XPPct: xpPct, XPMax: xpMax, Source: source, At: at, Target: target,
	}
	// The running session comes from the tracker; the store's open rows are the same session
	// and are left out of the finished ones.
	if g.sessions != nil {
		if se, ok := g.sessions.Current(); ok {
			in.Current = &se
		}
	}
	if g.store != nil {
		list, err := g.store.RecentSessions(paceRecent + 4)
		if err != nil {
			return nil, fmt.Errorf("leer las sesiones: %w", err)
		}
		for _, se := range list {
			if se.Open || se.EndedAt.IsZero() {
				continue
			}
			in.Recent = append(in.Recent, se)
		}
	}
	if g.pace != nil {
		in.Samples = g.pace.Window()
	}
	return buildPace(in), nil
}

// paceInput is everything the pace is computed from, as plain values so that the arithmetic
// can be tested without the bridge or the store.
type paceInput struct {
	Now     time.Time
	Level   int
	XPPct   float64 // 0..100 of the current level
	XPMax   float64 // XP needed for the current level; 0 when unknown
	Source  string
	At      time.Time
	Target  int             // level to estimate the time to; 0 for none
	Current *store.Session  // the running session, nil when none
	Recent  []store.Session // finished sessions, newest first
	Samples []PaceSample    // live samples, oldest first
}

// buildPace computes the answer of ritmo_subida from in.
func buildPace(in paceInput) map[string]any {
	now := in.Now
	xpPct, xpMax, level := in.XPPct, in.XPMax, in.Level
	fields := map[string]any{"nivel": level, "xpPct": round1(xpPct)}
	notes := []string{}

	var current *sessionRate
	if in.Current != nil {
		se := *in.Current
		if r, ok := rateOf(se.StartedAt, now, se.XPGainedPct); ok {
			current = &r
		}
		fields["sesionActual"] = sessionPaceView(se.StartedAt, now, se.XPGainedPct, xpMax, true)
	}
	var recent []sessionRate
	var views []map[string]any
	for _, se := range in.Recent {
		if len(recent) == paceRecent {
			break
		}
		if r, ok := rateOf(se.StartedAt, se.EndedAt, se.XPGainedPct); ok {
			recent = append(recent, r)
			views = append(views, sessionPaceView(se.StartedAt, se.EndedAt, se.XPGainedPct, xpMax, false))
		}
	}
	if len(views) > 0 {
		fields["ultimasSesiones"] = views
	}

	// Rolling window from the live samples.
	win := paceFromSamples(in.Samples, now)
	rolling := win.Duration() >= paceMinRate
	if win.Duration() > 0 {
		m := map[string]any{"minutos": round1(win.Duration().Minutes()), "pct": round1(win.GainedPct)}
		if xpMax > 0 {
			m["xp"] = math.Round(win.GainedPct / 100 * xpMax)
		}
		if rolling {
			m["pctHora"] = round1(win.GainedPct / win.Duration().Hours())
		}
		fields["ultimos30min"] = m
	}

	// The rate that the ETA uses: the rolling window, then the running session, then the last
	// sessions together.
	var rate float64
	var rateSource string
	switch {
	case rolling && win.GainedPct > 0:
		rate, rateSource = win.GainedPct/win.Duration().Hours(), "ultimos30min"
	case current != nil && current.PctHour > 0:
		rate, rateSource = current.PctHour, "sesionActual"
	case len(recent) > 0:
		var pct, hours float64
		for _, r := range recent {
			pct += r.Pct
			hours += r.Hours
		}
		if hours > 0 && pct > 0 {
			rate, rateSource = pct/hours, "ultimasSesiones"
		}
	}

	subida := map[string]any{"faltaPct": round1(100 - xpPct)}
	if xpMax > 0 {
		subida["faltaXP"] = math.Round((100 - xpPct) / 100 * xpMax)
	}
	if rate > 0 {
		subida["ritmoPctHora"] = round1(rate)
		subida["ritmoFuente"] = rateSource
		etaH := (100 - xpPct) / rate
		subida["etaSiguienteNivel"] = durationText(etaH)
		subida["etaSiguienteMin"] = math.Round(etaH * 60)
		if in.Target > level {
			if mins, ok := LevelingETA(level, xpPct, in.Target, rate); ok {
				subida["nivelObjetivo"] = in.Target
				subida["etaNivelObjetivo"] = durationText(mins / 60)
				subida["etaNivelObjetivoMin"] = math.Round(mins)
				notes = append(notes, "El tiempo al nivel objetivo es una estimación: supone que cada nivel cuesta lo mismo en porcentaje que el actual.")
			}
		}
	} else {
		notes = append(notes, "Sin progreso reciente para calcular el ritmo: juega un rato o revisa el puente.")
	}
	fields["subida"] = subida
	fields["xpHoraTexto"] = paceRateText(rate, rateSource)

	// The split of the rolling window: where the time goes.
	if total := win.Combat + win.Travel + win.Idle; total > 0 {
		fields["reparto30min"] = map[string]any{
			"combatePct": pctOfDur(win.Combat, total),
			"viajePct":   pctOfDur(win.Travel, total),
			"paradoPct":  pctOfDur(win.Idle, total),
		}
		notes = append(notes, "El reparto es una estimación con las banderas en vivo (combate, montura, posición), no una medida exacta.")
	}
	if xpMax == 0 {
		notes = append(notes, "No conozco el XP necesario del nivel: las cifras van en porcentaje del nivel.")
	}
	if len(notes) > 0 {
		fields["notas"] = notes
	}
	fields["texto"] = paceText(level, xpPct, subida, win, rateSource, rate)
	return available(in.Source, in.At, fields)
}

// LevelingETA is the time to reach level target at the current pace, in minutes. ok is false
// when there is no rate or target is not above the level. It assumes that every level in
// between takes as many percentage points as the current one, so the caller labels it as an
// estimate.
func LevelingETA(level int, xpPct float64, target int, pctPerHour float64) (minutes float64, ok bool) {
	if pctPerHour <= 0 || target <= level {
		return 0, false
	}
	pts := (100 - xpPct) + float64(target-level-1)*100
	return pts / pctPerHour * 60, true
}

// paceCharacter returns the level, the XP percentage, the XP needed for the level (0 when
// unknown), the source and the time of the character's pace. Live data wins; the
// SavedVariables character gives the XP needed when its level is the current one.
func (g *Service) paceCharacter(live bridge.LiveState, liveOK bool) (level int, xpPct float64, xpMax float64, source string, at time.Time) {
	ch, _, ok := g.activeCharacter()
	char := map[string]any{}
	if ok {
		char = asMap(ch.data["character"])
	}
	svLevel := int(numberOf(char["level"]))
	svXPMax := numberOf(char["xpMax"])
	if liveOK && live.Level > 0 {
		level = int(live.Level)
		xpPct = float64(live.XPPct)
		source, at = sourceLive, live.ReadAt
		if svLevel == level && svXPMax > 0 {
			xpMax = svXPMax
		}
		return
	}
	if ok && svLevel > 0 {
		level = svLevel
		xpMax = svXPMax
		if xpMax > 0 {
			xpPct = numberOf(char["xp"]) / xpMax * 100
		}
		source, at = sourceSV, ch.updatedAt
	}
	return
}

// sessionPaceView is one session as the pace shows it: percentage points, XP when the XP
// needed is known, and the hours.
func sessionPaceView(start, end time.Time, gained, xpMax float64, open bool) map[string]any {
	h := end.Sub(start).Hours()
	m := map[string]any{"pct": round1(gained), "horas": round2(h), "inicio": start.Format(time.RFC3339)}
	if h > 0 {
		m["pctHora"] = round1(gained / h)
		if xpMax > 0 {
			m["xpHora"] = math.Round(gained / 100 * xpMax / h)
		}
	}
	if xpMax > 0 {
		m["xp"] = math.Round(gained / 100 * xpMax)
	}
	if open {
		m["enCurso"] = true
	}
	return m
}

func pctOfDur(part, total time.Duration) float64 {
	if total <= 0 {
		return 0
	}
	return round1(float64(part) / float64(total) * 100)
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// durationText writes hours as "1 h 20 min" or "45 min".
func durationText(hours float64) string {
	mins := int(math.Round(hours * 60))
	if mins < 1 {
		return "menos de 1 min"
	}
	if mins < 60 {
		return fmt.Sprintf("%d min", mins)
	}
	return fmt.Sprintf("%d h %02d min", mins/60, mins%60)
}

func paceRateText(rate float64, source string) string {
	if rate <= 0 {
		return "sin ritmo reciente"
	}
	return fmt.Sprintf("%.1f %% de nivel por hora (%s)", rate, rateSourceText(source))
}

func rateSourceText(source string) string {
	switch source {
	case "ultimos30min":
		return "últimos 30 min"
	case "sesionActual":
		return "sesión actual"
	case "ultimasSesiones":
		return "últimas sesiones"
	}
	return source
}

// paceText is the short Spanish summary of the pace for the chat.
func paceText(level int, xpPct float64, subida map[string]any, win PaceWindow, source string, rate float64) string {
	line := fmt.Sprintf("Nivel %d, %.0f %% del nivel.", level, xpPct)
	if rate > 0 {
		line += fmt.Sprintf(" Ritmo: %.1f %% por hora (%s).", rate, rateSourceText(source))
		if eta, ok := subida["etaSiguienteNivel"].(string); ok {
			line += " Siguiente nivel en unos " + eta + "."
		}
	} else {
		line += " Sin ritmo reciente para estimar el siguiente nivel."
	}
	if total := win.Combat + win.Travel + win.Idle; total > 0 {
		line += fmt.Sprintf(" Últimos 30 min: combate %.0f %%, viaje %.0f %%, parado %.0f %%.",
			pctOfDur(win.Combat, total), pctOfDur(win.Travel, total), pctOfDur(win.Idle, total))
	}
	return line
}

// paceSummary is the compact pace that the player context carries in every prompt.
func (g *Service) paceSummary() map[string]any {
	v, err := g.LevelingPace(0)
	if err != nil {
		return nil
	}
	m := asMap(v)
	if !truthy(m["available"]) {
		return nil
	}
	out := map[string]any{"xpHoraTexto": m["xpHoraTexto"]}
	if s := asMap(m["subida"]); s != nil {
		if eta, ok := s["etaSiguienteNivel"]; ok {
			out["siguienteNivel"] = eta
		}
	}
	if r := asMap(m["reparto30min"]); r != nil {
		out["reparto30min"] = fmt.Sprintf("combate %v %%, viaje %v %%, parado %v %%", r["combatePct"], r["viajePct"], r["paradoPct"])
	}
	return out
}

// SetPace connects the live sample keeper that feeds the leveling pace. Without it the pace
// uses only the session tracker and the saved sessions.
func (g *Service) SetPace(p *PaceSampler) {
	g.pace = p
}
