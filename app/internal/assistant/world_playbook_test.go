package assistant

import (
	"strings"
	"testing"
)

// The world playbooks reach the per-intent excerpt: a travel question gets the route playbook, a
// buying question the vendor and consumables ones, a training question the cost one, and a quest
// question the chain one.
func TestWorldPlaybooksReachTheIntentExcerpts(t *testing.T) {
	for _, c := range []struct {
		in   Intent
		q    string
		want string
	}{
		{IntentTravel, "¿cómo llego a Forjaz?", "ruta_viaje"},
		{IntentTrade, "¿dónde compro pan duro?", "donde_comprar"},
		{IntentTrade, "¿qué me conviene llevar para 5 peleas?", "consumibles"},
		{IntentProfessions, "¿cuánto me cuesta entrenar Sastrería?", "coste_entrenamiento"},
		{IntentQuests, "¿qué misiones hago antes de esta?", "cadena_mision"},
	} {
		got := playbookFor(c.in, c.q)
		if !strings.Contains(got, c.want) {
			t.Errorf("playbookFor(%s, %q) lacks %s:\n%s", c.in, c.q, c.want, got)
		}
	}
}
