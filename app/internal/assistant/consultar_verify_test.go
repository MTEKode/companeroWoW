package assistant

import (
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/eval"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// The rows of consultar_db are grounded data: a name quoted from a row is backed by the row, even
// when the JSON escapes it (a quote or a backslash inside the value).
func TestConsultarRowsAreGroundedData(t *testing.T) {
	calls := []tools.CallRecord{{
		Name: "consultar_db",
		Text: `{"columnas":["vendedor","objeto"],"filas":[["Mercader \"Lo\\bo\"","Tela de lino"]],"n":1,"truncado":false}`,
	}}
	ev := eval.EvidenceFor(outputsOf(calls), "")
	answer := "Compra «Tela de lino» a «Mercader \"Lo\\bo\"»."
	if v := groundingOf(answer, ev); len(v) != 0 {
		t.Fatalf("grounded row values reported as unbacked: %+v", v)
	}
	// A name that is not in any row is still reported.
	if v := groundingOf("Compra «Espada inventada».", ev); len(v) == 0 {
		t.Fatal("an unbacked quoted name passed the check")
	}
}

func TestDBRowsTextFlattensRows(t *testing.T) {
	got := dbRowsText(`{"columnas":["a","b"],"filas":[["x",1.5],[null,"y"]],"n":2,"truncado":false}`)
	want := "a | b\nx | 1.5\n | y"
	if got != want {
		t.Fatalf("dbRowsText = %q, want %q", got, want)
	}
	if strings.Contains(got, `\"`) {
		t.Fatalf("JSON escapes left in the text: %q", got)
	}
	if plain := dbRowsText("no es json"); plain != "no es json" {
		t.Fatalf("a non-JSON output changed: %q", plain)
	}
}
