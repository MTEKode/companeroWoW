package tools_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

const macrosFixture = "../../testdata/CompaneroWoW_macros.lua"

// macrosRegistry builds the tools over the macros fixture: a Spanish warrior book, bags and four
// exported macros.
func macrosRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	w := bridge.NewSVWatcher(filepath.Clean(macrosFixture), time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll fixture: %v", err)
	}
	return tools.Build(gamedata.NewService(nil, w, nil), nil)
}

// decode reads a tool's JSON result into a map.
func decode(t *testing.T, res tools.Result) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res)), &m); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, textOf(t, res))
	}
	return m
}

func problemsOf(m map[string]any) []string {
	var out []string
	list, _ := m["problemas"].([]any)
	for _, p := range list {
		out = append(out, p.(string))
	}
	return out
}

func anyContains(list []string, fragment string) bool {
	for _, s := range list {
		if strings.Contains(s, fragment) {
			return true
		}
	}
	return false
}

func TestCrearMacroBuildsSpanishStanceDance(t *testing.T) {
	reg := macrosRegistry(t)
	m := decode(t, call(t, reg, "crear_macro", `{
		"objetivo": "Abrumar en postura de combate y Ejecutar después",
		"hechizos": [{"hechizo": "{overpower}", "condicion": "stance:2"}, "{execute}"],
		"ranura": 5
	}`))
	if m["ok"] != true {
		t.Fatalf("not ok: %v", m)
	}
	body, _ := m["cuerpo"].(string)
	if body != "#showtooltip Abrumar\n/cast [stance:2] Abrumar\n/cast Ejecutar" {
		t.Errorf("cuerpo = %q", body)
	}
	if strings.Contains(textOf(t, call(t, reg, "crear_macro", `{"objetivo":"x","lineas":["/cast [stance:2] {overpower}"]}`)), "Overpower") {
		t.Error("English names must never reach the macro")
	}
	cmds, _ := m["comandos"].([]any)
	if len(cmds) != 1 || !strings.HasPrefix(cmds[0].(string), "/compa macro crear ") {
		t.Fatalf("comandos = %v", cmds)
	}
	if !strings.Contains(cmds[0].(string), "Abrumar\\n/cast Ejecutar") {
		t.Errorf("command = %v", cmds[0])
	}
	if m["colocar"] != "/compa macro barra "+m["nombre"].(string)+" 5" {
		t.Errorf("colocar = %v", m["colocar"])
	}
	if m["icono"] != "INV_MISC_QUESTIONMARK" {
		t.Errorf("icono = %v", m["icono"])
	}
}

func TestCrearMacroRefusesEnglishAndAutomation(t *testing.T) {
	reg := macrosRegistry(t)
	m := decode(t, call(t, reg, "crear_macro", `{"objetivo":"Empuje","lineas":["/cast Overpower"]}`))
	if m["ok"] != false || !anyContains(problemsOf(m), "«Overpower»") {
		t.Errorf("English name: %v", m)
	}
	if _, has := m["comandos"]; has {
		t.Error("a refused macro must not return commands")
	}
	m = decode(t, call(t, reg, "crear_macro", `{"objetivo":"Clic","lineas":["/click ActionButton1"]}`))
	if m["ok"] != false || !anyContains(problemsOf(m), "/click") {
		t.Errorf("/click: %v", m)
	}
	m = decode(t, call(t, reg, "crear_macro", `{"objetivo":"Dos","lineas":["/cast Abrumar","/cast Ejecutar"]}`))
	if m["ok"] != false || !anyContains(problemsOf(m), "sin condiciones") {
		t.Errorf("two unconditional casts: %v", m)
	}
}

func TestCrearMacroNameLimit(t *testing.T) {
	reg := macrosRegistry(t)
	m := decode(t, call(t, reg, "crear_macro", `{"objetivo":"x","nombre":"Nombre demasiado largo para la macro","lineas":["/cast Abrumar"]}`))
	if m["ok"] != false || !anyContains(problemsOf(m), "máximo 16") {
		t.Errorf("long name: %v", m)
	}
	m = decode(t, call(t, reg, "crear_macro", `{"objetivo":"Abrumar y más","lineas":["/cast Abrumar"]}`))
	if n, _ := m["nombre"].(string); n == "" || len([]rune(n)) > 16 {
		t.Errorf("default name = %q", n)
	}
}

func TestCrearMacroChunksLongCommandsWithinChatLimit(t *testing.T) {
	reg := macrosRegistry(t)
	// Six conditional lines and one fallback: with the #showtooltip line the body is 232 bytes (under 255), but the
	// /compa macro crear command with its literal backslash-n separators is over 255, so it must be split.
	var lines []string
	for i := 0; i < 6; i++ {
		lines = append(lines, "/cast [mod:shift] Golpe heroico")
	}
	lines = append(lines, "/cast Abrumar")
	args, _ := json.Marshal(map[string]any{"objetivo": "Largo", "nombre": "Largo", "lineas": lines})
	m := decode(t, call(t, reg, "crear_macro", string(args)))
	if m["ok"] != true {
		t.Fatalf("long macro: %v", m)
	}
	cmds := m["comandos"].([]any)
	if len(cmds) < 2 {
		t.Fatalf("expected chunks, got %d command(s): %v", len(cmds), cmds)
	}
	var payload strings.Builder
	for i, c := range cmds {
		line := c.(string)
		if len(line) > 255 {
			t.Errorf("chunk %d is %d bytes (limit 255)", i+1, len(line))
		}
		prefix := "/compa macro parte " + strconv.Itoa(i+1) + "/" + strconv.Itoa(len(cmds)) + " "
		if !strings.HasPrefix(line, prefix) {
			t.Fatalf("chunk %d = %q, want prefix %q", i+1, line, prefix)
		}
		payload.WriteString(strings.TrimPrefix(line, prefix))
	}
	// The tool puts #showtooltip first (the first spell), so the payload the addon gets starts with it.
	want := "Largo|INV_MISC_QUESTIONMARK|#showtooltip Golpe heroico" + `\n` + strings.Join(lines, `\n`)
	if payload.String() != want {
		t.Errorf("joined payload differs:\n got %q\nwant %q", payload.String(), want)
	}
}

