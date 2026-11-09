package ui

import (
	"strings"
	"testing"
)

const answerWithMark = "Entrega en Orgrimmar.\n- Habla con Garrosh.\n📍 /compa marca 85 51.2 38.7 Garrosh Hellscream"

func TestMarkCommandsStripsEmojiAndKeepsOrder(t *testing.T) {
	got := MarkCommands(answerWithMark + "\n📍 /compa marca borrar")
	if len(got) != 2 || got[0] != "/compa marca 85 51.2 38.7 Garrosh Hellscream" || got[1] != "/compa marca borrar" {
		t.Fatalf("MarkCommands = %q", got)
	}
	if len(MarkCommands("Sin marcas aquí (51.2, 38.7).")) != 0 {
		t.Error("a plain coordinate is not a mark line")
	}
}

func TestMarkRunsHighlightsOnlyTheMarkLine(t *testing.T) {
	var marks []Span
	for _, s := range MarkRuns(answerWithMark) {
		if s.Style&StyleMark != 0 {
			marks = append(marks, s)
		}
	}
	if len(marks) != 2 {
		t.Fatalf("highlighted runs = %d, want the emoji and the command", len(marks))
	}
	if marks[0].Text != "📍 " {
		t.Errorf("emoji run = %q", marks[0].Text)
	}
	cmd := marks[1]
	if cmd.Style&StyleLink == 0 || !strings.HasPrefix(cmd.Text, "/compa marca 85 51.2 38.7") || !strings.Contains(cmd.Text, copyHint) {
		t.Errorf("command run = %+v", cmd)
	}
	joined := ""
	for _, s := range MarkRuns(answerWithMark) {
		joined += s.Text
	}
	if !strings.Contains(joined, "Habla con Garrosh.") {
		t.Errorf("normal lines must keep their text: %q", joined)
	}
}

func TestCommandFromLinkCopiesOnlyTheCommand(t *testing.T) {
	cmd, ok := CommandFromLink("/compa marca 85 51.2 38.7 Garrosh Hellscream  · Copiar marca")
	if !ok || cmd != "/compa marca 85 51.2 38.7 Garrosh Hellscream" {
		t.Fatalf("CommandFromLink = %q, %v", cmd, ok)
	}
	if _, ok := CommandFromLink("https://wowforever.example/guia"); ok {
		t.Error("web links must still open in the browser, not be copied")
	}
	if _, ok := CommandFromLink("/compa marca"); ok {
		t.Error("a bare command without arguments is not copied")
	}
}

func TestCopiedNoticeIsSpanish(t *testing.T) {
	if CopiedNotice != "Copiado: pégalo en el chat del juego" {
		t.Errorf("notice = %q", CopiedNotice)
	}
}

const answerWithBars = "Coloca los hechizos fuera de combate.\n⌨️ /compa barras aplicar 6:2054,7:8092\nLuego revisa las teclas."

func TestBarsLinesAreHighlightedAndCopyTheCommand(t *testing.T) {
	var marks []Span
	for _, s := range MarkRuns(answerWithBars) {
		if s.Style&StyleMark != 0 {
			marks = append(marks, s)
		}
	}
	if len(marks) != 2 {
		t.Fatalf("highlighted runs = %d, want the mark and the command", len(marks))
	}
	cmd := marks[1]
	if cmd.Style&StyleLink == 0 || !strings.Contains(cmd.Text, "· Copiar comando") || strings.Contains(cmd.Text, "Copiar marca") {
		t.Errorf("bars command run = %+v", cmd)
	}
	got, ok := CommandFromLink(cmd.Text)
	if !ok || got != "/compa barras aplicar 6:2054,7:8092" {
		t.Fatalf("CommandFromLink = %q, %v", got, ok)
	}
}

func TestMarkCommandsIncludesBarsCommands(t *testing.T) {
	got := MarkCommands(answerWithMark + "\n" + answerWithBars)
	if len(got) != 2 || got[1] != "/compa barras aplicar 6:2054,7:8092" {
		t.Fatalf("MarkCommands = %q", got)
	}
}

func TestCommandFromLinkRefusesBareBarsPrefix(t *testing.T) {
	if _, ok := CommandFromLink("/compa barras"); ok {
		t.Error("a bare bars prefix is not copied")
	}
}
