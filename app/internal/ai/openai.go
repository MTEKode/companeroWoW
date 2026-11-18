package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// maxToolRounds bounds the tool-calling loop of one question.
const maxToolRounds = 8

// maxResponseBytes bounds how much of an API response is read.
const maxResponseBytes = 8 << 20

type openAICompat struct {
	name     string
	provider config.APIProvider // the key is resolved on each call: literal, env var or .env
	baseURL  string
	model    string
	tools    ToolExecutor
	timeout  time.Duration
	client   *http.Client
}

// NewOpenAICompat returns a provider for an OpenAI-compatible chat completions endpoint.
// tools may be nil, in which case no tools are offered to the model.
func NewOpenAICompat(p config.APIProvider, tools ToolExecutor, timeout time.Duration) Provider {
	if timeout <= 0 {
		timeout = defaultAITimeout
	}
	return &openAICompat{
		name:     "api:" + p.Name,
		provider: p,
		baseURL:  strings.TrimRight(strings.TrimSpace(p.BaseURL), "/"),
		model:    strings.TrimSpace(p.Model),
		tools:    tools,
		timeout:  timeout,
		client:   &http.Client{},
	}
}

func (o *openAICompat) Name() string { return o.name }

func (o *openAICompat) Available() error {
	if _, err := o.provider.ResolveKey(); err != nil {
		return fmt.Errorf("%s: %w", o.name, err)
	}
	switch {
	case o.baseURL == "":
		return fmt.Errorf("falta baseUrl en %s", o.name)
	case o.model == "":
		return fmt.Errorf("falta model en %s", o.name)
	}
	return nil
}

// Wire types of the chat completions API.

type chatMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description,omitempty"`
		Parameters  map[string]any `json:"parameters,omitempty"`
	} `json:"function"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools,omitempty"`
	// Stream asks for the answer as server-sent events (see postStream).
	Stream bool `json:"stream,omitempty"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
}

type chatChoice struct {
	Message respMessage `json:"message"`
}

type respMessage struct {
	Content   *string        `json:"content"`
	ToolCalls []chatToolCall `json:"tool_calls"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

