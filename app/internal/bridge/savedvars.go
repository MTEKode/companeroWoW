package bridge

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
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
	path  string
	every time.Duration

	startOnce sync.Once
	changes   chan *SavedVars

	pollMu    sync.Mutex
	attempted bool
	lastMod   time.Time
	lastSize  int64

	mu      sync.RWMutex
	current *SavedVars
	lastErr error
}

// NewSVWatcher watches path, polling every `every` (default 3s).
func NewSVWatcher(path string, every time.Duration) *SVWatcher {
	if every <= 0 {
		every = svDefaultEvery
	}
	return &SVWatcher{
		path:    path,
		every:   every,
		changes: make(chan *SavedVars, svChangeBuffer),
	}
}

// Start launches the polling goroutine and returns immediately. It stops when ctx ends.
func (w *SVWatcher) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		go w.run(ctx)
	})
}

func (w *SVWatcher) run(ctx context.Context) {
	ticker := time.NewTicker(w.every)
	defer ticker.Stop()
	_ = w.Poll()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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

	info, err := os.Stat(w.path)
	if err != nil {
		return fmt.Errorf("stat saved variables: %w", err)
	}
	if w.attempted && info.ModTime().Equal(w.lastMod) && info.Size() == w.lastSize {
		return nil
	}
	w.attempted = true
	w.lastMod = info.ModTime()
	w.lastSize = info.Size()

	sv, err := LoadSavedVariables(w.path)
	w.mu.Lock()
	w.lastErr = err
	if err == nil {
		w.current = sv
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

// Current returns the last successfully loaded data, or nil if none.
func (w *SVWatcher) Current() *SavedVars {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.current
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
