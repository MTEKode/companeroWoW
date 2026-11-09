package eval

import (
	"strings"
	"testing"
)

// barsEvidence is what consejo_barras returns for the spells the command may use.
const barsEvidence = `{
  "propuesta": [
    {
      "ranura": 6,
      "id": 2054,
      "nombre": "Heal"
    }
  ],
  "comandos": ["/compa barras aplicar 6:2054"]
}`

func TestBarCommandCheckAcceptsKnownSpells(t *testing.T) {
	ans := "Propuesta: Heal en la ranura 6.\n⌨️ /compa barras aplicar 6:2054\nFuera de combate."
	ev := Evidence{Output: barsEvidence}
	ok, has := checkOK(checkAnswer(question("q20"), ans, ev, nil), "comando_barras")
	if !has || !ok {
		t.Fatal("a command with a known spell id must pass")
	}
}

func TestBarCommandCheckRejectsBadCommands(t *testing.T) {
	ev := Evidence{Output: barsEvidence}
	cases := map[string]string{
		"sin comando":        "Coloca Heal en la ranura 6.",
		"id desconocido":     "⌨️ /compa barras aplicar 6:9999",
		"ranura cero":        "⌨️ /compa barras aplicar 0:2054",
		"ranura de 3 cifras": "⌨️ /compa barras aplicar 121:2054",
		"par mal formado":    "⌨️ /compa barras aplicar 6-2054",
		"demasiado largo":    "⌨️ /compa barras aplicar " + strings.TrimSuffix(strings.Repeat("6:2054,", 40), ","),
	}
	for name, ans := range cases {
		if c := barCommandCheck(ans, ev); c.OK {
			t.Errorf("%s: must fail, got %q", name, c.Detail)
		}
	}
}

func TestBarCommandCheckLimitsPairs(t *testing.T) {
	var pairs []string
	for i := 1; i <= 41; i++ {
		pairs = append(pairs, "1:2054")
	}
	ans := "⌨️ /compa barras aplicar " + strings.Join(pairs, ",")
	if c := barCommandCheck(ans, Evidence{Output: barsEvidence}); c.OK {
		t.Errorf("41 pairs must fail: %s", c.Detail)
	}
}

func TestSpellsCheckNeedsTwoSpellsFromTheBook(t *testing.T) {
	ev := Evidence{Output: `{
  "hechizos": [
    {"nombre": "Smite", "id": 585},
    {"nombre": "Shadow Word: Pain", "id": 589},
    {"nombre": "Heal", "id": 2054}
  ],
  "instrucciones": "..."
}`}
	if c := spellsCheck("Prioridades: «Smite» y «Shadow Word: Pain» en orden.", ev); !c.OK {
		t.Errorf("two spells from the book must pass: %s", c.Detail)
	}
	if c := spellsCheck("Usa Smite y luego Penitencia.", ev); c.OK {
		t.Errorf("one spell from the book must fail: %s", c.Detail)
	}
}
