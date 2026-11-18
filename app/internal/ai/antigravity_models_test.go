package ai

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

const (
	modelHydrogen = "gemini-4-hydrogen-high"
	modelFlash    = "gemini-3.8-flash-medium"
)

// quotaStderr is what agy prints (exit 1) when the model's quota is used up.
const quotaStderr = "Error: 429 RESOURCE_EXHAUSTED: quota exceeded for this model"

// modelOfArgs returns the --model flag of a call ("" when absent).
func modelOfArgs(args []string) string { return argValue(args, "--model") }

// answerFor answers with the failure for the models in fail, and with the envelope for the others.
func answerFor(fail map[string]cmdResult) func(cmdSpec) (cmdResult, error) {
	return func(spec cmdSpec) (cmdResult, error) {
		if res, bad := fail[modelOfArgs(spec.Args)]; bad {
			return res, nil
		}
		return cmdResult{Stdout: []byte(envelopeSuccess)}, nil
	}
}

func TestAntigravityFallsBackToNextModelOnQuotaWithinOneAttempt(t *testing.T) {
	resetQuotaMemory()
	fr := &fakeRunner{fn: answerFor(map[string]cmdResult{modelHydrogen: {ExitCode: 1, Stderr: []byte(quotaStderr)}})}
	cfg := testConfig()
	p := newAntigravity(&cfg, t.TempDir(), fr, found)

	resp, err := p.Ask(context.Background(), Request{Prompt: "x"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Provider != "antigravity:"+modelFlash || resp.Model != modelFlash {
		t.Fatalf("answered by %q / %q, want the fallback %s", resp.Provider, resp.Model, modelFlash)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("runner called %d times, want 2 (hydrogen, then flash)", len(fr.calls))
	}
	if got := modelOfArgs(fr.calls[0].Args); got != modelHydrogen {
		t.Errorf("first call --model = %q, want %s", got, modelHydrogen)
	}
	if got := modelOfArgs(fr.calls[1].Args); got != modelFlash {
		t.Errorf("second call --model = %q, want %s", got, modelFlash)
	}
	if _, cooling := quota.exhaustedUntil(modelHydrogen, time.Now()); !cooling {
		t.Error("hydrogen must be remembered as out of quota")
	}
	if _, cooling := quota.exhaustedUntil(modelFlash, time.Now()); cooling {
		t.Error("flash answered, it must not be on cooldown")
	}
}

func TestAntigravityCooldownStartsWithFallbackThenPrimaryAgain(t *testing.T) {
	resetQuotaMemory()
	fr := &fakeRunner{fn: answerFor(map[string]cmdResult{modelHydrogen: {ExitCode: 1, Stderr: []byte(quotaStderr)}})}
	cfg := testConfig()
	p := newAntigravity(&cfg, t.TempDir(), fr, found)
	if _, err := p.Ask(context.Background(), Request{Prompt: "primera"}); err != nil {
		t.Fatalf("first Ask: %v", err)
	}

	// While the cooldown runs, the next question goes straight to the fallback.
	fr.calls = nil
	resp, err := p.Ask(context.Background(), Request{Prompt: "segunda"})
	if err != nil {
		t.Fatalf("second Ask: %v", err)
	}
	if len(fr.calls) != 1 || modelOfArgs(fr.calls[0].Args) != modelFlash {
		t.Fatalf("during cooldown the calls were %d (first %q), want one call to %s", len(fr.calls), modelOfArgs(fr.calls[0].Args), modelFlash)
	}
	if resp.Model != modelFlash {
		t.Errorf("Model = %q, want %s", resp.Model, modelFlash)
	}

	// Once the cooldown is over, hydrogen is the first again.
	quota.exhaust(modelHydrogen, time.Now().Add(-time.Second))
	fr.fn = answerFor(nil) // hydrogen has quota again
	fr.calls = nil
	resp, err = p.Ask(context.Background(), Request{Prompt: "tercera"})
	if err != nil {
		t.Fatalf("third Ask: %v", err)
	}
	if len(fr.calls) == 0 || modelOfArgs(fr.calls[0].Args) != modelHydrogen {
		t.Fatalf("after the cooldown the first call must be %s", modelHydrogen)
	}
	if resp.Model != modelHydrogen {
		t.Errorf("Model = %q, want %s back after the cooldown", resp.Model, modelHydrogen)
	}
}

func TestAntigravityCooldownUsesConfiguredMinutes(t *testing.T) {
	resetQuotaMemory()
	fr := &fakeRunner{fn: answerFor(map[string]cmdResult{modelHydrogen: {ExitCode: 1, Stderr: []byte(quotaStderr)}})}
	cfg := testConfig()
	cfg.AntigravityQuotaCooldownMin = 5
	p := newAntigravity(&cfg, t.TempDir(), fr, found)
	before := time.Now()
	if _, err := p.Ask(context.Background(), Request{Prompt: "x"}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	until, cooling := quota.exhaustedUntil(modelHydrogen, time.Now())
	if !cooling {
		t.Fatal("hydrogen not on cooldown")
	}
	if d := until.Sub(before); d < 5*time.Minute || d > 5*time.Minute+time.Minute {
		t.Errorf("cooldown = %v, want about 5 min", d)
	}
}

func TestAntigravityNonQuotaErrorDoesNotSwitchModel(t *testing.T) {
	resetQuotaMemory()
	fr := &fakeRunner{fn: answerFor(map[string]cmdResult{
		modelHydrogen: {ExitCode: 1, Stderr: []byte("Error: authentication required. Please sign in.")},
	})}
	cfg := testConfig()
	p := newAntigravity(&cfg, t.TempDir(), fr, found)

	_, err := p.Ask(context.Background(), Request{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "sesión no iniciada") {
		t.Fatalf("err = %v, want the auth failure", err)
	}
	if len(fr.calls) != 1 {
		t.Errorf("runner called %d times, an auth failure must not try another model", len(fr.calls))
	}
	var qe *quotaError
	if errors.As(err, &qe) {
		t.Error("an auth failure must not be a quota error")
	}
	if _, cooling := quota.exhaustedUntil(modelHydrogen, time.Now()); cooling {
		t.Error("an auth failure must not put the model on cooldown")
	}
}

func TestAntigravityQuotaInJSONStatusSwitchesModel(t *testing.T) {
	resetQuotaMemory()
	fr := &fakeRunner{fn: func(spec cmdSpec) (cmdResult, error) {
		if modelOfArgs(spec.Args) == modelHydrogen {
			return cmdResult{Stdout: []byte(`{"status":"ERROR","error":"429 RESOURCE_EXHAUSTED: quota exceeded"}`)}, nil
		}
		return cmdResult{Stdout: []byte(envelopeSuccess)}, nil
	}}
	cfg := testConfig()
	p := newAntigravity(&cfg, t.TempDir(), fr, found)
	resp, err := p.Ask(context.Background(), Request{Prompt: "x"})
	if err != nil || resp.Model != modelFlash {
		t.Fatalf("resp.Model = %q, err = %v; want the fallback answer", resp.Model, err)
	}
}

func TestAntigravityAllModelsOutOfQuotaIsAnError(t *testing.T) {
	resetQuotaMemory()
	fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
		return cmdResult{ExitCode: 1, Stderr: []byte(quotaStderr)}, nil
	}}
	cfg := testConfig()
	p := newAntigravity(&cfg, t.TempDir(), fr, found)
	_, err := p.Ask(context.Background(), Request{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "cuota agotada en todos los modelos") {
		t.Fatalf("err = %v, want the all-models quota error", err)
	}
	if len(fr.calls) != 2 {
		t.Errorf("runner called %d times, want one per model", len(fr.calls))
	}
	// With every model cooling down, the next question tries them all again, in order.
	fr.calls = nil
	_, _ = p.Ask(context.Background(), Request{Prompt: "y"})
	if len(fr.calls) != 2 || modelOfArgs(fr.calls[0].Args) != modelHydrogen {
		t.Errorf("all cooling down: calls = %d, want both models again starting with hydrogen", len(fr.calls))
	}
}

func TestAntigravityModelOrderFromConfigAndLegacyField(t *testing.T) {
	resetQuotaMemory()
	cfg := testConfig()
	cfg.AntigravityModels = []string{"gemini-4-argon-high", modelFlash}
	p := newAntigravity(&cfg, t.TempDir(), &fakeRunner{}, found)
	if want := []string{"gemini-4-argon-high", modelFlash}; strings.Join(p.models, ",") != strings.Join(want, ",") {
		t.Errorf("models = %v, want %v", p.models, want)
	}

	cfg = testConfig()
	cfg.AntigravityModels = nil
	cfg.AntigravityModel = "gemini-4-argon-low"
	p = newAntigravity(&cfg, t.TempDir(), &fakeRunner{}, found)
	if len(p.models) != 1 || p.models[0] != "gemini-4-argon-low" {
		t.Errorf("legacy single model not read: %v", p.models)
	}
}

func TestAntigravityBudgetIsTheSumOfModelTimeouts(t *testing.T) {
	cfg := testConfig()
	cfg.AntigravityModels = []string{modelHydrogen, modelFlash}
	p := newAntigravity(&cfg, t.TempDir(), &fakeRunner{}, found)
	if got, want := p.Budget(), hydrogenTimeout+flashTimeout; got != want {
		t.Errorf("Budget = %v, want %v", got, want)
	}
	if got := modelTimeout(modelHydrogen, time.Second); got != 150*time.Second {
		t.Errorf("hydrogen timeout = %v, want 150s", got)
	}
	if got := modelTimeout(modelFlash, time.Second); got != 90*time.Second {
		t.Errorf("flash timeout = %v, want 90s", got)
	}
	if got := modelTimeout("claude-opus-5-5-high", 42*time.Second); got != 42*time.Second {
		t.Errorf("other models use the configured timeout, got %v", got)
	}
}

func TestAntigravityEffortFlagOnlyForSlugsWithoutEffort(t *testing.T) {
	cases := []struct {
		name       string
		models     []string
		legacy     string
		effort     string
		wantModel  string
		wantEffort string
	}{
		{"default order: hydrogen slug carries its effort", nil, "", "low", modelHydrogen, ""},
		{"slug without suffix takes --effort", []string{"gemini-4-argon"}, "", "high", "gemini-4-argon", "high"},
		{"invalid names fall back to the default order", []string{"bad model"}, "", "medium", modelHydrogen, ""},
		{"unknown effort is dropped", []string{"gemini-4-argon"}, "", "turbo", "gemini-4-argon", ""},
		{"legacy antigravityModel is read", nil, "gemini-4-argon-low", "low", "gemini-4-argon-low", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetQuotaMemory()
			fr := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) {
				return cmdResult{Stdout: []byte(envelopeSuccess)}, nil
			}}
			cfg := testConfig()
			cfg.AntigravityModels = c.models
			cfg.AntigravityModel = c.legacy
			cfg.AntigravityEffort = c.effort
			p := newAntigravity(&cfg, t.TempDir(), fr, found)
			if _, err := p.Ask(context.Background(), Request{Prompt: "x"}); err != nil {
				t.Fatalf("Ask: %v", err)
			}
			args := fr.calls[0].Args
			if got := modelOfArgs(args); got != c.wantModel {
				t.Errorf("--model = %q, want %q (args %v)", got, c.wantModel, args)
			}
			if got := argValue(args, "--effort"); got != c.wantEffort {
				t.Errorf("--effort = %q, want %q (args %v)", got, c.wantEffort, args)
			}
			if args[len(args)-2] != "-p" {
				t.Errorf("the prompt must come last: %v", args)
			}
		})
	}
}

