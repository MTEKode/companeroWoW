package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/assistant"
	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/ondemand"
	"github.com/MTEKode/companeroWoW/app/internal/ui"
)

// modeGate holds the settings that decide whether the game is read between questions: the way
// the app reads the game (continuous or on demand) and the tips mode. It is read from the
// reader's goroutine and written from the settings screen, so it has its own lock.
type modeGate struct {
	mu         sync.Mutex
	continuous bool
	tips       string
}

func newModeGate(cfg *config.Config) *modeGate {
	return &modeGate{continuous: !cfg.OnDemand(), tips: cfg.TipsMode}
}

// isContinuous reports whether the game is read all the time.
func (m *modeGate) isContinuous() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.continuous
}

func (m *modeGate) setContinuous(on bool) {
	m.mu.Lock()
	m.continuous = on
	m.mu.Unlock()
}

func (m *modeGate) setTips(mode string) {
	m.mu.Lock()
	m.tips = mode
	m.mu.Unlock()
}

// tipsOn reports whether proactive tips are produced: only in the continuous mode, where the
// game is read between questions, and only when the tips mode is not off.
func (m *modeGate) tipsOn() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.continuous && m.tips != config.TipsOff
}

// onDemandBackend is the assistant as the panel sees it. In the on-demand mode a question
// makes the reader active, waits for fresh game data (up to the configured wait), shows the
// progress in the status bar and then goes ahead, naming any block that is still old. In the
// continuous mode it is the assistant itself.
type onDemandBackend struct {
	*assistant.Assistant
	ctl    *ondemand.Controller
	mode   *modeGate
	waiter *ondemand.Waiter
	g      *gamedata.Service
	wait   time.Duration
}

// Ask answers one question (see onDemandBackend).
func (b *onDemandBackend) Ask(ctx context.Context, question string, withScreenshot bool) (ai.Response, error) {
	if b.mode.isContinuous() {
		return b.Assistant.Ask(ctx, question, withScreenshot)
	}
	b.ctl.BeginQuestion()
	defer b.ctl.EndQuestion()
	defer ui.ReportReading("")
	refreshSavedVariables(b.g)
	// While the addon streams its share window the key data is on its way: wait up to 20 s;
	// otherwise wait at most 3 s and go ahead with what is cached.
	remaining := 0
	if b.g != nil {
		remaining = b.g.ShareRemaining()
	}
	stale, err := b.waiter.Wait(ctx, b.ctl.ActivatedAt(), ondemand.WaitFor(remaining, b.wait), func(have, total int) {
		ui.ReportReading(fmt.Sprintf("Leyendo el juego… (%d/%d)", have, total))
	})
	if err != nil {
		return ai.Response{}, err
	}
	ctx = assistant.WithDataNote(ctx, ondemand.StaleNote(stale))
	return b.Assistant.Ask(ctx, question, withScreenshot)
}

// refreshSavedVariables checks the SavedVariables file once. A file that is not there yet (WoW
// not set up, the addon never synced) is normal and is not logged.
func refreshSavedVariables(g *gamedata.Service) {
	if err := g.RefreshSavedVariables(); err != nil &&
		!errors.Is(err, fs.ErrNotExist) && !errors.Is(err, config.ErrNoSavedVariables) {
		log.Printf("saved variables: %v", err)
	}
}

// saveOnDemandState writes the runtime state of the app, and, when it goes inactive, the last
// known game data (livecache.json). hold is when the reader goes off if nothing keeps it on (zero
// when unknown). --diag reads the runtime state to report the captures of the running app.
func saveOnDemandState(dir string, reader *bridge.Reader, mode string, active bool, hold time.Time) {
	if !active {
		if err := ondemand.SaveCache(filepath.Join(dir, ondemand.CacheFile), reader.Channel().Data, time.Now()); err != nil {
			log.Printf("caché de datos: %v", err)
		}
	}
	writeRuntime(dir, reader, mode, active, hold)
}

// writeRuntime writes runtime.json: the reads (captures, decoded and failed, the last error), the
// complete messages per type and the hold. The app rewrites it each second while the reader is on
// (see runRuntimeTicker), so a read of the file follows the game.
func writeRuntime(dir string, reader *bridge.Reader, mode string, active bool, hold time.Time) {
	stats := reader.Stats()
	counts := make(map[string]int64)
	for _, it := range reader.Channel().Data.All() {
		counts[it.Name] = it.Count
	}
	st := ondemand.RuntimeState{
		PID:             os.Getpid(),
		Mode:            mode,
		Active:          active,
		Captures:        stats.Captures,
		At:              time.Now(),
		Source:          stats.Source,
		FramesOK:        stats.FramesOK,
		FramesErr:       stats.FramesErr,
		LastDecodeError: stats.LastError,
		LastDecodeAt:    timePtr(stats.LastErrorAt),
		LastDecodeOKAt:  timePtr(stats.LastOKAt),
		MessagesByType:  counts,
		HoldUntil:       timePtr(hold),
	}
	if err := ondemand.SaveRuntime(filepath.Join(dir, ondemand.RuntimeFile), st); err != nil {
		log.Printf("estado de la app: %v", err)
	}
}

// logReadingEnd writes the counts of the reads that ended with a hold: how many captures and
// decoded or failed reads it made, and the last error. The controller has already logged why the
// reader went off.
func logReadingEnd(reader *bridge.Reader) {
	st := reader.Stats()
	line := fmt.Sprintf("lectura del juego: fin: capturas %d, leídas %d, fallidas %d", st.Captures, st.FramesOK, st.FramesErr)
	if st.LastError != "" {
		line += "; último error: " + st.LastError
	}
	log.Print(line)
}

// timePtr returns &t, or nil for the zero time (omitted from the runtime state).
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// runRuntimeTicker calls tick each second until ctx ends. The app uses it to keep runtime.json
// current while the reader is on.
func runRuntimeTicker(ctx context.Context, tick func()) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

// onDemandAfter is the timer of the controller: time.AfterFunc, which fires once per schedule.
func onDemandAfter(d time.Duration, f func()) func() {
	t := time.AfterFunc(d, f)
	return func() { t.Stop() }
}

// modeName is the config value of the mode, for the runtime state.
func (m *modeGate) modeName() string {
	if m.isContinuous() {
		return config.ModeContinuous
	}
	return config.ModeOnDemand
}
