package gamedata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spanishBook is a warrior's book on a Spanish client: names in Spanish, ids of the same spells.
var spanishBook = MacroBook{
	Spells: []SpellRef{
		{ID: 7384, Name: "Abrumar"},
		{ID: 5308, Name: "Ejecutar"},
		{ID: 78, Name: "Golpe heroico"},
		{ID: 100, Name: "Cargar"},
		{ID: 772, Name: "Desgarrar"},
		{ID: 2457, Name: "Posición de batalla"},
		{ID: 71, Name: "Posición defensiva"},
	},
	Items:      []string{"Poción de curación menor", "Espada de hierro"},
	HaveSpells: true,
	HaveItems:  true,
}

func hasProblem(c MacroCheck, fragment string) bool {
	for _, p := range c.Problems {
		if strings.Contains(p, fragment) {
			return true
		}
	}
	return false
}

func hasWarning(c MacroCheck, fragment string) bool {
	for _, w := range c.Warnings {
		if strings.Contains(w, fragment) {
			return true
		}
	}
	return false
}

func TestCheckMacroAcceptsSpanishStanceDance(t *testing.T) {
	c := CheckMacro("#showtooltip Abrumar\n/cast [stance:2] Abrumar; Golpe heroico", spanishBook)
	if !c.OK() {
		t.Fatalf("problems: %v", c.Problems)
	}
	if len(c.Names) != 2 || c.Names[0] != "Abrumar" || c.Names[1] != "Golpe heroico" {
		t.Errorf("names = %v", c.Names)
	}
	if len(c.Warnings) != 0 {
		t.Errorf("warnings = %v", c.Warnings)
	}
}

func TestCheckMacroRefusesEnglishNames(t *testing.T) {
	c := CheckMacro("/cast [stance:2] Overpower\n/cast Heroic Strike", spanishBook)
	if c.OK() {
		t.Fatal("English names must not pass")
	}
	if !hasProblem(c, "«Overpower» no está en tu libro de hechizos") {
		t.Errorf("problems = %v", c.Problems)
	}
	if !hasProblem(c, "«Heroic Strike»") {
		t.Errorf("problems = %v", c.Problems)
	}
}

func TestCheckMacroSuggestsCloseSpanishName(t *testing.T) {
	c := CheckMacro("/cast Ejecutr", spanishBook)
	if !hasProblem(c, "¿quisiste decir «Ejecutar»?") {
		t.Errorf("no suggestion: %v", c.Problems)
	}
}

func TestCheckMacroRefusesAutomation(t *testing.T) {
	for _, body := range []string{
		"/click ActionButton1",
		"/run SendChatMessage('x')",
		"/script DoSomething()",
		"/dostring print(1)",
		"/runmacro Otra",
	} {
		c := CheckMacro(body, spanishBook)
		if c.OK() {
			t.Errorf("%q must be refused", body)
		}
	}
}

func TestCheckMacroRefusesTwoUnconditionalCasts(t *testing.T) {
	c := CheckMacro("/cast Abrumar\n/cast Ejecutar", spanishBook)
	if !hasProblem(c, "más de un hechizo o acción sin condiciones") {
		t.Errorf("problems = %v", c.Problems)
	}
	ok := CheckMacro("/cast [mod:shift] Abrumar; Ejecutar", spanishBook)
	if !ok.OK() {
		t.Errorf("one conditional and one fallback must pass: %v", ok.Problems)
	}
}

func TestCheckMacroLengthLimit(t *testing.T) {
	long := strings.Repeat("/cast Abrumar\n", 20)
	c := CheckMacro(long, spanishBook)
	if !hasProblem(c, "máximo 255") {
		t.Errorf("problems = %v", c.Problems)
	}
	fits := CheckMacro("/cast Abrumar\n/cast [mod:shift] Cargar", spanishBook)
	if hasProblem(fits, "caracteres") {
		t.Errorf("short macro refused for length: %v", fits.Problems)
	}
}

func TestCheckMacroConditionals(t *testing.T) {
	valid := []string{
		"/cast [mod:shift] Abrumar",
		"/cast [@mouseover,harm,nodead] Abrumar",
		"/cast [stance:1/2/3] Cargar",
		"/cast [combat] Abrumar",
		"/cast [nocombat] Abrumar",
		"/cast [exists] Abrumar",
		"/cast [help] Abrumar",
		"/cast [harm] Abrumar",
		"/cast [dead] Abrumar",
		"/cast [@player] Abrumar",
		"/cast [@focus,exists] Abrumar",
		"/cast [mod:shift/ctrl] Abrumar",
	}
	for _, l := range valid {
		if c := CheckMacro(l, spanishBook); !c.OK() {
			t.Errorf("%s: %v", l, c.Problems)
		}
	}
	c := CheckMacro("/cast [sorcery] Abrumar", spanishBook)
	if !hasProblem(c, "condición no reconocida [sorcery]") {
		t.Errorf("unknown conditional: %v", c.Problems)
	}
}

