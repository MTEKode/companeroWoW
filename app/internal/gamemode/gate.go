package gamemode

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Gate decides which questions are new. The addon numbers the questions of each character with
// an increasing id and sends the latest one again every 2 s, so a question is answered once: its
// id must be above the last one answered for that character. The last ids are kept in a small
// JSON file, so a restart does not answer the same question again.
type Gate struct {
	mu   sync.Mutex
	path string // "" keeps the ids in memory only (tests)
	last map[string]uint64
}

// NewGate loads the ids kept at path. A missing file starts empty; a damaged one is replaced
// by an empty set on the next save, so the error is only logged by the caller.
func NewGate(path string) (*Gate, error) {
	g := &Gate{path: path, last: map[string]uint64{}}
	if path == "" {
		return g, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return g, nil
	}
	if err != nil {
		return g, err
	}
	if err := json.Unmarshal(data, &g.last); err != nil {
		g.last = map[string]uint64{}
		return g, err
	}
	return g, nil
}

// Accept reports whether question id is new for key (the character) and, when it is, records
// it. A question at or below the last one recorded is a repeat or an older question. The record
// is saved before Accept returns, so a restart right after does not answer it again.
func (g *Gate) Accept(key string, id uint64) (bool, error) {
	if id == 0 {
		return false, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if id <= g.last[key] {
		return false, nil
	}
	g.last[key] = id
	return true, g.saveLocked()
}

// Last returns the last question id recorded for key (0 when none).
func (g *Gate) Last(key string) uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.last[key]
}

// saveLocked writes the ids atomically. It does nothing without a path.
func (g *Gate) saveLocked() error {
	if g.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(g.last, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(g.path), 0o755); err != nil {
		return err
	}
	tmp := g.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, g.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
