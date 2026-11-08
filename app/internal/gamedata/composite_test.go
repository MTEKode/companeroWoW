package gamedata

import (
	"strings"
	"testing"
)

func TestPlayerContextAnswersSituationInOneMap(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, svc.PlayerContext)
	if m["available"] != true {
		t.Fatalf("envelope = %#v", m)
	}
	ub := asMap(m["ubicacion"])
	if ub["zona"] != "Orgrimmar" || ub["subZona"] != "Valley of Strength" || ub["mapId"] != 85 {
		t.Errorf("ubicacion = %#v", ub)
	}
	if ub["coords"] != "(51.2, 38.7)" {
		t.Errorf("coords = %v", ub["coords"])
	}
	pj := asMap(m["personaje"])
	if pj["nombre"] != "Thrall" || pj["clase"] != "Shaman" || pj["nivel"] != float64(38) {
		t.Errorf("personaje = %#v", pj)
	}
	if !strings.Contains(m["dinero"].(string), "123 oro") {
		t.Errorf("dinero = %v", m["dinero"])
	}
	mr := asMap(m["misionRastreada"])
	if mr["questId"] != int64(90001) || mr["titulo"] != "The Warchief's Orders" {
		t.Fatalf("misionRastreada = %#v", mr)
	}
	step := asMap(mr["siguientePaso"])
	if step["tipo"] != stepObjetivo || step["marca"] != "/compa marca 85 31.0 55.0 Gnoll Raider" {
		t.Errorf("siguientePaso = %#v", step)
	}
	if fresh := asMap(m["datos"]); fresh["questieDisponible"] != true {
		t.Errorf("datos = %#v", fresh)
	}
	if objs, ok := mr["objetivos"].([]map[string]any); !ok || len(objs) != 2 || objs[0]["progreso"] != "3/8" {
		t.Errorf("objetivos = %#v", mr["objetivos"])
	}
}

func TestPlayerContextWithoutDataSaysSync(t *testing.T) {
	svc := NewService(nil, nil, nil)
	m := objOf(t, svc.PlayerContext)
	if m["available"] != false || !strings.Contains(m["hint"].(string), "/compa sync") {
		t.Fatalf("no data must be unavailable with a hint: %#v", m)
	}
}

func TestQuestGuideNearestSpawnOnSameMapAndMarca(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, func() (any, error) { return svc.QuestGuide(90001) })
	if m["available"] != true || m["questId"] != int64(90001) || m["enRegistro"] != true {
		t.Fatalf("guide envelope = %#v", m)
	}
	step := asMap(m["nextStep"])
	if step["tipo"] != stepObjetivo {
		t.Fatalf("next step = %#v", step)
	}
	if step["mapId"] != 85 || step["x"] != 31.0 || step["y"] != 55.0 {
		t.Errorf("nearest spawn must be the one on the player's map (85): %#v", step)
	}
	if step["sameMap"] != true || step["distancia"] != 26.0 {
		t.Errorf("distance on the same map = %#v", step["distancia"])
	}
	if step["marca"] != "/compa marca 85 31.0 55.0 Gnoll Raider" {
		t.Errorf("marca = %v", step["marca"])
	}
	starters := m["inicio"].([]any)
	if len(starters) != 1 || asMap(asMap(starters[0])["nearest"])["marca"] == "" {
		t.Errorf("starters = %#v", starters)
	}
}

func TestQuestGuideCompletedQuestGoesToFinisherOnOtherMap(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, func() (any, error) { return svc.QuestGuide(90002) })
	step := asMap(m["nextStep"])
	if m["completa"] != true || step["tipo"] != stepEntregar {
		t.Fatalf("complete quest must point to the finisher: %#v", m)
	}
	if step["mapId"] != 1 || step["sameMap"] != false {
		t.Errorf("finisher is on map 1, player on 85: %#v", step)
	}
	if _, has := step["distancia"]; has {
		t.Error("no distance across maps")
	}
	if step["marca"] != "/compa marca 1 44.8 56.0 Herbalist Ruth" {
		t.Errorf("marca = %v", step["marca"])
	}
}

func TestQuestGuideTrackedDefaultAndUnknown(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, func() (any, error) { return svc.QuestGuide(0) })
	if m["questId"] != int64(90001) {
		t.Errorf("default must be the tracked quest, got %v", m["questId"])
	}
	unknown := objOf(t, func() (any, error) { return svc.QuestGuide(999999) })
	if unknown["available"] != false {
		t.Errorf("unknown quest must be unavailable: %#v", unknown)
	}
}

func TestQuestGuideWithoutQuestieFallsBackToKnowledge(t *testing.T) {
	svc, _ := fixtureService(t)
	// Quest 90001 is in the log; drop the Questie block for this check.
	svc.watcher.Current().Raw["characters"].(map[string]any)["Thrall-Forever"].(map[string]any)["questie"] = map[string]any{"available": false}
	m := objOf(t, func() (any, error) { return svc.QuestGuide(90001) })
	if m["questieDisponible"] != false {
		t.Fatalf("questie must be reported missing: %#v", m)
	}
	step := asMap(m["nextStep"])
	if step["tipo"] != stepObjetivo || step["marca"] != nil {
		t.Errorf("without Questie there is no marca for the objective: %#v", step)
	}
}

