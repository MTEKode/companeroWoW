package gamedata

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

var t0 = time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestGainedBetweenCountsSameLevelAndLevelUps(t *testing.T) {
	cases := []struct {
		name string
		a, b PaceSample
		want float64
	}{
		{"same level", PaceSample{Level: 10, XPPct: 40}, PaceSample{Level: 10, XPPct: 45}, 5},
		{"one level up", PaceSample{Level: 10, XPPct: 90}, PaceSample{Level: 11, XPPct: 5}, 15},
		{"two levels up", PaceSample{Level: 10, XPPct: 90}, PaceSample{Level: 12, XPPct: 5}, 115},
		{"glitch drop adds nothing", PaceSample{Level: 10, XPPct: 50}, PaceSample{Level: 10, XPPct: 20}, 0},
		{"invalid percent adds nothing", PaceSample{Level: 10, XPPct: 50}, PaceSample{Level: 10, XPPct: 255}, 0},
	}
	for _, c := range cases {
		if got := gainedBetween(c.a, c.b); !near(got, c.want, 1e-9) {
			t.Errorf("%s: gained = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSamplerKeepsOneSamplePerInterval(t *testing.T) {
	now := t0
	p := NewPaceSampler(func() time.Time { return now })
	for i := 0; i < 12; i++ { // one state per second for 12 seconds
		p.Observe(bridge.LiveState{Level: 20, XPPct: uint8(10 + i), ReadAt: t0.Add(time.Duration(i) * time.Second)})
	}
	got := p.Window()
	// Samples are kept at 0s, 5s and 10s only.
	if len(got) != 3 {
		t.Fatalf("kept %d samples, want 3 (one per 5 s)", len(got))
	}
	// 10→10 at t0, 14 at 5s, 20 at 10s: the level-20 steps add 4 and 6 points.
	if !near(got[2].CumPct, 10, 1e-9) {
		t.Errorf("cumulative = %v, want 10", got[2].CumPct)
	}
}

func TestSamplerIgnoresStatesWithoutLevel(t *testing.T) {
	p := NewPaceSampler(nil)
	p.Observe(bridge.LiveState{Level: 0, ReadAt: t0})
	if n := len(p.Window()); n != 0 {
		t.Errorf("kept %d samples for a state without level", n)
	}
	var nilSampler *PaceSampler
	nilSampler.Observe(bridge.LiveState{Level: 5, ReadAt: t0}) // must not panic
}

// samplesEvery builds one sample every 5 s from start for n samples, with the flags of flag(i).
func samplesEvery(start time.Time, n int, perStep float64, flag func(i int) PaceSample) []PaceSample {
	var out []PaceSample
	cum := 0.0
	for i := 0; i < n; i++ {
		s := flag(i)
		s.At = start.Add(time.Duration(i) * paceEvery)
		s.Level = 20
		s.CumPct = cum
		cum += perStep
		out = append(out, s)
	}
	return out
}

func TestRollingWindowGainAndTimeSplit(t *testing.T) {
	// 60 minutes of samples. The first 20 min: combat. The next 20: mounted travel. The last 20:
	// standing still (not resting, not moving).
	const total = 60 * 60 / 5
	samples := samplesEvery(t0, total, 0.01, func(i int) PaceSample {
		s := PaceSample{X: 0.5, Y: 0.5, MapID: 85}
		switch {
		case i < 20*60/5:
			s.InCombat = true
		case i < 40*60/5:
			s.Mounted = true
			s.X = 0.5 + float64(i)*0.001 // moving
		default:
		}
		return s
	})
	now := samples[len(samples)-1].At
	w := paceFromSamples(samples, now)
	if !near(w.Duration().Minutes(), 30, 0.2) {
		t.Fatalf("window = %v, want 30 min", w.Duration())
	}
	// 0.01 points per 5 s over 30 min is 360 steps: 3.6 points.
	if !near(w.GainedPct, 3.6, 0.05) {
		t.Errorf("gained = %v, want about 3.6", w.GainedPct)
	}
	total2 := w.Combat + w.Travel + w.Idle
	if total2 == 0 {
		t.Fatal("no split")
	}
	// The last 30 min: 10 min combat is not in it (the combat block ended at 20 min, the window starts at
	// 30 min), so the split is travel then idle.
	if w.Combat != 0 {
		t.Errorf("combat = %v, want 0 in the last 30 min", w.Combat)
	}
	if !near(w.Travel.Minutes(), 10, 0.2) || !near(w.Idle.Minutes(), 20, 0.2) {
		t.Errorf("travel %v, idle %v; want about 10 min travel and 20 min idle", w.Travel, w.Idle)
	}
}

func TestRollingWindowUsesBaselineOutsideWindow(t *testing.T) {
	samples := samplesEvery(t0, 720, 1, func(int) PaceSample { return PaceSample{} }) // 60 minutes
	now := samples[len(samples)-1].At
	w := paceFromSamples(samples, now)
	// The baseline is the newest sample at or before now-30min, so the gain is 30 min of steps.
	if !near(w.GainedPct, 360, 1e-6) {
		t.Errorf("gained = %v, want 360 (one step per 5 s over 30 min)", w.GainedPct)
	}
	if paceFromSamples(nil, now).Duration() != 0 {
		t.Error("no samples must give an empty window")
	}
}

func TestLevelingETAForTargetLevel(t *testing.T) {
	// Level 10 at 50 %, 25 points an hour. To reach 12: 50 + 100 = 150 points, six hours.
	mins, ok := LevelingETA(10, 50, 12, 25)
	if !ok || !near(mins, 360, 1e-6) {
		t.Fatalf("eta = %v (%v), want 360 min", mins, ok)
	}
	if _, ok := LevelingETA(10, 50, 10, 25); ok {
		t.Error("a target that is not above the level has no eta")
	}
	if _, ok := LevelingETA(10, 50, 12, 0); ok {
		t.Error("no rate has no eta")
	}
}

func TestBuildPaceETAFromCurrentSession(t *testing.T) {
	now := t0.Add(2 * time.Hour)
	cur := store.Session{StartedAt: now.Add(-1 * time.Hour), XPGainedPct: 30, Open: true}
	out := buildPace(paceInput{
		Now: now, Level: 38, XPPct: 50, XPMax: 98000, Source: "live", At: now,
		Current: &cur,
	})
	subida := asMap(out["subida"])
	if subida == nil {
		t.Fatalf("no subida in %v", out)
	}
	// 30 points an hour: 50 points left take 100 minutes.
	if got := numberOf(subida["etaSiguienteMin"]); !near(got, 100, 0.5) {
		t.Errorf("eta = %v min, want 100", got)
	}
	if subida["ritmoFuente"] != "sesionActual" {
		t.Errorf("rate source = %v", subida["ritmoFuente"])
	}
	if got := numberOf(subida["faltaXP"]); !near(got, 49000, 1) {
		t.Errorf("faltaXP = %v, want 49000", got)
	}
	if txt, _ := out["texto"].(string); !strings.Contains(txt, "1 h 40 min") {
		t.Errorf("texto = %q, want the eta", txt)
	}
	cs := asMap(out["sesionActual"])
	if got := numberOf(cs["xp"]); !near(got, 29400, 1) {
		t.Errorf("session xp = %v, want 29400", got)
	}
	if got := numberOf(cs["xpHora"]); !near(got, 29400, 1) {
		t.Errorf("session xp/h = %v, want 29400", got)
	}
}

func TestBuildPaceRollingWindowBeatsSessionsAndTargets(t *testing.T) {
	// The rolling window gives 40 points in 30 minutes (80 an hour); the older sessions 10 an hour.
	base := t0
	steps := 30 * 60 / 5
	samples := samplesEvery(base, steps+1, 40.0/float64(steps), func(int) PaceSample { return PaceSample{} })
	now := samples[len(samples)-1].At
	old := store.Session{StartedAt: base.Add(-10 * time.Hour), EndedAt: base.Add(-9 * time.Hour), XPGainedPct: 10}
	out := buildPace(paceInput{
		Now: now, Level: 20, XPPct: 20, Source: "live", At: now,
		Recent: []store.Session{old}, Samples: samples, Target: 22,
	})
	subida := asMap(out["subida"])
	if subida["ritmoFuente"] != "ultimos30min" {
		t.Fatalf("rate source = %v, want the rolling window", subida["ritmoFuente"])
	}
	if got := numberOf(subida["ritmoPctHora"]); !near(got, 80, 0.5) {
		t.Errorf("rate = %v, want 80", got)
	}
	// 80 points left at 80 an hour is one hour; the target 22 adds 100 points: 180 points, 135 min.
	if got := numberOf(subida["etaNivelObjetivoMin"]); !near(got, 135, 1) {
		t.Errorf("eta to level 22 = %v min, want 135", got)
	}
	if subida["nivelObjetivo"] != 22 {
		t.Errorf("target = %v", subida["nivelObjetivo"])
	}
}

func TestBuildPaceFallsBackToSavedSessions(t *testing.T) {
	now := t0
	recent := []store.Session{
		{StartedAt: now.Add(-5 * time.Hour), EndedAt: now.Add(-4 * time.Hour), XPGainedPct: 20},
		{StartedAt: now.Add(-3 * time.Hour), EndedAt: now.Add(-2 * time.Hour), XPGainedPct: 10},
		{StartedAt: now.Add(-2 * time.Hour), EndedAt: now.Add(-time.Hour), XPGainedPct: 15},
		{StartedAt: now.Add(-20 * time.Hour), EndedAt: now.Add(-19 * time.Hour), XPGainedPct: 99},
	}
	out := buildPace(paceInput{Now: now, Level: 30, XPPct: 0, Source: "savedvariables", At: now, Recent: recent})
	subida := asMap(out["subida"])
	if subida["ritmoFuente"] != "ultimasSesiones" {
		t.Fatalf("rate source = %v", subida["ritmoFuente"])
	}
	// Three sessions of one hour each: 45 points over 3 hours, 15 an hour. The fourth is ignored.
	if got := numberOf(subida["ritmoPctHora"]); !near(got, 15, 0.01) {
		t.Errorf("rate = %v, want 15", got)
	}
	ultimas, _ := out["ultimasSesiones"].([]map[string]any)
	if len(ultimas) != 3 {
		t.Errorf("sessions listed = %d, want 3", len(ultimas))
	}
}

func TestBuildPaceWithoutAnyRateSaysSo(t *testing.T) {
	out := buildPace(paceInput{Now: t0, Level: 5, XPPct: 40, Source: "live", At: t0})
	subida := asMap(out["subida"])
	if _, has := subida["etaSiguienteNivel"]; has {
		t.Error("no rate must not give an eta")
	}
	notes, _ := out["notas"].([]string)
	if len(notes) == 0 || !strings.Contains(strings.Join(notes, " "), "Sin progreso") {
		t.Errorf("notes = %v, want the no-progress note", notes)
	}
	if _, has := out["xpHoraTexto"]; !has {
		t.Error("xpHoraTexto must always be present")
	}
}

func TestLevelingPaceReadsSavedVariablesAndStoredSessions(t *testing.T) {
	g, st := fixtureService(t)
	// Two finished sessions of one hour and 20 points each.
	for i := 0; i < 2; i++ {
		start := time.Now().Add(-time.Duration(10-i*2) * time.Hour)
		se := &store.Session{StartedAt: start, EndedAt: start.Add(time.Hour), XPGainedPct: 20}
		if err := st.SaveSession(se); err != nil {
			t.Fatal(err)
		}
	}
	v, err := g.LevelingPace(40)
	if err != nil {
		t.Fatalf("LevelingPace: %v", err)
	}
	m := asMap(v)
	if !truthy(m["available"]) {
		t.Fatalf("pace unavailable: %v", m)
	}
	// The fixture character: level 38, 41250 of 98000 xp.
	if got := numberOf(m["nivel"]); got != 38 {
		t.Errorf("level = %v, want 38", got)
	}
	if got := numberOf(m["xpPct"]); !near(got, 42.09, 0.05) {
		t.Errorf("xpPct = %v, want about 42.1", got)
	}
	subida := asMap(m["subida"])
	if subida["ritmoFuente"] != "ultimasSesiones" {
		t.Errorf("rate source = %v", subida["ritmoFuente"])
	}
	if got := numberOf(subida["ritmoPctHora"]); !near(got, 20, 0.5) {
		t.Errorf("rate = %v, want 20", got)
	}
	if got := numberOf(subida["faltaXP"]); !near(got, 98000-41250, 2) {
		t.Errorf("faltaXP = %v", got)
	}
}

func TestLevelingPaceWithoutCharacterHintsToSync(t *testing.T) {
	g := NewService(nil, nil, nil)
	v, err := g.LevelingPace(0)
	if err != nil {
		t.Fatal(err)
	}
	if truthy(asMap(v)["available"]) {
		t.Errorf("no data must not be available: %v", v)
	}
}
