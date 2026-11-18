package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const (
	svEvalTimeout   = 5 * time.Second
	svMaxDepth      = 64
	svDefaultEvery  = 3 * time.Second
	svChangeBuffer  = 4
	svGlobalName    = "CompaneroWoWDB"
	svChunkNameBase = "CompaneroWoW.lua"
)

// SavedVars is the decoded CompaneroWoWDB global plus the file's modification time.
type SavedVars struct {
	Raw     map[string]any
	ModTime time.Time
}

// LoadSavedVariables executes the SavedVariables file in a sandboxed Lua state
// (base and table libraries only, 5 s timeout) and converts CompaneroWoWDB to Go.
// A missing file returns an error wrapping os.ErrNotExist.
func LoadSavedVariables(path string) (*SavedVars, error) {
	return loadSavedVariables(context.Background(), path, svEvalTimeout)
}

func loadSavedVariables(parent context.Context, path string, timeout time.Duration) (*SavedVars, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat saved variables: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read saved variables %s: %w", path, err)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	raw, err := evalSavedVariables(ctx, data, svChunkNameBase)
	if err != nil {
		return nil, fmt.Errorf("evaluate %s: %w", path, err)
	}
	// Evaluating a SavedVariables file of a few MB makes hundreds of MB of Lua garbage, and the
	// Go runtime keeps the peak as idle heap. Give it back now, so the app does not hold it.
	debug.FreeOSMemory()
	return &SavedVars{Raw: raw, ModTime: info.ModTime()}, nil
}

// evalSavedVariables runs data in a sandbox that has only base and table functions
// and returns the CompaneroWoWDB global as a Go map.
func evalSavedVariables(ctx context.Context, data []byte, chunk string) (map[string]any, error) {
	L := lua.NewState(lua.Options{SkipOpenLibs: true, CallStackSize: 128, RegistrySize: 8192})
	defer L.Close()

	lua.OpenBase(L)
	lua.OpenTable(L)
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "require", "print"} {
		L.SetGlobal(name, lua.LNil)
	}
	L.SetContext(ctx)

	fn, err := L.Load(bytes.NewReader(data), chunk)
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	L.Push(fn)
	if err := L.PCall(0, 0, nil); err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	tbl, ok := L.GetGlobal(svGlobalName).(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("global %s is missing or not a table", svGlobalName)
	}
	return luaMap(tbl, 0), nil
}

// luaValue converts a Lua value. ok is false for values with no Go equivalent
// (functions, userdata, threads) and for values nested beyond svMaxDepth.
func luaValue(v lua.LValue, depth int) (any, bool) {
	if depth > svMaxDepth {
		return nil, false
	}
	switch t := v.(type) {
	case lua.LBool:
		return bool(t), true
	case lua.LNumber:
		return float64(t), true
	case lua.LString:
		return string(t), true
	case *lua.LTable:
		return luaTable(t, depth+1), true
	}
	return nil, false
}

type luaPair struct {
	key lua.LValue
	val any
}

// luaTable converts a table. A table whose keys are exactly 1..n integers becomes
// []any (an empty table becomes an empty []any); any other table becomes a
// map[string]any with numeric keys stringified.
func luaTable(t *lua.LTable, depth int) any {
	pairs := collectPairs(t, depth)
	if len(pairs) == 0 {
		return []any{}
	}
	if arr, ok := asSequence(pairs); ok {
		return arr
	}
	return pairsToMap(pairs)
}

// luaMap converts the top-level CompaneroWoWDB table to a map, even if it looks like a sequence.
func luaMap(t *lua.LTable, depth int) map[string]any {
	return pairsToMap(collectPairs(t, depth))
}

func collectPairs(t *lua.LTable, depth int) []luaPair {
	var pairs []luaPair
	t.ForEach(func(k, v lua.LValue) {
		val, ok := luaValue(v, depth)
		if !ok {
			return
		}
		pairs = append(pairs, luaPair{key: k, val: val})
	})
	return pairs
}

func asSequence(pairs []luaPair) ([]any, bool) {
	arr := make([]any, len(pairs))
	filled := make([]bool, len(pairs))
	for _, p := range pairs {
		n, ok := p.key.(lua.LNumber)
		if !ok {
			return nil, false
		}
		f := float64(n)
		if f != math.Trunc(f) || f < 1 || f > float64(len(pairs)) || filled[int(f)-1] {
			return nil, false
		}
		arr[int(f)-1] = p.val
		filled[int(f)-1] = true
	}
	return arr, true
}

