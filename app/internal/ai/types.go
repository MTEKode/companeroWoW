// Package ai answers the companion's questions through an ordered chain of providers:
// the Antigravity (agy) and Codex CLIs (spawned on demand, never resident) and
// OpenAI-compatible HTTP APIs that run their own tool-calling loop.
package ai

import (
	"context"
	"time"
)

// Message is one previous turn of the conversation.
type Message struct {
	Role    string `json:"role"`    // "user" | "assistant"
	Content string `json:"content"` // plain text
}

// Request is one question for the providers.
type Request struct {
	System   string    // Spanish system prompt (built by the assistant task)
	History  []Message // previous turns, oldest first
	Prompt   string
	ImagePNG []byte // optional screenshot to attach
}

// Response is the answer of the provider that succeeded, plus every attempt made.
type Response struct {
	Text     string
	Provider string
	Attempts []Attempt
}

// Attempt records one provider try inside a Chain.Ask call.
type Attempt struct {
	Provider string
	Err      string // empty on success
	Duration time.Duration
}

// Provider is one way of asking the model.
type Provider interface {
	Name() string     // "antigravity", "codex", "api:<Name>"
	Available() error // cheap check: binary on PATH / API key configured
	Ask(ctx context.Context, req Request) (Response, error)
}

// ToolSpec describes an in-process tool offered to API providers.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema object
}

// ToolOutput is what a tool returns to the model.
type ToolOutput struct {
	Text     string
	ImagePNG []byte
	IsError  bool
}

// ToolExecutor runs in-process tools for API providers.
type ToolExecutor interface {
	Specs() []ToolSpec
	Execute(ctx context.Context, name string, argsJSON string) (ToolOutput, error)
}
