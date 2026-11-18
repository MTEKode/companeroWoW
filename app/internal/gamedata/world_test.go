package gamedata

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// worldFixture is a human priest of level 19 in Elwynn (map 37) with the world block of the
// contract: three learned flight masters (one more is not learned), measured flights, three vendors,
// two trainers, a quest chain and three consumables. It has three won fights and one lost one.
const worldFixture = "../../testdata/CompaneroWoW_world.lua"

func worldFixtureService(t *testing.T) *Service {
	t.Helper()
	w := bridge.NewSVWatcher(worldFixture, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll world fixture: %v", err)
	}
	return NewService(nil, w, openStore(t))
}

// approx compares two seconds values to one decimal place.
func approx(got, want float64) bool { return math.Abs(got-want) < 0.05 }

// walkToVentormenta is the walk from Elwynn's player position (0.41, 0.52) to the flight master
// Ventormenta at (0.45, 0.60): 8.94 map units, at 12 yards a unit and 7 yards a second.
const walkToVentormenta = 8.944 * 12 / 7

func TestLoadWorldReadsContractAndFiltersFactionAndLearnedNodes(t *testing.T) {
	g := worldFixtureService(t)
	w, ok := g.loadWorld()
	if !ok || !w.Known {
		t.Fatal("world block not read")
	}
	if w.Faction != "Alliance" {
		t.Errorf("faction = %q, want Alliance from the race", w.Faction)
	}
	if _, horde := w.Nodes[10]; horde {
		t.Error("the Horde node must not be used by an Alliance character")
	}
	if w.KnownNodes[4] {
		t.Error("Puerto Aguafria is not in the character's learned list")
	}
	if !w.KnownNodes[1] || !w.KnownNodes[2] || !w.KnownNodes[3] {
		t.Errorf("known nodes = %v, want 1, 2 and 3", w.KnownNodes)
	}
	if e, ok := w.Edges[[2]int64{1, 2}]; !ok || e.Seconds != 95 || e.Copper != 1200 {
		t.Errorf("edge 1>2 = %+v", e)
	}
	if len(w.Vendors) != 3 || len(w.Trainers) != 2 || len(w.Chains) != 3 || len(w.Consumables) != 3 {
		t.Errorf("counts: vendors %d trainers %d chains %d consumables %d", len(w.Vendors), len(w.Trainers), len(w.Chains), len(w.Consumables))
	}
	if v := w.Vendors[3001]; len(v.Items) != 2 || !v.HasPos || v.X != 0.42 {
		t.Errorf("vendor 3001 = %+v", v)
	}
}

func TestWorldPointScalesPercentages(t *testing.T) {
	if x, y, ok := worldPoint(map[string]any{"x": 45.0, "y": 60.0}); !ok || x != 0.45 || y != 0.6 {
		t.Errorf("percent point = %v %v %v", x, y, ok)
	}
	if _, _, ok := worldPoint(map[string]any{"x": 150.0, "y": 20.0}); ok {
		t.Error("a point beyond 100 % must be rejected")
	}
	if _, _, ok := worldPoint(map[string]any{"x": 0.1}); ok {
		t.Error("a point without y must be rejected")
	}
}

func TestMergeWorldLaysLiveDeltaOverSaved(t *testing.T) {
	base := map[string]any{
		"flight":  map[string]any{"Alliance": map[string]any{"costs": map[string]any{"1>2": map[string]any{"seconds": 95.0}}}},
		"vendors": map[string]any{"1": map[string]any{"name": "A"}},
	}
	over := map[string]any{
		"flight": map[string]any{"Alliance": map[string]any{"costs": map[string]any{"2>1": map[string]any{"seconds": 100.0}}}},
	}
	out := mergeWorld(base, over)
	costs := out["flight"].(map[string]any)["Alliance"].(map[string]any)["costs"].(map[string]any)
	if len(costs) != 2 {
		t.Errorf("merged costs = %v, want both edges", costs)
	}
	if _, ok := out["vendors"]; !ok {
		t.Error("untouched table lost")
	}
	// The inputs are not modified.
	if len(base["flight"].(map[string]any)["Alliance"].(map[string]any)["costs"].(map[string]any)) != 1 {
		t.Error("mergeWorld changed its base")
	}
}

