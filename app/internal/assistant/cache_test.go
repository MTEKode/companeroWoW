package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// streamScript is a scripted asker that can also stream: the whole answer goes to the stream as one
// delta, the way a provider that streams whole sentences would.
type streamScript struct{ *scriptAsker }

func (s streamScript) AskStream(ctx context.Context, req ai.Request, st ai.Stream) (ai.Response, error) {
	resp, err := s.Ask(ctx, req)
	if err == nil && st.Delta != nil {
		st.Delta(resp.Text)
	}
	return resp, err
}

// cachedCfg is the default configuration with the answer cache on for ten minutes.
func cachedCfg() *config.Config {
	cfg := config.Defaults()
	cfg.AnswerCacheMin = 10
	return &cfg
}

func TestAnswerCacheKeyNormalizesTheQuestion(t *testing.T) {
	req := ai.Request{System: "Estado actual:\n- Zona: Elwynn", Skill: "skill v1"}
	a := answerCacheKey("¿Dónde   voy?", req, "m", true)
	b := answerCacheKey("  ¿dónde voy? ", req, "m", true)
	if a != b {
		t.Error("case and the spacing between words must not change the key")
	}
	if answerCacheKey("¿Dónde voy ahora?", req, "m", true) == a {
		t.Error("a different question must be a different key")
	}
}

func TestAnswerCacheKeyChangesWithContextSkillModelAndVerifier(t *testing.T) {
	base := ai.Request{System: "Estado actual: Zona Elwynn", Skill: "skill v1"}
	key := answerCacheKey("¿qué hago?", base, "modelo-a", true)

	moved := base
	moved.System = "Estado actual: Zona Westfall"
	if answerCacheKey("¿qué hago?", moved, "modelo-a", true) == key {
		t.Error("a change in the injected context must invalidate the answer")
	}
	newSkill := base
	newSkill.Skill = "skill v2"
	if answerCacheKey("¿qué hago?", newSkill, "modelo-a", true) == key {
		t.Error("a new skill version must invalidate the answer")
	}
	if answerCacheKey("¿qué hago?", base, "modelo-b", true) == key {
		t.Error("another model must be a different key")
	}
	if answerCacheKey("¿qué hago?", base, "modelo-a", false) == key {
		t.Error("the verifier setting must be part of the key")
	}
	if answerCacheKey("¿qué hago?", base, "modelo-a", true) != key {
		t.Error("the same question in the same context must keep its key")
	}
}

func TestChainModelKeyChangesWithTheModels(t *testing.T) {
	a := cachedCfg()
	b := cachedCfg()
	if chainModelKey(a) != chainModelKey(b) {
		t.Fatal("the same configuration must give the same key")
	}
	b.AntigravityModels = []string{"otro-modelo"}
	if chainModelKey(a) == chainModelKey(b) {
		t.Error("a different antigravity model list must change the key")
	}
	if chainModelKey(nil) != "" {
		t.Error("no configuration gives an empty key")
	}
}

func TestAnswerCacheExpiresAfterItsTTL(t *testing.T) {
	c := newAnswerCache(10 * time.Minute)
	t0 := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	c.put("k", cachedAnswer{text: "Hola.", provider: "codex"}, t0)

	if got, ok := c.get("k", t0.Add(9*time.Minute)); !ok || got.text != "Hola." || got.provider != "codex" {
		t.Errorf("within the TTL: got %+v, ok=%v", got, ok)
	}
	if _, ok := c.get("k", t0.Add(10*time.Minute)); ok {
		t.Error("at the TTL the answer must be gone")
	}
	if c.len() != 0 {
		t.Errorf("expired entries must be dropped, len = %d", c.len())
	}
}

