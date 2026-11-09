package gamedata

import (
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// gearFixture is a cloth priest of level 19 with every gear block of the contract.
const gearFixture = "../../testdata/CompaneroWoW_gear.lua"

func gearService(t *testing.T) *Service {
	t.Helper()
	w := bridge.NewSVWatcher(gearFixture, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll gear fixture: %v", err)
	}
	return NewService(nil, w, openStore(t))
}

// ranuraOf returns the slot entry of an analysis by its name.
func ranuraOf(t *testing.T, m map[string]any, name string) map[string]any {
	t.Helper()
	for _, r := range m["ranuras"].([]map[string]any) {
		if r["ranura"] == name {
			return r
		}
	}
	t.Fatalf("no slot %q in %v", name, m["ranuras"])
	return nil
}

func listOfRows(t *testing.T, r map[string]any, window string) []map[string]any {
	t.Helper()
	list, ok := r[window].([]map[string]any)
	if !ok {
		t.Fatalf("%v has no %s list: %#v", r["ranura"], window, r)
	}
	return list
}

func names(rows []map[string]any) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r["objeto"].(string))
	}
	return out
}

func TestGearAnalysisOrdersUpgradesAndSeparatesWindows(t *testing.T) {
	m := objOf(t, func() (any, error) { return gearService(t).GearAnalysis("ambas", "") })
	if m["available"] != true || m["ventana"] != "ambas" {
		t.Fatalf("envelope = %#v", m)
	}
	p := m["personaje"].(map[string]any)
	if p["clase"] != "Priest" || p["especializacion"] != "Disciplina" || p["nivel"] != 19 {
		t.Errorf("character = %#v", p)
	}

	if got := names(listOfRows(t, ranuraOf(t, m, "Cabeza"), winNow)); len(got) != 1 || got[0] != "Capucha de Hilo Sagrada" {
		t.Errorf("Cabeza ahora = %v", got)
	}
	if got := names(listOfRows(t, ranuraOf(t, m, "Pecho"), winNow)); len(got) != 1 || got[0] != "Túnica de Hilo Arcano" {
		t.Errorf("Pecho ahora = %v (the mail chest and the weak robe must be out)", got)
	}
	if got := names(listOfRows(t, ranuraOf(t, m, "Mano derecha"), winNow)); len(got) != 1 || got[0] != "Bastón de Brasa Lenta" {
		t.Errorf("Mano derecha ahora = %v", got)
	}
	if got := names(listOfRows(t, ranuraOf(t, m, "Cintura"), winSoon)); len(got) != 1 || got[0] != "Cinturón de Lino Ritual" {
		t.Errorf("Cintura pronto = %v", got)
	}
	if got := listOfRows(t, ranuraOf(t, m, "Cintura"), winNow); len(got) != 0 {
		t.Errorf("the locked belt must not be listed as reachable now: %v", got)
	}

	cabeza := listOfRows(t, ranuraOf(t, m, "Cabeza"), winNow)[0]
	if cabeza["ilvlDelta"] != "+4" || cabeza["calidad"] != "verde" {
		t.Errorf("head delta/quality = %v / %v", cabeza["ilvlDelta"], cabeza["calidad"])
	}
	if cambios := cabeza["cambios"].(string); cambios != "+4 Espíritu, +2 Intelecto, -2 Aguante" {
		t.Errorf("head stat diff = %q", cambios)
	}
	if ranuraOf(t, m, "Cabeza")["actual"].(map[string]any)["objeto"] != "Capucha de Lino Usada" {
		t.Errorf("current head = %v", ranuraOf(t, m, "Cabeza")["actual"])
	}
	if ranuraOf(t, m, "Cintura")["actual"] != "ranura vacía" {
		t.Errorf("empty belt must say so: %v", ranuraOf(t, m, "Cintura")["actual"])
	}

	// Best upgrades: reachable now first, then the rest.
	best := m["mejores"].([]map[string]any)
	if len(best) < 2 || best[0]["ventana"] != winNow || best[0]["objeto"] != "Bastón de Brasa Lenta" {
		t.Errorf("mejores top = %v", best)
	}
	last := best[len(best)-1]
	if last["ventana"] != winSoon {
		t.Errorf("the soon upgrade must come after the ones reachable now: %v", best)
	}
}

