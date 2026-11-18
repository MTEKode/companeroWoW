package skill

import (
	"strings"
	"testing"
)

// block returns the text of one intent playbook block (between its pb marker and the close).
func block(t *testing.T, name string) string {
	t.Helper()
	open := "<!-- pb:" + name + " -->"
	i := strings.Index(Body(), open)
	if i < 0 {
		t.Fatalf("no playbook block %s", name)
	}
	rest := Body()[i+len(open):]
	j := strings.Index(rest, "<!-- /pb -->")
	if j < 0 {
		t.Fatalf("block %s is not closed", name)
	}
	return rest[:j]
}

// The world tools and their playbooks are part of the instructions, each in the intent block that
// the per-intent excerpt reads: travel, trade, training and quests.
func TestWorldToolsAndPlaybooksAreInTheSkill(t *testing.T) {
	for _, c := range []struct {
		block string
		wants []string
	}{
		{"viaje.npc", []string{"`ruta_viaje`", "(viaje) \"¿cómo llego a X?\""}},
		{"comercio.oro", []string{"`donde_comprar`", "(compra) \"¿dónde compro X?\"", "`consumibles`", "(consumibles)"}},
		{"profesiones.entrenar", []string{"`coste_entrenamiento`", "(coste) \"¿cuánto me cuesta entrenar?\""}},
		{"misiones.estado", []string{"`cadena_mision`", "(cadena) \"¿qué misiones hago antes de X?\""}},
	} {
		text := block(t, c.block)
		for _, want := range c.wants {
			if !strings.Contains(text, want) {
				t.Errorf("block %s lacks %q", c.block, want)
			}
		}
	}
	for _, tool := range []string{"`ruta_viaje`", "`donde_comprar`", "`coste_entrenamiento`", "`cadena_mision`", "`consumibles`"} {
		if !strings.Contains(Body(), tool) {
			t.Errorf("skill lacks the tool %s", tool)
		}
	}
}
