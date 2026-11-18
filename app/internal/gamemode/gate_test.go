package gamemode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGateAcceptsOnlyNewerIds(t *testing.T) {
	g, err := NewGate("")
	if err != nil {
		t.Fatal(err)
	}
	mustAccept(t, g, "Ana", 1, true)
	mustAccept(t, g, "Ana", 1, false) // the same question sent again every 2 s
	mustAccept(t, g, "Ana", 2, true)
	mustAccept(t, g, "Ana", 1, false) // an older question
	mustAccept(t, g, "Ana", 0, false) // no id
	if got := g.Last("Ana"); got != 2 {
		t.Fatalf("Last = %d, want 2", got)
	}
}

func TestGateKeepsCharactersApart(t *testing.T) {
	g, _ := NewGate("")
	mustAccept(t, g, "Ana", 5, true)
	mustAccept(t, g, "Luis", 1, true) // another character starts its own count
	mustAccept(t, g, "Luis", 1, false)
}

func TestGatePersistsAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "questions.json")
	g, err := NewGate(path)
	if err != nil {
		t.Fatal(err)
	}
	mustAccept(t, g, "Ana", 7, true)
	mustAccept(t, g, "Luis", 3, true)

	again, err := NewGate(path)
	if err != nil {
		t.Fatal(err)
	}
	mustAccept(t, again, "Ana", 7, false) // answered before the restart: not again
	mustAccept(t, again, "Ana", 8, true)
	mustAccept(t, again, "Luis", 3, false)
}

func TestGateDamagedFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "questions.json")
	if err := os.WriteFile(path, []byte("{no es json"), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := NewGate(path)
	if err == nil {
		t.Fatal("a damaged file should be reported")
	}
	mustAccept(t, g, "Ana", 1, true)
}

func mustAccept(t *testing.T, g *Gate, key string, id uint64, want bool) {
	t.Helper()
	got, err := g.Accept(key, id)
	if err != nil {
		t.Fatalf("Accept(%q, %d): %v", key, id, err)
	}
	if got != want {
		t.Fatalf("Accept(%q, %d) = %v, want %v", key, id, got, want)
	}
}
