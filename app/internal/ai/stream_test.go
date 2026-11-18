package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// ---- SSE reader ----

func TestReadSSEJoinsDataLinesAndSkipsOtherFields(t *testing.T) {
	body := ": keep-alive\n" +
		"event: message\n" +
		"data: {\"a\":1}\n" +
		"id: 7\n\n" +
		"data: primera\r\ndata: segunda\r\n\r\n" +
		"data:sin-espacio\n\n" +
		"data: final-sin-linea-vacia"
	var got []string
	err := readSSE(strings.NewReader(body), func(ev string) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`{"a":1}`, "primera\nsegunda", "sin-espacio", "final-sin-linea-vacia"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("events = %q, want %q", got, want)
	}
}

func TestReadSSEStopsOnErrSSEStop(t *testing.T) {
	calls := 0
	err := readSSE(strings.NewReader("data: a\n\ndata: [DONE]\n\ndata: nunca\n\n"), func(ev string) error {
		calls++
		if ev == "[DONE]" {
			return errSSEStop
		}
		return nil
	})
	if err != nil {
		t.Fatalf("errSSEStop must end the read without error, got %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestReadSSEReturnsCallbackError(t *testing.T) {
	boom := errors.New("boom")
	err := readSSE(strings.NewReader("data: a\n\n"), func(string) error { return boom })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the callback's error", err)
	}
}

// ---- openai-compatible streaming ----

// sseWrite writes the events of a text/event-stream response, then [DONE].
func sseWrite(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		fmt.Fprintf(w, "data: %s\n\n", e)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// readBody decodes the chat request the server received.
func readBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("request body: %v", err)
	}
	return body
}

type lookupTool struct{ calls []string }

func (l *lookupTool) Specs() []ToolSpec {
	return []ToolSpec{{Name: "lookup", Description: "busca", Parameters: map[string]any{"type": "object"}}}
}

func (l *lookupTool) Execute(_ context.Context, name, args string) (ToolOutput, error) {
	l.calls = append(l.calls, name+" "+args)
	return ToolOutput{Text: "dato"}, nil
}

func streamProvider(t *testing.T, url string, tools ToolExecutor) streamer {
	t.Helper()
	p := NewOpenAICompat(config.APIProvider{Name: "NIM", BaseURL: url + "/v1", APIKey: "k", Model: "m"}, tools, 5*time.Second)
	s, ok := p.(streamer)
	if !ok {
		t.Fatal("the openai-compatible provider must stream")
	}
	return s
}

