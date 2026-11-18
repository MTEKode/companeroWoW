package skill

import (
	"strings"
	"testing"
)

// The world tools (flights, vendors, trainers, chains, consumables) and their playbooks are part
// of the instructions the assistant follows.
func TestWorldToolsAndPlaybooksAreInTheSkill(t *testing.T) {
	body := Body()
	for _, want := range []string{
		"`ruta_viaje`", "`donde_comprar`", "`coste_entrenamiento`", "`cadena_mision`", "`consumibles`",
		"(v) \"¿cómo llego a X?\"", "(w) \"¿dónde compro X?\"", "(y) \"¿cuánto me cuesta entrenar?\"",
		"(z) \"¿qué misiones hago antes de X?\"", "(u) \"¿qué me conviene llevar?\"",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("skill lacks %q", want)
		}
	}
}
