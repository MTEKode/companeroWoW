package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

type toolCall struct {
	Name string
	Args string
}

type fakeTools struct {
	mu    sync.Mutex
	calls []toolCall
}

func (f *fakeTools) Specs() []ToolSpec {
	return []ToolSpec{
		{
			Name:        "get_zone",
			Description: "Devuelve la descripción de una zona",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"zone": map[string]any{"type": "string"}},
				"required":   []string{"zone"},
			},
		},
		{Name: "map_view", Description: "Captura el mapa"},
	}
}

func (f *fakeTools) Execute(_ context.Context, name, argsJSON string) (ToolOutput, error) {
	f.mu.Lock()
	f.calls = append(f.calls, toolCall{Name: name, Args: argsJSON})
	f.mu.Unlock()
	switch name {
	case "get_zone":
		return ToolOutput{Text: "Eldoria: zona del norte"}, nil
	case "map_view":
		return ToolOutput{Text: "mapa", ImagePNG: []byte("\x89PNG-map")}, nil
	}
	return ToolOutput{Text: "desconocida", IsError: true}, nil
}

// apiTestServer answers each request with the next canned body and records the decoded requests.
type apiTestServer struct {
	t        *testing.T
	bodies   []string
	statuses []int
	seen     []map[string]any
	auth     []string
}