func TestAnswerCacheNeverKeepsBlankOrDisabled(t *testing.T) {
	c := newAnswerCache(time.Minute)
	t0 := time.Now()
	c.put("vacia", cachedAnswer{text: "   "}, t0)
	if _, ok := c.get("vacia", t0); ok {
		t.Error("a blank answer must not be kept")
	}

	off := newAnswerCache(0)
	off.put("k", cachedAnswer{text: "Hola."}, t0)
	if _, ok := off.get("k", t0); ok || off.len() != 0 {
		t.Error("a cache with a zero TTL keeps nothing")
	}
}

func TestAnswerCacheTTLFromConfig(t *testing.T) {
	if answerCacheTTL(nil) != 0 {
		t.Error("no configuration: cache off")
	}
	cfg := config.Defaults()
	cfg.AnswerCacheMin = 0
	if answerCacheTTL(&cfg) != 0 {
		t.Error("answerCacheMin 0 turns the cache off")
	}
	cfg.AnswerCacheMin = 10
	if answerCacheTTL(&cfg) != 10*time.Minute {
		t.Errorf("answerCacheMin 10 = %v, want 10m", answerCacheTTL(&cfg))
	}
}

func TestAskStreamAnswersAnExactRepeatFromTheCache(t *testing.T) {
	fake := &scriptAsker{steps: []scriptStep{{resp: ai.Response{Text: "Hola, aventurero.", Provider: "codex"}}}}
	a := newWithAsker(streamScript{fake}, nil, nil, cachedCfg())

	first, err := a.AskStream(context.Background(), "¿Dónde voy?", false, ai.Stream{})
	if err != nil || first.Cached {
		t.Fatalf("first answer: %+v, err = %v", first, err)
	}

	var deltas []string
	second, err := a.AskStream(context.Background(), "  ¿dónde   voy? ", false, ai.Stream{
		Delta: func(text string) { deltas = append(deltas, text) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cached || second.Text != first.Text || second.Provider != "codex" {
		t.Errorf("the repeat must come from the cache: %+v", second)
	}
	if len(fake.reqs) != 1 {
		t.Errorf("provider asked %d times, want 1", len(fake.reqs))
	}
	if strings.Join(deltas, "") != first.Text {
		t.Errorf("a cached answer is streamed whole: deltas = %q", deltas)
	}
}

func TestAskWithCacheOffAsksEveryTime(t *testing.T) {
	fake := &scriptAsker{steps: []scriptStep{
		{resp: ai.Response{Text: "Uno.", Provider: "codex"}},
		{resp: ai.Response{Text: "Dos.", Provider: "codex"}},
	}}
	cfg := cachedCfg()
	cfg.AnswerCacheMin = 0
	a := newWithAsker(fake, nil, nil, cfg)
	for i := 0; i < 2; i++ {
		if _, err := a.AskStream(context.Background(), "hola", false, ai.Stream{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(fake.reqs) != 2 {
		t.Errorf("provider asked %d times, want 2", len(fake.reqs))
	}
}

func TestAskStreamNeverCachesErrors(t *testing.T) {
	fake := &scriptAsker{steps: []scriptStep{
		{err: errors.New("cuota agotada")},
		{resp: ai.Response{Text: "Hola.", Provider: "codex"}},
	}}
	a := newWithAsker(streamScript{fake}, nil, nil, cachedCfg())

	if _, err := a.AskStream(context.Background(), "hola", false, ai.Stream{}); err == nil {
		t.Fatal("the first question must fail")
	}
	resp, err := a.AskStream(context.Background(), "hola", false, ai.Stream{})
	if err != nil || resp.Cached || resp.Text != "Hola." {
		t.Fatalf("after an error the question is asked again: %+v, err = %v", resp, err)
	}
	if len(fake.reqs) != 2 {
		t.Errorf("provider asked %d times, want 2", len(fake.reqs))
	}
}

func TestAskStreamDoesNotCacheScreenshotsOrUnbackedAnswers(t *testing.T) {
	old := CaptureFunc
	CaptureFunc = func() ([]byte, error) { return []byte{0x89, 'P', 'N', 'G'}, nil }
	defer func() { CaptureFunc = old }()

	fake := &scriptAsker{steps: []scriptStep{
		{resp: ai.Response{Text: "Veo la pantalla.", Provider: "codex"}},
		{resp: ai.Response{Text: "Veo la pantalla otra vez.", Provider: "codex"}},
	}}
	a := newWithAsker(streamScript{fake}, nil, nil, cachedCfg())
	for i := 0; i < 2; i++ {
		if _, err := a.AskStream(context.Background(), "¿qué ves?", true, ai.Stream{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(fake.reqs) != 2 {
		t.Errorf("a question with a screenshot is asked each time: asked %d", len(fake.reqs))
	}
}

func TestAskStreamForwardsTheFirstAnswerAndFlagsCorrections(t *testing.T) {
	steps := []scriptStep{
		{resp: ai.Response{Text: "Ve a (12.3, 45.6) y entrega.", Provider: "codex"}},
		{
			resp:  ai.Response{Text: "Ve a (51.3, 38.5) y entrega.", Provider: "codex"},
			calls: []tools.CallRecord{toolOutput("guia_mision", groundedOutput)},
		},
	}
	a, fake := newVerifyAssistant(t, steps, verifiedCfg())
	a.asker = streamScript{fake}

	var streamed []string
	resp, err := a.AskStream(context.Background(), "¿Dónde entrego?", false, ai.Stream{
		Delta: func(text string) { streamed = append(streamed, text) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(streamed) != 1 || streamed[0] != "Ve a (12.3, 45.6) y entrega." {
		t.Errorf("the first answer is what streams: %q", streamed)
	}
	if !resp.Corrected || !strings.Contains(resp.Text, "(51.3, 38.5)") {
		t.Errorf("the repaired answer must come back flagged as corrected: %+v", resp)
	}
	if len(fake.reqs) != 2 || fake.reqs[1].Prompt == "" {
		t.Errorf("the repair round must not be streamed; requests = %d", len(fake.reqs))
	}
}

func TestUncorrectedAnswerIsNotFlagged(t *testing.T) {
	steps := []scriptStep{{resp: ai.Response{Text: "Hola.", Provider: "codex"}}}
	a, fake := newVerifyAssistant(t, steps, verifiedCfg())
	a.asker = streamScript{fake}
	resp, err := a.AskStream(context.Background(), "hola", false, ai.Stream{})
	if err != nil || resp.Corrected || resp.Cached {
		t.Errorf("resp = %+v, err = %v", resp, err)
	}
}

func TestAnswerDoesNotUseTheCache(t *testing.T) {
	fake := &scriptAsker{steps: []scriptStep{
		{resp: ai.Response{Text: "Uno.", Provider: "codex"}},
		{resp: ai.Response{Text: "Dos.", Provider: "codex"}},
	}}
	a := newWithAsker(fake, nil, nil, cachedCfg())
	for i := 0; i < 2; i++ {
		if _, err := a.Answer(context.Background(), "hola", false); err != nil {
			t.Fatal(err)
		}
	}
	if len(fake.reqs) != 2 {
		t.Errorf("the evaluation harness must always ask: asked %d", len(fake.reqs))
	}
}

func TestClearHistoryClearsTheAnswerCache(t *testing.T) {
	fake := &scriptAsker{steps: []scriptStep{{resp: ai.Response{Text: "Hola.", Provider: "codex"}}}}
	a := newWithAsker(fake, nil, nil, cachedCfg())
	if _, err := a.AskStream(context.Background(), "hola", false, ai.Stream{}); err != nil {
		t.Fatal(err)
	}
	if a.cache.len() != 1 {
		t.Fatalf("cache entries = %d, want 1", a.cache.len())
	}
	if err := a.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if a.cache.len() != 0 {
		t.Errorf("cache entries after clearing the chat = %d", a.cache.len())
	}
}
