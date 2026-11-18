package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// Per-model timeouts of the Antigravity chain: Gemini 4 Hydrogen is the slower model, Gemini 3.8
// Flash the fast one. Other models use the configured aiTimeoutSec.
const (
	hydrogenTimeout = 150 * time.Second
	flashTimeout    = 90 * time.Second
)

// effortSuffixes are the effort endings of agy model slugs (gemini-4-hydrogen-high...).
var effortSuffixes = []string{"-low", "-medium", "-high", "-xhigh", "-max"}

// hasEffortSuffix reports whether the slug already names its effort, so that --effort is left out.
func hasEffortSuffix(model string) bool {
	for _, s := range effortSuffixes {
		if strings.HasSuffix(model, s) {
			return true
		}
	}
	return false
}

// modelTimeout is how long one model may take to answer: Hydrogen 150 s, Flash 90 s, any other
// model def (the configured per-provider timeout).
func modelTimeout(model string, def time.Duration) time.Duration {
	switch {
	case strings.Contains(model, "hydrogen"):
		return hydrogenTimeout
	case strings.Contains(model, "flash"):
		return flashTimeout
	}
	return def
}

// DisplayModel is the name of a slug for people: "gemini-4-hydrogen-high" is "Gemini 4 Hydrogen".
// The effort is dropped; digits separated by a dash join with a dot ("claude-opus-5-5" is
// "Claude Opus 5.5").
func DisplayModel(slug string) string {
	s := strings.TrimSpace(slug)
	for _, suf := range effortSuffixes {
		if strings.HasSuffix(s, suf) {
			s = strings.TrimSuffix(s, suf)
			break
		}
	}
	words := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' })
	var b strings.Builder
	prevNumeric := false
	for i, w := range words {
		numeric := isNumeric(w)
		switch {
		case i == 0:
		case numeric && prevNumeric:
			b.WriteByte('.')
		default:
			b.WriteByte(' ')
		}
		if !numeric {
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			w = string(r)
		}
		b.WriteString(w)
		prevNumeric = numeric
	}
	return b.String()
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

// isQuotaFailure reports whether a failure text says that the model's quota or rate limit is
// used up (or the model is unavailable for it). Login problems are never quota problems.
func isQuotaFailure(text string) bool {
	lower := strings.ToLower(text)
	if containsAny(lower, "login", "auth", "sign in", "api key", "not logged in") {
		return false
	}
	return containsAny(lower, "quota", "429", "resource_exhausted", "resource exhausted",
		"rate limit", "rate_limit", "too many requests", "model unavailable", "model_unavailable",
		"model not available")
}

// quotaError is a failure of one model that the provider answers by trying the next model.
type quotaError struct{ err error }

func (e *quotaError) Error() string { return e.err.Error() }
func (e *quotaError) Unwrap() error { return e.err }

// agyExitError is the error of an agy run that exited with a non-zero code. A quota text turns it
// into a quotaError.
func agyExitError(res cmdResult) error {
	err := cliFailure("antigravity", res)
	if isQuotaFailure(string(res.Stderr) + "\n" + string(res.Stdout)) {
		return &quotaError{err}
	}
	return err
}

// quotaMemory remembers, for the whole process, until when each model is out of quota. It is
// shared by every provider so that the answer and the status bar agree.
type quotaMemory struct {
	mu    sync.Mutex
	until map[string]time.Time
}

var quota = &quotaMemory{until: map[string]time.Time{}}

// exhaust marks model as out of quota until the given time.
func (q *quotaMemory) exhaust(model string, until time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.until[model] = until
}

// exhaustedUntil returns the time until which model is out of quota, and whether that time is
// still ahead of now.
func (q *quotaMemory) exhaustedUntil(model string, now time.Time) (time.Time, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	until, ok := q.until[model]
	if !ok || !now.Before(until) {
		return time.Time{}, false
	}
	return until, true
}

// resetQuotaMemory forgets every exhausted model (tests).
func resetQuotaMemory() {
	quota.mu.Lock()
	defer quota.mu.Unlock()
	quota.until = map[string]time.Time{}
}

// ModelStatus says which Antigravity model answers the next question.
type ModelStatus struct {
	Model     string    // the model that answers now
	Exhausted bool      // the primary model is out of quota
	Until     time.Time // when the primary model is tried again (when Exhausted)
}

// AntigravityStatusLabel is the status bar line for the models: "Modelo: Gemini 4 Hydrogen", or
// with the primary out of quota "Modelo: Gemini 3.8 Flash (cuota agotada hasta 15:42)". It is
// empty when agy is not in the provider order.
func AntigravityStatusLabel(cfg *config.Config, now time.Time) string {
	if cfg == nil || !slices.Contains(cfg.ProviderOrder, "antigravity") {
		return ""
	}
	st := AntigravityModelStatus(antigravityModelList(cfg), now)
	label := "Modelo: " + DisplayModel(st.Model)
	if st.Exhausted {
		label += " (cuota agotada hasta " + st.Until.Local().Format("15:04") + ")"
	}
	return label
}

// AntigravityModelStatus is ModelStatus for an ordered model list at the given time: the first
// model out of cooldown answers; the primary is named when every model is out.
func AntigravityModelStatus(models []string, now time.Time) ModelStatus {
	if len(models) == 0 {
		return ModelStatus{}
	}
	primary := models[0]
	until, out := quota.exhaustedUntil(primary, now)
	if !out {
		return ModelStatus{Model: primary}
	}
	for _, m := range models[1:] {
		if _, cooled := quota.exhaustedUntil(m, now); !cooled {
			return ModelStatus{Model: m, Exhausted: true, Until: until}
		}
	}
	return ModelStatus{Model: primary, Exhausted: true, Until: until}
}

// candidates returns the models to try for one question: the ones out of cooldown, in order.
// When every model is cooling down, all of them are tried again, in order.
func (a *antigravity) candidates(now time.Time) []string {
	var out []string
	for _, m := range a.models {
		if _, cooling := quota.exhaustedUntil(m, now); !cooling {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), a.models...)
	}
	return out
}

