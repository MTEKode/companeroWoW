package tools_test

import (
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

func TestQueEntrenarAnswersWithSpellsAndTrainer(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	text := textOf(t, call(t, reg, "que_entrenar", ""))
	for _, want := range []string{`"available": true`, "Frost Shock", "Healing Wave", "Korla Windtotem", `"costeAhoraTexto": "70 plata"`} {
		if !strings.Contains(text, want) {
			t.Errorf("que_entrenar misses %q:\n%s", want, text)
		}
	}
}

func TestRutaProfesionAcceptsNameAndSkillLineAliases(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	byName := textOf(t, call(t, reg, "ruta_profesion", `{"profesion":"Sastrería"}`))
	if !strings.Contains(byName, "Pantalones de lana") || !strings.Contains(byName, `"encontrada": true`) {
		t.Errorf("by name:\n%s", byName)
	}
	for _, args := range []string{`{"skill_line_id":197}`, `{"skillLineID":"197"}`, `{"profession":"sastrería"}`} {
		text := textOf(t, call(t, reg, "ruta_profesion", args))
		if !strings.Contains(text, "Sastrería") || !strings.Contains(text, `"encontrada": true`) {
			t.Errorf("args %s:\n%s", args, text)
		}
	}
	missing := textOf(t, call(t, reg, "ruta_profesion", `{"profesion":"Cocina"}`))
	if !strings.Contains(missing, `"encontrada": false`) {
		t.Errorf("unknown profession:\n%s", missing)
	}
}

func TestRutaProfesionRejectsBadSkillLine(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	res := call(t, reg, "ruta_profesion", `{"skill_line_id":"no es un número"}`)
	if !res.IsError || !strings.Contains(textOf(t, res), "skill_line_id") {
		t.Errorf("bad skill line must be an error result: %+v", res)
	}
}

func TestResumenSesionWithoutSessionSaysSo(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	text := textOf(t, call(t, reg, "resumen_sesion", ""))
	if !strings.Contains(text, `"available": false`) || !strings.Contains(text, "Aún no hay sesiones") {
		t.Errorf("resumen_sesion without data:\n%s", text)
	}
}
