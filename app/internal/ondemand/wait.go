package ondemand

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// KeyMessages are the message types a question needs before it is answered: the character
// (1), the character sheet (12), the quest log (2), the equipment (4) and the play session
// (14). The addon sends them first after an activation (see the on-demand contract).
var KeyMessages = []uint8{
	bridge.MsgCharacter, bridge.MsgSheet, bridge.MsgQuests, bridge.MsgEquipment, bridge.MsgSession,
}

// ShareWait is the longest a question waits for the key data while the addon is streaming its
// share window (the panel key was pressed and shareRemainingSec counts down).
const ShareWait = 20 * time.Second

// PlainWait is the longest a question waits when no share window is running: the addon then
// sends the key data only if the player asks for it, so the wait is short.
const PlainWait = 3 * time.Second

// WaitFor returns how long a question waits for the key data. While the share window counts
// down (remaining above 0) it is ShareWait; otherwise the configured wait, but at most PlainWait.
func WaitFor(remaining int, configured time.Duration) time.Duration {
	if remaining > 0 {
		return ShareWait
	}
	if configured <= 0 || configured > PlainWait {
		return PlainWait
	}
	return configured
}

// RecentWindow is how old a key message may be and still count as fresh, when it arrived
// before the activation.
const RecentWindow = 30 * time.Second

// pollEvery is how often the wait looks at the feed.
const pollEvery = 250 * time.Millisecond

// Feed is what the wait reads from the game side: whether the status payload is fresh, and
// when each message type last arrived.
type Feed interface {
	StatusFresh() bool
	MessageAt(t uint8) (time.Time, bool)
}

// ReaderFeed reads the feed of a pixel reader.
type ReaderFeed struct{ R *bridge.Reader }

// StatusFresh reports whether the reader has a status payload read in the last five seconds.
func (f ReaderFeed) StatusFresh() bool {
	if f.R == nil {
		return false
	}
	_, ok := f.R.Latest()
	return ok
}

// MessageAt returns when the latest message of type t arrived.
func (f ReaderFeed) MessageAt(t uint8) (time.Time, bool) {
	if f.R == nil {
		return time.Time{}, false
	}
	it, ok := f.R.Channel().Data.Get(t)
	return it.At, ok
}

// Readiness counts the key messages that count for a question at now: received since the
// activation, or within RecentWindow. ready is true when the status payload is fresh and all
// key messages count.
func Readiness(now, activatedAt time.Time, statusFresh bool, feed func(uint8) (time.Time, bool)) (have int, ready bool) {
	for _, t := range KeyMessages {
		at, ok := feed(t)
		if !ok {
			continue
		}
		if !at.Before(activatedAt) || now.Sub(at) < RecentWindow {
			have++
		}
	}
	return have, statusFresh && have == len(KeyMessages)
}

// Stale is one data block the question goes ahead with although it did not refresh.
type Stale struct {
	Block string
	// Age is how old the cached message is; zero when the block never arrived.
	Age time.Duration
}

// blockNames are the Spanish names of the key blocks, in the words of the answers.
var blockNames = map[uint8]string{
	bridge.MsgCharacter: "personaje",
	bridge.MsgSheet:     "hoja del personaje",
	bridge.MsgQuests:    "misiones",
	bridge.MsgEquipment: "equipo",
	bridge.MsgSession:   "sesión",
}

// Waiter holds a question until the game data is fresh, or until the wait runs out.
type Waiter struct {
	Feed Feed
	Now  func() time.Time
	// Sleep waits d or until ctx ends; tests replace it with a fake clock.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Wait returns when the data is ready, or when timeout has passed since the call. It reports
// the number of key messages through progress (1..len(KeyMessages)) whenever it changes. The
// result lists the key blocks that did not refresh, with their ages, when it timed out; it is
// empty when the data was ready. A cancelled ctx returns its error.
func (w *Waiter) Wait(ctx context.Context, activatedAt time.Time, timeout time.Duration, progress func(have, total int)) ([]Stale, error) {
	now := w.Now
	if now == nil {
		now = time.Now
	}
	sleep := w.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	deadline := now().Add(timeout)
	last := -1
	for {
		t := now()
		have, ready := Readiness(t, activatedAt, w.Feed.StatusFresh(), w.Feed.MessageAt)
		if have != last {
			last = have
			if progress != nil {
				progress(have, len(KeyMessages))
			}
		}
		if ready {
			return nil, nil
		}
		if !t.Before(deadline) {
			return w.staleBlocks(t, activatedAt), nil
		}
		step := pollEvery
		if left := deadline.Sub(t); left < step {
			step = left
		}
		if err := sleep(ctx, step); err != nil {
			return nil, err
		}
	}
}

// staleBlocks lists the key blocks that did not refresh at now, oldest first in the key order.
func (w *Waiter) staleBlocks(now, activatedAt time.Time) []Stale {
	var out []Stale
	for _, t := range KeyMessages {
		at, ok := w.Feed.MessageAt(t)
		if ok && (!at.Before(activatedAt) || now.Sub(at) < RecentWindow) {
			continue
		}
		s := Stale{Block: blockNames[t]}
		if ok {
			s.Age = now.Sub(at)
		}
		out = append(out, s)
	}
	return out
}

// StaleNote is the line that tells the model which blocks are old, with their ages. It is ""
// when nothing is stale.
func StaleNote(stale []Stale) string {
	if len(stale) == 0 {
		return ""
	}
	parts := make([]string, 0, len(stale))
	for _, s := range stale {
		if s.Age <= 0 {
			parts = append(parts, s.Block+" (sin datos del juego)")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (datos de hace %s)", s.Block, humanAge(s.Age)))
	}
	return "Datos del juego no actualizados antes de preguntar: " + strings.Join(parts, ", ") +
		". Si dependen de ellos, dilo y pide al jugador que haga /reload o abra el juego."
}

// humanAge is an age in Spanish: "menos de 1 min", "3 min", "2 h", "1 d".
func humanAge(d time.Duration) string {
	sec := int64(d.Seconds())
	switch {
	case sec < 60:
		return "menos de 1 min"
	case sec < 3600:
		return fmt.Sprintf("%d min", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%d h", sec/3600)
	}
	return fmt.Sprintf("%d d", sec/86400)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
