package assistant

import (
	"context"
	"testing"
)

func TestDataNoteRoundTripsThroughContext(t *testing.T) {
	if got := dataNote(context.Background()); got != "" {
		t.Fatalf("no note expected, got %q", got)
	}
	ctx := WithDataNote(context.Background(), "Datos del juego no actualizados: personaje (datos de hace 3 min).")
	if got := dataNote(ctx); got != "Datos del juego no actualizados: personaje (datos de hace 3 min)." {
		t.Fatalf("note = %q", got)
	}
	if WithDataNote(context.Background(), "") != context.Background() {
		t.Fatal("an empty note must leave the context unchanged")
	}
}
