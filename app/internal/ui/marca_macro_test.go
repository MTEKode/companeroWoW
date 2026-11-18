package ui

import (
	"strings"
	"testing"
)

// A macro command line is highlighted and copies its command, like the action bar lines.
func TestMacroCommandLineIsCopyable(t *testing.T) {
	text := "Ponla en tu barra:\n🧩 /compa macro crear Tanque|INV_MISC_QUESTIONMARK|/cast Abrumar"
	cmds := MarkCommands(text)
	if len(cmds) != 1 || cmds[0] != "/compa macro crear Tanque|INV_MISC_QUESTIONMARK|/cast Abrumar" {
		t.Fatalf("MarkCommands = %q", cmds)
	}
	var cmd Span
	for _, r := range MarkRuns(text) {
		if strings.HasPrefix(r.Text, "/compa macro") {
			cmd = r
		}
	}
	if cmd.Style&StyleLink == 0 || !strings.Contains(cmd.Text, copyHintCommand) {
		t.Fatalf("macro command run = %+v", cmd)
	}
	got, ok := CommandFromLink(cmd.Text)
	if !ok || got != "/compa macro crear Tanque|INV_MISC_QUESTIONMARK|/cast Abrumar" {
		t.Errorf("CommandFromLink = %q, %v", got, ok)
	}
	if _, ok := CommandFromLink("/compa macro"); ok {
		t.Error("a bare macro prefix is not a command")
	}
}

// A macro body is monospace and copies with its line breaks (the literal \n becomes a real one).
func TestMacroBodyIsMonospaceAndCopies(t *testing.T) {
	text := `🧩 cuerpo: #showtooltip Abrumar\n/cast [stance:2] Abrumar; Ejecutar`
	var body Span
	for _, r := range MarkRuns(text) {
		if strings.Contains(r.Text, copyHintMacro) {
			body = r
		}
	}
	if body.Style&StyleCode == 0 || body.Style&StyleLink == 0 {
		t.Fatalf("body run = %+v", body)
	}
	got, ok := MacroBodyFromLink(body.Text)
	if !ok || got != "#showtooltip Abrumar\n/cast [stance:2] Abrumar; Ejecutar" {
		t.Errorf("MacroBodyFromLink = %q, %v", got, ok)
	}
	if _, ok := MacroBodyFromLink("https://example.com/" + copyHintMacro); ok {
		t.Error("a web link is never a macro body")
	}
	if _, ok := CommandFromLink(body.Text); ok {
		t.Error("a body is not a command")
	}
}