func pairsToMap(pairs []luaPair) map[string]any {
	m := make(map[string]any, len(pairs))
	for _, p := range pairs {
		key, ok := luaKeyString(p.key)
		if !ok {
			continue
		}
		m[key] = p.val
	}
	return m
}

func luaKeyString(k lua.LValue) (string, bool) {
	switch t := k.(type) {
	case lua.LString:
		return string(t), true
	case lua.LNumber:
		f := float64(t)
		if f == math.Trunc(f) && math.Abs(f) < 1<<53 {
			return strconv.FormatInt(int64(f), 10), true
		}
		return strconv.FormatFloat(f, 'g', -1, 64), true
	}
	return "", false
}

// SVWatcher polls the SavedVariables file and reloads it when its size or
// modification time changes.
type SVWatcher struct {
	every time.Duration

	// resolve finds the file path. It is called until it succeeds and again every
	// resolveEvery after that, so the file is picked up when WoW has been installed or
	// /compa sync has run after the app started.
	resolve      func() (string, error)
	resolveEvery time.Duration
	lastResolve  time.Time
	path         string
	pathErr      error

	startOnce sync.Once
	changes   chan *SavedVars

	// paused stops the periodic checks (SetPaused); pauseCh wakes the run loop when it changes.
	pauseMu sync.Mutex
	paused  bool
	pauseCh chan struct{}

	pollMu    sync.Mutex
	attempted bool
	lastMod   time.Time
	lastSize  int64
	curPath   string // the file the data was read from; guarded by mu

	mu       sync.RWMutex
	current  *SavedVars
	lastErr  error
	lastUse  time.Time // when Current was last called or the file last loaded; guarded by mu
	released bool      // the data was dropped by ReleaseIdle and is read again on demand; guarded by mu
}

// svResolveEvery is how often the file path is looked up again.
const svResolveEvery = 5 * time.Second

// NewSVWatcher watches path, polling every `every` (default 3s).
func NewSVWatcher(path string, every time.Duration) *SVWatcher {
	return NewSVWatcherFunc(func() (string, error) {
		if path == "" {
			return "", errors.New("saved variables path is empty")
		}
		return path, nil
	}, every)
}

// NewSVWatcherFunc watches the file that resolve returns, polling every `every` (default
// 3s). resolve may fail while WoW is not set up; the watcher keeps trying.
func NewSVWatcherFunc(resolve func() (string, error), every time.Duration) *SVWatcher {
	if every <= 0 {
		every = svDefaultEvery
	}
	return &SVWatcher{
		every:        every,
		resolve:      resolve,
		resolveEvery: svResolveEvery,
		changes:      make(chan *SavedVars, svChangeBuffer),
		pauseCh:      make(chan struct{}, 1),
	}
}

// PathError returns the error of the latest path lookup, or nil when the file path is
// known. It is what the status bar explains to the user.
func (w *SVWatcher) PathError() error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.pathErr
}

// locate returns the file path, looking it up again when it is unknown or the last
// lookup is older than resolveEvery. It must be called with pollMu held.
func (w *SVWatcher) locate(now time.Time) (string, error) {
	if w.path != "" && now.Sub(w.lastResolve) < w.resolveEvery {
		return w.path, nil
	}
	w.lastResolve = now
	path, err := w.resolve()
	w.mu.Lock()
	w.pathErr = err
	w.mu.Unlock()
	if err != nil {
		if w.path != "" {
			// Keep the known path: a failed lookup right after a success is not a reason
			// to forget it. The error is still reported through PathError.
			return w.path, nil
		}
		return "", err
	}
	if path != w.path {
		w.path = path
		w.attempted = false
	}
	return w.path, nil
}

// Start launches the polling goroutine and returns immediately. It stops when ctx ends.
func (w *SVWatcher) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		go w.run(ctx)
	})
}

