package gamemode

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestConvertCommandLines(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		mapID int
		want  string
	}{
		{"mark with map id", "📍 /compa marca 85 51.2 38.7 Entregar", 0, "@marca 85 51.2 38.7 Entregar"},
		{"mark takes the question's map", "📍 /compa marca 51.2 38.7 Entregar", 85, "@marca 85 51.2 38.7 Entregar"},
		{"mark without any map is text", "📍 /compa marca 51.2 38.7 Entregar", 0, "Marca: Entregar (51.2, 38.7)"},
		{"whole numbers without map id are coordinates", "📍 /compa marca 51 38 Cofre", 0, "Marca: Cofre (51, 38)"},
		{"mark text pipes are escaped", "📍 /compa marca 85 1 2 A|B", 0, "@marca 85 1 2 A||B"},
		{"barras aplicar", "⌨️ /compa barras aplicar 6:2054,7:1234", 0, "@barras 6:2054,7:1234"},
		{"macro with parts", "🧩 /compa macro crear Cocina|inv_misc_food|/cast Pan\\n/say hola", 0, `@macro Cocina|inv_misc_food|/cast Pan\n/say hola`},
		{"macro without icon gets the default", "🧩 /compa macro crear Cocina|| /cast Pan", 0, "@macro Cocina|inv_misc_questionmark|/cast Pan"},
		{"macro pipes in body are escaped", "🧩 /compa macro crear Uno|inv_x|/a b|c", 0, "@macro Uno|inv_x|/a b||c"},
		{"macro with spaces in its name", "🧩 /compa macro crear Mi cosa|inv_x|/say hola", 0, "@macro Mi cosa|inv_x|/say hola"},
		{"macro without parts takes the first word", "/compa macro crear Pan /cast Pan", 0, "@macro Pan|inv_misc_questionmark|/cast Pan"},
		{"barras deshacer is text", "⌨️ /compa barras deshacer", 0, "/compa barras deshacer"},
		{"source line", "Fuente: wowforevertalents.com", 0, "@fuente wowforevertalents.com"},
		{"bold source line", "**Fuentes:** warcraft.wiki.gg", 0, "@fuente warcraft.wiki.gg"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ConvertBody(tc.in, tc.mapID)
			if got != tc.want {
				t.Fatalf("ConvertBody(%q, %d)\n got: %q\nwant: %q", tc.in, tc.mapID, got, tc.want)
			}
		})
	}
}

func TestConvertTextRules(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bold stays", "**Ya:** Pesca en el lago", "**Ya:** Pesca en el lago"},
		{"bullets become dashes", "• Casco\n* Anillo\n◦ Capa", "- Casco\n- Anillo\n- Capa"},
		{"pipes escaped", "a | b", "a || b"},
		{"emoji removed", "✅ Hecho 🎯 listo", "Hecho listo"},
		{"warning sign removed", "⚠ datos sin verificar: x", "datos sin verificar: x"},
		{"blank lines collapse", "uno\n\n\n\ndos", "uno\n\ndos"},
		{"crlf normalised", "uno\r\ndos", "uno\ndos"},
		{"arrows are kept", "A → B", "A → B"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConvertBody(tc.in, 0); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatAnswerBlock(t *testing.T) {
	got := FormatAnswer(Answer{
		QuestionID: 42,
		Provider:   "antigravity:gemini-4-hydrogen",
		MapID:      85,
		Text:       "**Ve a Entregar**\n📍 /compa marca 51.2 38.7 Entregar\n- Paso uno | paso dos\nFuente: wowforevertalents.com",
	})
	want := "[CWOW1 #42 antigravity/gemini-4-hydrogen]\n" +
		"**Ve a Entregar**\n" +
		"@marca 85 51.2 38.7 Entregar\n" +
		"- Paso uno || paso dos\n" +
		"@fuente wowforevertalents.com\n" +
		"[/CWOW1]"
	if got != want {
		t.Fatalf("block\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatAnswerHeaderIsSafe(t *testing.T) {
	got := FormatAnswer(Answer{QuestionID: 1, Provider: "a]b\nc", Text: "hola"})
	if !strings.HasPrefix(got, "[CWOW1 #1 ab c]\n") {
		t.Fatalf("header not sanitised: %q", got)
	}
	got = FormatAnswer(Answer{QuestionID: 2, Provider: "", Text: "hola"})
	if !strings.HasPrefix(got, "[CWOW1 #2 app]\n") {
		t.Fatalf("empty provider header: %q", got)
	}
}

func TestFormatErrorBlock(t *testing.T) {
	got := FormatError(7, "No pude responder. Revisa companero.log.")
	want := "[CWOW1 #7 app/error]\nNo pude responder. Revisa companero.log.\n[/CWOW1]"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = FormatError(8, "⚠ falla")
	if !strings.Contains(got, "\nfalla\n") {
		t.Fatalf("emoji not removed from the error: %q", got)
	}
}

func TestFormatAnswerCap(t *testing.T) {
	long := strings.Repeat("Línea de respuesta con texto de relleno. ", 200) // about 8000 characters
	got := FormatAnswer(Answer{QuestionID: 9, Provider: "codex", Text: long})
	if n := utf8.RuneCountInString(got); n > MaxBlockRunes {
		t.Fatalf("block has %d runes, want at most %d", n, MaxBlockRunes)
	}
	if !strings.HasSuffix(got, "\n"+trimmedNote+"\n[/CWOW1]") {
		t.Fatalf("trimmed block must end with the note: %q", got[len(got)-80:])
	}
	if !strings.HasPrefix(got, "[CWOW1 #9 codex]\n") {
		t.Fatalf("header lost: %q", got[:40])
	}
}

func TestFormatAnswerCapDoesNotLeaveLonePipe(t *testing.T) {
	// The cut lands between the two bars of an escaped pair: the lone bar must go.
	room := MaxBlockRunes - utf8.RuneCountInString("[CWOW1 #3 app]\n\n[/CWOW1]\n"+trimmedNote)
	body := strings.Repeat("x", room-1) + "||" + strings.Repeat("y", 300)
	got := FormatAnswer(Answer{QuestionID: 3, Text: body})
	if n := utf8.RuneCountInString(got); n > MaxBlockRunes {
		t.Fatalf("block too long: %d", n)
	}
	cut := strings.TrimSuffix(got, "\n"+trimmedNote+"\n[/CWOW1]")
	if strings.HasSuffix(cut, "|") && !strings.HasSuffix(cut, "||") {
		t.Fatalf("lone pipe left at the cut")
	}
}

func TestFormatEmptyAnswerHasNoBody(t *testing.T) {
	got := FormatAnswer(Answer{QuestionID: 5, Text: "  \n  "})
	if got != "[CWOW1 #5 app]\n[/CWOW1]" {
		t.Fatalf("got %q", got)
	}
}

func TestStripEmoji(t *testing.T) {
	if got := StripEmoji("🧩 Hola 👋🏽 mundo ⌨️ fin"); got != "Hola mundo fin" {
		t.Fatalf("got %q", got)
	}
	if got := StripEmoji("ñandú ¿qué? ¡sí!"); got != "ñandú ¿qué? ¡sí!" {
		t.Fatalf("accents must stay: %q", got)
	}
}
