package gamedata

import (
	"strings"
	"testing"
)

// fight builds one recorded combat as the addon stores it.
func fight(name string, npcID float64, dur float64, died bool, mobDamage float64, killer string, endHP float64, level float64, class string) map[string]any {
	c := map[string]any{
		"duration": dur,
		"died":     died,
		"targets": []any{map[string]any{
			"name": name, "npcID": npcID, "level": level, "classification": class,
		}},
		"damageTaken": map[string]any{
			"total":   mobDamage + 10,
			"sources": []any{map[string]any{"name": name, "amount": mobDamage}, map[string]any{"name": "Otro", "amount": 10.0}},
		},
		"resourcesAtEnd": map[string]any{"healthPct": endHP},
	}
	if died {
		c["death"] = map[string]any{"killer": killer}
	}
	return c
}

func TestMobFightsCountsOnlyTheMob(t *testing.T) {
	block := []any{
		fight("Jabalí", 1001, 20, false, 30, "", 70, 10, "normal"),
		fight("Lobo", 2002, 15, false, 25, "", 80, 10, "normal"),
		fight("jabalí", 0, 25, true, 40, "Jabalí", 0, 10, "normal"), // matched by name, case-insensitive
	}
	got := mobFights(block, "Jabalí", 0)
	if len(got) != 2 {
		t.Fatalf("fights = %d, want 2 (the wolf is another mob)", len(got))
	}
	if got[0].taken != 30 {
		t.Errorf("damage from the mob = %v, want only its own 30 (not the whole fight)", got[0].taken)
	}
	if !got[1].died || !got[1].killer {
		t.Errorf("the second fight is a death caused by the mob: %+v", got[1])
	}
	byID := mobFights(block, "", 2002)
	if len(byID) != 1 || byID[0].taken != 25 {
		t.Errorf("match by npcID = %+v", byID)
	}
}

func TestObservedStatsAreComputedFromFights(t *testing.T) {
	fs := []mobFight{
		{duration: 20, taken: 30, endHealth: 70},
		{duration: 20, taken: 30, endHealth: 50},
		{duration: 20, taken: 30, endHealth: 90},
	}
	s := observedStatsOf(fs)
	if s.Fights != 3 || s.Deaths != 0 {
		t.Fatalf("fights %d deaths %d", s.Fights, s.Deaths)
	}
	if !near(s.TakenPerSec, 1.5, 1e-9) {
		t.Errorf("damage per second = %v, want 1.5", s.TakenPerSec)
	}
	if !near(s.EndHealth, 70, 1e-9) {
		t.Errorf("median end health = %v, want 70", s.EndHealth)
	}
	view := s.view()
	if view["combates"] != 3 || view["duracionMediaSeg"] != 20.0 {
		t.Errorf("view = %v", view)
	}
	empty := observedStatsOf(nil).view()
	if _, has := empty["danoRecibidoPorSeg"]; has {
		t.Error("no fights must not invent damage numbers")
	}
}

func TestVerdictFromObservedFights(t *testing.T) {
	won := func(endHP int) observedStats {
		return observedStatsOf([]mobFight{{duration: 20, taken: 30, endHealth: endHP}, {duration: 20, taken: 30, endHealth: endHP}})
	}
	cases := []struct {
		name    string
		s       observedStats
		wantPre string
	}{
		{"comfortable win", won(75), "Sí"},
		{"win with little life left", won(30), "Sí, pero con cuidado"},
		{"died once in three", observedStatsOf([]mobFight{
			{duration: 20, endHealth: 60}, {duration: 20, endHealth: 60}, {duration: 20, died: true},
		}), "Con cuidado"},
		{"died most of the time", observedStatsOf([]mobFight{
			{duration: 20, died: true, killer: true}, {duration: 20, died: true, killer: true}, {duration: 20, endHealth: 60},
		}), "No"},
	}
	for _, c := range cases {
		v, reasons := mobVerdict(c.s, "normal", 10, 10, 100, 1)
		if !strings.HasPrefix(v, c.wantPre) {
			t.Errorf("%s: verdict %q, want prefix %q (reasons %v)", c.name, v, c.wantPre, reasons)
		}
		if len(reasons) == 0 {
			t.Errorf("%s: a verdict must come with reasons", c.name)
		}
	}
}