func TestGearAnalysisSourcesCarryPlacesAndMarks(t *testing.T) {
	m := objOf(t, func() (any, error) { return gearService(t).GearAnalysis("ahora", "pecho") })
	rows := listOfRows(t, ranuraOf(t, m, "Pecho"), winNow)
	if len(rows) != 1 {
		t.Fatalf("pecho rows = %v", rows)
	}
	fuentes := rows[0]["fuentes"].([]map[string]any)
	var quest, dungeon map[string]any
	for _, f := range fuentes {
		switch f["tipo"] {
		case "mision":
			quest = f
		case "mazmorra":
			dungeon = f
		}
	}
	if quest == nil || quest["mision"] != "Encargo de la Hermana Lurdes" || quest["dondeAceptar"] != "Hermana Lurdes" {
		t.Fatalf("quest source = %#v", fuentes)
	}
	if quest["coords"] != "(41.0, 53.0)" || quest["marca"] != "/compa marca 37 41.0 53.0 Hermana Lurdes" {
		t.Errorf("quest place = %v / %v", quest["coords"], quest["marca"])
	}
	if quest["aElegir"] != true {
		t.Errorf("reward must be marked as a choice: %#v", quest)
	}
	if dungeon == nil || dungeon["mazmorra"] != "Fortaleza Colmillo Oscuro" || dungeon["jefe"] != "Lord Godfrey" {
		t.Fatalf("dungeon source = %#v", fuentes)
	}
	if dungeon["marca"] != "/compa marca 10 35.0 62.0 Fortaleza Colmillo Oscuro" {
		t.Errorf("dungeon mark = %v", dungeon["marca"])
	}
}

func TestGearAnalysisLockedQuestIsSoonAndNamesWhatIsMissing(t *testing.T) {
	m := objOf(t, func() (any, error) { return gearService(t).GearAnalysis("pronto", "cintura") })
	rows := listOfRows(t, ranuraOf(t, m, "Cintura"), winSoon)
	if len(rows) != 1 {
		t.Fatalf("cintura pronto = %v", rows)
	}
	if rows[0]["ilvlDelta"] != "+21" {
		t.Errorf("empty slot delta = %v", rows[0]["ilvlDelta"])
	}
	fuente := rows[0]["fuentes"].([]map[string]any)[0]
	faltan, _ := fuente["faltan"].([]string)
	if fuente["estado"] != winSoon || len(faltan) != 1 || faltan[0] != "«Ruinas de la Brasa»" {
		t.Errorf("locked source = %#v", fuente)
	}
	if fuente["requiereNivel"] != 18 {
		t.Errorf("required level = %v", fuente["requiereNivel"])
	}
}

func TestGearAnalysisExcludesUnusableAndNonUpgrades(t *testing.T) {
	out := objOf(t, func() (any, error) { return gearService(t).GearAnalysis("ambas", "") })
	all := strings.Builder{}
	for _, r := range out["ranuras"].([]map[string]any) {
		for _, w := range []string{winNow, winSoon} {
			for _, row := range listOfRows(t, r, w) {
				all.WriteString(row["objeto"].(string) + "|")
			}
		}
	}
	for _, banned := range []string{"Coraza de Malla Forjada", "Túnica de Hilo Gastada"} {
		if strings.Contains(all.String(), banned) {
			t.Errorf("%q must not be an upgrade: %s", banned, all.String())
		}
	}
}

func TestGearAnalysisSlotFilterAndBadArguments(t *testing.T) {
	svc := gearService(t)
	byNumber := objOf(t, func() (any, error) { return svc.GearAnalysis("ambas", "1") })
	if rs := byNumber["ranuras"].([]map[string]any); len(rs) != 1 || rs[0]["ranura"] != "Cabeza" {
		t.Errorf("slot 1 = %v", rs)
	}
	unknown := objOf(t, func() (any, error) { return svc.GearAnalysis("ambas", "tabardo") })
	if unknown["available"] != false || !strings.Contains(unknown["hint"].(string), "tabardo") {
		t.Errorf("unknown slot = %#v", unknown)
	}
	if _, err := svc.GearAnalysis("mañana", ""); err == nil {
		t.Error("a window that is not ahora, pronto or ambas must be an error")
	}
}

