package gamedata

import (
	"math"
	"testing"
)

func TestClassicQuestXPScalesWithLevelAndDropsWhenGray(t *testing.T) {
	if got := classicQuestXP(10, 10); got != 600 {
		t.Errorf("level 10 quest at level 10 = %d, want 600", got)
	}
	if got := classicQuestXP(10, 14); got != 600 {
		t.Errorf("four levels below is still full value, got %d", got)
	}
	// Ten levels below the player: 6 levels of reduction, 40 % of the base (240).
	if got := classicQuestXP(10, 20); got != 240 {
		t.Errorf("ten levels below = %d, want 240", got)
	}
	if classicQuestXP(30, 30) <= classicQuestXP(10, 10) {
		t.Error("a higher quest must give more XP")
	}
	if classicQuestXP(0, 10) != 0 {
		t.Error("a quest without level gives no estimate")
	}
}

func TestTravelSecondsUsesMapUnitsAndMapHops(t *testing.T) {
	a := questStop{MapID: 85, X: 0.50, Y: 0.50}
	b := questStop{MapID: 85, X: 0.50, Y: 0.60} // 10 map units
	want := 10 * questYardsPerUnit / questSpeedYPS
	if got := travelSeconds(a, b); !near(got, want, 1e-9) {
		t.Errorf("same map = %v s, want %v", got, want)
	}
	c := questStop{MapID: 1, X: 0.5, Y: 0.5}
	if got := travelSeconds(a, c); got != questMapHopSec {
		t.Errorf("different maps = %v s, want the hop %v", got, questMapHopSec)
	}
}

func TestEstimateQuestWalksStarterObjectiveTurnIn(t *testing.T) {
	pos := questStop{MapID: 85, X: 0.50, Y: 0.40}
	c := questCandidate{
		ID: 1, Name: "Test", Zone: "Valle", Level: 10, PlayerLevel: 10,
		Starter:    &questStop{MapID: 85, X: 0.50, Y: 0.50},    // 10 units from the player
		Objectives: []questStop{{MapID: 85, X: 0.50, Y: 0.70}}, // 20 units from the starter
		Finisher:   &questStop{MapID: 85, X: 0.50, Y: 0.70},    // the same place as the objective
		Kills:      4,
		KillSec:    20,
		XP:         600,
	}
	e := estimateQuest(c, pos, true)
	walk := (10 + 20 + 0) * questYardsPerUnit / questSpeedYPS
	if !near(e.TravelSec, walk, 1e-6) {
		t.Errorf("travel = %v s, want %v (starter, objective, turn-in)", e.TravelSec, walk)
	}
	if e.CombatSec != 80 {
		t.Errorf("combat = %v s, want 80 (4 kills at 20 s)", e.CombatSec)
	}
	if want := (walk + 80) / 60; !near(e.Minutes, want, 1e-6) {
		t.Errorf("minutes = %v, want %v", e.Minutes, want)
	}
	if !near(e.XPPerMin, 600/e.Minutes, 1e-9) {
		t.Errorf("xp per minute = %v", e.XPPerMin)
	}
	if e.PrimaryPos == nil || e.PrimaryPos.Y != 0.50 {
		t.Errorf("primary stop should be the starter while the quest is not in the log: %+v", e.PrimaryPos)
	}
}

func TestEstimateQuestInLogSkipsTheStarter(t *testing.T) {
	pos := questStop{MapID: 85, X: 0.5, Y: 0.5}
	c := questCandidate{
		ID: 2, InLog: true, Starter: &questStop{MapID: 85, X: 0.9, Y: 0.9},
		Objectives: []questStop{{MapID: 85, X: 0.5, Y: 0.6}},
		XP:         300, KillSec: 20,
	}
	e := estimateQuest(c, pos, true)
	if want := 10 * questYardsPerUnit / questSpeedYPS; !near(e.TravelSec, want, 1e-6) {
		t.Errorf("travel = %v, want only the walk to the objective (%v)", e.TravelSec, want)
	}
}