func TestCheckMacroCommandsAndItems(t *testing.T) {
	if c := CheckMacro("/castsequence reset=combat Golpe heroico, Abrumar", spanishBook); !c.OK() {
		t.Errorf("castsequence: %v", c.Problems)
	}
	if c := CheckMacro("/equip Espada de hierro", spanishBook); !c.OK() {
		t.Errorf("equip owned item: %v", c.Problems)
	}
	if c := CheckMacro("/equip Espada de acero", spanishBook); c.OK() {
		t.Error("equip of an item not in the bags must be refused")
	}
	if c := CheckMacro("/use Poción de curación menor", spanishBook); !c.OK() {
		t.Errorf("use of a bag item: %v", c.Problems)
	}
	if c := CheckMacro("/stopattack\n/targetenemy\n/startattack", spanishBook); !c.OK() {
		t.Errorf("targeting commands: %v", c.Problems)
	}
	if c := CheckMacro("/foo Abrumar", spanishBook); c.OK() {
		t.Error("unknown command must be refused")
	}
	if c := CheckMacro("Abrumar", spanishBook); c.OK() {
		t.Error("a line without / or # must be refused")
	}
	if c := CheckMacro("/cast [mod:shift]", spanishBook); c.OK() {
		t.Error("a cast without a name must be refused")
	}
	if c := CheckMacro("   \n  ", spanishBook); c.OK() {
		t.Error("an empty body must be refused")
	}
}

func TestCheckMacroShowtooltipAdvice(t *testing.T) {
	c := CheckMacro("/cast Abrumar", spanishBook)
	if !c.OK() || !hasWarning(c, "falta #showtooltip") {
		t.Errorf("missing showtooltip must warn, not refuse: ok=%v warnings=%v", c.OK(), c.Warnings)
	}
	late := CheckMacro("/cast Abrumar\n#showtooltip Abrumar", spanishBook)
	if !hasWarning(late, "primera línea") {
		t.Errorf("late showtooltip: %v", late.Warnings)
	}
}

func TestCheckMacroWithoutBookWarnsInsteadOfRefusing(t *testing.T) {
	c := CheckMacro("/cast Overpower", MacroBook{})
	if !c.OK() {
		t.Errorf("without a book names cannot be refused: %v", c.Problems)
	}
	if !hasWarning(c, "no compruebo los nombres") {
		t.Errorf("warnings = %v", c.Warnings)
	}
}

func TestMacroNamesListsSpellsAndItems(t *testing.T) {
	got := MacroNames("#showtooltip Abrumar\n/cast [stance:2] Abrumar; Golpe heroico\n/equip Espada de hierro\n/stopattack")
	want := []string{"Abrumar", "Golpe heroico", "Espada de hierro"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("MacroNames = %v, want %v", got, want)
	}
}

func TestResolvePlaceholders(t *testing.T) {
	out, probs := ResolveMacroPlaceholders("/cast [stance:2] {overpower}", spanishBook)
	if out != "/cast [stance:2] Abrumar" || len(probs) != 0 {
		t.Errorf("key: %q %v", out, probs)
	}
	out, probs = ResolveMacroPlaceholders("/cast {id:5308}", spanishBook)
	if out != "/cast Ejecutar" || len(probs) != 0 {
		t.Errorf("id: %q %v", out, probs)
	}
	// The highest rank the player knows wins: Heroic Strike rank 1 (78) is the one in the book.
	out, _ = ResolveMacroPlaceholders("{heroic_strike}", spanishBook)
	if out != "Golpe heroico" {
		t.Errorf("heroic strike: %q", out)
	}
	_, probs = ResolveMacroPlaceholders("/cast {execute}", MacroBook{Spells: []SpellRef{{ID: 7384, Name: "Abrumar"}}, HaveSpells: true})
	if len(probs) != 1 || !strings.Contains(probs[0], "execute") {
		t.Errorf("unknown ability must be a problem: %v", probs)
	}
	_, probs = ResolveMacroPlaceholders("/cast {sorcery}", spanishBook)
	if len(probs) != 1 {
		t.Errorf("unknown key: %v", probs)
	}
	_, probs = ResolveMacroPlaceholders("/cast {id:99999}", spanishBook)
	if len(probs) != 1 {
		t.Errorf("unknown id: %v", probs)
	}
}

