package ai

import (
	"context"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// fakeRunner replaces the process runner; fn decides what each call returns.
type fakeRunner struct {
	fn    func(spec cmdSpec) (cmdResult, error)
	calls []cmdSpec
}

func (f *fakeRunner) run(ctx context.Context, spec cmdSpec) (cmdResult, error) {
	f.calls = append(f.calls, spec)
	if f.fn == nil {
		return cmdResult{}, nil
	}
	return f.fn(spec)
}

// found is a lookPath that resolves every command.
func found(name string) (string, error) { return "/fake/bin/" + name, nil }

func testConfig() config.Config {
	c := config.Defaults()
	c.MCPPort = 47321
	c.AITimeoutSec = 30
	return c
}

func testAPIProviders() []config.APIProvider {
	return []config.APIProvider{
		{Name: "NIM", BaseURL: "https://integrate.example/v1", APIKey: "k1", Model: "m1"},
		{Name: "Z.ai", BaseURL: "https://zai.example/v4", APIKey: "k2", Model: "m2"},
	}
}
