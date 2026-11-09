package gamedata

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// nativeFixture is a priest with no Questie and no ForeverDungeonJournal: every quest, reward and
// dungeon comes from the game ("juego") or from what the addon learned ("aprendido").
const nativeFixture = "../../testdata/CompaneroWoW_native.lua"

func nativeService(t *testing.T) *Service {
	t.Helper()
	w := bridge.NewSVWatcher(nativeFixture, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll native fixture: %v", err)
	}
	return NewService(nil, w, openStore(t))
}

// jsonOf is the answer as the assistant reads it, so that tests check what the model sees.
func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal answer: %v", err)
	}
	return string(b)
}

func TestNativeBlockIsReadWithoutQuestie(t *testing.T) {
	g := nativeService(t)
	ch, _, ok := g.activeCharacter()
	if !ok {
		t.Fatal("no active character in the native fixture")
	}
	nat := readNative(ch.data)
	if !nat.Available {
		t.Fatal("native block must be available")
	}
	if !nat.Sources.Juego || nat.Sources.Questie || nat.Sources.FDJ || !nat.Sources.Aprendido {
		t.Errorf("sources = %+v, want juego and aprendido only", nat.Sources)
	}
	if len(nat.Offered) != 2 {
		t.Errorf("offered quests = %d, want 2", len(nat.Offered))
	}
	if a, ok := nat.Active[90102]; !ok || a.Spawn.MapID != 37 {
		t.Errorf("next waypoint of 90102 = %+v, want map 37", a)
	}
	if readQuestie(ch.data).Available {
		t.Error("the fixture has no Questie block: questie must be unavailable")
	}
	if len(nat.NPCs) != 2 || nat.NPCs[0].AvailableCount != 2 {
		t.Errorf("learned NPCs = %+v", nat.NPCs)
	}
}

func TestQuestWindowLabelsEverySourceWithoutQuestie(t *testing.T) {
	g := nativeService(t)
	gd, ok := g.loadGearData()
	if !ok || !gd.QuestsKnown || !gd.CandidatesKnown || !gd.DungeonsKnown {
		t.Fatalf("native fixture must give quests, candidates and dungeons (ok=%v)", ok)
	}
	if !gd.QuestSources.Juego || gd.QuestSources.Questie {
		t.Errorf("quest sources = %+v", gd.QuestSources)
	}
	if q := gd.Quests[90106]; q.Source != "juego" || !q.Accessible {
		t.Errorf("90106 = %+v, want offered by the game", q)
	}
	if q := gd.Quests[90104]; q.Source != "aprendido" || q.Accessible {
		t.Errorf("90104 = %+v, want learned and not offered now", q)
	}
	c, ok := gd.Candidates[91001]
	if !ok || len(c.Sources) != 1 || c.Sources[0].Source != "aprendido" || !c.Sources[0].Choice {
		t.Errorf("91001 sources = %+v, want the learned choice reward", c.Sources)
	}
	if !gd.DungeonSources.Juego || gd.DungeonSources.FDJ {
		t.Errorf("dungeon sources = %+v", gd.DungeonSources)
	}
	if len(gd.Dungeons) != 2 || gd.Dungeons[0].LevelsKnown {
		t.Errorf("dungeons = %+v, want two levelless entries", gd.Dungeons)
	}
}

func TestAvailableQuestsNativeOnlyShowsSources(t *testing.T) {
	g := nativeService(t)
	res, err := g.AvailableQuests("ambas", "")
	if err != nil {
		t.Fatal(err)
	}
	text := jsonOf(t, res)
	for _, want := range []string{`"fuente":"juego"`, `"fuente":"aprendido"`, `"Encargo de la Hermana Lurdes"`, `"juego":true`, `"questie":false`} {
		if !strings.Contains(text, want) {
			t.Errorf("misiones_disponibles misses %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Questie es") || strings.Contains(text, `"questie":true`) {
		t.Errorf("Questie must not be asked for when quests are found:\n%s", text)
	}
}

func TestAvailableQuestsEmptyNamesQuestieAsOptional(t *testing.T) {
	g := nativeService(t)
	res, err := g.AvailableQuests("ahora", "Ventormenta")
	if err != nil {
		t.Fatal(err)
	}
	m := asMap(res)
	if n, _ := m["nota"].(string); !strings.Contains(n, "Questie es opcional") {
		t.Errorf("nota = %q, want the optional Questie suggestion", n)
	}
}

func TestQuestGuideUsesTheGameWhenQuestieIsMissing(t *testing.T) {
	g := nativeService(t)

	res, err := g.QuestGuide(90102)
	if err != nil {
		t.Fatal(err)
	}
	step := asMap(asMap(res)["nextStep"])
	if step["texto"] != "Haz: Recoge capuchas de lino" || step["fuente"] != "juego" || step["mapId"] != 37 {
		t.Errorf("in-log step = %v, want the game's waypoint (map 37)", step)
	}

	res, err = g.QuestGuide(90106)
	if err != nil {
		t.Fatal(err)
	}
	step = asMap(asMap(res)["nextStep"])
	if step["tipo"] != stepAccept || step["fuente"] != "juego" || step["mapId"] != 37 {
		t.Errorf("accept step = %v, want the game's offer position", step)
	}

	// Learned quest the game does not offer here: its giver position comes from questWindow
	// (learned), and nothing is invented for a quest without a known place.
	res, err = g.QuestGuide(90104)
	if err != nil {
		t.Fatal(err)
	}
	step = asMap(asMap(res)["nextStep"])
	if step["mapId"] != 40 || step["fuente"] != "aprendido" {
		t.Errorf("learned giver step = %v, want map 40 from learned data", step)
	}
	if _, err := g.QuestGuide(90999); err != nil {
		t.Fatal(err)
	}
}

func TestLearnedTrainersMatchByServiceName(t *testing.T) {
	g := nativeService(t)
	var found *trainerRef
	trs := g.knownTrainers()
	for i, tr := range trs {
		if tr.id == 3201 {
			found = &trs[i]
		}
		if tr.id == 3202 {
			t.Errorf("vendors are not trainers: %+v", tr)
		}
	}
	if found == nil {
		t.Fatal("learned trainer 3201 is missing")
	}
	if found.source != srcAprendido {
		t.Errorf("trainer source = %q, want aprendido", found.source)
	}
	if !found.matches("Sastrería") {
		t.Errorf("trainer must teach sastrería through its service name %v", found.services)
	}
	if found.matches("Herrería") {
		t.Error("trainer must not match a profession it does not teach")
	}
}

func TestDungeonsWithoutLevelRangeAreNotJudgedByLevel(t *testing.T) {
	g := nativeService(t)
	res, err := g.DungeonInfo("")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := asMap(res)["sugerencias"].([]map[string]any)
	if len(list) != 2 {
		t.Fatalf("suggestions = %d, want the two levelless dungeons", len(list))
	}
	for _, d := range list {
		if d["nivel"] != "sin dato" || d["fuente"] != "juego" {
			t.Errorf("dungeon %v: want level 'sin dato' and source juego", d)
		}
		if d["estado"] != "sin rango de nivel conocido" {
			t.Errorf("dungeon state = %v", d["estado"])
		}
	}

	res, err = g.DungeonInfo("Monasterio")
	if err != nil {
		t.Fatal(err)
	}
	if m := asMap(res); m["encontrada"] != true || m["nivel"] != "sin dato" {
		t.Errorf("dungeon detail = %v", m)
	}
}