func TestGearAnalysisSaysWhichBlockIsMissing(t *testing.T) {
	noGear := svService(t, charLua(`["character"] = { ["class"] = "Priest", ["level"] = 19 },
		["questWindow"] = { ["available"] = true, ["quests"] = {} },
		["gearCandidates"] = { ["available"] = true, ["items"] = {} }`))
	if got := objOf(t, func() (any, error) { return noGear.GearAnalysis("ambas", "") }); got["available"] != false || got["hint"] != hintGear {
		t.Errorf("missing gear = %#v", got)
	}
	noQuests := svService(t, charLua(`["character"] = { ["class"] = "Priest", ["level"] = 19 },
		["gear"] = { [1] = { ["itemID"] = 1, ["name"] = "X", ["itemLevel"] = 10, ["equipLoc"] = "INVTYPE_HEAD" } },
		["questWindow"] = { ["available"] = false, ["reason"] = "Questie no cargado" },
		["gearCandidates"] = { ["available"] = true, ["items"] = {} }`))
	got := objOf(t, func() (any, error) { return noQuests.GearAnalysis("ambas", "") })
	if got["hint"] != hintQuestWindow || got["motivo"] != "Questie no cargado" {
		t.Errorf("missing quest window = %#v", got)
	}
	noCandidates := svService(t, charLua(`["character"] = { ["class"] = "Priest", ["level"] = 19 },
		["gear"] = { [1] = { ["itemID"] = 1, ["name"] = "X", ["itemLevel"] = 10, ["equipLoc"] = "INVTYPE_HEAD" } },
		["questWindow"] = { ["available"] = true, ["quests"] = {} }`))
	if got := objOf(t, func() (any, error) { return noCandidates.GearAnalysis("ambas", "") }); got["hint"] != hintCandidates {
		t.Errorf("missing candidates = %#v", got)
	}
}

func TestAvailableQuestsGroupsByZoneAndOrdersByDistance(t *testing.T) {
	m := objOf(t, func() (any, error) { return gearService(t).AvailableQuests("ambas", "") })
	zonas := m["zonas"].([]map[string]any)
	var order []string
	for _, z := range zonas {
		order = append(order, z["zona"].(string))
	}
	if strings.Join(order, "|") != "Bosque de Elwynn|Ventormenta|Páramos de Poniente|Bosque del Ocaso" {
		t.Fatalf("zone order = %v", order)
	}
	elwynn := zonas[0]["misiones"].([]map[string]any)
	if elwynn[0]["nombre"] != "Encargo de la Hermana Lurdes" || elwynn[0]["mismoMapa"] != true {
		t.Errorf("nearest quest first = %#v", elwynn[0])
	}
	if elwynn[1]["nombre"] != "Recados del Cuartel" || elwynn[2]["nombre"] != "Capucha para el Guardián" {
		t.Errorf("distance order = %v, %v", elwynn[1]["nombre"], elwynn[2]["nombre"])
	}
	if mejora := elwynn[0]["mejoraEquipo"]; mejora == nil || mejora.([]string)[0] != "Túnica de Hilo Arcano" {
		t.Errorf("reward upgrade mark = %v", elwynn[0]["mejoraEquipo"])
	}
	if rw := elwynn[0]["recompensas"].(map[string][]string); rw["elegir"][0] != "Túnica de Hilo Arcano" {
		t.Errorf("rewards = %v", rw)
	}
	for _, z := range zonas {
		for _, q := range z["misiones"].([]map[string]any) {
			if q["nombre"] == "Ritual de la Sacerdotisa" && (q["ventana"] != winSoon || len(q["faltan"].([]string)) != 1) {
				t.Errorf("locked quest = %#v", q)
			}
		}
	}
	if got := strings.Join(zoneQuestNames(m), "|"); !strings.Contains(got, "Ritual de la Sacerdotisa") {
		t.Errorf("the locked quest must still be offered as soon: %s", got)
	}
}