func TestOpenAIAskStreamDeliversDeltasInOrder(t *testing.T) {
	var sawStream bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawStream, _ = readBody(t, r)["stream"].(bool)
		sseWrite(w,
			`{"choices":[{"delta":{"role":"assistant","content":"Hola"}}]}`,
			`{"choices":[{"delta":{"content":", "}}]}`,
			`{"choices":[{"delta":{"content":"mundo."},"finish_reason":"stop"}]}`,
		)
	}))
	defer srv.Close()

	var deltas []string
	resp, err := streamProvider(t, srv.URL, nil).AskStream(context.Background(), Request{Prompt: "hola"}, Stream{
		Delta: func(text string) { deltas = append(deltas, text) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawStream {
		t.Error("the request must ask for a stream")
	}
	if resp.Text != "Hola, mundo." || strings.Join(deltas, "|") != "Hola|, |mundo." {
		t.Errorf("text = %q, deltas = %q", resp.Text, deltas)
	}
}

func TestOpenAIAskStreamAccumulatesToolCallsAndRestartsText(t *testing.T) {
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		round++
		msgs := body["messages"].([]any)
		if last := msgs[len(msgs)-1].(map[string]any); last["role"] == "tool" {
			sseWrite(w, `{"choices":[{"delta":{"content":"Respuesta final."}}]}`)
			return
		}
		// The first round writes a sentence, then calls the tool; its arguments come in two pieces.
		sseWrite(w,
			`{"choices":[{"delta":{"content":"Miro."}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}`,
		)
	}))
	defer srv.Close()
	tools := &lookupTool{}

	var events []string
	resp, err := streamProvider(t, srv.URL, tools).AskStream(context.Background(), Request{Prompt: "busca"}, Stream{
		Delta:   func(text string) { events = append(events, "d:"+text) },
		Restart: func() { events = append(events, "restart") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if round != 2 {
		t.Errorf("rounds = %d, want 2", round)
	}
	if len(tools.calls) != 1 || tools.calls[0] != `lookup {"q":"x"}` {
		t.Errorf("tool calls = %q", tools.calls)
	}
	if resp.Text != "Respuesta final." {
		t.Errorf("text = %q", resp.Text)
	}
	if got, want := strings.Join(events, "|"), "d:Miro.|restart|d:Respuesta final."; got != want {
		t.Errorf("events = %q, want %q", got, want)
	}
}

func TestOpenAIAskStreamFallsBackWhenStreamingIsRefused(t *testing.T) {
	var streamRequests, plainRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stream, _ := readBody(t, r)["stream"].(bool); stream {
			streamRequests++
			http.Error(w, `{"error":"stream not supported"}`, http.StatusBadRequest)
			return
		}
		plainRequests++
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hola"}}]}`)
	}))
	defer srv.Close()

	var deltas int
	resp, err := streamProvider(t, srv.URL, nil).AskStream(context.Background(), Request{Prompt: "hola"}, Stream{
		Delta: func(string) { deltas++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hola" || streamRequests != 1 || plainRequests != 1 {
		t.Errorf("text = %q, stream requests = %d, plain = %d", resp.Text, streamRequests, plainRequests)
	}
	if deltas != 0 {
		t.Errorf("deltas = %d; the refused stream wrote nothing, the whole answer comes back in the response", deltas)
	}
}

func TestOpenAIAskStreamPlainJSONBodyIsOneDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An endpoint that ignores "stream" answers whole.
		fmt.Fprint(w, `{"choices":[{"message":{"content":"entero"}}]}`)
	}))
	defer srv.Close()

	var deltas []string
	resp, err := streamProvider(t, srv.URL, nil).AskStream(context.Background(), Request{Prompt: "x"}, Stream{
		Delta: func(text string) { deltas = append(deltas, text) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "entero" || strings.Join(deltas, "|") != "entero" {
		t.Errorf("text = %q, deltas = %q", resp.Text, deltas)
	}
}

func TestOpenAIAskStreamRateLimitIsAnErrorWithoutRetry(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "rate limit", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := streamProvider(t, srv.URL, nil).AskStream(context.Background(), Request{Prompt: "x"}, Stream{Delta: func(string) {}})
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("err = %v, want HTTP 429", err)
	}
	if requests != 1 {
		t.Errorf("requests = %d; a quota error must not be asked again", requests)
	}
}

func TestOpenAIAskWithoutStreamStaysWhole(t *testing.T) {
	var stream any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream = readBody(t, r)["stream"]
		fmt.Fprint(w, `{"choices":[{"message":{"content":"tal cual"}}]}`)
	}))
	defer srv.Close()
	p := NewOpenAICompat(config.APIProvider{Name: "NIM", BaseURL: srv.URL + "/v1", APIKey: "k", Model: "m"}, nil, 5*time.Second)
	resp, err := p.Ask(context.Background(), Request{Prompt: "x"})
	if err != nil || resp.Text != "tal cual" {
		t.Fatalf("resp = %q, err = %v", resp.Text, err)
	}
	if stream != nil {
		t.Errorf("Ask must not ask for a stream, got stream=%v", stream)
	}
}

// ---- agy stream-json ----

// fixture reads a file of testdata/agy.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "agy", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// lineRunner plays the process: fn decides the result, and each stdout line is passed to
// spec.OnLine as the real runner does.
type lineRunner struct {
	fn    func(spec cmdSpec) (cmdResult, error)
	calls []cmdSpec
}

func (l *lineRunner) run(_ context.Context, spec cmdSpec) (cmdResult, error) {
	l.calls = append(l.calls, spec)
	res, err := l.fn(spec)
	if spec.OnLine != nil {
		for _, line := range bytes.Split(res.Stdout, []byte("\n")) {
			if len(line) > 0 {
				spec.OnLine(line)
			}
		}
	}
	return res, err
}

