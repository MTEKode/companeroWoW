package assistant

import (
	"context"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

// ToolAdapter exposes the tool registry to the API providers. Specs maps every
// definition to an ai.ToolSpec; Execute runs the tool and joins its text parts. The
// first image part becomes ImagePNG.
func ToolAdapter(reg *tools.Registry) ai.ToolExecutor {
	return toolAdapter{reg: reg}
}

type toolAdapter struct {
	reg *tools.Registry
}

// Specs lists the registry's tools in registration order.
func (t toolAdapter) Specs() []ai.ToolSpec {
	if t.reg == nil {
		return nil
	}
	defs := t.reg.Defs()
	out := make([]ai.ToolSpec, 0, len(defs))
	for _, d := range defs {
		out = append(out, ai.ToolSpec{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.InputSchema,
		})
	}
	return out
}

// Execute runs the named tool with its JSON arguments. An unknown tool is an error; a
// tool failure is returned as output with IsError set, so the model can react to it.
func (t toolAdapter) Execute(ctx context.Context, name string, argsJSON string) (ai.ToolOutput, error) {
	if t.reg == nil {
		return ai.ToolOutput{Text: "No hay herramientas disponibles.", IsError: true}, nil
	}
	args := strings.TrimSpace(argsJSON)
	if args == "" {
		args = "{}"
	}
	res, err := t.reg.Call(ctx, name, []byte(args))
	if err != nil {
		return ai.ToolOutput{}, err
	}
	var out ai.ToolOutput
	var texts []string
	for _, c := range res.Content {
		switch c.Type {
		case "text":
			texts = append(texts, c.Text)
		case "image":
			if out.ImagePNG == nil && len(c.Data) > 0 {
				out.ImagePNG = c.Data
			}
		}
	}
	out.Text = strings.Join(texts, "\n")
	out.IsError = res.IsError
	return out, nil
}