func (s *apiTestServer) handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/chat/completions" {
		s.t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
	}
	raw, _ := io.ReadAll(r.Body)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		s.t.Errorf("request body is not JSON: %v", err)
	}
	s.seen = append(s.seen, doc)
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	i := len(s.seen) - 1
	status := http.StatusOK
	if i < len(s.statuses) {
		status = s.statuses[i]
	}
	body := `{"error":"sin respuesta"}`
	if len(s.bodies) > 0 {
		body = s.bodies[min(i, len(s.bodies)-1)] // the last body repeats for further requests
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func newAPIProvider(t *testing.T, srv *httptest.Server, tools ToolExecutor) Provider {
	t.Helper()
	return NewOpenAICompat(config.APIProvider{Name: "NIM", BaseURL: srv.URL + "/v1/", APIKey: "secret", Model: "modelo-x"}, tools, 5*time.Second)
}

func TestOpenAICompatToolRoundThenAnswer(t *testing.T) {
	s := &apiTestServer{t: t, bodies: []string{
		`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_zone","arguments":"{\"zone\":\"Eldoria\"}"}}]}}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"Eldoria está al norte."}}]}`,
	}}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()

	tools := &fakeTools{}
	p := newAPIProvider(t, srv, tools)
	if p.Name() != "api:NIM" {
		t.Fatalf("Name = %q", p.Name())
	}
	if err := p.Available(); err != nil {
		t.Fatalf("Available: %v", err)
	}

	resp, err := p.Ask(context.Background(), Request{
		System:  "Sistema",
		History: []Message{{Role: "user", Content: "hola"}, {Role: "assistant", Content: "¡hola!"}},
		Prompt:  "¿dónde está Eldoria?",
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Text != "Eldoria está al norte." || resp.Provider != "api:NIM" {
		t.Fatalf("resp = %+v", resp)
	}

	if len(tools.calls) != 1 || tools.calls[0].Name != "get_zone" {
		t.Fatalf("tool calls = %+v", tools.calls)
	}
	if tools.calls[0].Args != `{"zone":"Eldoria"}` {
		t.Fatalf("tool args = %q", tools.calls[0].Args)
	}
	if s.auth[0] != "Bearer secret" {
		t.Errorf("Authorization = %q", s.auth[0])
	}

	first := s.seen[0]
	if first["model"] != "modelo-x" {
		t.Errorf("model = %v", first["model"])
	}
	specs, _ := first["tools"].([]any)
	if len(specs) != 2 {
		t.Fatalf("tools sent = %v", first["tools"])
	}
	fn := specs[0].(map[string]any)
	if fn["type"] != "function" || fn["function"].(map[string]any)["name"] != "get_zone" {
		t.Errorf("first tool = %v", fn)
	}
	msgs := first["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("first request has %d messages, want system+2 history+user", len(msgs))
	}
	if msgs[0].(map[string]any)["role"] != "system" || msgs[3].(map[string]any)["content"] != "¿dónde está Eldoria?" {
		t.Errorf("messages = %v", msgs)
	}

	second := s.seen[1]
	msgs2 := second["messages"].([]any)
	last := msgs2[len(msgs2)-1].(map[string]any)
	if last["role"] != "tool" || last["tool_call_id"] != "call_1" || last["content"] != "Eldoria: zona del norte" {
		t.Errorf("tool result message = %v", last)
	}
	prevAssistant := msgs2[len(msgs2)-2].(map[string]any)
	if prevAssistant["role"] != "assistant" || prevAssistant["tool_calls"] == nil {
		t.Errorf("assistant tool_calls turn not echoed: %v", prevAssistant)
	}
}

func TestOpenAICompatImageAttachedAsDataURL(t *testing.T) {
	s := &apiTestServer{t: t, bodies: []string{`{"choices":[{"message":{"content":"veo algo"}}]}`}}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()

	p := newAPIProvider(t, srv, nil)
	resp, err := p.Ask(context.Background(), Request{Prompt: "¿qué ves?", ImagePNG: []byte("\x89PNG-shot")})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Text != "veo algo" {
		t.Fatalf("Text = %q", resp.Text)
	}
	raw, _ := json.Marshal(s.seen[0]["messages"])
	if !strings.Contains(string(raw), `"type":"image_url"`) || !strings.Contains(string(raw), "data:image/png;base64,") {
		t.Fatalf("image not attached as data URL: %s", raw)
	}
	if _, ok := s.seen[0]["tools"]; ok {
		t.Errorf("no tools configured but tools were sent")
	}
}

func TestOpenAICompatToolImageFollowsAsUserMessage(t *testing.T) {
	s := &apiTestServer{t: t, bodies: []string{
		`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"map_view","arguments":"{}"}}]}}]}`,
		`{"choices":[{"message":{"content":"el mapa muestra Eldoria"}}]}`,
	}}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()

	tools := &fakeTools{}
	resp, err := newAPIProvider(t, srv, tools).Ask(context.Background(), Request{Prompt: "mapa"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Text != "el mapa muestra Eldoria" {
		t.Fatalf("Text = %q", resp.Text)
	}
	msgs := s.seen[1]["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	raw, _ := json.Marshal(last)
	if last["role"] != "user" || !strings.Contains(string(raw), "data:image/png;base64,") {
		t.Fatalf("tool image not sent as user message: %s", raw)
	}
}

func TestOpenAICompatRateLimitIsError(t *testing.T) {
	s := &apiTestServer{t: t, statuses: []int{http.StatusTooManyRequests}, bodies: []string{`{"error":"rate limit exceeded"}`}}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()

	_, err := newAPIProvider(t, srv, nil).Ask(context.Background(), Request{Prompt: "x"})
	if err == nil {
		t.Fatal("429 must be an error so the chain falls back")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "cuota") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenAICompatAuthAndServerErrors(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		s := &apiTestServer{t: t, statuses: []int{status}, bodies: []string{`{}`}}
		srv := httptest.NewServer(http.HandlerFunc(s.handler))
		_, err := newAPIProvider(t, srv, nil).Ask(context.Background(), Request{Prompt: "x"})
		srv.Close()
		if err == nil {
			t.Errorf("status %d: expected error", status)
		}
	}
}

func TestOpenAICompatRoundLimit(t *testing.T) {
	loop := `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"get_zone","arguments":"{}"}}]}}]}`
	s := &apiTestServer{t: t, bodies: []string{loop}}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()

	_, err := newAPIProvider(t, srv, &fakeTools{}).Ask(context.Background(), Request{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "rondas") {
		t.Fatalf("err = %v, want round-limit error", err)
	}
	if len(s.seen) != maxToolRounds {
		t.Fatalf("requests = %d, want %d", len(s.seen), maxToolRounds)
	}
}

func TestOpenAICompatToolCallWithoutExecutorIsError(t *testing.T) {
	s := &apiTestServer{t: t, bodies: []string{
		`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"get_zone","arguments":"{}"}}]}}]}`,
	}}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()
	if _, err := newAPIProvider(t, srv, nil).Ask(context.Background(), Request{Prompt: "x"}); err == nil {
		t.Fatal("expected error when the model asks for tools but none are configured")
	}
}

func TestOpenAICompatMissingSettingsNotAvailable(t *testing.T) {
	p := NewOpenAICompat(config.APIProvider{Name: "Vacio", BaseURL: "https://x", Model: "m"}, nil, time.Second)
	if err := p.Available(); err == nil {
		t.Fatal("missing apiKey must not be available")
	}
}

func TestOpenAICompatTimeoutHonoured(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-block:
		}
	}))
	defer srv.Close()
	defer close(block)

	p := NewOpenAICompat(config.APIProvider{Name: "Lento", BaseURL: srv.URL, APIKey: "k", Model: "m"}, nil, 100*time.Millisecond)
	start := time.Now()
	_, err := p.Ask(context.Background(), Request{Prompt: "x"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout not honoured")
	}
}