// agyModelsFile caches the output of `agy models` in the data folder.
const agyModelsFile = "agy-models.json"

// agyModelsRefreshTimeout bounds one `agy models` call.
const agyModelsRefreshTimeout = 60 * time.Second

// builtinAntigravityModels are the models agy listed when the list was written; they are offered
// until the list is fetched.
var builtinAntigravityModels = []string{
	"gemini-4-hydrogen-high", "gemini-4-hydrogen-medium", "gemini-4-hydrogen-low", "gemini-4-hydrogen-xhigh",
	"gemini-4-argon-high", "gemini-4-argon-medium", "gemini-4-argon-low",
	"gemini-3.8-flash-high", "gemini-3.8-flash-medium", "gemini-3.8-flash-low",
	"claude-opus-5-5-high", "claude-opus-5-5-medium", "claude-opus-5-5-low",
	"claude-sonnet-5-5-high", "claude-sonnet-5-5-medium", "claude-sonnet-5-5-low",
	"gpt-oss-120b-medium",
}

// ModelCatalog is the list of models agy offers, with when it was fetched (zero for the built-in list).
type ModelCatalog struct {
	Models    []string  `json:"models"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// ParseAgyModels reads the output of `agy models`: one model per line, the slug first and its
// label after it. Other lines ("Fetching available models...") are skipped.
func ParseAgyModels(out []byte) []string {
	var models []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.Contains(fields[0], "-") {
			continue
		}
		if slug := fields[0]; agyNameRE.MatchString(slug) && !hasString(models, slug) {
			models = append(models, slug)
		}
	}
	return models
}

// CachedAntigravityModels returns the cached `agy models` list in dataDir, or the built-in list
// when there is no cache.
func CachedAntigravityModels(dataDir string) ModelCatalog {
	data, err := os.ReadFile(filepath.Join(dataDir, agyModelsFile))
	if err == nil {
		var c ModelCatalog
		if json.Unmarshal(data, &c) == nil && len(c.Models) > 0 {
			return c
		}
	}
	return ModelCatalog{Models: append([]string(nil), builtinAntigravityModels...)}
}

// RefreshAntigravityModels runs `agy models`, caches the list in dataDir and returns it. The
// cache is not written when agy lists nothing.
func RefreshAntigravityModels(ctx context.Context, command, dataDir string) (ModelCatalog, error) {
	return refreshAntigravityModels(ctx, execRunner{}, resolveAntigravity, command, dataDir)
}

func refreshAntigravityModels(ctx context.Context, r runner, lookPath func(string) (string, error), command, dataDir string) (ModelCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, agyModelsRefreshTimeout)
	defer cancel()
	if command == "" {
		command = "agy"
	}
	exe, err := lookPath(command)
	if err != nil {
		return ModelCatalog{}, fmt.Errorf("CLI antigravity (agy) no disponible: %w", err)
	}
	if err := refuseBatchFile(exe); err != nil {
		return ModelCatalog{}, err
	}
	res, err := r.run(ctx, cmdSpec{Path: exe, Args: []string{"models"}, Dir: dataDir})
	if err != nil {
		return ModelCatalog{}, fmt.Errorf("agy models: %w", err)
	}
	if res.ExitCode != 0 {
		return ModelCatalog{}, cliFailure("agy models", res)
	}
	models := ParseAgyModels(res.Stdout)
	if len(models) == 0 {
		return ModelCatalog{}, errors.New("agy models no listó ningún modelo")
	}
	c := ModelCatalog{Models: models, FetchedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return c, fmt.Errorf("codificar la lista de modelos: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return c, fmt.Errorf("crear %s: %w", dataDir, err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, agyModelsFile), append(data, '\n'), 0o644); err != nil {
		return c, fmt.Errorf("guardar la lista de modelos: %w", err)
	}
	return c, nil
}

func hasString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