func TestIsQuotaFailure(t *testing.T) {
	cases := map[string]bool{
		"429 RESOURCE_EXHAUSTED: quota exceeded":   true,
		"Rate limit reached, try later":            true,
		"model unavailable for this account":       true,
		"too many requests":                        true,
		"authentication required (quota page)":     false,
		"authentication required. Please sign in.": false,
		"tiempo agotado":                           false,
		"connection reset by peer":                 false,
	}
	for text, want := range cases {
		if got := isQuotaFailure(text); got != want {
			t.Errorf("isQuotaFailure(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestDisplayModel(t *testing.T) {
	cases := map[string]string{
		"gemini-4-hydrogen-high":  "Gemini 4 Hydrogen",
		"gemini-3.8-flash-medium": "Gemini 3.8 Flash",
		"claude-opus-5-5-low":     "Claude Opus 5.5",
		"gemini-4-argon":          "Gemini 4 Argon",
	}
	for slug, want := range cases {
		if got := DisplayModel(slug); got != want {
			t.Errorf("DisplayModel(%q) = %q, want %q", slug, got, want)
		}
	}
}

func TestAntigravityModelStatusAndLabel(t *testing.T) {
	resetQuotaMemory()
	now := time.Now()
	models := []string{modelHydrogen, modelFlash}
	if st := AntigravityModelStatus(models, now); st.Model != modelHydrogen || st.Exhausted {
		t.Fatalf("status = %+v, want hydrogen and no exhaustion", st)
	}
	cfg := testConfig()
	if got := AntigravityStatusLabel(&cfg, now); got != "Modelo: Gemini 4 Hydrogen" {
		t.Errorf("label = %q", got)
	}

	until := now.Add(20 * time.Minute)
	quota.exhaust(modelHydrogen, until)
	st := AntigravityModelStatus(models, now)
	if st.Model != modelFlash || !st.Exhausted || !st.Until.Equal(until) {
		t.Fatalf("status = %+v, want flash with hydrogen exhausted until %v", st, until)
	}
	want := "Modelo: Gemini 3.8 Flash (cuota agotada hasta " + until.Local().Format("15:04") + ")"
	if got := AntigravityStatusLabel(&cfg, now); got != want {
		t.Errorf("label = %q, want %q", got, want)
	}

	cfg.ProviderOrder = []string{"codex"}
	if got := AntigravityStatusLabel(&cfg, now); got != "" {
		t.Errorf("label without antigravity = %q, want empty", got)
	}
}

func TestParseAgyModels(t *testing.T) {
	out := "Fetching available models...\n" +
		"gemini-4-argon-medium\tGemini 4 Argon (Medium)\n" +
		"gemini-4-hydrogen-high\tGemini 4 Hydrogen (High)\n" +
		"gemini-4-argon-medium\tduplicado\n" +
		"\n"
	got := ParseAgyModels([]byte(out))
	if want := []string{"gemini-4-argon-medium", "gemini-4-hydrogen-high"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ParseAgyModels = %v, want %v", got, want)
	}
}

func TestRefreshAntigravityModelsCachesTheList(t *testing.T) {
	dir := t.TempDir()
	fr := &fakeRunner{fn: func(spec cmdSpec) (cmdResult, error) {
		if len(spec.Args) != 1 || spec.Args[0] != "models" {
			t.Errorf("args = %v, want [models]", spec.Args)
		}
		return cmdResult{Stdout: []byte("Fetching available models...\ngemini-4-hydrogen-high\tGemini 4 Hydrogen (High)\n")}, nil
	}}
	c, err := refreshAntigravityModels(context.Background(), fr, found, "agy", dir)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(c.Models) != 1 || c.FetchedAt.IsZero() {
		t.Fatalf("catalog = %+v", c)
	}
	cached := CachedAntigravityModels(dir)
	if len(cached.Models) != 1 || cached.Models[0] != "gemini-4-hydrogen-high" || cached.FetchedAt.IsZero() {
		t.Errorf("cache = %+v", cached)
	}
	if _, err := os.Stat(filepath.Join(dir, agyModelsFile)); err != nil {
		t.Errorf("cache file: %v", err)
	}

	// Without a cache, the built-in list is offered.
	if got := CachedAntigravityModels(t.TempDir()); len(got.Models) != len(builtinAntigravityModels) || !got.FetchedAt.IsZero() {
		t.Errorf("built-in catalog = %+v", got)
	}

	// Nothing listed is an error, and the cache keeps the old list.
	empty := &fakeRunner{fn: func(cmdSpec) (cmdResult, error) { return cmdResult{Stdout: []byte("Fetching...\n")}, nil }}
	if _, err := refreshAntigravityModels(context.Background(), empty, found, "agy", dir); err == nil {
		t.Error("an empty listing must be an error")
	}
	if got := CachedAntigravityModels(dir); got.Models[0] != "gemini-4-hydrogen-high" {
		t.Errorf("the cache changed after a failed refresh: %+v", got)
	}
}

// budgetProvider is a fake provider that needs more time than the chain's per-provider timeout.
type budgetProvider struct {
	fakeProvider
	budget time.Duration
}

func (b *budgetProvider) Budget() time.Duration { return b.budget }

func TestChainUsesProviderBudget(t *testing.T) {
	slow := &budgetProvider{
		fakeProvider: fakeProvider{name: "antigravity", ask: func(ctx context.Context, _ Request) (Response, error) {
			time.Sleep(60 * time.Millisecond)
			return Response{Text: "respuesta", Provider: "antigravity:" + modelFlash}, nil
		}},
		budget: 2 * time.Second,
	}
	c := newChain(20*time.Millisecond, slow)
	resp, err := c.Ask(context.Background(), Request{Prompt: "x"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Provider != "antigravity:"+modelFlash {
		t.Errorf("Provider = %q, want the provider's own name for the model that answered", resp.Provider)
	}
	if resp.Attempts[0].Provider != "antigravity:"+modelFlash {
		t.Errorf("attempt provider = %q", resp.Attempts[0].Provider)
	}
}

func TestBuildChainAntigravityBudgetAndDefaults(t *testing.T) {
	cfg := config.Defaults()
	c := BuildChain(&cfg, t.TempDir(), nil)
	if len(c.providers) == 0 {
		t.Fatal("no providers")
	}
	b, ok := c.providers[0].(budgeted)
	if !ok {
		t.Fatalf("antigravity provider %T has no Budget", c.providers[0])
	}
	if b.Budget() != hydrogenTimeout+flashTimeout {
		t.Errorf("Budget = %v", b.Budget())
	}
}