func TestRouteToOtherMapUsesTheFlight(t *testing.T) {
	g := worldFixtureService(t)
	m := objOf(t, func() (any, error) { return g.RouteTo("Forjaz") })
	if m["encontrado"] != true || m["destino"] != "Forjaz" {
		t.Fatalf("route = %#v", m)
	}
	mejor := m["mejor"].(map[string]any)
	if mejor["tipo"] != "vuelo" || mejor["vuelos"] != 1 {
		t.Fatalf("best = %#v, want one flight", mejor)
	}
	if got := mejor["segundos"].(float64); !approx(got, walkToVentormenta+95) {
		t.Errorf("seconds = %v, want walk %.2f + flight 95", got, walkToVentormenta)
	}
	if m["medido"] != true {
		t.Error("a flight route is measured")
	}
	steps := m["pasos"].([]string)
	if len(steps) != 2 || !strings.Contains(steps[1], "Toma vuelo en Ventormenta hacia Forjaz") {
		t.Errorf("steps = %v", steps)
	}
	if mark, _ := m["marca"].(string); !strings.HasPrefix(mark, "/compa marca 37 ") {
		t.Errorf("marca = %q, want the first leg on map 37", mark)
	}
}

func TestRouteReachesNodeThroughTheCheapestChain(t *testing.T) {
	g := worldFixtureService(t)
	m := objOf(t, func() (any, error) { return g.RouteTo("Villa Darkshire") })
	mejor := m["mejor"].(map[string]any)
	// Ventormenta to Villa Darkshire is 60 s; the route 1>3 is the only one.
	if got := mejor["segundos"].(float64); !approx(got, walkToVentormenta+60) {
		t.Errorf("seconds = %v, want %.2f", got, walkToVentormenta+60)
	}
}

func TestRouteOnOneMapIsWalkingAndEstimated(t *testing.T) {
	g := worldFixtureService(t)
	m := objOf(t, func() (any, error) { return g.RouteTo("37 45 53") })
	if m["encontrado"] != true {
		t.Fatalf("coordinates not found: %#v", m)
	}
	mejor := m["mejor"].(map[string]any)
	if mejor["tipo"] != "caminar" || m["medido"] != false {
		t.Errorf("same-map coordinates = %#v, want a walk, estimated", mejor)
	}
	want := math.Hypot(0.45-0.41, 0.53-0.52) * 100 * 12 / 7
	if got := mejor["segundos"].(float64); !approx(got, want) {
		t.Errorf("walk = %v, want %v", got, want)
	}
}

func TestRouteUnknownDestinationSaysSo(t *testing.T) {
	g := worldFixtureService(t)
	m := objOf(t, func() (any, error) { return g.RouteTo("Zona que no existe") })
	if m["encontrado"] != false || m["nota"] == nil {
		t.Errorf("unknown destination = %#v", m)
	}
}

func TestValorHopUsesMeasuredFlightAcrossMaps(t *testing.T) {
	g := worldFixtureService(t)
	hop := g.travelHopper()
	if hop == nil {
		t.Fatal("no travel hopper with flight data")
	}
	sec, measured := hop(questStop{MapID: 37, X: 0.41, Y: 0.52}, questStop{MapID: 1, X: 0.30, Y: 0.40})
	if !measured || !approx(sec, walkToVentormenta+95) {
		t.Errorf("hop = %v %v, want the measured flight", sec, measured)
	}
	// Same map: the walk estimate, not measured.
	if _, measured := hop(questStop{MapID: 37, X: 0.1, Y: 0.1}, questStop{MapID: 37, X: 0.2, Y: 0.1}); measured {
		t.Error("a walk on one map is never measured")
	}
}

func TestEstimateQuestHopLabelsMeasuredTravel(t *testing.T) {
	g := worldFixtureService(t)
	c := questCandidate{ID: 1, Name: "Test", Level: 10, PlayerLevel: 10, InLog: true,
		Objectives: []questStop{{MapID: 1, X: 0.30, Y: 0.40}}, Kills: 0, KillSec: 20, XP: 600}
	e := estimateQuestHop(c, questStop{MapID: 37, X: 0.41, Y: 0.52}, true, g.travelHopper())
	if !e.TravelMeasured || !approx(e.TravelSec, walkToVentormenta+95) {
		t.Errorf("travel = %v measured %v", e.TravelSec, e.TravelMeasured)
	}
	plain := estimateQuest(c, questStop{MapID: 37, X: 0.41, Y: 0.52}, true)
	if plain.TravelMeasured || plain.TravelSec != questMapHopSec {
		t.Errorf("without the hop: travel %v measured %v, want the 90 s estimate", plain.TravelSec, plain.TravelMeasured)
	}
}

