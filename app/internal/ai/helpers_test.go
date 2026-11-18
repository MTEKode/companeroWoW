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
	// The quota memory is shared by the process: each test starts with no model on cooldown.
	resetQuotaMemory()
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

// argValue returns the argument that follows flag, or "" when flag is absent.
func argValue(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

// containsString reports whether s is in list.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// hasArg reports whether flag appears in args.
func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
