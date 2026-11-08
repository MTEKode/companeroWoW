package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeProvider struct {
	name     string
	availErr error
	ask      func(ctx context.Context, req Request) (Response, error)
	calls    int
}

func (f *fakeProvider) Name() string     { return f.name }
func (f *fakeProvider) Available() error { return f.availErr }
func (f *fakeProvider) Ask(ctx context.Context, req Request) (Response, error) {
	f.calls++
	return f.ask(ctx, req)
}

func answer(text string) func(context.Context, Request) (Response, error) {
	return func(context.Context, Request) (Response, error) { return Response{Text: text}, nil }
}

func failWith(msg string) func(context.Context, Request) (Response, error) {
	return func(context.Context, Request) (Response, error) { return Response{}, errors.New(msg) }
}

func TestChainFallsThroughToThirdProvider(t *testing.T) {
	first := &fakeProvider{name: "gemini", ask: failWith("sin sesión")}
	second := &fakeProvider{name: "codex", ask: answer("   ")}
	third := &fakeProvider{name: "api:NIM", ask: answer("hola desde la API")}
	c := newChain(time.Second, first, second, third)

	resp, err := c.Ask(context.Background(), Request{Prompt: "¿qué hora es?"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Provider != "api:NIM" {
		t.Fatalf("Provider = %q, want api:NIM", resp.Provider)
	}
	if resp.Text != "hola desde la API" {
		t.Fatalf("Text = %q", resp.Text)
	}
	if len(resp.Attempts) != 3 {
		t.Fatalf("Attempts = %d, want 3: %+v", len(resp.Attempts), resp.Attempts)
	}
	if resp.Attempts[0].Err != "sin sesión" || resp.Attempts[0].Provider != "gemini" {
		t.Errorf("attempt 0 = %+v", resp.Attempts[0])
	}
	if resp.Attempts[1].Err != "respuesta vacía" {
		t.Errorf("attempt 1 = %+v, want empty-response error", resp.Attempts[1])
	}
	if resp.Attempts[2].Err != "" {
		t.Errorf("attempt 2 should be a success, got %+v", resp.Attempts[2])
	}
}

func TestChainSkipsUnavailableProvider(t *testing.T) {
	missing := &fakeProvider{name: "gemini", availErr: errors.New("no está en PATH"), ask: answer("no debería llamarse")}
	ok := &fakeProvider{name: "codex", ask: answer("ok")}
	c := newChain(time.Second, missing, ok)

	resp, err := c.Ask(context.Background(), Request{Prompt: "x"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if missing.calls != 0 {
		t.Fatalf("unavailable provider was called %d times", missing.calls)
	}
	if resp.Provider != "codex" || len(resp.Attempts) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestChainAllFailListsEveryAttempt(t *testing.T) {
	c := newChain(time.Second,
		&fakeProvider{name: "gemini", ask: failWith("cuota agotada")},
		&fakeProvider{name: "codex", availErr: errors.New("no está en PATH")},
		&fakeProvider{name: "api:Z.ai", ask: failWith("HTTP 401")},
	)
	_, err := c.Ask(context.Background(), Request{Prompt: "x"})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	for _, want := range []string{"gemini", "cuota agotada", "codex", "api:Z.ai", "HTTP 401"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestChainNoProviders(t *testing.T) {
	_, err := newChain(time.Second).Ask(context.Background(), Request{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "no hay proveedores") {
		t.Fatalf("err = %v, want Spanish no-providers error", err)
	}
}

func TestChainPerProviderTimeout(t *testing.T) {
	blocking := &fakeProvider{name: "gemini", ask: func(ctx context.Context, _ Request) (Response, error) {
		<-ctx.Done()
		return Response{}, ctx.Err()
	}}
	ok := &fakeProvider{name: "codex", ask: answer("rápido")}
	c := newChain(50*time.Millisecond, blocking, ok)

	start := time.Now()
	resp, err := c.Ask(context.Background(), Request{Prompt: "x"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout not honoured, took %s", elapsed)
	}
	if resp.Provider != "codex" {
		t.Fatalf("Provider = %q, want codex", resp.Provider)
	}
	if !strings.Contains(resp.Attempts[0].Err, "tiempo agotado") {
		t.Fatalf("attempt 0 = %+v, want timeout", resp.Attempts[0])
	}
}

func TestChainParentCancelStopsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	next := &fakeProvider{name: "codex", ask: answer("no")}
	c := newChain(time.Second, &fakeProvider{name: "gemini", ask: answer("no")}, next)
	if _, err := c.Ask(ctx, Request{Prompt: "x"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if next.calls != 0 {
		t.Fatal("providers were called after cancel")
	}
}

func TestBuildChainOrderAndAPIExpansion(t *testing.T) {
	cfg := testConfig()
	cfg.ProviderOrder = []string{"codex", "api", "antigravity", "bogus"}
	cfg.APIProviders = testAPIProviders()
	c := BuildChain(&cfg, t.TempDir(), nil)
	want := []string{"codex", "api:NIM", "api:Z.ai", "antigravity"}
	got := c.Providers()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Providers() = %v, want %v", got, want)
	}
}

func TestPromptFlatteningKeepsLastTurns(t *testing.T) {
	var hist []Message
	for i := 0; i < 14; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		hist = append(hist, Message{Role: role, Content: "turno " + string(rune('a'+i))})
	}
	text := buildPrompt(Request{System: "Sistema", History: hist, Prompt: "  pregunta final  "}, "")
	if !strings.HasPrefix(text, "Sistema\n\nHistorial:\n") {
		t.Fatalf("prompt does not start with system then Historial:\n%s", text)
	}
	if strings.Contains(text, "turno a") || strings.Contains(text, "turno d") {
		t.Fatalf("old turns were not dropped:\n%s", text)
	}
	if !strings.Contains(text, "Usuario: turno e") || !strings.Contains(text, "Asistente: turno f") {
		t.Fatalf("recent turns missing or mislabelled:\n%s", text)
	}
	if !strings.HasSuffix(text, "Pregunta: pregunta final\n") {
		t.Fatalf("prompt does not end with the question:\n%s", text)
	}
}
