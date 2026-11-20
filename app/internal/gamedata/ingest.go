package gamedata

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// Storing the channel. Each complete message of a type that has a table goes to the store
// (store.IngestMessage) from a worker goroutine, so the channel callback never waits for SQLite.
// The queue is bounded: when it is full the message is dropped and counted. The addon sends
// the same data again in later share windows, and the outbox keeps it until it is delivered.

const (
	ingestQueueSize = 256
	// storeCombatLimit is how many of the newest combats the readers take from the store. The
	// store keeps more (see store.maxCombats); the readers work on this window.
	storeCombatLimit = 50
	// combatCacheTTL bounds how long the combat list read from the store is reused: one tool call
	// reads the active character many times.
	combatCacheTTL = 2 * time.Second
)

// storedTypes are the channel message types that the store keeps.
var storedTypes = map[uint8]bool{
	bridge.MsgCharacter:       true,
	bridge.MsgQuests:          true,
	bridge.MsgEquipment:       true,
	bridge.MsgCompletedQuests: true,
	bridge.MsgCombat:          true,
	bridge.MsgActionBars:      true,
	bridge.MsgKnowledge:       true,
	bridge.MsgHeavy:           true,
	bridge.MsgSheet:           true,
	bridge.MsgMacros:          true,
	bridge.MsgSession:         true,
	bridge.MsgWorld:           true,
}

type ingestJob struct {
	t    uint8
	body []byte
	at   time.Time
}

// ingestQueue is the bounded queue of channel messages waiting for the store.
type ingestQueue struct {
	ch      chan ingestJob
	dropped atomic.Int64
}

// combatCache keeps the combat list read from the store for combatCacheTTL.
type combatCache struct {
	mu   sync.Mutex
	key  string
	at   time.Time
	list []any
}

// StartIngest starts the worker that stores the channel messages, until ctx ends. Without a
// store it does nothing. Call it before the reader starts, so no message is missed.
func (g *Service) StartIngest(ctx context.Context) {
	if g == nil || g.store == nil || g.ingest != nil {
		return
	}
	q := &ingestQueue{ch: make(chan ingestJob, ingestQueueSize)}
	g.ingest = q
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-q.ch:
				g.storeJob(job)
			}
		}
	}()
}

// QueueChannel hands one complete channel message to the store worker. It never blocks: a
// message of a type the store does not keep, or one that finds the queue full, is dropped.
func (g *Service) QueueChannel(t uint8, body []byte, at time.Time) {
	if g == nil || g.ingest == nil || !storedTypes[t] {
		return
	}
	select {
	case g.ingest.ch <- ingestJob{t: t, body: body, at: at}:
	default:
		g.ingest.dropped.Add(1)
	}
}

// IngestDropped is how many channel messages were dropped because the store was behind.
func (g *Service) IngestDropped() int64 {
	if g == nil || g.ingest == nil {
		return 0
	}
	return g.ingest.dropped.Load()
}

// storeJob writes one message. The character is the active one, as the readers see it.
func (g *Service) storeJob(job ingestJob) {
	if _, err := g.store.IngestMessage(job.t, job.body, g.ActiveCharacterKey(), job.at); err != nil {
		log.Printf("gamedata: store message %s: %v", bridge.MsgNames[job.t], err)
	}
	g.combats.invalidate()
	g.invalidateState()
}

// withStoreCombats lays the combat history of the character from the store under its own combats
// list: the newest combats of both, unique by id, newest first. Without stored combats ch is unchanged.
func (g *Service) withStoreCombats(ch charEntry) charEntry {
	if g.store == nil || ch.key == "" {
		return ch
	}
	list := g.combats.get(g, ch.key)
	if len(list) == 0 {
		return ch
	}
	merged := make(map[string]any, len(ch.data)+1)
	for k, v := range ch.data {
		merged[k] = v
	}
	merged["combats"] = mergeCombats(list, merged["combats"])
	ch.data = merged
	return ch
}

// get returns the stored combats of key, from the cache when it is fresh.
func (c *combatCache) get(g *Service, key string) []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == key && !c.at.IsZero() && time.Since(c.at) < combatCacheTTL {
		return c.list
	}
	list, err := g.store.CombatRecords(key, storeCombatLimit)
	if err != nil {
		log.Printf("gamedata: read combats: %v", err)
		list = nil
	}
	c.key, c.at, c.list = key, time.Now(), list
	return list
}

// invalidate drops the cached list: a message was stored, so the next read must see it.
func (c *combatCache) invalidate() {
	c.mu.Lock()
	c.at = time.Time{}
	c.mu.Unlock()
}

// mergeCombats returns the stored combats and the live ones that are not stored yet, newest first.
func mergeCombats(stored []any, live any) []any {
	seen := map[float64]bool{}
	out := make([]any, 0, len(stored)+4)
	for _, v := range stored {
		if m := asMap(v); m != nil {
			seen[numberOf(m["id"])] = true
		}
		out = append(out, v)
	}
	for _, v := range listOf(live) {
		m := asMap(v)
		if m == nil || seen[numberOf(m["id"])] {
			continue
		}
		seen[numberOf(m["id"])] = true
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return numberOf(asMap(out[i])["id"]) > numberOf(asMap(out[j])["id"])
	})
	return out
}

// QueueLiveData queues the messages a LiveData holds (the on-demand cache restored at start), so
// the store gets what the app had before it stopped.
func (g *Service) QueueLiveData(d *bridge.LiveData) {
	if g == nil || d == nil {
		return
	}
	for _, it := range d.All() {
		if !storedTypes[it.Type] || it.Value == nil {
			continue
		}
		body, err := json.Marshal(it.Value)
		if err != nil {
			continue
		}
		g.QueueChannel(it.Type, body, it.At)
	}
}