func TestEstimateQuestWithoutCoordinatesIsMarked(t *testing.T) {
	e := estimateQuest(questCandidate{ID: 3, XP: 100, KillSec: 20}, questStop{}, false)
	if !e.NoCoords {
		t.Error("a quest without places must say so")
	}
	if e.Minutes < questMinMinutes {
		t.Errorf("minutes = %v, below the floor", e.Minutes)
	}
}

func TestRankingByXPPerMinuteAndGold(t *testing.T) {
	pos := questStop{MapID: 85, X: 0.5, Y: 0.5}
	nearQ := questCandidate{ID: 10, Name: "cerca", Zone: "Z", Level: 10, XP: 600, InLog: true,
		Objectives: []questStop{{MapID: 85, X: 0.5, Y: 0.51}}, KillSec: 20, Kills: 2, Copper: 500, HasCopper: true}
	farQ := questCandidate{ID: 11, Name: "lejos", Zone: "Z", Level: 10, XP: 900, InLog: true,
		Objectives: []questStop{{MapID: 85, X: 0.5, Y: 0.95}}, KillSec: 20, Kills: 5, Copper: 9000, HasCopper: true}
	ests := []questEstimate{estimateQuest(farQ, pos, true), estimateQuest(nearQ, pos, true)}
	// The far quest gives more XP but takes about three minutes (walk and five kills); the near one, a
	// minute, wins on XP per minute.
	if ests[1].XPPerMin <= ests[0].XPPerMin {
		t.Fatalf("the near quest must win on XP per minute: near %v, far %v", ests[1].XPPerMin, ests[0].XPPerMin)
	}
	v := questEstimateView(ests[1])
	if v["titulo"] != "cerca" || v["xpFuente"] != "estimado" || v["matas"] != 2 {
		t.Errorf("view = %v", v)
	}
	if v["oroCobre"] != int64(500) {
		t.Errorf("gold = %v", v["oroCobre"])
	}
	noGold := questEstimateView(estimateQuest(questCandidate{ID: 12, XP: 1, KillSec: 20}, pos, true))
	if _, ok := noGold["oroCobre"]; ok {
		t.Error("gold must only show when the reward is known")
	}
}

func TestOrderRouteIsNearestNeighbourAndFinishesAZone(t *testing.T) {
	start := questStop{MapID: 85, X: 0.5, Y: 0.5}
	mk := func(id int64, zone string, x, y float64, mapID int) questEstimate {
		p := questStop{MapID: mapID, X: x, Y: y}
		return questEstimate{Candidate: questCandidate{ID: id, Zone: zone}, PrimaryPos: &p}
	}
	es := []questEstimate{
		mk(1, "A", 0.9, 0.9, 85),                      // far
		mk(2, "A", 0.5, 0.51, 85),                     // closest
		mk(3, "A", 0.5, 0.53, 85),                     // second closest, same zone
		mk(4, "B", 0.5, 0.52, 85),                     // nearly as close as 3 but another zone: the penalty keeps zone A first
		mk(5, "C", 0.1, 0.1, 99),                      // another map: visited last
		{Candidate: questCandidate{ID: 6, Zone: "A"}}, // no place: left out
	}
	got := orderRoute(start, true, es)
	var ids []int64
	for _, i := range got {
		ids = append(ids, es[i].Candidate.ID)
	}
	want := []int64{2, 3, 4, 1, 5}
	if len(ids) != len(want) {
		t.Fatalf("route = %v, want %v", ids, want)
	}
	for k := range want {
		if ids[k] != want[k] {
			t.Fatalf("route = %v, want %v", ids, want)
		}
	}
}

