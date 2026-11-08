package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNoticeExpiresAfterTTL(t *testing.T) {
	set := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	if noticeExpired(set, set.Add(noticeTTL-time.Millisecond)) {
		t.Error("notice expired before 5 s")
	}
	if !noticeExpired(set, set.Add(noticeTTL)) {
		t.Error("notice still shown at 5 s")
	}
	if noticeExpired(time.Time{}, set) {
		t.Error("a notice that was never set reports as expired")
	}
}

func TestFriendlyErrorSummaries(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"no provider answered",
			fmt.Errorf("ningún proveedor de IA respondió: agy: salió con código 1; codex: tiempo agotado"),
			"No respondió ningún proveedor; revisa que hayas iniciado sesión en agy/codex o la clave de NVIDIA."},
		{"no providers configured",
			errors.New("no hay proveedores de IA configurados (revisa providerOrder y apiProviders)"),
			"No hay proveedores de IA configurados; revisa providerOrder y apiProviders en config.json."},
		{"deadline",
			fmt.Errorf("consulta cancelada: %w", context.DeadlineExceeded),
			"La respuesta tardó demasiado (más de 2 min 30 s). Inténtalo de nuevo."},
		{"cancelled",
			fmt.Errorf("consulta cancelada: %w", context.Canceled),
			"Pregunta cancelada."},
		{"unknown", errors.New("panic: nil pointer deref at internal/ai/x.go:12"),
			"No pude responder. El detalle está en companero.log."},
	}
	for _, c := range cases {
		got := friendlyError(c.err)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		if strings.ContainsAny(got, "\n") || strings.Contains(got, "panic") {
			t.Errorf("%s: not a one-line summary: %q", c.name, got)
		}
	}
	if askError(nil) != "" {
		t.Error("askError(nil) is not empty")
	}
}