// SetPaused stops the periodic checks of the file while on is true: no timer runs, and the file
// is looked at only when Poll is called (the on-demand mode calls it when the app becomes active
// and before each question). Clearing it resumes the checks at once.
func (w *SVWatcher) SetPaused(on bool) {
	w.pauseMu.Lock()
	w.paused = on
	w.pauseMu.Unlock()
	if w.pauseCh == nil {
		return
	}
	select {
	case w.pauseCh <- struct{}{}:
	default:
	}
}

func (w *SVWatcher) isPaused() bool {
	w.pauseMu.Lock()
	defer w.pauseMu.Unlock()
	return w.paused
}

func (w *SVWatcher) run(ctx context.Context) {
	var ticker *time.Ticker
	var tick <-chan time.Time // nil while paused: the select then never takes it
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()
	// sync starts or stops the timer to match the paused state; a first check runs when it starts.
	sync := func() {
		if w.isPaused() {
			if ticker != nil {
				ticker.Stop()
				ticker, tick = nil, nil
			}
			return
		}
		if ticker == nil {
			ticker = time.NewTicker(w.every)
			tick = ticker.C
			_ = w.Poll()
		}
	}
	sync()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.pauseCh:
			sync()
		case <-tick:
			_ = w.Poll()
		}
	}
}

// Poll checks the file once and reloads it if it changed. It returns an error
// wrapping os.ErrNotExist when the file is missing, or the load error if the file
// could not be evaluated (the previous good data is kept).
func (w *SVWatcher) Poll() error {
	w.pollMu.Lock()
	defer w.pollMu.Unlock()

	path, err := w.locate(time.Now())
	if err != nil {
		return fmt.Errorf("locate saved variables: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat saved variables: %w", err)
	}
	if w.attempted && info.ModTime().Equal(w.lastMod) && info.Size() == w.lastSize {
		return nil
	}
	w.attempted = true
	w.lastMod = info.ModTime()
	w.lastSize = info.Size()

	sv, err := LoadSavedVariables(path)
	w.mu.Lock()
	w.lastErr = err
	w.curPath = path
	if err == nil {
		w.current = sv
		w.released = false
		w.lastUse = time.Now()
	}
	w.mu.Unlock()
	if err != nil {
		return err
	}
	w.publish(sv)
	return nil
}

func (w *SVWatcher) publish(sv *SavedVars) {
	for {
		select {
		case w.changes <- sv:
			return
		default:
		}
		select {
		case <-w.changes:
		default:
		}
	}
}

// Current returns the last successfully loaded data, or nil if none. Data dropped by
// ReleaseIdle is read again from the file here, so the caller never sees the release.
func (w *SVWatcher) Current() *SavedVars {
	w.mu.Lock()
	w.lastUse = time.Now()
	cur, released, path := w.current, w.released, w.curPath
	w.mu.Unlock()
	if cur != nil || !released || path == "" {
		return cur
	}
	return w.reload(path)
}

// reload reads the file again after ReleaseIdle. pollMu serializes it with Poll, so the two
// never hold two copies of the data at once.
func (w *SVWatcher) reload(path string) *SavedVars {
	w.pollMu.Lock()
	defer w.pollMu.Unlock()
	w.mu.RLock()
	cur := w.current
	w.mu.RUnlock()
	if cur != nil {
		return cur
	}
	sv, err := LoadSavedVariables(path)
	if err != nil {
		w.mu.Lock()
		w.lastErr = err
		w.mu.Unlock()
		return nil
	}
	w.mu.Lock()
	w.current = sv
	w.released = false
	w.lastUse = time.Now()
	w.mu.Unlock()
	return sv
}

// ReleaseIdle drops the loaded data when nothing has used it for at least idle, so an idle
// app does not keep the decoded SavedVariables tree in memory. It reports whether data was
// dropped. The next Current call reads the file again.
func (w *SVWatcher) ReleaseIdle(now time.Time, idle time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current == nil || now.Sub(w.lastUse) < idle {
		return false
	}
	w.current = nil
	w.released = true
	return true
}

// Released reports whether the data is dropped and not loaded again yet.
func (w *SVWatcher) Released() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.current == nil && w.released
}

// LastError returns the error of the most recent load attempt (nil on success).
func (w *SVWatcher) LastError() error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastErr
}

// Changes emits each newly loaded SavedVariables. Buffered (4); the oldest is dropped when full.
func (w *SVWatcher) Changes() <-chan *SavedVars {
	return w.changes
}