func TestAvailableQuestsZoneFilterAndCompletedAreOut(t *testing.T) {
	svc := gearService(t)
	m := objOf(t, func() (any, error) { return svc.AvailableQuests("ahora", "ventormenta") })
	zonas := m["zonas"].([]map[string]any)
	if len(zonas) != 1 || len(zonas[0]["misiones"].([]map[string]any)) != 2 {
		t.Fatalf("ventormenta now = %#v", zonas)
	}
	// 90107 is completed (completedQuests) and must not be offered.
	all := objOf(t, func() (any, error) { return svc.AvailableQuests("ambas", "") })
	for _, n := range zoneQuestNames(all) {
		if n == "Sombras de Brazaloscura" {
			t.Error("a completed quest must not be offered")
		}
	}
}

func zoneQuestNames(m map[string]any) []string {
	var out []string
	for _, z := range m["zonas"].([]map[string]any) {
		for _, q := range z["misiones"].([]map[string]any) {
			out = append(out, q["nombre"].(string))
		}
	}
	return out
}

func TestDungeonInfoByNameAndSuggestions(t *testing.T) {
	svc := gearService(t)
	m := objOf(t, func() (any, error) { return svc.DungeonInfo("minas") })
	if m["nombre"] != "Minas de Brazaloscura" || m["estado"] != winNow {
		t.Fatalf("dungeon = %#v", m)
	}
	entrada := m["entrada"].(map[string]any)
	if entrada["coords"] != "(40.0, 60.0)" || entrada["marca"] != "/compa marca 40 40.0 60.0 Minas de Brazaloscura" {
		t.Errorf("entrance = %#v", entrada)
	}
	misiones := m["misiones"].([]map[string]any)
	if len(misiones) != 1 || misiones[0]["estado"] != "ya completada" {
		t.Errorf("quests = %#v", misiones)
	}
	jefe := m["jefes"].([]map[string]any)[0]
	ups := jefe["mejorasParaTi"].([]map[string]any)
	if len(ups) != 1 || ups[0]["objeto"] != "Bastón de Brasa Lenta" || ups[0]["ilvlDelta"] != "+7" {
		t.Errorf("boss upgrades = %#v", ups)
	}

	godfrey := objOf(t, func() (any, error) { return svc.DungeonInfo("colmillo") })
	gjefe := godfrey["jefes"].([]map[string]any)[0]
	if gjefe["nombre"] != "Lord Godfrey" || len(gjefe["mejorasParaTi"].([]map[string]any)) != 2 {
		t.Errorf("Lord Godfrey upgrades = %#v", gjefe)
	}

	sug := objOf(t, func() (any, error) { return svc.DungeonInfo("") })
	list := sug["sugerencias"].([]map[string]any)
	if len(list) != 2 || list[0]["nombre"] != "Minas de Brazaloscura" || list[1]["nombre"] != "Fortaleza Colmillo Oscuro" {
		t.Errorf("suggestions = %#v", list)
	}

	none := objOf(t, func() (any, error) { return svc.DungeonInfo("Sala Inexistente") })
	if none["encontrada"] != false || len(none["disponibles"].([]string)) != 2 {
		t.Errorf("unknown dungeon = %#v", none)
	}
}

