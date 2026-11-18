package ai

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

const defaultAITimeout = 120 * time.Second

// aiTimeout returns the per-provider timeout from cfg.AITimeoutSec.
func aiTimeout(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.AITimeoutSec <= 0 {
		return defaultAITimeout
	}
	return time.Duration(cfg.AITimeoutSec) * time.Second
}

// budgeted is implemented by a provider that needs more time than the chain's per-provider
// timeout, because it runs several attempts of its own (see antigravity).
type budgeted interface {
	Budget() time.Duration
}

// Chain asks its providers in order and returns the first usable answer.
type Chain struct {
	providers []Provider
	timeout   time.Duration
}

// newChain is the test-friendly constructor.
func newChain(timeout time.Duration, providers ...Provider) *Chain {
	if timeout <= 0 {
		timeout = defaultAITimeout
	}
	return &Chain{providers: providers, timeout: timeout}
}

// BuildChain builds the chain from cfg.ProviderOrder. "antigravity" and "codex" are the CLIs,
// "api" expands to one provider per cfg.APIProviders entry. Unknown names (such as the retired
// "gemini") are skipped and logged.
func BuildChain(cfg *config.Config, workDir string, tools ToolExecutor) *Chain {
	timeout := aiTimeout(cfg)
	c := &Chain{timeout: timeout}
	for _, name := range cfg.ProviderOrder {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "antigravity":
			c.providers = append(c.providers, NewAntigravity(cfg, workDir))
		case "codex":
			c.providers = append(c.providers, NewCodex(cfg, workDir))
		case "api":
			for _, p := range cfg.APIProviders {
				c.providers = append(c.providers, NewOpenAICompat(p, tools, timeout))
			}
		default:
			log.Printf("ai: proveedor %q omitido (no existe en esta versión)", name)
		}
	}
	return c
}

// Providers returns the names of the providers in the chain, in order.
func (c *Chain) Providers() []string {
	names := make([]string, 0, len(c.providers))
	for _, p := range c.providers {
		names = append(names, p.Name())
	}
	return names
}

// Ask tries each provider in order. Providers whose Available check fails are skipped
// (they are not recorded as attempts). A provider that errors, times out or returns
// empty text is recorded and the next one is tried. The first success is returned with
// every attempt filled in. If all fail, the error lists each attempt in Spanish.
func (c *Chain) Ask(ctx context.Context, req Request) (Response, error) {
	return c.run(ctx, req, Stream{})
}

// AskStream is Ask with the answer written to s as it comes. A provider that cannot stream
// answers whole, as with Ask. When a provider fails after it wrote part of the answer, s.Restart
// is called before the next provider starts.
func (c *Chain) AskStream(ctx context.Context, req Request, s Stream) (Response, error) {
	return c.run(ctx, req, s)
}

func (c *Chain) run(ctx context.Context, req Request, s Stream) (Response, error) {
	if len(c.providers) == 0 {
		return Response{}, errors.New("no hay proveedores de IA configurados (revisa providerOrder y apiProviders)")
	}
	var attempts []Attempt
	var skipped []string
	for _, p := range c.providers {
		if err := ctx.Err(); err != nil {
			return Response{Attempts: attempts}, fmt.Errorf("consulta cancelada: %w", err)
		}
		name := p.Name()
		if err := p.Available(); err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", name, err))
			continue
		}

		budget := c.timeout
		if b, ok := p.(budgeted); ok {
			budget = b.Budget()
		}
		start := time.Now()
		pctx, cancel := context.WithTimeout(ctx, budget)
		var resp Response
		var err error
		written := false
		if st, ok := p.(streamer); ok && s.on() {
			ps := Stream{
				Delta: func(text string) {
					written = true
					s.delta(text)
				},
				Restart: s.Restart,
			}
			resp, err = st.AskStream(pctx, req, ps)
		} else {
			resp, err = p.Ask(pctx, req)
		}
		timedOut := errors.Is(pctx.Err(), context.DeadlineExceeded)
		cancel()
		elapsed := time.Since(start)

		if err != nil {
			if ctx.Err() != nil {
				return Response{Attempts: attempts}, fmt.Errorf("consulta cancelada: %w", ctx.Err())
			}
			if timedOut {
				err = fmt.Errorf("tiempo agotado tras %s", budget)
			}
		} else if strings.TrimSpace(resp.Text) == "" {
			err = errors.New("respuesta vacía")
		}
		if err != nil {
			if written {
				s.restart()
			}
			attempts = append(attempts, Attempt{Provider: name, Err: err.Error(), Duration: elapsed})
			continue
		}

		// A provider that names what answered (antigravity:<model>) keeps its name.
		if resp.Provider == "" {
			resp.Provider = name
		}
		attempts = append(attempts, Attempt{Provider: resp.Provider, Duration: elapsed})
		resp.Attempts = attempts
		return resp, nil
	}
	return Response{Attempts: attempts}, allFailedError(attempts, skipped)
}

func allFailedError(attempts []Attempt, skipped []string) error {
	parts := make([]string, 0, len(attempts)+len(skipped))
	for _, a := range attempts {
		parts = append(parts, fmt.Sprintf("%s: %s (%.1fs)", a.Provider, a.Err, a.Duration.Seconds()))
	}
	for _, s := range skipped {
		parts = append(parts, "omitido, "+s)
	}
	if len(parts) == 0 {
		return errors.New("ningún proveedor de IA respondió")
	}
	return fmt.Errorf("ningún proveedor de IA respondió: %s", strings.Join(parts, "; "))
}
