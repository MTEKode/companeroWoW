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
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// maxToolRounds bounds the tool-calling loop of one question.
const maxToolRounds = 8

// maxResponseBytes bounds how much of an API response is read.
const maxResponseBytes = 8 << 20

type openAICompat struct {
	name    string
	baseURL string
	apiKey  string
	model   string
	tools   ToolExecutor
	timeout time.Duration
	client  *http.Client
}

// NewOpenAICompat returns a provider for an OpenAI-compatible chat completions endpoint.
// tools may be nil, in which case no tools are offered to the model.
func NewOpenAICompat(p config.APIProvider, tools ToolExecutor, timeout time.Duration) Provider {
	if timeout <= 0 {
		timeout = defaultAITimeout
	}
	return &openAICompat{
		name:    "api:" + p.Name,
		baseURL: strings.TrimRight(strings.TrimSpace(p.BaseURL), "/"),
		apiKey:  strings.TrimSpace(p.APIKey),
		model:   strings.TrimSpace(p.Model),
		tools:   tools,
		timeout: timeout,
		client:  &http.Client{},
	}
}

func (o *openAICompat) Name() string { return o.name }

func (o *openAICompat) Available() error {
	switch {
	case o.apiKey == "":
		return fmt.Errorf("falta apiKey en %s", o.name)
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
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   *string        `json:"content"`
			ToolCalls []chatToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
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
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	msgs := make([]chatMessage, 0, len(req.History)+3)
	if sys := strings.TrimSpace(req.System); sys != "" {
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
		resp, err := o.post(ctx, chatRequest{Model: o.model, Messages: msgs, Tools: tools})
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
func (o *openAICompat) post(ctx context.Context, body chatRequest) (chatResponse, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return chatResponse{}, fmt.Errorf("%s: codificar petición: %w", o.name, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return chatResponse{}, fmt.Errorf("%s: crear petición: %w", o.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

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