func TestBuyOptionsSortsByPriceWithRoute(t *testing.T) {
	g := worldFixtureService(t)
	m := objOf(t, func() (any, error) { return g.BuyOptions("Pan duro", 0) })
	rows := m["compras"].([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want the two vendors of Pan duro", len(rows))
	}
	if rows[0]["vendedor"] != "Vendedora Marta" || rows[0]["precio"] != int64(8) {
		t.Errorf("cheapest = %#v", rows[0])
	}
	if rows[0]["tipoRuta"] != "vuelo" {
		t.Errorf("Forjaz is reached by flight from Elwynn, got %v", rows[0]["tipoRuta"])
	}
}

func TestBuyOptionsByItemID(t *testing.T) {
	g := worldFixtureService(t)
	m := objOf(t, func() (any, error) { return g.BuyOptions("", 1251) })
	rows := m["compras"].([]map[string]any)
	if len(rows) != 2 || rows[0]["vendedor"] != "Hermano Rodrigo" {
		t.Errorf("bandage rows = %#v", rows)
	}
	if rows[0]["tipoRuta"] != "caminar" {
		t.Errorf("Hermano Rodrigo is on the player's map: %v", rows[0]["tipoRuta"])
	}
}

func TestBuyOptionsUnknownItem(t *testing.T) {
	g := worldFixtureService(t)
	m := objOf(t, func() (any, error) { return g.BuyOptions("Espada de nadie", 0) })
	if m["encontrado"] != false {
		t.Errorf("unknown item = %#v", m)
	}
}

func TestClassTrainingCostNowAndNextLevel(t *testing.T) {
	g := worldFixtureService(t)
	m := objOf(t, func() (any, error) { return g.TrainingCost("", "") })
	if m["costeAhora"] != int64(2000) {
		t.Errorf("now = %v, want 2000 (Golpe de escudo, level 18)", m["costeAhora"])
	}
	if m["nivelProximo"] != 22 || m["costeProximo"] != int64(13000) {
		t.Errorf("next level = %v cost %v, want 22 and 13000", m["nivelProximo"], m["costeProximo"])
	}
	if m["puedesPagarAhora"] != true {
		t.Errorf("25 gold pays 20 silver: %v", m["puedesPagarAhora"])
	}
}

func TestProfessionTrainingCostUsesRank(t *testing.T) {
	g := worldFixtureService(t)
	m := objOf(t, func() (any, error) { return g.TrainingCost("Sastrería", "") })
	if m["costeAhora"] != int64(800) {
		t.Errorf("now = %v, want 800 (rank 68 Oficial)", m["costeAhora"])
	}
	if m["costeProximo"] != int64(1500) || m["habilidadProxima"] != 75 {
		t.Errorf("next = %v at %v, want 1500 at rank 75", m["costeProximo"], m["habilidadProxima"])
	}
	ahora := m["ahora"].([]map[string]any)
	if len(ahora) != 1 || ahora[0]["entrenador"] != "Lana Hilotejido" || ahora[0]["marca"] == nil {
		t.Errorf("trainer row = %#v", ahora)
	}
}

func TestChainMissingPrerequisiteAndOrder(t *testing.T) {
	g := worldFixtureService(t)
	m := objOf(t, func() (any, error) { return g.ChainOf(90102, "") })
	faltan := m["faltan"].([]map[string]any)
	if len(faltan) != 1 || faltan[0]["id"] != int64(90101) {
		t.Errorf("missing = %#v, want 90101", faltan)
	}
	mision := m["mision"].(map[string]any)
	if mision["estado"] != "hecha" {
		t.Errorf("90102 is in the completed list: %v", mision["estado"])
	}
	if m["desbloquea"] != "«Tercer encargo»" {
		t.Errorf("unlocks = %v", m["desbloquea"])
	}
	order := m["ordenSugerido"].([]map[string]any)
	if len(order) != 3 || order[0]["id"] != int64(90101) || order[2]["id"] != int64(90103) {
		t.Errorf("order = %#v", order)
	}
}

func TestChainLaterQuestHasNoMissingPrerequisites(t *testing.T) {
	g := worldFixtureService(t)
	m := objOf(t, func() (any, error) { return g.ChainOf(90103, "") })
	if faltan := m["faltan"].([]map[string]any); len(faltan) != 0 {
		t.Errorf("missing = %#v, want none", faltan)
	}
	anteriores := m["anteriores"].([]map[string]any)
	if len(anteriores) != 2 || anteriores[0]["id"] != int64(90101) || anteriores[1]["estado"] != "hecha" {
		t.Errorf("before = %#v, want 90101 then 90102 (done)", anteriores)
	}
}

func TestConsumablesPlanForFivePulls(t *testing.T) {
	g := worldFixtureService(t)
	m := objOf(t, func() (any, error) { return g.ConsumablesPlan(5) })
	pelea := m["pelea"].(map[string]any)
	if pelea["peleasMedidas"] != 3 {
		t.Errorf("won fights = %v, want 3 (the lost one is left out)", pelea["peleasMedidas"])
	}
	if got := pelea["danoMedio"].(float64); !approx(got, 400) {
		t.Errorf("mean damage = %v, want 400", got)
	}
	if got := pelea["vidaNecesaria"].(float64); !approx(got, 2000) {
		t.Errorf("need = %v, want 2000 for 5 pulls", got)
	}
	llevar := m["llevar"].([]map[string]any)
	if len(llevar) == 0 || llevar[0]["objeto"] != "Pan duro" || llevar[0]["cantidad"] != 10 || llevar[0]["faltan"] != 8 {
		t.Errorf("carry = %#v, want 10 bread, 8 to buy", llevar)
	}
	compra := m["mejorPorCobre"].([]map[string]any)
	if compra[0]["objeto"] != "Pan duro" {
		t.Errorf("best per copper = %v, want the bread at 8 copper", compra[0]["objeto"])
	}
	habs := m["habilidades"].([]map[string]any)
	if habs[0]["aprendida"] != true || habs[0]["rango"] != 60 {
		t.Errorf("First Aid = %#v", habs[0])
	}
}

func TestConsumablesWithoutWorldSaysSync(t *testing.T) {
	g := gearService(t)
	m := objOf(t, func() (any, error) { return g.ConsumablesPlan(0) })
	if m["available"] != false {
		t.Errorf("without a world block = %#v", m)
	}
}

func TestLiveWorldDeltaLaysOverSavedWorld(t *testing.T) {
	r := bridge.NewReader(time.Second)
	w := bridge.NewSVWatcher(worldFixture, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll world fixture: %v", err)
	}
	g := NewService(r, w, openStore(t))
	// A changed part on the channel (msgType 16): one new flight, the saved ones stay.
	if err := r.Channel().Data.Put(bridge.MsgWorld, []byte(`{"flight":{"Alliance":{"costs":{"2>3":{"copper":900,"hops":2,"seconds":80}}}}}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	wd, ok := g.loadWorld()
	if !ok {
		t.Fatal("no world block")
	}
	if e, ok := wd.Edges[[2]int64{2, 3}]; !ok || e.Seconds != 80 {
		t.Errorf("live edge 2>3 = %+v %v", e, ok)
	}
	if e, ok := wd.Edges[[2]int64{1, 2}]; !ok || e.Seconds != 95 {
		t.Errorf("saved edge 1>2 = %+v %v, want the saved 95 s", e, ok)
	}
}

// Two changed-only live deltas accumulate: the flight from the first and the vendor from the second
// are both used, on top of the saved block.
func TestTwoLiveWorldDeltasAccumulate(t *testing.T) {
	r := bridge.NewReader(time.Second)
	w := bridge.NewSVWatcher(worldFixture, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll world fixture: %v", err)
	}
	g := NewService(r, w, openStore(t))
	now := time.Now()
	if err := r.Channel().Data.Put(bridge.MsgWorld, []byte(`{"flight":{"Alliance":{"costs":{"2>3":{"copper":900,"hops":2,"seconds":80}}}}}`), now); err != nil {
		t.Fatal(err)
	}
	if err := r.Channel().Data.Put(bridge.MsgWorld, []byte(`{"vendors":{"3004":{"npcID":3004,"name":"Nuevo","mapID":37,"x":0.4,"y":0.5,"items":[{"itemID":118,"name":"Poción de vida menor","price":150,"stack":1}]}}}`), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	wd, ok := g.loadWorld()
	if !ok {
		t.Fatal("no world block")
	}
	if e, ok := wd.Edges[[2]int64{2, 3}]; !ok || e.Seconds != 80 {
		t.Errorf("first delta's edge 2>3 = %+v %v", e, ok)
	}
	if _, ok := wd.Vendors[3004]; !ok {
		t.Error("second delta's vendor missing")
	}
	if _, ok := wd.Vendors[3001]; !ok {
		t.Error("saved vendor 3001 lost")
	}
}
