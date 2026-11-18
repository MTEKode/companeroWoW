package gamedata

import (
	"context"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// Share on key: the addon is idle until the player asks for its data, with the panel key
// (or the WoW binding "Compañero: compartir datos"). It then streams for a share window and
// the status payload's shareRemainingSec counts it down. The types below are the ones it
// sends first on every share; the rest follow.

const (
	// shareLimit is the longest wait for the key data while the addon is streaming.
	shareLimit = 20 * time.Second
	// plainLimit is the longest wait when the addon is not streaming (no share window).
	plainLimit = 3 * time.Second
	// shareWaitEvery is how often the wait looks at the channel.
	shareWaitEvery = 200 * time.Millisecond
)

// keyTypes are the message types a share sends first: character, sheet, quests, equipment.
var keyTypes = []uint8{bridge.MsgCharacter, bridge.MsgSheet, bridge.MsgQuests, bridge.MsgEquipment}

// shareState is what the share on key needs to remember: when the data was last asked for.
type shareState struct {
	mu          sync.Mutex
	activatedAt time.Time
}

// ActivateLive is called when the user asks for the game's data (the panel key opens the
// panel). The reader reads at once and follows the share window; the next question waits, at
// most shareLimit, for the key data sent since then.
func (g *Service) ActivateLive(now time.Time) {
	g.share.mu.Lock()
	g.share.activatedAt = now
	g.share.mu.Unlock()
	if g.reader != nil {
		g.reader.Activate(now)
	}
}

// activated returns when the data was last asked for; zero when never.
func (g *Service) activated() time.Time {
	g.share.mu.Lock()
	defer g.share.mu.Unlock()
	return g.share.activatedAt
}

// ShareRemaining is the countdown of the addon's share window in seconds, from the status
// payload; 0 when the addon is at rest or the strip is not read.
func (g *Service) ShareRemaining() int {
	st, ok := g.Live()
	if !ok {
		return 0
	}
	return int(st.ShareRemainingSec)
}

// KeyAge is the age at now of the oldest key data type. ok is false while one of them has
// not arrived.
func (g *Service) KeyAge(now time.Time) (age time.Duration, ok bool) {
	ch := g.LiveChannel()
	if ch == nil {
		return 0, false
	}
	for _, t := range keyTypes {
		it, found := ch.Data.Get(t)
		if !found {
			return 0, false
		}
		if a := now.Sub(it.At); a > age {
			age = a
		}
	}
	return age, true
}

// AwaitKeyTypes waits before an answer until the key data has arrived since the last
// ActivateLive. The wait is at most shareLimit while the addon is streaming (shareRemainingSec
// above 0), otherwise plainLimit; then the answer goes on with what is cached. Without an
// activation there is nothing to wait for.
func (g *Service) AwaitKeyTypes(ctx context.Context) {
	since := g.activated()
	if since.IsZero() {
		return
	}
	start := time.Now()
	for !g.keyTypesSince(since) {
		limit := plainLimit
		if g.ShareRemaining() > 0 {
			limit = shareLimit
		}
		if time.Since(start) >= limit {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(shareWaitEvery):
		}
	}
}

// keyTypesSince reports whether every key type has arrived after since.
func (g *Service) keyTypesSince(since time.Time) bool {
	ch := g.LiveChannel()
	if ch == nil {
		return false
	}
	for _, t := range keyTypes {
		it, ok := ch.Data.Get(t)
		if !ok || !it.At.After(since) {
			return false
		}
	}
	return true
}

// freeBagSlots is the free bag slots of the live character message (freeBagSlots); 0 when
// the addon does not send it.
func (g *Service) freeBagSlots() float64 {
	ch := g.LiveChannel()
	if ch == nil {
		return 0
	}
	it, ok := ch.Data.Get(bridge.MsgCharacter)
	if !ok {
		return 0
	}
	m, _ := it.Value.(map[string]any)
	n, _ := m["freeBagSlots"].(float64)
	return n
}
