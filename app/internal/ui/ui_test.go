package ui

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowStateFallbacks(t *testing.T) {
	dir := t.TempDir()
	if _, found := loadWindowState(filepath.Join(dir, "missing.json")); found {
		t.Error("missing file should not be found")
	}
	path := filepath.Join(dir, "window.json")
	want := windowState{X: 10, Y: 20, W: 500, H: 700}
	if err := saveWindowState(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got, found := loadWindowState(path); !found || got != want {
		t.Errorf("round trip = %+v (found %v), want %+v", got, found, want)
	}
	if err := saveWindowState(path, windowState{X: 1, Y: 2, W: 50, H: 50}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, found := loadWindowState(path); found {
		t.Error("a size below the minimum should fall back to the default placement")
	}
}

func TestDefaultStateScales(t *testing.T) {
	if s := defaultState(1); s.W != 420 || s.H != 560 {
		t.Errorf("defaultState(1) = %+v", s)
	}
	if s := defaultState(1.5); s.W != 630 || s.H != 840 {
		t.Errorf("defaultState(1.5) = %+v", s)
	}
}

func TestSafeURL(t *testing.T) {
	for _, ok := range []string{"https://warcraft.wiki.gg/wiki/Foo", "HTTP://example.com/a?b=c&d=e"} {
		if _, err := safeURL(ok); err != nil {
			t.Errorf("safeURL(%q) rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"file:///C:/Windows", "javascript:alert(1)", "ms-settings:", "https://a b", "https://x\"y"} {
		if _, err := safeURL(bad); err == nil {
			t.Errorf("safeURL(%q) accepted", bad)
		}
	}
}

// visible joins the text of runs, which is what the user reads.
func visible(runs []Span) string {
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

func TestMarkdownBold(t *testing.T) {
	got := Markdown("**Hola** mundo")
	want := []Span{{Text: "Hola", Style: StyleBold}, {Text: " mundo"}}
	assertSpans(t, got, want)
}

func TestMarkdownUnmatchedBoldIsLiteral(t *testing.T) {
	got := Markdown("precio **sin cerrar")
	if visible(got) != "precio **sin cerrar" {
		t.Errorf("unmatched ** changed the text: %q", visible(got))
	}
	for _, r := range got {
		if r.Style&StyleBold != 0 {
			t.Errorf("unmatched ** produced bold run %q", r.Text)
		}
	}
}

func TestMarkdownCode(t *testing.T) {
	got := Markdown("usa `ls -la` ya")
	want := []Span{{Text: "usa "}, {Text: "ls -la", Style: StyleCode}, {Text: " ya"}}
	assertSpans(t, got, want)
}

func TestMarkdownBullets(t *testing.T) {
	got := Markdown("- uno\n* dos")
	want := []Span{
		{Text: "• "}, {Text: "uno"},
		{Text: "\n"},
		{Text: "• "}, {Text: "dos"},
	}
	assertSpans(t, got, want)
}

func TestMarkdownLinkShowsURL(t *testing.T) {
	got := Markdown("[Wiki](https://example.com/x)")
	want := []Span{
		{Text: "Wiki"},
		{Text: " ("},
		{Text: "https://example.com/x", Style: StyleLink},
		{Text: ")"},
	}
	assertSpans(t, got, want)
}

func TestMarkdownBareURLDropsTrailingPunctuation(t *testing.T) {
	got := Markdown("mira https://a.example/c.")
	want := []Span{{Text: "mira "}, {Text: "https://a.example/c", Style: StyleLink}, {Text: "."}}
	assertSpans(t, got, want)
}

func TestMarkdownBareURLKeepsBalancedParens(t *testing.T) {
	got := Markdown("(https://en.wikipedia.org/wiki/Foo_(bar))")
	var link string
	for _, r := range got {
		if r.Style&StyleLink != 0 {
			link = r.Text
		}
	}
	if link != "https://en.wikipedia.org/wiki/Foo_(bar)" {
		t.Errorf("link = %q", link)
	}
}

func TestMarkdownRejectsNonHTTPLinks(t *testing.T) {
	for _, src := range []string{"[x](javascript:alert(1))", "[x](file:///C:/Windows)"} {
		for _, r := range Markdown(src) {
			if r.Style&StyleLink != 0 {
				t.Errorf("%q produced link %q", src, r.Text)
			}
		}
		if visible(Markdown(src)) != src {
			t.Errorf("%q should stay literal, got %q", src, visible(Markdown(src)))
		}
	}
}

func TestMarkdownNeverInterpretsHTML(t *testing.T) {
	src := "<b>x</b> <script>alert(1)</script>"
	if visible(Markdown(src)) != src {
		t.Errorf("HTML changed: %q", visible(Markdown(src)))
	}
}

func TestMarkdownHeading(t *testing.T) {
	got := Markdown("## Objetivos")
	want := []Span{{Text: "Objetivos", Style: StyleGold | StyleBold}}
	assertSpans(t, got, want)
}

func TestEntryRuns(t *testing.T) {
	user := visible(entryRuns(entry{Kind: entryUser, Text: "hola"}))
	if user != "Tú:\nhola\n\n" {
		t.Errorf("user entry = %q", user)
	}
	if runs := entryRuns(entry{Kind: entryUser, Text: "x"}); runs[0].Style != StyleGold|StyleBold {
		t.Errorf("user label style = %v", runs[0].Style)
	}

	ans := entryRuns(entry{Kind: entryAssistant, Text: "**sí**", Provider: "antigravity", At: "10:02"})
	if !strings.HasPrefix(visible(ans), "Compañero · antigravity · 10:02\n") {
		t.Errorf("assistant header = %q", visible(ans))
	}
	found := false
	for _, r := range ans {
		if r.Text == "sí" && r.Style&StyleBold != 0 {
			found = true
		}
	}
	if !found {
		t.Error("assistant body is not rendered as Markdown")
	}

	tip := entryRuns(entry{Kind: entryTip, Title: "Subida de nivel", Text: "Prueba la misión"})
	if !strings.HasPrefix(visible(tip), "💡 Consejo: Subida de nivel\n") {
		t.Errorf("tip header = %q", visible(tip))
	}
	for _, r := range tip {
		if r.Text == "Prueba la misión" && r.Style&StyleTip == 0 {
			t.Error("tip text lacks the tip style")
		}
	}

	if runs := entryRuns(entry{Kind: entryError, Text: "No pude responder"}); runs[0].Style != StyleError {
		t.Errorf("error style = %v", runs[0].Style)
	}
}

func TestEntryFromMsg(t *testing.T) {
	e := entryFromMsg(answerMsg{Role: "assistant", Content: "hola", Provider: "codex", At: "09:00"})
	if e.Kind != entryAssistant || e.Provider != "codex" || e.At != "09:00" {
		t.Errorf("assistant entry = %+v", e)
	}
	if e := entryFromMsg(answerMsg{Role: "user", Content: "q"}); e.Kind != entryUser {
		t.Errorf("user entry kind = %v", e.Kind)
	}
	if e := entryFromMsg(answerMsg{Role: "system", Content: "s"}); e.Kind != entryInfo {
		t.Errorf("system entry kind = %v", e.Kind)
	}
}

func assertSpans(t *testing.T, got, want []Span) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d spans %+v, want %d %+v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