// macrosFixture is a character with a Spanish book, bags and two exported macros (account and character).
const macrosFixture = `CompaneroWoWDB = { ["characters"] = { ["Thrall-Forever"] = { ["updatedAt"] = 1795000000,
	["character"] = { ["class"] = "Warrior", ["level"] = 20 },
	["spells"] = {
		{ ["spellID"] = 7384, ["name"] = "Abrumar", ["rank"] = "" },
		{ ["spellID"] = 5308, ["name"] = "Ejecutar", ["rank"] = "" },
	},
	["inventory"] = { ["items"] = { { ["bag"] = 0, ["slot"] = 1, ["itemID"] = 118, ["name"] = "Poción de curación menor" } } },
	["equipment"] = { [16] = { ["slotID"] = 16, ["itemID"] = 17070, ["name"] = "Espada de hierro" } },
	["macros"] = {
		["account"] = { ["used"] = 1, ["max"] = 120, ["list"] = { { ["index"] = 1, ["name"] = "Global", ["icon"] = 134400, ["body"] = "/cast Ejecutar" } } },
		["character"] = { ["used"] = 2, ["max"] = 18, ["list"] = {
			{ ["index"] = 121, ["name"] = "Tanque", ["icon"] = 134400, ["body"] = "#showtooltip Abrumar\n/cast [stance:2] Abrumar; Overpower" },
			{ ["index"] = 122, ["name"] = "Limpia", ["icon"] = 134400, ["body"] = "/cast Ejecutr" },
		} },
	},
} } }`

func macrosService(t *testing.T, lua string) *Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "CompaneroWoW.lua")
	if err := os.WriteFile(path, []byte(lua), 0o644); err != nil {
		t.Fatal(err)
	}
	return rotationService(t, path)
}

func TestMacroBookOfReadsSpellsAndBags(t *testing.T) {
	svc := macrosService(t, macrosFixture)
	book, ok := svc.MacroBookOf()
	if !ok || !book.HaveSpells || !book.HaveItems {
		t.Fatalf("book ok=%v %+v", ok, book)
	}
	if len(book.Spells) != 2 || book.Spells[0].Name != "Abrumar" || book.Spells[0].ID != 7384 {
		t.Errorf("spells = %+v", book.Spells)
	}
	found := map[string]bool{}
	for _, it := range book.Items {
		found[it] = true
	}
	if !found["Poción de curación menor"] || !found["Espada de hierro"] {
		t.Errorf("items = %v", book.Items)
	}
}

func TestMacrosOfReadsExportedMacros(t *testing.T) {
	svc := macrosService(t, macrosFixture)
	list, usedA, usedC, maxA, maxC, ok, hint := svc.MacrosOf()
	if !ok || hint != "" {
		t.Fatalf("ok=%v hint=%q", ok, hint)
	}
	if usedA != 1 || usedC != 2 || maxA != 120 || maxC != 18 {
		t.Errorf("counts = %d %d %d %d", usedA, usedC, maxA, maxC)
	}
	if len(list) != 3 || list[0].Scope != "account" || list[1].Name != "Tanque" || list[1].Index != 121 {
		t.Errorf("list = %+v", list)
	}
	m := objOf(t, svc.Macros)
	if m["available"] != true {
		t.Errorf("Macros = %#v", m)
	}
}

func TestMacrosWithoutExportHintsSync(t *testing.T) {
	svc := macrosService(t, `CompaneroWoWDB = { ["characters"] = { ["Thrall-Forever"] = { ["updatedAt"] = 1795000000, ["character"] = { ["class"] = "Warrior" } } } }`)
	_, _, _, _, _, ok, hint := svc.MacrosOf()
	if ok || hint != HintMacros {
		t.Errorf("ok=%v hint=%q", ok, hint)
	}
	m := objOf(t, svc.Macros)
	if m["available"] != false || m["hint"] != HintMacros {
		t.Errorf("Macros = %#v", m)
	}
}

func TestLiveMacrosBlockReplacesSavedOne(t *testing.T) {
	merged := map[string]any{"macros": map[string]any{"available": false}}
	block := map[string]any{"account": map[string]any{"used": float64(0)}}
	if !applyLiveBlock(merged, msgMacros, block) {
		t.Fatal("macros block not applied")
	}
	if got := merged["macros"].(map[string]any); got["account"] == nil {
		t.Errorf("macros = %#v", got)
	}
}