func TestOrderRouteWithoutPositionStillOrders(t *testing.T) {
	p1 := questStop{MapID: 85, X: 0.2, Y: 0.2}
	p2 := questStop{MapID: 85, X: 0.8, Y: 0.8}
	es := []questEstimate{
		{Candidate: questCandidate{ID: 1}, PrimaryPos: &p2},
		{Candidate: questCandidate{ID: 2}, PrimaryPos: &p1},
	}
	got := orderRoute(questStop{}, false, es)
	if len(got) != 2 {
		t.Fatalf("route = %v", got)
	}
}

func TestKillsFromObjectiveText(t *testing.T) {
	cases := map[string]int{
		"Kill 8 Boars":        8,
		"Mata 3 jabalíes":     3,
		"Slay 12 murlocs":     12,
		"Talk to the innkeep": 0,
		"":                    0,
	}
	for text, want := range cases {
		if got := killsFromText(text); got != want {
			t.Errorf("killsFromText(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestKillSecondsPrefersModelThenObservedThenDefault(t *testing.T) {
	g := NewService(nil, nil, nil)
	if s, src := g.killSecondsFor(10, nil); s != questDefaultKillSec || src != "supuesto" {
		t.Errorf("default = %v %s", s, src)
	}
	if s, src := g.killSecondsFor(10, []float64{10, 30, 20}); s != 20 || src != "observado" {
		t.Errorf("observed median = %v %s, want 20 observado", s, src)
	}
	g.SetKillTime(func(level int) (float64, bool) { return 7, level == 10 })
	if s, src := g.killSecondsFor(10, []float64{10, 30, 20}); s != 7 || src != "modelo" {
		t.Errorf("model = %v %s, want 7 modelo", s, src)
	}
	if s, src := g.killSecondsFor(11, []float64{10}); s != 10 || src != "observado" {
		t.Errorf("model without answer must fall back: %v %s", s, src)
	}
}

func TestQuestValueWithoutQuestWindowSaysSync(t *testing.T) {
	g, _ := fixtureService(t)
	v, err := g.QuestValue(QuestValueOptions{})
	if err != nil {
		t.Fatalf("QuestValue: %v", err)
	}
	if truthy(asMap(v)["available"]) {
		t.Errorf("the fixture has no quest window; the answer must not be available: %v", v)
	}
	if _, err := g.QuestValue(QuestValueOptions{Ventana: "nunca"}); err == nil {
		t.Error("an unknown window must be an error")
	}
	_ = math.Pi
}

func TestQuestValueRanksTheGearFixtureQuests(t *testing.T) {
	g := gearService(t)
	g.SetKillTime(func(level int) (float64, bool) { return 15, true })
	m := objOf(t, func() (any, error) { return g.QuestValue(QuestValueOptions{Ventana: "ambas"}) })
	if m["available"] != true {
		t.Fatalf("gear fixture must have quests to value: %v", m)
	}
	ranking, _ := m["ranking"].([]map[string]any)
	if len(ranking) == 0 {
		t.Fatalf("empty ranking: %v", m)
	}
	for i := 1; i < len(ranking); i++ {
		if numberOf(ranking[i]["xpPorMin"]) > numberOf(ranking[i-1]["xpPorMin"]) {
			t.Errorf("ranking not sorted by XP per minute at %d", i)
		}
	}
	for _, r := range ranking {
		if r["xpFuente"] != "addon" && r["xpFuente"] != "estimado" {
			t.Errorf("XP source must be labelled: %v", r)
		}
	}
	if sup, _ := m["supuestos"].([]string); len(sup) < 3 {
		t.Errorf("assumptions must be listed: %v", m["supuestos"])
	}
	if criterio := m["criterio"]; criterio != "xp por minuto" {
		t.Errorf("criterio = %v", criterio)
	}
	oro := objOf(t, func() (any, error) { return g.QuestValue(QuestValueOptions{Ventana: "ambas", Oro: true}) })
	if oro["criterio"] != "oro por minuto" {
		t.Errorf("gold criterio = %v", oro["criterio"])
	}
}