func TestVerdictWithoutFightsUsesLevelAndClass(t *testing.T) {
	none := observedStats{}
	v, reasons := mobVerdict(none, "elite", 12, 10, 100, 1)
	if !strings.HasPrefix(v, "Con cuidado") || !strings.Contains(v, "sin datos") {
		t.Errorf("elite two levels up without data = %q", v)
	}
	if !strings.Contains(strings.Join(reasons, " "), "No hay combates grabados") {
		t.Errorf("reasons must say there is no data: %v", reasons)
	}
	if v, _ := mobVerdict(none, "normal", 8, 10, 100, 1); !strings.HasPrefix(v, "Probablemente sí") {
		t.Errorf("two levels below = %q", v)
	}
	if v, _ := mobVerdict(none, "worldboss", -1, 10, 100, 1); !strings.HasPrefix(v, "No") {
		t.Errorf("a boss without data = %q", v)
	}
}

func TestVerdictAsksToRecoverWhenLowAndAddsGroupFactor(t *testing.T) {
	s := observedStatsOf([]mobFight{{duration: 20, endHealth: 80}})
	v, reasons := mobVerdict(s, "normal", 10, 10, 35, 3)
	if !strings.HasPrefix(v, "Con cuidado: en grupo") {
		t.Errorf("three at once with a clear win = %q", v)
	}
	joined := strings.Join(reasons, " ")
	if !strings.Contains(joined, "Ahora tienes 35 %") || !strings.Contains(joined, "6 veces") {
		t.Errorf("reasons = %v", reasons)
	}
}

func TestGroupRowsUseTheSequentialFactor(t *testing.T) {
	rows := groupRows(observedStatsOf([]mobFight{{duration: 10, taken: 20}}))
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0]["factorDano"] != 3 || rows[1]["factorDano"] != 6 {
		t.Errorf("factors = %v, %v; want 3 and 6", rows[0]["factorDano"], rows[1]["factorDano"])
	}
	if rows[1]["danoRecibidoEstimadoPorCombate"] != 120.0 {
		t.Errorf("group of three damage = %v, want 120", rows[1]["danoRecibidoEstimadoPorCombate"])
	}
}

func TestMobInfoByNameWithoutRecordedFights(t *testing.T) {
	g, _ := fixtureService(t)
	v, err := g.MobInfo(MobQuery{Name: "Enemigo inventado", Group: 2})
	if err != nil {
		t.Fatalf("MobInfo: %v", err)
	}
	m := asMap(v)
	if !truthy(m["available"]) {
		t.Fatalf("a named mob must answer even without fights: %v", m)
	}
	if !strings.Contains(m["veredicto"].(string), "sin datos") {
		t.Errorf("verdict = %v, want a no-data verdict", m["veredicto"])
	}
	if m["grupoAnalizado"] != 2 {
		t.Errorf("group analysed = %v", m["grupoAnalizado"])
	}
	if _, has := m["predicho"]; has {
		t.Error("no model hook means no predicted time")
	}
}

func TestMobInfoPredictionFromModelHook(t *testing.T) {
	g, _ := fixtureService(t)
	g.SetKillTime(func(level int) (float64, bool) { return 14, true })
	if p := g.predictedKill(10); p == nil || p["tiempoMatarSeg"] != 14.0 {
		t.Errorf("predicted kill = %v", p)
	}
	if p := g.predictedKill(0); p != nil {
		t.Error("no level, no prediction")
	}
}

func TestMobInfoWithoutTargetOrNameAsksForOne(t *testing.T) {
	g := NewService(nil, nil, nil)
	v, err := g.MobInfo(MobQuery{})
	if err != nil {
		t.Fatal(err)
	}
	m := asMap(v)
	if truthy(m["available"]) || m["hint"] == nil {
		t.Errorf("no target must say so with a hint: %v", m)
	}
}