func imageDataURL(png []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// userContent is plain text, or text plus an image part when a screenshot is attached.
func userContent(text string, png []byte) any {
	if len(png) == 0 {
		return text
	}
	return []contentPart{
		{Type: "text", Text: text},
		{Type: "image_url", ImageURL: &imageURL{URL: imageDataURL(png)}},
	}
}

func (o *openAICompat) Ask(ctx context.Context, req Request) (Response, error) {
	return o.ask(ctx, req, Stream{})
}

// AskStream is Ask with the text of each round written to s as the server sends it. A round
// that ends in tool calls restarts the text, since only the last round is the answer.
func (o *openAICompat) AskStream(ctx context.Context, req Request, s Stream) (Response, error) {
	return o.ask(ctx, req, s)
}

func (o *openAICompat) ask(ctx context.Context, req Request, s Stream) (Response, error) {
	key, err := o.provider.ResolveKey()
	if err != nil {
		return Response{}, fmt.Errorf("%s: %w", o.name, err)
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	msgs := make([]chatMessage, 0, len(req.History)+3)
	if sys := systemText(Request{Skill: req.Skill, System: req.System}); sys != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: sys})
	}
	for _, m := range req.History {
		role := "user"
		if m.Role == "assistant" {
			role = "assistant"
		}
		msgs = append(msgs, chatMessage{Role: role, Content: m.Content})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: userContent(req.Prompt, req.ImagePNG)})

	var tools []chatTool
	if o.tools != nil {
		for _, s := range o.tools.Specs() {
			var t chatTool
			t.Type = "function"
			t.Function.Name = s.Name
			t.Function.Description = s.Description
			t.Function.Parameters = s.Parameters
			tools = append(tools, t)
		}
	}

	for round := 0; round < maxToolRounds; round++ {
		body := chatRequest{Model: o.model, Messages: msgs, Tools: tools}
		resp, err := o.round(ctx, key, body, s)
		if err != nil {
			return Response{}, err
		}
		if len(resp.Choices) == 0 {
			return Response{}, fmt.Errorf("%s: la respuesta no trae choices", o.name)
		}
		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			text := ""
			if msg.Content != nil {
				text = strings.TrimSpace(*msg.Content)
			}
			return Response{Text: text, Provider: o.name}, nil
		}
		if o.tools == nil {
			return Response{}, fmt.Errorf("%s: el modelo pidió herramientas pero no hay ninguna configurada", o.name)
		}
		// The text written so far was the model's intermediate turn, not the answer.
		s.restart()

		// Echo the assistant turn with its tool calls, then answer each call.
		var assistantContent any
		if msg.Content != nil && *msg.Content != "" {
			assistantContent = *msg.Content
		}
		calls := make([]chatToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			if tc.Type == "" {
				tc.Type = "function"
			}
			calls[i] = tc
		}
		msgs = append(msgs, chatMessage{Role: "assistant", Content: assistantContent, ToolCalls: calls})

		type pendingImage struct {
			tool string
			png  []byte
		}
		var images []pendingImage
		for _, tc := range calls {
			out, err := o.tools.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
			text := out.Text
			switch {
			case err != nil:
				text = "error: " + err.Error()
			case out.IsError:
				text = "error: " + text
			}
			msgs = append(msgs, chatMessage{Role: "tool", ToolCallID: tc.ID, Content: text})
			if err == nil && len(out.ImagePNG) > 0 {
				images = append(images, pendingImage{tool: tc.Function.Name, png: out.ImagePNG})
			}
		}
		// Images cannot ride in a tool message, so they follow as a user message.
		for _, img := range images {
			msgs = append(msgs, chatMessage{
				Role: "user",
				Content: []contentPart{
					{Type: "text", Text: "Imagen devuelta por la herramienta " + img.tool + ":"},
					{Type: "image_url", ImageURL: &imageURL{URL: imageDataURL(img.png)}},
				},
			})
		}
	}
	return Response{}, fmt.Errorf("%s: se superó el máximo de %d rondas de herramientas", o.name, maxToolRounds)
}

// post sends one chat completions request. Non-2xx statuses are errors so the chain falls back.
func (o *openAICompat) post(ctx context.Context, key string, body chatRequest) (chatResponse, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return chatResponse{}, fmt.Errorf("%s: codificar petición: %w", o.name, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return chatResponse{}, fmt.Errorf("%s: crear petición: %w", o.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return chatResponse{}, fmt.Errorf("%s: %w", o.name, ctx.Err())
		}
		return chatResponse{}, fmt.Errorf("%s: %w", o.name, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return chatResponse{}, fmt.Errorf("%s: leer respuesta: %w", o.name, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return chatResponse{}, httpStatusError(o.name, resp.StatusCode, raw)
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return chatResponse{}, fmt.Errorf("%s: respuesta JSON inválida: %w", o.name, err)
	}
	return out, nil
}

// round asks for one turn of the tool loop: streamed when s wants the text, else whole. An
// endpoint that refuses the streaming request before it writes anything is asked again whole.
func (o *openAICompat) round(ctx context.Context, key string, body chatRequest, s Stream) (chatResponse, error) {
	if !s.on() {
		return o.post(ctx, key, body)
	}
	body.Stream = true
	resp, err := o.postStream(ctx, key, body, s)
	if errors.Is(err, errStreamUnsupported) && ctx.Err() == nil {
		body.Stream = false
		return o.post(ctx, key, body)
	}
	return resp, err
}

// postStream sends a streamed chat completions request and accumulates the chunks into one
// response, passing each text piece to s as it arrives. A body that is plain JSON (an
// endpoint that ignores stream) is read whole. An error before any text is written may be
// errStreamUnsupported; a refused request that is not about streaming is an HTTP error.
func (o *openAICompat) postStream(ctx context.Context, key string, body chatRequest, s Stream) (chatResponse, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return chatResponse{}, fmt.Errorf("%s: codificar petición: %w", o.name, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return chatResponse{}, fmt.Errorf("%s: crear petición: %w", o.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+key)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return chatResponse{}, fmt.Errorf("%s: %w", o.name, ctx.Err())
		}
		return chatResponse{}, fmt.Errorf("%s: %w", o.name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		switch resp.StatusCode {
		case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed,
			http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity, http.StatusNotImplemented:
			return chatResponse{}, errStreamUnsupported
		}
		return chatResponse{}, httpStatusError(o.name, resp.StatusCode, raw)
	}

	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return chatResponse{}, fmt.Errorf("%s: leer respuesta: %w", o.name, err)
		}
		var out chatResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return chatResponse{}, fmt.Errorf("%s: respuesta JSON inválida: %w", o.name, err)
		}
		if len(out.Choices) > 0 && out.Choices[0].Message.Content != nil {
			s.delta(*out.Choices[0].Message.Content)
		}
		return out, nil
	}

	var acc streamAcc
	err = readSSE(resp.Body, func(ev string) error {
		if ev == "[DONE]" {
			return errSSEStop
		}
		var ch chatStreamChunk
		if err := json.Unmarshal([]byte(ev), &ch); err != nil {
			return fmt.Errorf("evento SSE inválido: %w", err)
		}
		if msg := strings.TrimSpace(string(ch.Error)); msg != "" && msg != "null" {
			return fmt.Errorf("error en el flujo: %s", clipRunes(strings.Join(strings.Fields(msg), " "), 200))
		}
		acc.add(ch, s)
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return chatResponse{}, fmt.Errorf("%s: %w", o.name, ctx.Err())
		}
		return chatResponse{}, fmt.Errorf("%s: %w", o.name, err)
	}
	return acc.response(), nil
}