func TestAgyStepTextReadsTheAnswerSteps(t *testing.T) {
	var got []string
	for _, line := range bytes.Split(fixture(t, "stream-json-answer.ndjson"), []byte("\n")) {
		if _, text := agyStepText(line); text != "" {
			got = append(got, text)
		}
	}
	if strings.Join(got, "") != "ok\n" {
		t.Errorf("answer text = %q, want %q", strings.Join(got, ""), "ok\n")
	}

	var steps []string
	for _, line := range bytes.Split(fixture(t, "stream-json-steps.ndjson"), []byte("\n")) {
		if i, text := agyStepText(line); text != "" {
			steps = append(steps, fmt.Sprintf("%d:%q", i, text))
		}
	}
	want := `1:"Voy a mirar" 1:" tu zona." 3:"Estás en Elwynn." 3:"\n"`
	if strings.Join(steps, " ") != want {
		t.Errorf("steps = %s, want %s", strings.Join(steps, " "), want)
	}
}

func TestAgyStepTextIgnoresOtherEvents(t *testing.T) {
	for _, line := range []string{
		`{"event":"init","init":{"cwd":"x"}}`,
		`{"event":"step_update","step_update":{"step_index":2,"step_type":"tool_call","text_delta":"no"}}`,
		`no es json`,
	} {
		if _, text := agyStepText([]byte(line)); text != "" {
			t.Errorf("%s gave text %q", line, text)
		}
	}
}

