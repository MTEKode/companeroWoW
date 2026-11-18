package tools

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// chunkPayload must keep every piece within the budget, never cut next to a space (the chat may drop
// a trailing space) and never split a UTF-8 character; joining the pieces gives the payload back.
func TestChunkPayloadRoundTripsWithinBudget(t *testing.T) {
	payloads := []string{
		strings.Repeat("/cast [mod:shift] Golpe heroico\\n", 12) + "Posición defensiva ñandú Ñu",
		strings.Repeat("ábcdéfghíjklmnñopqrstuvwxyz0123456789 ", 20),
		strings.Repeat("x", 700),
	}
	for _, p := range payloads {
		for _, budget := range []int{40, 97, 230} {
			pieces := chunkPayload(p, budget)
			if strings.Join(pieces, "") != p {
				t.Fatalf("budget %d: join differs", budget)
			}
			for i, piece := range pieces {
				if len(piece) > budget {
					t.Errorf("budget %d: piece %d is %d bytes", budget, i, len(piece))
				}
				if !utf8.ValidString(piece) {
					t.Errorf("budget %d: piece %d splits a UTF-8 character", budget, i)
				}
				if i < len(pieces)-1 && (strings.HasSuffix(piece, " ") || strings.HasPrefix(pieces[i+1], " ")) {
					t.Errorf("budget %d: boundary next to a space at piece %d", budget, i)
				}
			}
		}
	}
}

func TestChunkPayloadShortPayloadIsOnePiece(t *testing.T) {
	if got := chunkPayload("corto", 230); len(got) != 1 || got[0] != "corto" {
		t.Errorf("chunkPayload = %q", got)
	}
}