func TestCrearMacroNeedsObjetivo(t *testing.T) {
	reg := macrosRegistry(t)
	res, err := reg.Call(context.Background(), "crear_macro", json.RawMessage(`{"lineas":["/cast Abrumar"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("missing objetivo must be an error")
	}
}

func TestCrearMacroRejectsBadSlot(t *testing.T) {
	reg := macrosRegistry(t)
	res, _ := reg.Call(context.Background(), "crear_macro", json.RawMessage(`{"objetivo":"x","lineas":["/cast Abrumar"],"ranura":200}`))
	if !res.IsError {
		t.Error("slot 200 must be refused")
	}
}

func TestCrearMacroWithoutSyncHintsSync(t *testing.T) {
	w := bridge.NewSVWatcher(filepath.Join(t.TempDir(), "none.lua"), time.Hour)
	reg := tools.Build(gamedata.NewService(nil, w, nil), nil)
	m := decode(t, call(t, reg, "crear_macro", `{"objetivo":"x","lineas":["/cast Abrumar"]}`))
	if m["disponible"] != false {
		t.Errorf("no data: %v", m)
	}
}

func TestRevisarMacrosFindsProblemsAndRenames(t *testing.T) {
	reg := macrosRegistry(t)
	m := decode(t, call(t, reg, "revisar_macros", `{}`))
	if m["disponible"] != true {
		t.Fatalf("revisar: %v", m)
	}
	byName := map[string]map[string]any{}
	for _, it := range m["macros"].([]any) {
		e := it.(map[string]any)
		byName[e["nombre"].(string)] = e
	}
	if len(byName) != 5 {
		t.Fatalf("macros = %d, want 5", len(byName))
	}
	if p := problemsOf(byName["Tanque"]); len(p) != 0 {
		t.Errorf("Tanque should be clean: %v", p)
	}
	if p := problemsOf(byName["Limpia"]); !anyContains(p, "¿quisiste decir «Ejecutar»?") {
		t.Errorf("typo suggestion missing: %v", p)
	}
	if p := problemsOf(byName["Vieja"]); !anyContains(p, "«Overpower»") || !anyContains(p, "«Heroic Strike»") {
		t.Errorf("renamed/English names not found: %v", p)
	}
	if p := problemsOf(byName["Clic"]); !anyContains(p, "/click") {
		t.Errorf("/click not flagged: %v", p)
	}
	if !strings.Contains(m["resumen"].(string), "5 macros revisadas") {
		t.Errorf("resumen = %v", m["resumen"])
	}
	if byName["Global"]["ambito"] != "cuenta" || byName["Tanque"]["ambito"] != "personaje" {
		t.Errorf("ambito wrong: %v / %v", byName["Global"]["ambito"], byName["Tanque"]["ambito"])
	}
	if mejoras, _ := byName["Global"]["mejoras"].([]any); len(mejoras) == 0 {
		t.Error("a macro without #showtooltip should get an improvement")
	}
}

// The name limit counts bytes, as the game does: an accented name of 9 letters can be over 16 bytes.
func TestCrearMacroNameLimitCountsBytes(t *testing.T) {
	reg := macrosRegistry(t)
	m := decode(t, call(t, reg, "crear_macro", `{"objetivo":"x","nombre":"Ñandú rápido ñu","lineas":["/cast Abrumar"]}`))
	if m["ok"] != false || !anyContains(problemsOf(m), "máximo 16") {
		t.Errorf("accented name over 16 bytes must be refused: %v", m)
	}
	m = decode(t, call(t, reg, "crear_macro", `{"objetivo":"Ñandú rápido en postura","lineas":["/cast Abrumar"]}`))
	if n, _ := m["nombre"].(string); len(n) > 16 {
		t.Errorf("default name %q is over 16 bytes", n)
	}
}

// A | in the name or the icon would break the name|icon|body payload the addon reads.
func TestCrearMacroRefusesPipeInNameAndIcon(t *testing.T) {
	reg := macrosRegistry(t)
	m := decode(t, call(t, reg, "crear_macro", `{"objetivo":"x","nombre":"A|B","lineas":["/cast Abrumar"]}`))
	if m["ok"] != false || !anyContains(problemsOf(m), "«|»") {
		t.Errorf("pipe in name: %v", m)
	}
	m = decode(t, call(t, reg, "crear_macro", `{"objetivo":"x","nombre":"Pipa","icono":"A|B","lineas":["/cast Abrumar"]}`))
	if m["ok"] != false || !anyContains(problemsOf(m), "icono") {
		t.Errorf("pipe in icon: %v", m)
	}
}