func TestAgyAskStreamDeliversAnswerStepsAndRestartsOnNewStep(t *testing.T) {
	cfg := testConfig()
	lr := &lineRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{Stdout: fixture(t, "stream-json-steps.ndjson")}, nil
	}}
	p := newAntigravity(&cfg, t.TempDir(), lr, found)

	var events []string
	resp, err := p.AskStream(context.Background(), Request{Prompt: "¿dónde estoy?"}, Stream{
		Delta:   func(text string) { events = append(events, text) },
		Restart: func() { events = append(events, "<restart>") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(events, "|"), "Voy a mirar| tu zona.|<restart>|Estás en Elwynn.|\n"; got != want {
		t.Errorf("events = %q, want %q", got, want)
	}
	if resp.Text != "Estás en Elwynn." {
		t.Errorf("final text = %q", resp.Text)
	}
	if len(lr.calls) != 1 || argValue(lr.calls[0].Args, "--output-format") != "stream-json" {
		t.Fatalf("agy must run once with stream-json, calls = %d", len(lr.calls))
	}
	if lr.calls[0].OnLine == nil {
		t.Error("the stream must read stdout line by line")
	}
}

func TestAgyAskStreamFallsBackToJSONWhenTheStreamHoldsNoAnswer(t *testing.T) {
	cfg := testConfig()
	lr := &lineRunner{fn: func(spec cmdSpec) (cmdResult, error) {
		if argValue(spec.Args, "--output-format") == "stream-json" {
			return cmdResult{Stdout: []byte(`{"event":"init","init":{"cwd":"x"}}` + "\n")}, nil
		}
		return cmdResult{Stdout: []byte(`{"status":"SUCCESS","response":"hola"}`)}, nil
	}}
	p := newAntigravity(&cfg, t.TempDir(), lr, found)

	var deltas int
	resp, err := p.AskStream(context.Background(), Request{Prompt: "hola"}, Stream{Delta: func(string) { deltas++ }})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hola" || deltas != 0 {
		t.Errorf("text = %q, deltas = %d", resp.Text, deltas)
	}
	if len(lr.calls) != 2 || argValue(lr.calls[1].Args, "--output-format") != "json" {
		t.Errorf("want a second run in json, calls = %d", len(lr.calls))
	}
}

func TestAgyAskStreamQuotaMovesToTheNextModelAndRestarts(t *testing.T) {
	cfg := testConfig()
	if len(cfg.AntigravityModels) < 2 {
		t.Skip("the default model list has one model")
	}
	first, second := cfg.AntigravityModels[0], cfg.AntigravityModels[1]
	lr := &lineRunner{fn: func(spec cmdSpec) (cmdResult, error) {
		if argValue(spec.Args, "--model") == first {
			out := `{"event":"step_update","step_update":{"step_index":1,"step_type":"agent_response","text_delta":"a medias"}}` + "\n" +
				`{"event":"result","result":{"status":"ERROR","error":"429 RESOURCE_EXHAUSTED: quota exceeded"}}` + "\n"
			return cmdResult{Stdout: []byte(out)}, nil
		}
		return cmdResult{Stdout: fixture(t, "stream-json-answer.ndjson")}, nil
	}}
	p := newAntigravity(&cfg, t.TempDir(), lr, found)

	var events []string
	resp, err := p.AskStream(context.Background(), Request{Prompt: "hola"}, Stream{
		Delta:   func(text string) { events = append(events, text) },
		Restart: func() { events = append(events, "<restart>") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Model != second {
		t.Errorf("model = %q, want %q", resp.Model, second)
	}
	if got := strings.Join(events, "|"); got != "a medias|<restart>|ok|\n" {
		t.Errorf("events = %q", got)
	}
}

func TestLineSplitterPassesEveryLineOnce(t *testing.T) {
	var lines []string
	ls := &lineSplitter{fn: func(line []byte) { lines = append(lines, string(line)) }}
	ls.Write([]byte("uno\r\ndo"))
	ls.Write([]byte("s\n\ntres"))
	ls.flush()
	ls.flush()
	if got := strings.Join(lines, "|"); got != "uno|dos||tres" {
		t.Errorf("lines = %q", got)
	}
}

// ---- chain streaming ----

// streamingFake is a provider that can stream; run decides what it writes and returns.
type streamingFake struct {
	name string
	run  func(ctx context.Context, req Request, s Stream) (Response, error)
}

func (f *streamingFake) Name() string     { return f.name }
func (f *streamingFake) Available() error { return nil }
func (f *streamingFake) Ask(ctx context.Context, req Request) (Response, error) {
	return f.run(ctx, req, Stream{})
}
func (f *streamingFake) AskStream(ctx context.Context, req Request, s Stream) (Response, error) {
	return f.run(ctx, req, s)
}

func TestChainAskStreamRestartsWhenAProviderFailsAfterWriting(t *testing.T) {
	broken := &streamingFake{name: "uno", run: func(_ context.Context, _ Request, s Stream) (Response, error) {
		s.Delta("parcial")
		return Response{}, errors.New("cortado")
	}}
	good := &streamingFake{name: "dos", run: func(_ context.Context, _ Request, s Stream) (Response, error) {
		s.Delta("final")
		return Response{Text: "final"}, nil
	}}
	c := newChain(time.Second, broken, good)

	var events []string
	resp, err := c.AskStream(context.Background(), Request{Prompt: "x"}, Stream{
		Delta:   func(text string) { events = append(events, text) },
		Restart: func() { events = append(events, "<restart>") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(events, "|"); got != "parcial|<restart>|final" {
		t.Errorf("events = %q", got)
	}
	if resp.Provider != "dos" || len(resp.Attempts) != 2 {
		t.Errorf("provider = %q, attempts = %+v", resp.Provider, resp.Attempts)
	}
}

func TestChainAskStreamAnswersWholeFromAProviderThatCannotStream(t *testing.T) {
	plain := &fakeProvider{name: "sin-stream", ask: answer("entero")}
	c := newChain(time.Second, plain)

	var deltas int
	resp, err := c.AskStream(context.Background(), Request{Prompt: "x"}, Stream{
		Delta: func(string) { deltas++ },
	})
	if err != nil || resp.Text != "entero" || plain.calls != 1 {
		t.Fatalf("resp = %q, err = %v, calls = %d", resp.Text, err, plain.calls)
	}
	if deltas != 0 {
		t.Errorf("deltas = %d, want none from a provider that cannot stream", deltas)
	}
}

func TestChainAskPassesNoStreamToProviders(t *testing.T) {
	stream := &streamingFake{name: "s", run: func(_ context.Context, _ Request, s Stream) (Response, error) {
		if s.Delta != nil {
			t.Error("Ask must not pass a stream to the provider")
		}
		return Response{Text: "ok"}, nil
	}}
	resp, err := newChain(time.Second, stream).Ask(context.Background(), Request{Prompt: "x"})
	if err != nil || resp.Text != "ok" {
		t.Fatalf("resp = %+v, err = %v", resp, err)
	}
}
