package ondemand

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// CacheFile is the name of the last-known-data file in the app's data folder.
const CacheFile = "livecache.json"

// RuntimeFile is the name of the state file the running app writes at each transition, so
// that --diag can report the capture counter of the app that is running.
const RuntimeFile = "runtime.json"

// cacheVersion is bumped when the file's shape changes; an unknown version is ignored.
const cacheVersion = 1

type cacheDoc struct {
	Version int                 `json:"version"`
	SavedAt time.Time           `json:"savedAt"`
	Items   []bridge.CachedItem `json:"items"`
}

// SaveCache writes the latest message of each type to path, atomically. An empty store is
// not written, so that a run without data does not erase the previous cache.
func SaveCache(path string, d *bridge.LiveData, now time.Time) error {
	if d == nil {
		return nil
	}
	items := d.Export()
	if len(items) == 0 {
		return nil
	}
	return writeJSON(path, cacheDoc{Version: cacheVersion, SavedAt: now, Items: items})
}

// LoadCache reads the cache at path into d and reports how many messages were loaded. A missing
// or unreadable file loads nothing and is not an error for the caller: the app then starts
// without cached data.
func LoadCache(path string, d *bridge.LiveData) (int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("leer %s: %w", path, err)
	}
	var doc cacheDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return 0, fmt.Errorf("leer %s: %w", path, err)
	}
	if doc.Version != cacheVersion {
		return 0, nil
	}
	return d.Import(doc.Items), nil
}

// RuntimeState is what the running app reports in runtime.json.
type RuntimeState struct {
	PID      int       `json:"pid"`
	Mode     string    `json:"mode"`
	Active   bool      `json:"active"`
	Captures int64     `json:"captures"`
	At       time.Time `json:"at"`
}

// SaveRuntime writes the state of the running app to path, atomically.
func SaveRuntime(path string, s RuntimeState) error {
	return writeJSON(path, s)
}

// ReadRuntime reads the state of the running app. A missing file returns ok false.
func ReadRuntime(path string) (RuntimeState, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return RuntimeState{}, false, nil
	}
	if err != nil {
		return RuntimeState{}, false, err
	}
	var s RuntimeState
	if err := json.Unmarshal(data, &s); err != nil {
		return RuntimeState{}, false, err
	}
	return s, true, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("codificar %s: %w", filepath.Base(path), err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("crear la carpeta de %s: %w", filepath.Base(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("escribir %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("reemplazar %s: %w", path, err)
	}
	return nil
}
