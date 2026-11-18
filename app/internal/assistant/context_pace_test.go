package assistant

import (
	"strings"
	"testing"
)

// The leveling pace is part of the player context that every question carries, so it must
// survive the compact block.
func TestPaceReachesTheCompactContext(t *testing.T) {
	v := map[string]any{
		"ubicacion": map[string]any{"zona": "Orgrimmar"},
		"ritmo": map[string]any{
			"xpHoraTexto":    "42.0 % de nivel por hora (últimos 30 min)",
			"siguienteNivel": "1 h 20 min",
		},
	}
	out := compactContext(v, contextLimit)
	if !strings.Contains(out, `"ritmo"`) || !strings.Contains(out, "1 h 20 min") {
		t.Errorf("the pace must be in the compact context: %s", out)
	}
}
