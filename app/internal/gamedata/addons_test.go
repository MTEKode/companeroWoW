package gamedata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// svService returns a service over a SavedVariables file with the given Lua body, and the
// knowledge store of the fixture-free test (empty).
func svService(t *testing.T, lua string) *Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	if err := os.WriteFile(path, []byte(lua), 0o644); err != nil {
		t.Fatal(err)
	}
	st := openStore(t)
	w := bridge.NewSVWatcher(path, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	return NewService(nil, w, st)
}

// charLua wraps character fields into a SavedVariables file for one character.
func charLua(fields string) string {
	return "CompaneroWoWDB = {\n\t[\"characters\"] = {\n\t\t[\"Thrall-Forever\"] = {\n\t\t\t[\"updatedAt\"] = 1793880000,\n" +
		fields + "\n\t\t},\n\t},\n}\n"
}

func TestMoneyTextIsCompactSpanish(t *testing.T) {
	cases := map[int64]string{
		0:       "0 cobre",
		1234567: "123 oro, 45 plata, 67 cobre",
		7000:    "70 plata",
		37000:   "3 oro, 70 plata",
		-2500:   "-25 plata",
		10000:   "1 oro",
		42:      "42 cobre",
	}
	for in, want := range cases {
		if got := moneyText(in); got != want {
			t.Errorf("moneyText(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestTrainingPlanNowNextLevelCostAndTrainer(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, svc.TrainingPlan)
	if m["available"] != true || m["clase"] != "Shaman" {
		t.Fatalf("envelope/class = %#v", m)
	}
	now, ok := m["ahora"].([]map[string]any)
	if !ok || len(now) != 2 {
		t.Fatalf("ahora = %#v", m["ahora"])
	}
	if now[0]["nombre"] != "Frost Shock" {
		t.Errorf("AVAILABLE spells must come first ordered by level then name: %#v", now[0])
	}
	if m["costeAhora"] != int64(7000) || m["costeAhoraTexto"] != "70 plata" {
		t.Errorf("total cost = %v / %v", m["costeAhora"], m["costeAhoraTexto"])
	}
	next, ok := m["proximoNivel"].(map[string]any)
	if !ok || next["nivel"] != 40 || next["coste"] != int64(37000) {
		t.Fatalf("proximoNivel = %#v", m["proximoNivel"])
	}
	if m["puedesPagarAhora"] != true {
		t.Errorf("123 gold must cover 70 silver: %v", m["puedesPagarAhora"])
	}
	trs, ok := m["entrenadores"].([]map[string]any)
	if !ok || len(trs) != 1 || trs[0]["nombre"] != "Korla Windtotem" {
		t.Fatalf("shaman trainer from Questie = %#v", m["entrenadores"])
	}
	if trs[0]["marca"] != "/compa marca 85 48.0 43.0 Korla Windtotem" {
		t.Errorf("trainer mark = %v", trs[0]["marca"])
	}
}

func TestTrainingPlanSaysWhenTrainerIsUnknown(t *testing.T) {
	svc := svService(t, charLua(`["character"] = { ["class"] = "Druid", ["money"] = 100 },
		["training"] = { ["available"] = true, ["spells"] = {} }`))
	m := objOf(t, svc.TrainingPlan)
	if _, has := m["entrenadores"]; has {
		t.Fatalf("no trainer must be invented: %#v", m)
	}
	if note, _ := m["nota"].(string); !strings.Contains(note, "Druid") {
		t.Fatalf("nota must say the trainer is unknown: %q", note)
	}
}

func TestMissingAndUnavailableAddonBlocksGiveHints(t *testing.T) {
	missing := svService(t, charLua(`["character"] = { ["class"] = "Shaman" }`))
	m := objOf(t, missing.TrainingPlan)
	if m["available"] != false || !strings.Contains(m["hint"].(string), "WhatsTraining") {
		t.Fatalf("missing training block = %#v", m)
	}

	off := svService(t, charLua(`["character"] = { ["class"] = "Shaman" },
		["profPlan"] = { ["available"] = false, ["reason"] = "SkillUpForever no cargado" }`))
	got := objOf(t, func() (any, error) { return off.ProfessionRoute("", 0) })
	if got["available"] != false || got["motivo"] != "SkillUpForever no cargado" {
		t.Fatalf("addon's own reason must be passed on: %#v", got)
	}
}

func TestProfessionRouteByNameIdAndAll(t *testing.T) {
	svc, _ := fixtureService(t)

	byName := objOf(t, func() (any, error) { return svc.ProfessionRoute("Sastrería", 0) })
	if byName["encontrada"] != true || byName["nombre"] != "Sastrería" || byName["skillLineID"] != int64(197) {
		t.Fatalf("route by name = %#v", byName)
	}
	steps, ok := byName["pasos"].([]map[string]any)
	if !ok || len(steps) != 2 || steps[0]["receta"] != "Pantalones de lana" || steps[0]["desde"] != 68 {
		t.Fatalf("steps = %#v", byName["pasos"])
	}
	mats := steps[0]["materiales"].([]map[string]any)
	if len(mats) != 1 || mats[0]["nombre"] != "Lino" || mats[0]["cantidad"] != 10 {
		t.Errorf("materials = %#v", mats)
	}
	shop := byName["compras"].([]map[string]any)
	if len(shop) != 2 || shop[0]["cantidad"] != 30 {
		t.Errorf("shopping = %#v", shop)
	}
	if byName["entrenarYa"] != true || !strings.Contains(byName["consejo"].(string), "7 puntos") {
		t.Errorf("Tailoring at 68/75 is within 15 points of the cap: train now (%#v)", byName)
	}

	byID := objOf(t, func() (any, error) { return svc.ProfessionRoute("", 197) })
	if byID["encontrada"] != true || byID["nombre"] != "Sastrería" {
		t.Errorf("route by skill line = %#v", byID)
	}

	missing := objOf(t, func() (any, error) { return svc.ProfessionRoute("Cocina", 0) })
	if missing["encontrada"] != false || !strings.Contains(missing["nota"].(string), "Sastrería") {
		t.Errorf("unknown profession must list the available ones: %#v", missing)
	}

	all := objOf(t, func() (any, error) { return svc.ProfessionRoute("", 0) })
	if list, ok := all["profesiones"].([]map[string]any); !ok || len(list) != 1 {
		t.Errorf("all routes = %#v", all["profesiones"])
	}
}

func TestPlanProfesionesFoldsRouteSummary(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, svc.ProfessionPlan)
	var tailoring map[string]any
	for _, p := range m["profesiones"].([]map[string]any) {
		if p["nombre"] == "Tailoring" {
			tailoring = p
		}
	}
	if tailoring == nil {
		t.Fatal("Tailoring missing from the plan")
	}
	ruta, ok := tailoring["rutaSkillUp"].(map[string]any)
	if !ok || ruta["pasos"] != 2 || ruta["compras"] != 2 || ruta["siguiente"] != "Pantalones de lana (68→75)" {
		t.Errorf("rutaSkillUp = %#v", tailoring["rutaSkillUp"])
	}
}

func TestPlayerContextCarriesCompactAddonSummaries(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, svc.PlayerContext)
	ent, ok := m["entrenamiento"].(map[string]any)
	if !ok || ent["ahora"] != 2 || ent["top"] != "Frost Shock (Rango 2)" || ent["proximoNivel"] != 40 {
		t.Errorf("entrenamiento summary = %#v", m["entrenamiento"])
	}
	ruta, ok := m["rutaProfesiones"].(map[string]any)
	if !ok || ruta["profesiones"] != 1 || ruta["pasos"] != 2 || ruta["compras"] != 2 {
		t.Errorf("rutaProfesiones summary = %#v", m["rutaProfesiones"])
	}
}

func TestHealthReaderAndStatusDiagnosis(t *testing.T) {
	svc, _ := fixtureService(t)
	h := objOf(t, svc.Health)
	if h["erroresInternos"] != int64(3) || !strings.Contains(h["ultimoError"].(string), "nil") {
		t.Errorf("health = %#v", h)
	}
	st := objOf(t, svc.Status)
	if _, ok := st["addonSalud"].(map[string]any); !ok {
		t.Errorf("status must carry the addon health: %#v", st["addonSalud"])
	}

	none := svService(t, charLua(`["character"] = { ["class"] = "Shaman" }`))
	if objOf(t, none.Health)["available"] != false {
		t.Error("missing health block must be unavailable")
	}
}

func TestSessionSummaryFromLastSavedSession(t *testing.T) {
	svc, st := fixtureService(t)
	start := time.Date(2026, 11, 7, 18, 2, 0, 0, time.Local)
	se := &store.Session{
		StartedAt:       start,
		LastDataAt:      start.Add(47 * time.Minute),
		EndedAt:         start.Add(47 * time.Minute),
		StartLevel:      38,
		EndLevel:        40,
		LevelsGained:    2,
		XPGainedPct:     35.4,
		MoneyStart:      1000,
		MoneyEnd:        3500,
		QuestsCompleted: []int64{90001, 90002},
		QuestsTurnedIn:  []int64{90002},
		ZoneMapIDs:      []int{85, 1},
		Deaths:          1,
	}
	if err := st.SaveSession(se); err != nil {
		t.Fatal(err)
	}
	m := objOf(t, svc.SessionSummary)
	if m["available"] != true || m["enCurso"] != false || m["nivelesSubidos"] != 2 {
		t.Fatalf("summary = %#v", m)
	}
	if m["dineroCambio"] != int64(2500) || m["dineroCambioTexto"] != "+25 plata" {
		t.Errorf("money = %v / %v", m["dineroCambio"], m["dineroCambioTexto"])
	}
	text := m["texto"].(string)
	for _, want := range []string{"Sesión de", "(47 min)", "+2 niveles (nivel 38 → 40)", "+35 % de nivel",
		"Oro: +25 plata", "completadas 2 (The Warchief's Orders, Herbs for the Shamans)", "entregadas 1 (Herbs for the Shamans)",
		"Zonas: Orgrimmar, Durotar", "Muertes: 1."} {
		if !strings.Contains(text, want) {
			t.Errorf("summary misses %q:\n%s", want, text)
		}
	}
}

func TestSessionSummaryWithoutSessionSaysSo(t *testing.T) {
	svc, _ := fixtureService(t)
	m := objOf(t, svc.SessionSummary)
	if m["available"] != false || m["hint"] == nil {
		t.Fatalf("no session must be unavailable with a hint: %#v", m)
	}
}

func TestSessionTextListsAndSigns(t *testing.T) {
	text := sessionText(sessionLines{
		Active:     true,
		Start:      time.Date(2026, 11, 7, 9, 30, 0, 0, time.Local),
		Minutes:    5,
		StartLevel: 10, EndLevel: 10,
		MoneyDelta: -2500,
		Completed:  []string{"A", "B", "C", "D", "E"},
		Zones:      []string{"Z1"},
	})
	for _, want := range []string{"Sesión en curso desde las 09:30 (5 min).", "XP: sin cambios.", "Oro: -25 plata.",
		"completadas 5 (A, B, C y 2 más)", "Zonas: Z1.", "Muertes: ninguna."} {
		if !strings.Contains(text, want) {
			t.Errorf("text misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "entregadas") {
		t.Errorf("no turn-ins, no line: %s", text)
	}
}

func TestTrimTextKeepsWholeRunes(t *testing.T) {
	got := trimText("ñandú ñandú ñandú", 6)
	if got != "ñandú…" {
		t.Errorf("trimText = %q", got)
	}
	if trimText("corto", 10) != "corto" {
		t.Error("short text must be unchanged")
	}
}
