// Package tools is the transport-agnostic catalogue of tools the AI can call. The MCP
// server and the in-process tool calling both dispatch through a Registry. Names,
// descriptions and all text returned to the AI are in Spanish.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Content is one piece of a tool result. Type is "text" or "image".
type Content struct {
	Type     string
	Text     string
	Data     []byte
	MimeType string
}

// Result is what a tool returns to the AI.
type Result struct {
	Content []Content
	IsError bool
}

// Def describes one tool and how to run it.
type Def struct {
	Name        string
	Description string
	InputSchema map[string]any // JSON Schema object
	Handler     func(ctx context.Context, args json.RawMessage) (Result, error)
}

// CallRecord describes one finished call, for observers such as the evaluation harness.
type CallRecord struct {
	Name     string
	Args     string
	Text     string
	IsError  bool
	Err      string
	Duration time.Duration
}

// Registry holds tools in registration order. It is not safe for concurrent Add;
// build it once and then call it from any goroutine.
type Registry struct {
	defs     []Def
	index    map[string]int
	observer func(CallRecord)
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{index: make(map[string]int)}
}

// Add registers d. A tool with the same name replaces the earlier one in place.
func (r *Registry) Add(d Def) {
	if i, ok := r.index[d.Name]; ok {
		r.defs[i] = d
		return
	}
	r.index[d.Name] = len(r.defs)
	r.defs = append(r.defs, d)
}

// Defs returns the tools in registration order.
func (r *Registry) Defs() []Def {
	out := make([]Def, len(r.defs))
	copy(out, r.defs)
	return out
}

// SetObserver makes f receive a CallRecord after every call, from any transport. Nil
// removes the observer. Set it before the registry serves calls; f must be safe for
// concurrent use because MCP and API calls may overlap.
func (r *Registry) SetObserver(f func(CallRecord)) {
	r.observer = f
}

// Call runs the named tool. An unknown name is an error. Errors and panics raised by
// the handler are returned as a Result with IsError set and a Spanish message.
func (r *Registry) Call(ctx context.Context, name string, args json.RawMessage) (Result, error) {
	start := time.Now()
	res, err := r.call(ctx, name, args)
	if r.observer != nil {
		rec := CallRecord{Name: name, Args: string(args), IsError: res.IsError, Duration: time.Since(start)}
		for _, c := range res.Content {
			if c.Type == "text" {
				rec.Text += c.Text
			}
		}
		if err != nil {
			rec.Err = err.Error()
		}
		r.observer(rec)
	}
	return res, err
}

func (r *Registry) call(ctx context.Context, name string, args json.RawMessage) (res Result, err error) {
	i, ok := r.index[name]
	if !ok {
		return Result{}, fmt.Errorf("herramienta desconocida: %s", name)
	}
	d := r.defs[i]
	if d.Handler == nil {
		return errorResult(fmt.Sprintf("La herramienta %s no está implementada.", name)), nil
	}
	defer func() {
		if p := recover(); p != nil {
			res = errorResult(fmt.Sprintf("Error interno al ejecutar %s: %v", name, p))
			err = nil
		}
	}()
	out, herr := d.Handler(ctx, args)
	if herr != nil {
		return errorResult(herr.Error()), nil
	}
	return out, nil
}

func errorResult(msg string) Result {
	return Result{IsError: true, Content: []Content{{Type: "text", Text: msg}}}
}

// textResult wraps a string as a single text content item.
func textResult(s string) Result {
	return Result{Content: []Content{{Type: "text", Text: s}}}
}
