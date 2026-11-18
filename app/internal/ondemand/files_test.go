package ondemand

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// The cache keeps the last message of each type with its arrival time, so that an answer
// after a restart still has the data and its age.
func TestCacheRoundTripKeepsValuesAndAges(t *testing.T) {
	path := filepath.Join(t.TempDir(), CacheFile)
	src := bridge.NewLiveData()
	at := time.Date(2026, 11, 18, 19, 30, 0, 0, time.UTC)
	if err := src.Put(bridge.MsgCharacter, []byte(`{"name":"Ana","level":19}`), at); err != nil {
		t.Fatal(err)
	}
	if err := src.Put(bridge.MsgSession, []byte(`{"xpGained":123,"levels":{"start":18}}`), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := SaveCache(path, src, at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	dst := bridge.NewLiveData()
	n, err := LoadCache(path, dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("loaded %d messages, want 2", n)
	}
	it, ok := dst.Get(bridge.MsgCharacter)
	if !ok {
		t.Fatal("character missing after load")
	}
	if !it.At.Equal(at) {
		t.Errorf("At = %v, want the original arrival %v", it.At, at)
	}
	m, _ := it.Value.(map[string]any)
	if m["name"] != "Ana" || m["level"] != float64(19) {
		t.Errorf("value = %#v", it.Value)
	}
	if it.Name != "character" {
		t.Errorf("name = %q, want character", it.Name)
	}
	if s, ok := dst.Get(bridge.MsgSession); !ok || !s.At.Equal(at.Add(time.Minute)) {
		t.Errorf("session missing or wrong time: %+v %v", s, ok)
	}
}

func TestCacheNotWrittenWhenEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), CacheFile)
	if err := os.WriteFile(path, []byte(`{"version":1,"items":[{"type":1,"at":"2026-01-01T00:00:00Z","value":{"a":1}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveCache(path, bridge.NewLiveData(), time.Now()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"value":{"a":1}`) {
		t.Fatalf("an empty store must not replace the previous cache; file now %q", data)
	}
}

func TestLoadCacheMissingFileLoadsNothing(t *testing.T) {
	n, err := LoadCache(filepath.Join(t.TempDir(), "none.json"), bridge.NewLiveData())
	if err != nil || n != 0 {
		t.Fatalf("missing cache: n=%d err=%v, want 0 and nil", n, err)
	}
}

func TestLoadCacheUnknownVersionIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), CacheFile)
	if err := os.WriteFile(path, []byte(`{"version":99,"items":[{"type":1,"at":"2026-01-01T00:00:00Z","value":{}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := LoadCache(path, bridge.NewLiveData())
	if err != nil || n != 0 {
		t.Fatalf("unknown version: n=%d err=%v, want 0 and nil", n, err)
	}
}

func TestLoadCacheDoesNotOverwriteNewerMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), CacheFile)
	old := bridge.NewLiveData()
	_ = old.Put(bridge.MsgQuests, []byte(`{"v":"old"}`), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := SaveCache(path, old, time.Now()); err != nil {
		t.Fatal(err)
	}
	dst := bridge.NewLiveData()
	_ = dst.Put(bridge.MsgQuests, []byte(`{"v":"new"}`), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if _, err := LoadCache(path, dst); err != nil {
		t.Fatal(err)
	}
	it, _ := dst.Get(bridge.MsgQuests)
	if m, _ := it.Value.(map[string]any); m["v"] != "new" {
		t.Fatalf("a newer live message was replaced by the cache: %#v", it.Value)
	}
}

func TestRuntimeStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), RuntimeFile)
	if _, ok, err := ReadRuntime(path); ok || err != nil {
		t.Fatalf("missing runtime file: ok=%v err=%v", ok, err)
	}
	want := RuntimeState{PID: 4242, Mode: "bajo_demanda", Active: false, Captures: 17, At: time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)}
	if err := SaveRuntime(path, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ReadRuntime(path)
	if err != nil || !ok {
		t.Fatalf("read: ok=%v err=%v", ok, err)
	}
	if got.PID != want.PID || got.Captures != want.Captures || got.Active != want.Active || got.Mode != want.Mode || !got.At.Equal(want.At) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
