package gamedata

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// rotationFixture is a priest at level 19 with ten recorded combats (one death) and action bars with
// bindings. It is separate from CompaneroWoW.lua, which the other tests depend on.
const rotationFixture = "../../testdata/CompaneroWoW_rotacion.lua"

func rotationService(t *testing.T, path string) *Service {
	t.Helper()
	w := bridge.NewSVWatcher(path, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll %s: %v", path, err)
	}
	return NewService(nil, w, nil)
}

func hints(t *testing.T, m map[string]any) []string {
	t.Helper()
	list, ok := m["pistas"].([]string)
	if !ok {
		t.Fatalf("pistas = %T, want []string", m["pistas"])
	}
	return list
}

func hasHint(list []string, parts ...string) bool {
	for _, h := range list {
		ok := true
		for _, p := range parts {
			if !strings.Contains(h, p) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func TestRotationListsKnownSpellsWithTags(t *testing.T) {
	svc := rotationService(t, rotationFixture)
	m := objOf(t, svc.Rotation)
	if m["available"] != true {
		t.Fatalf("rotation unavailable: %#v", m)
	}
	if m["clase"] != "Priest" || m["nivel"] != 19 {
		t.Errorf("clase/nivel = %v/%v", m["clase"], m["nivel"])
	}
	if m["busquedaWeb"] != "Priest leveling rotation Forever" {
		t.Errorf("busquedaWeb = %v", m["busquedaWeb"])
	}
	if m["talentos"] != "2/3/16" {
		t.Errorf("talentos = %v", m["talentos"])
	}
	if m["especializacion"] != "árbol 3 (2/3/16)" || m["especializacionSupuesta"] != true {
		t.Errorf("especializacion = %v (supuesta %v)", m["especializacion"], m["especializacionSupuesta"])
	}
	spells, _ := m["hechizos"].([]map[string]any)
	if len(spells) != 13 || m["numeroHechizos"] != 13 {
		t.Fatalf("hechizos = %d", len(spells))
	}
	tags := map[string][]string{}
	for _, s := range spells {
		if roles, ok := s["etiquetas"].([]string); ok {
			tags[s["nombre"].(string)] = roles
		}
	}
	want := map[string]string{
		"Smite": "daño", "Power Word: Shield": "defensivo", "Heal": "curación", "Psychic Scream": "control", "Dispel Magic": "utilidad",
	}
	for name, role := range want {
		if !containsString(tags[name], role) {
			t.Errorf("%s tags = %v, want %s", name, tags[name], role)
		}
	}
	if !strings.Contains(m["instrucciones"].(string), "buscar_web") {
		t.Error("instructions must point to buscar_web")
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestSpellRolesFromNames(t *testing.T) {
	cases := map[string][]string{
		"Power Word: Shield": {"defensivo"},
		"Lesser Heal":        {"curación"},
		"Psychic Scream":     {"control"},
		"Smite":              {"daño"},
		"Inner Fire":         {"utilidad"},
		"Auto Attack":        nil,
		"Lay on Hands":       {"defensivo"},
	}
	for name, want := range cases {
		got := spellRoles(name)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("spellRoles(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestCombatReviewWindowAndFilters(t *testing.T) {
	svc := rotationService(t, rotationFixture)

	all := objOf(t, func() (any, error) { return svc.CombatReview(0, false, false) })
	combats, _ := all["combates"].([]map[string]any)
	if len(combats) != 10 || all["analizados"] != 10 || all["muertesEnAnalisis"] != 1 {
		t.Fatalf("default window: %d combats, analizados %v, muertes %v", len(combats), all["analizados"], all["muertesEnAnalisis"])
	}
	if combats[0]["id"] != int64(10) {
		t.Errorf("newest first: first id = %v", combats[0]["id"])
	}

	last := objOf(t, func() (any, error) { return svc.CombatReview(10, true, false) })
	if l, _ := last["combates"].([]map[string]any); len(l) != 1 || l[0]["id"] != int64(10) {
		t.Errorf("ultimo: %#v", last["combates"])
	}

	deaths := objOf(t, func() (any, error) { return svc.CombatReview(10, false, true) })
	d, _ := deaths["combates"].([]map[string]any)
	if len(d) != 1 || d[0]["murio"] != true || d[0]["id"] != int64(6) {
		t.Fatalf("muertes: %#v", deaths["combates"])
	}
	death, _ := d[0]["muerte"].(map[string]any)
	if death["asesino"] != "Defias Pillager" {
		t.Errorf("asesino = %v", death["asesino"])
	}
	if hits, _ := death["ultimosGolpes"].([]map[string]any); len(hits) != 3 {
		t.Errorf("ultimos golpes = %d", len(hits))
	}

	small := objOf(t, func() (any, error) { return svc.CombatReview(3, false, false) })
	if small["analizados"] != 3 {
		t.Errorf("n=3 analizados = %v", small["analizados"])
	}
}

func TestCombatHintsFromFixture(t *testing.T) {
	svc := rotationService(t, rotationFixture)
	m := objOf(t, func() (any, error) { return svc.CombatReview(10, false, false) })
	list := hints(t, m)

	if !hasHint(list, "Mind Blast", "No aparecen en ninguno") {
		t.Errorf("unused spell hint missing: %q", list)
	}
	if !hasHint(list, "Holy Fire", "poco daño") {
		t.Errorf("low share hint for Holy Fire missing: %q", list)
	}
	if !hasHint(list, "Combate 3", "96 s", "dps") {
		t.Errorf("long low-dps hint for combat 3 missing: %q", list)
	}
	if !hasHint(list, "Defias Pillager", "más del 40 %") {
		t.Errorf("dominant damage source hint missing: %q", list)
	}
	if !hasHint(list, "Muerte en el combate 6", "te mató «Defias Pillager»", "Últimos golpes") {
		t.Errorf("death hint missing: %q", list)
	}
	if !hasHint(list, "Muerte en el combate 6", "ninguno de tus defensivos conocidos", "Fade") {
		t.Errorf("unused defensives hint missing: %q", list)
	}
	if len(list) > maxReviewHints {
		t.Errorf("hints = %d, limit %d", len(list), maxReviewHints)
	}
}

func TestCombatsMissingAnswersHint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CompaneroWoW.lua")
	lua := `CompaneroWoWDB = { ["characters"] = { ["Lirael-Forever"] = { ["updatedAt"] = 1795000000,
		["character"] = { ["class"] = "Priest", ["level"] = 19 }, ["spells"] = { { ["spellID"] = 585, ["name"] = "Smite", ["rank"] = "" } } } } }`
	if err := os.WriteFile(path, []byte(lua), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := rotationService(t, path)
	m := objOf(t, func() (any, error) { return svc.CombatReview(0, false, false) })
	if m["available"] != false || m["hint"] != HintCombats {
		t.Errorf("missing combats: %#v", m)
	}
	bars := objOf(t, svc.BarsAdvice)
	if bars["available"] != false || bars["hint"] != HintBars {
		t.Errorf("missing bars: %#v", bars)
	}
}

func TestBarsAdviceLayoutAndProposal(t *testing.T) {
	svc := rotationService(t, rotationFixture)
	m := objOf(t, svc.BarsAdvice)
	if m["available"] != true {
		t.Fatalf("bars: %#v", m)
	}
	layout, _ := m["disposicion"].([]map[string]any)
	if len(layout) != 12 {
		t.Errorf("layout slots = %d, want 12", len(layout))
	}

	dups, _ := m["duplicados"].([]map[string]any)
	if len(dups) != 1 || dups[0]["nombre"] != "Smite" {
		t.Errorf("duplicates = %#v", dups)
	}

	far, _ := m["teclasLejanas"].([]map[string]any)
	farKeys := []string{}
	for _, f := range far {
		farKeys = append(farKeys, f["tecla"].(string))
	}
	if strings.Join(farKeys, ",") != "CTRL-1,SHIFT-6" {
		t.Errorf("far keys = %v", farKeys)
	}

	noKey, _ := m["sinTecla"].([]map[string]any)
	if len(noKey) != 1 || noKey[0]["nombre"] != "Dispel Magic" {
		t.Errorf("sin tecla = %#v", noKey)
	}

	missing, _ := m["noColocados"].([]map[string]any)
	if len(missing) != 1 || missing[0]["nombre"] != "Heal" || missing[0]["motivo"] != "curación" {
		t.Errorf("no colocados = %#v", missing)
	}

	prop, _ := m["propuesta"].([]map[string]any)
	if len(prop) != 1 || prop[0]["ranura"] != 6 || prop[0]["id"] != int64(2054) || prop[0]["teclaPorDefecto"] != "6" {
		t.Errorf("propuesta = %#v", prop)
	}
	cmds, _ := m["comandos"].([]string)
	if len(cmds) != 1 || cmds[0] != "/compa barras aplicar 6:2054" {
		t.Errorf("comandos = %v", cmds)
	}
}

func TestBarCommandsValidatesAgainstSpellbook(t *testing.T) {
	svc := rotationService(t, rotationFixture)
	m := objOf(t, func() (any, error) {
		return svc.BarCommands([]BarRequest{
			{Slot: 6, Spell: "heal"},
			{Slot: 12, SpellID: 8092},
			{Slot: 13, Spell: "Pyroblast"},
			{Slot: 6, Spell: "Smite"},
			{Slot: 121, Spell: "Smite"},
		})
	})
	cmds, _ := m["comandos"].([]string)
	if len(cmds) != 1 || cmds[0] != "/compa barras aplicar 6:2054,12:8092" {
		t.Errorf("comandos = %v", cmds)
	}
	errs, _ := m["errores"].([]string)
	if len(errs) != 3 {
		t.Errorf("errores = %v, want 3 (unknown, repeated slot, out of range)", errs)
	}
	if m["pares"] != 2 {
		t.Errorf("pares = %v", m["pares"])
	}
}

func TestBarCommandsLimitPairsAndLength(t *testing.T) {
	var reqs []BarRequest
	for i := 0; i < maxBarPairs+1; i++ {
		reqs = append(reqs, BarRequest{Slot: int64(i + 1), SpellID: 585})
	}
	svc := rotationService(t, rotationFixture)
	m := objOf(t, func() (any, error) { return svc.BarCommands(reqs) })
	if m["pares"] != maxBarPairs {
		t.Errorf("pares = %v, want %d", m["pares"], maxBarPairs)
	}
	errs, _ := m["errores"].([]string)
	if len(errs) != 1 || !strings.Contains(errs[0], "máximo") {
		t.Errorf("errores = %v", errs)
	}
}

func TestBarCommandsSplitBelowLimit(t *testing.T) {
	var pairs []string
	for i := 1; i <= 40; i++ {
		pairs = append(pairs, strconv.Itoa(100+i)+":"+strconv.Itoa(10000+i))
	}
	cmds := barCommands(pairs)
	if len(cmds) < 2 {
		t.Fatalf("expected a split, got %d command(s)", len(cmds))
	}
	seen := 0
	for _, c := range cmds {
		if len(c) > maxBarCommandChars {
			t.Errorf("command has %d chars: %s", len(c), c)
		}
		if !strings.HasPrefix(c, barCommandPrefix+" ") {
			t.Errorf("bad prefix: %s", c)
		}
		seen += strings.Count(c, ":")
	}
	if seen != 40 {
		t.Errorf("pairs in commands = %d, want 40", seen)
	}
}
