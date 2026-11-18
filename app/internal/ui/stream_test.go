package ui

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// The repaint throttle is platform-neutral: it decides when the window is asked to repaint a
// streamed answer, and it keeps every piece of the text.

func TestDeltaThrottleRepaintsAtMostTenTimesASecond(t *testing.T) {
	th := newDeltaThrottle(streamInterval)
	base := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

	// A provider that writes a piece every 10 ms for three seconds.
	var posts []time.Time
	var due time.Time // when a scheduled repaint fires; zero when none is scheduled
	for i := 0; i < 300; i++ {
		now := base.Add(time.Duration(i) * 10 * time.Millisecond)
		if !due.IsZero() && !now.Before(due) {
			if th.due(now) {
				posts = append(posts, now)
			}
			due = time.Time{}
		}
		post, after := th.add("x", now)
		switch {
		case post:
			posts = append(posts, now)
		case after > 0:
			due = now.Add(after)
		}
	}
	sort.Slice(posts, func(i, j int) bool { return posts[i].Before(posts[j]) })
	if len(posts) < 25 {
		t.Fatalf("repaints = %d over three seconds; the answer must still show as it grows", len(posts))
	}
	for i := 1; i < len(posts); i++ {
		if gap := posts[i].Sub(posts[i-1]); gap < streamInterval {
			t.Fatalf("repaints %v apart, want at least %v", gap, streamInterval)
		}
	}
	if text, _ := th.take(); len(text) != 300 {
		t.Errorf("text taken = %d pieces, want all 300", len(text))
	}
}

func TestDeltaThrottleSchedulesTheRepaintAfterTheInterval(t *testing.T) {
	th := newDeltaThrottle(100 * time.Millisecond)
	t0 := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

	if post, _ := th.add("a", t0); !post {
		t.Fatal("the first piece asks for a repaint at once")
	}
	th.take() // the UI takes the first piece
	post, after := th.add("b", t0.Add(10*time.Millisecond))
	if post || after != 90*time.Millisecond {
		t.Fatalf("second piece: post=%v after=%v, want a repaint in 90ms", post, after)
	}
	if post, after := th.add("c", t0.Add(20*time.Millisecond)); post || after != 0 {
		t.Errorf("while a repaint is scheduled no other is: post=%v after=%v", post, after)
	}
	if !th.due(t0.Add(100 * time.Millisecond)) {
		t.Error("the scheduled repaint must be asked")
	}
	if text, reset := th.take(); text != "bc" || reset {
		t.Errorf("take = %q, %v; want \"bc\", false", text, reset)
	}
	if text, _ := th.take(); text != "" {
		t.Errorf("a second take must be empty, got %q", text)
	}
}

func TestDeltaThrottleRestartVoidsTheTextTaken(t *testing.T) {
	th := newDeltaThrottle(100 * time.Millisecond)
	t0 := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

	th.add("abc", t0)
	if text, reset := th.take(); text != "abc" || reset {
		t.Fatalf("take = %q, %v", text, reset)
	}
	if post, _ := th.restart(t0.Add(time.Second)); !post {
		t.Error("a restart after the interval asks for a repaint at once")
	}
	th.add("xy", t0.Add(time.Second))
	if text, reset := th.take(); text != "xy" || !reset {
		t.Errorf("take after a restart = %q, %v; want \"xy\", true", text, reset)
	}
	if text, reset := th.take(); text != "" || reset {
		t.Errorf("the reset is reported once: %q, %v", text, reset)
	}
}

func TestEntryRunsShowsCorrectionAndCacheInTheAnswerLine(t *testing.T) {
	runs := entryRuns(entry{
		Kind:      entryAssistant,
		Text:      "Ve a la plaza.",
		Provider:  "antigravity:gemini-4-hydrogen-high",
		At:        "10:31",
		Corrected: true,
		Cached:    true,
	})
	meta := runs[0].Text
	for _, want := range []string{"Compañero", "10:31", "(respuesta en caché)", "(corregida)"} {
		if !strings.Contains(meta, want) {
			t.Errorf("meta line %q lacks %q", meta, want)
		}
	}
	plain := entryRuns(entry{Kind: entryAssistant, Text: "Hola.", Provider: "codex", At: "10:32"})
	if strings.Contains(plain[0].Text, "corregida") || strings.Contains(plain[0].Text, "caché") {
		t.Errorf("an answer without changes must not be marked: %q", plain[0].Text)
	}
}
