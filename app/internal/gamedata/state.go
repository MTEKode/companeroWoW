package gamedata

import (
	"log"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// The character state in the store (store.CharState): the sections of each character that the channel
// and the outbox of schema 2 wrote. The readers take them over the SavedVariables table (activeCharacter).
// One tool call reads the active character many times, so the state is reused for stateTTL, and the
// writes drop it (invalidateState).

const stateTTL = 2 * time.Second

// stateCache keeps the character keys and the sections read from the store.
type stateCache struct {
	mu     sync.Mutex
	at     time.Time
	keys   map[string]time.Time
	states map[string]store.CharState
}

// refreshLocked reloads the keys of the stored characters when the cache is stale. The caller holds mu.
func (c *stateCache) refreshLocked(st *store.Store) {
	if !c.at.IsZero() && time.Since(c.at) < stateTTL {
		return
	}
	keys, err := st.CharKeysState()
	if err != nil {
		log.Printf("gamedata: read character keys: %v", err)
		keys = nil
	}
	c.keys, c.states, c.at = keys, map[string]store.CharState{}, time.Now()
}

// invalidateState drops the cached state: a message was stored, so the next read must see it.
func (g *Service) invalidateState() {
	g.state.mu.Lock()
	g.state.at = time.Time{}
	g.state.mu.Unlock()
}

// storedCharacters returns the keys of the characters that have stored state, with the newest time of
// each. It is nil without a store.
func (g *Service) storedCharacters() map[string]time.Time {
	if g == nil || g.store == nil {
		return nil
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	g.state.refreshLocked(g.store)
	return g.state.keys
}

// storedState returns the stored sections of key. ok is false when the character has none.
func (g *Service) storedState(key string) (store.CharState, bool) {
	if g == nil || g.store == nil || key == "" {
		return store.CharState{}, false
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	g.state.refreshLocked(g.store)
	if st, ok := g.state.states[key]; ok {
		return st, len(st.Sections) > 0
	}
	st, err := g.store.CharState(key)
	if err != nil {
		log.Printf("gamedata: read character state %s: %v", key, err)
		return store.CharState{}, false
	}
	g.state.states[key] = st
	return st, len(st.Sections) > 0
}
