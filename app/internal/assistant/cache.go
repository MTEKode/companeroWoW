package assistant

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// The answer cache. An exact repeat of a question, with the same context, skill and models,
// is answered from memory for answerCacheMin minutes (the setting; 0 turns the cache off). The
// key holds everything the answer depends on except the chat history: the normalized question,
// the hash of the system prompt that was sent (the intent's playbook excerpt and context blocks,
// or the game state, the profile and the player context), the hash of the skill (its version) and
// the models of the chain. Answers with a screenshot are not cached (the picture changes), and
// neither are answers the verifier could not back.

// cachedAnswer is one answer kept by the cache.
type cachedAnswer struct {
	text     string
	provider string
	at       time.Time
}

// answerCache holds the answers of the last TTL. Expired entries are dropped when the cache is
// read or written. It is safe for concurrent use.
type answerCache struct {
	mu      sync.Mutex
	ttl     time.Duration // zero or less: the cache is off
	entries map[string]cachedAnswer
}

func newAnswerCache(ttl time.Duration) *answerCache {
	return &answerCache{ttl: ttl, entries: map[string]cachedAnswer{}}
}

// enabled reports whether the cache keeps answers.
func (c *answerCache) enabled() bool {
	return c != nil && c.ttl > 0
}

// get returns the answer kept under key when it is younger than the TTL.
func (c *answerCache) get(key string, now time.Time) (cachedAnswer, bool) {
	if !c.enabled() {
		return cachedAnswer{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return cachedAnswer{}, false
	}
	if now.Sub(e.at) >= c.ttl {
		delete(c.entries, key)
		return cachedAnswer{}, false
	}
	return e, true
}

// put keeps an answer under key from now on, and drops the entries that have expired.
func (c *answerCache) put(key string, e cachedAnswer, now time.Time) {
	if !c.enabled() || strings.TrimSpace(e.text) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, old := range c.entries {
		if now.Sub(old.at) >= c.ttl {
			delete(c.entries, k)
		}
	}
	e.at = now
	c.entries[key] = e
}

// clear forgets every answer (the chat was cleared).
func (c *answerCache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]cachedAnswer{}
}

// len is the number of answers held, expired ones included until they are next read or written.
func (c *answerCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// normalizeQuestion makes the question comparable: case and the spacing between words do not
// count.
func normalizeQuestion(q string) string {
	return strings.ToLower(strings.Join(strings.Fields(q), " "))
}

// answerCacheKey is the cache key of one question. system and skill are the request's context
// blocks and skill; they are hashed, so the key stays short and a change in either one is a
// different key.
func answerCacheKey(question string, req ai.Request, model string, verify bool) string {
	h := sha256.New()
	fmt.Fprintf(h, "q:%s\x1f", normalizeQuestion(question))
	fmt.Fprintf(h, "ctx:%s\x1f", digest(req.System))
	fmt.Fprintf(h, "skill:%s\x1f", digest(req.Skill))
	fmt.Fprintf(h, "model:%s\x1f", model)
	fmt.Fprintf(h, "verify:%t", verify)
	return hex.EncodeToString(h.Sum(nil))
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// chainModelKey names the providers and models of the configuration. A change of any of them is a
// different key: the answer of another model is not the same answer.
func chainModelKey(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	parts := []string{
		strings.Join(cfg.ProviderOrder, ","),
		strings.Join(cfg.AntigravityModels, ","),
		cfg.AntigravityModel,
		cfg.AntigravityEffort,
		cfg.CodexModel,
		cfg.CodexEffort,
	}
	for _, p := range cfg.APIProviders {
		parts = append(parts, p.Name+"="+p.Model)
	}
	return strings.Join(parts, "|")
}

// answerCacheTTL is the time an answer stays in the cache, from answerCacheMin. Zero when the
// configuration is missing or turns the cache off.
func answerCacheTTL(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.AnswerCacheMin <= 0 {
		return 0
	}
	return time.Duration(cfg.AnswerCacheMin) * time.Minute
}