func TestItemLookupByNameIdAndKnowledge(t *testing.T) {
	svc := gearService(t)
	m := objOf(t, func() (any, error) { return svc.ItemLookup("Túnica de Hilo Arcano", 0) })
	obj := m["objeto"].(map[string]any)
	if obj["ranura"] != "Pecho" || obj["usable"] != true || obj["estadisticas"] != "Intelecto +9, Aguante +6" {
		t.Errorf("item = %#v", obj)
	}
	if fuentes := m["fuentes"].([]map[string]any); len(fuentes) != 2 {
		t.Errorf("sources = %#v", fuentes)
	}
	up := m["mejoraTuEquipo"].(map[string]any)
	if up["si"] != true || up["ilvlDelta"] != "+4" || up["ventana"] != winNow {
		t.Errorf("upgrade = %#v", up)
	}

	mail := objOf(t, func() (any, error) { return svc.ItemLookup("", 91005) })
	mobj := mail["objeto"].(map[string]any)
	if mobj["usable"] != false || !strings.Contains(mobj["motivoNoUsable"].(string), "Mail") {
		t.Errorf("mail = %#v", mobj)
	}

	pergamino := objOf(t, func() (any, error) { return svc.ItemLookup("Pergamino", 0) })
	pobj := pergamino["objeto"].(map[string]any)
	if pobj["nombre"] != "Pergamino de Ritual Antiguo" {
		t.Errorf("knowledge item name = %#v", pobj)
	}
	fp := pergamino["fuentes"].([]map[string]any)
	if len(fp) != 1 || fp[0]["mision"] != "Recados del Cuartel" {
		t.Errorf("knowledge source = %#v", fp)
	}

	bag := objOf(t, func() (any, error) { return svc.ItemLookup("Túnica de Hilo Gastada", 0) })
	if bag["enInventario"] != 1 {
		t.Errorf("bag count = %v", bag["enInventario"])
	}

	none := objOf(t, func() (any, error) { return svc.ItemLookup("Espada Inexistente", 0) })
	if none["encontrado"] != false {
		t.Errorf("unknown item = %#v", none)
	}
}

func TestPlayerContextCountsGearAndQuests(t *testing.T) {
	m := objOf(t, gearService(t).PlayerContext)
	eq, ok := m["equipo"].(map[string]any)
	if !ok || eq["mejorasAhora"] != 3 || eq["mejorasPronto"] != 1 || eq["misionesAhora"] != 6 {
		t.Errorf("equipo summary = %#v", m["equipo"])
	}
}

func TestEvaluateFlagsTwoHandedConflicts(t *testing.T) {
	gd := gearData{Level: 19, Equipped: map[int]gearItem{
		16: {SlotID: 16, Name: "Bastón Doble", EquipLoc: "INVTYPE_2HWEAPON", ItemLevel: 20},
	}}
	off := gearItem{Name: "Escudo", ItemLevel: 22, EquipLoc: "INVTYPE_SHIELD"}
	grp, _ := groupByKey("OFFHAND")
	up, keep := gd.evaluate(off, grp)
	if !keep || len(up.Notes) != 1 || !strings.Contains(up.Notes[0], "Bastón Doble") {
		t.Errorf("offhand next to a two-hander = %+v", up)
	}
	gd.Equipped = map[int]gearItem{17: {SlotID: 17, Name: "Escudo Viejo", ItemLevel: 10}}
	two := gearItem{Name: "Bastón Nuevo", ItemLevel: 30, EquipLoc: "INVTYPE_2HWEAPON"}
	mh, _ := groupByKey("MAINHAND")
	up, _ = gd.evaluate(two, mh)
	if len(up.Notes) != 1 || !strings.Contains(up.Notes[0], "Escudo Viejo") {
		t.Errorf("two-hander over an offhand = %+v", up)
	}
}

func TestStatNamesChangesAndWindows(t *testing.T) {
	if got := changesText(statChanges(map[string]float64{"ITEM_MOD_INTELLECT_SHORT": 5}, map[string]float64{"ITEM_MOD_STAMINA_SHORT": 3})); got != "+5 Intelecto, -3 Aguante" {
		t.Errorf("changes = %q", got)
	}
	if got := statName("ITEM_MOD_UNKNOWN_SHORT"); got != "ITEM_MOD_UNKNOWN_SHORT" {
		t.Errorf("unknown stat must stay as the key: %q", got)
	}
	for in, want := range map[string]map[string]bool{
		"":       {winNow: true, winSoon: true},
		"ambas":  {winNow: true, winSoon: true},
		"ya":     {winNow: true},
		"pronto": {winSoon: true},
	} {
		got, err := windowsOf(in)
		if err != nil || len(got) != len(want) {
			t.Errorf("windowsOf(%q) = %v, %v", in, got, err)
		}
	}
	if g, ok := groupForSlotArg("Mano derecha"); !ok || g.Key != "MAINHAND" {
		t.Errorf("slot name = %v", g)
	}
	if g, ok := groupForSlotArg("anillo"); !ok || g.Key != "FINGER" {
		t.Errorf("ring = %v", g)
	}
}
