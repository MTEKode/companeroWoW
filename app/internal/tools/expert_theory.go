package tools

import (
	"context"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/theory"
	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

// The time-to-kill model of the expert tools comes from the theory engine: the simulated time
// the character needs to kill one mob of a level, with its build. It is the answer that
// valor_misiones (combat time of each quest) and info_enemigo (predicted time) use when the
// engine is available; without a sheet or a class model they fall back to the recorded fights.

const (
	// theoryHookTimeout bounds the talent data read behind one estimate.
	theoryHookTimeout = 3 * time.Second
	// theoryHookFights is the number of simulated fights of one estimate: enough for a stable
	// mean and quick enough to run inside a tool call.
	theoryHookFights = 60
	// theoryHookRetry is how long a failed estimate waits before it is tried again.
	theoryHookRetry = 5 * time.Minute
	// theoryHookMaxLevel is the highest mob level the model is asked about.
	theoryHookMaxLevel = 80
)

// theoryKillTime returns the time-to-kill hook for the gamedata service. Answers are cached per
// level; a failure is remembered for theoryHookRetry, so that a slow site does not stall every
// tool call. It returns nil when there is no talent source to read.
func theoryKillTime(g *gamedata.Service, ws *websearch.Client) gamedata.KillTimeFunc {
	if g == nil || ws == nil {
		return nil
	}
	type entry struct {
		sec    float64
		failed time.Time
	}
	var (
		mu    sync.Mutex
		cache = map[int]entry{}
	)
	return func(level int) (float64, bool) {
		if level <= 0 || level > theoryHookMaxLevel {
			return 0, false
		}
		mu.Lock()
		defer mu.Unlock()
		if e, ok := cache[level]; ok {
			if e.sec > 0 {
				return e.sec, true
			}
			if time.Since(e.failed) < theoryHookRetry {
				return 0, false
			}
		}
		sec := simulateTTK(g, ws, level)
		if sec > 0 {
			cache[level] = entry{sec: sec}
		} else {
			cache[level] = entry{failed: time.Now()}
		}
		return sec, sec > 0
	}
}

// simulateTTK runs the class model against a mob of the level and returns its mean time to kill
// in seconds, or 0 when the model cannot answer.
func simulateTTK(g *gamedata.Service, ws *websearch.Client, level int) float64 {
	ctx, cancel := context.WithTimeout(context.Background(), theoryHookTimeout)
	defer cancel()
	cls, in, err := theoryFor(ctx, g, ws)
	if err != nil {
		return 0
	}
	in.Opt = theory.Options{Fights: theoryHookFights, MobLevel: level}
	rep, err := cls.Evaluate(in)
	if err != nil {
		return 0
	}
	return rep.TTK.Mean
}