func TestNearestSpawnWithoutPosition(t *testing.T) {
	spawns := []spawn{{MapID: 1, X: 0.4, Y: 0.6}, {MapID: 85, X: 0.5, Y: 0.5}}
	n, ok := nearest(spawns, position{})
	if !ok || n.Spawn.MapID != 1 || n.SameMap {
		t.Fatalf("without a position the first spawn is used: %#v", n)
	}
	if _, ok := nearest(nil, position{MapID: 1, X: 0.1, Y: 0.1, OK: true}); ok {
		t.Error("no spawns means no nearest spawn")
	}
	n, _ = nearest(spawns, position{MapID: 85, X: 0.5, Y: 0.5, OK: true})
	if n.Spawn.MapID != 85 || n.Distance != 0 {
		t.Errorf("same map and same point: %#v", n)
	}
}

func TestMarcaCommandSyntaxAndCleanLabel(t *testing.T) {
	cmd := marcaCommand(spawn{MapID: 85, X: 0.4523, Y: 0.6708}, "Entregar | a\nMarshal Dughan con un nombre muy largo que supera el límite de sesenta caracteres")
	if !strings.HasPrefix(cmd, "/compa marca 85 45.2 67.1 Entregar a Marshal Dughan") {
		t.Errorf("marca = %q", cmd)
	}
	if strings.ContainsAny(cmd, "\n|") {
		t.Errorf("marca must be one chat line without pipes: %q", cmd)
	}
	if n := len([]rune(strings.TrimPrefix(cmd, "/compa marca 85 45.2 67.1 "))); n > markLabelRunes {
		t.Errorf("label = %d runes, limit %d", n, markLabelRunes)
	}
	if got := marcaCommand(spawn{MapID: 1, X: 0.5, Y: 0.25}, ""); got != "/compa marca 1 50.0 25.0" {
		t.Errorf("no label: %q", got)
	}
}

func TestQuestieReadsOnlyValidPoints(t *testing.T) {
	data := map[string]any{"questie": map[string]any{
		"available": true,
		"quests": map[string]any{
			"5": map[string]any{"name": "Prueba", "objectives": []any{
				map[string]any{"index": 1, "name": "Bien", "spawns": []any{
					map[string]any{"mapID": 85, "x": 0.1, "y": 0.2},
					map[string]any{"mapID": 85, "x": 1.5, "y": 0.2},
					map[string]any{"mapID": 0, "x": 0.1, "y": 0.2},
					map[string]any{"mapID": 85, "x": 0.3},
				}},
			}},
		},
	}}
	qx := readQuestie(data)
	q, ok := qx.quest(5)
	if !ok || len(q.Objectives) != 1 {
		t.Fatalf("quest 5 not read: %#v", qx)
	}
	if got := q.Objectives[0].Spawns; len(got) != 1 || got[0].X != 0.1 {
		t.Errorf("only the valid spawn must survive: %#v", got)
	}
}

func TestProfessionPlanTrainersAndTrainNow(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, svc.ProfessionPlan)
	profs := m["profesiones"].([]map[string]any)
	if len(profs) != 2 {
		t.Fatalf("professions = %#v", profs)
	}
	herb := profs[0]
	if herb["nombre"] != "Herbalism" || herb["entrenarYa"] != true {
		t.Errorf("Herbalism at 150/150 must train now: %#v", herb)
	}
	trainers := herb["entrenadores"].([]map[string]any)
	if len(trainers) != 2 {
		t.Fatalf("Herbalism trainers = %#v", trainers)
	}
	var fuentes []string
	for _, tr := range trainers {
		fuentes = append(fuentes, tr["fuente"].(string))
		if !strings.HasPrefix(tr["marca"].(string), "/compa marca ") {
			t.Errorf("trainer marca = %v", tr["marca"])
		}
	}
	if strings.Join(fuentes, ",") != "conocimiento,questie" && strings.Join(fuentes, ",") != "questie,conocimiento" {
		t.Errorf("trainer sources = %v", fuentes)
	}
	skin := profs[1]
	if skin["nombre"] != "Skinning" || len(skin["entrenadores"].([]map[string]any)) != 0 || skin["nota"] == nil {
		t.Errorf("Skinning has no known trainer: %#v", skin)
	}
}

func TestProfessionPlanFarFromCapWaits(t *testing.T) {
	if n := nearMaxRankLeft; n <= 0 {
		t.Fatalf("threshold must be positive")
	}
	svc, _ := fixtureService(t)
	ch, _, _ := svc.activeCharacter()
	profs := ch.data["professions"].([]any)
	profs[0].(map[string]any)["rank"] = 40.0
	m := objOf(t, svc.ProfessionPlan)
	herb := m["profesiones"].([]map[string]any)[0]
	if herb["entrenarYa"] != false {
		t.Errorf("40/150 must wait: %#v", herb)
	}
}

func TestObtenerMisionesCarriesQuestieStartersAndFinishers(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, svc.Quests)
	quests := m["quests"].([]map[string]any)
	q := quests[0]
	qq, ok := q["questie"].(map[string]any)
	if !ok {
		t.Fatalf("questie block missing from quest 90001: %#v", q)
	}
	if len(qq["starters"].([]any)) != 1 || len(qq["finishers"].([]any)) != 1 {
		t.Errorf("starters/finishers = %#v", qq)
	}
}