// chatStreamChunk is one server-sent event of a streamed chat completion.
type chatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   *string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Error json.RawMessage `json:"error"`
}

// streamAcc joins the chunks of one streamed answer: the text in order, and each tool call
// from its fragments (the arguments arrive in pieces, keyed by the call's index).
type streamAcc struct {
	content strings.Builder
	calls   map[int]*chatToolCall
}

func (a *streamAcc) add(ch chatStreamChunk, s Stream) {
	for _, c := range ch.Choices {
		if c.Delta.Content != nil && *c.Delta.Content != "" {
			a.content.WriteString(*c.Delta.Content)
			s.delta(*c.Delta.Content)
		}
		for _, tc := range c.Delta.ToolCalls {
			if a.calls == nil {
				a.calls = map[int]*chatToolCall{}
			}
			cur := a.calls[tc.Index]
			if cur == nil {
				cur = &chatToolCall{}
				a.calls[tc.Index] = cur
			}
			if tc.ID != "" {
				cur.ID = tc.ID
			}
			if tc.Type != "" {
				cur.Type = tc.Type
			}
			if cur.Function.Name == "" {
				cur.Function.Name = tc.Function.Name
			}
			cur.Function.Arguments += tc.Function.Arguments
		}
	}
}

// response is the accumulated answer in the shape of a whole response.
func (a *streamAcc) response() chatResponse {
	text := a.content.String()
	msg := respMessage{}
	if text != "" {
		msg.Content = &text
	}
	idx := make([]int, 0, len(a.calls))
	for i := range a.calls {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		msg.ToolCalls = append(msg.ToolCalls, *a.calls[i])
	}
	return chatResponse{Choices: []chatChoice{{Message: msg}}}
}

func httpStatusError(name string, status int, body []byte) error {
	var hint string
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		hint = "clave API rechazada"
	case status == http.StatusTooManyRequests:
		hint = "límite de uso o cuota agotada"
	case status >= 500:
		hint = "error del servidor"
	}
	snippet := strings.Join(strings.Fields(string(body)), " ")
	if r := []rune(snippet); len(r) > 200 {
		snippet = string(r[:200]) + "…"
	}
	msg := fmt.Sprintf("%s: HTTP %d", name, status)
	if hint != "" {
		msg += " (" + hint + ")"
	}
	if snippet != "" {
		msg += ": " + snippet
	}
	return errors.New(msg)
}
