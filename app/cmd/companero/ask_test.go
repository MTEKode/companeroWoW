package main

import (
	"net"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// With the configured port held by another process (the running app), --ask serves on another free port.
func TestAskUsesAnotherPortWhenTheConfiguredOneIsBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	held := busy.Addr().(*net.TCPAddr).Port

	port, err := askMCPPort(held)
	if err != nil {
		t.Fatal(err)
	}
	if port == held || port == 0 {
		t.Fatalf("port = %d while %d is held", port, held)
	}
	// The port it chose is free: a server can listen on it now.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("the chosen port %d is not free: %v", port, err)
	}
	_ = ln.Close()
}

// A free configured port is kept.
func TestAskKeepsAFreeConfiguredPort(t *testing.T) {
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	want := free.Addr().(*net.TCPAddr).Port
	_ = free.Close()
	got, err := askMCPPort(want)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("port = %d, want the free configured %d", got, want)
	}
}

// The run's store is a copy: its writes stay in the copy, and the data directory's database is not changed by
// them. The database is open by another handle while the run starts, as the app keeps it open.
func TestAskStoreWorksOnACopyWhileTheAppHoldsTheDatabase(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "companero.db")
	app, err := store.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	if err := app.CachePut("teoria.app", "escrito por la app"); err != nil {
		t.Fatal(err)
	}

	run, closeRun, err := askStore(dir)
	if err != nil {
		t.Fatalf("askStore: %v", err)
	}
	if v, ok, err := run.CacheGet("teoria.app", 0); err != nil || !ok || v != "escrito por la app" {
		t.Fatalf("the run does not see the app's write: v=%q ok=%v err=%v", v, ok, err)
	}
	if err := run.CachePut("teoria.run", "solo en la copia"); err != nil {
		t.Fatal(err)
	}
	closeRun()

	if _, ok, err := app.CacheGet("teoria.run", 0); err != nil || ok {
		t.Fatalf("the run wrote into the app's database: ok=%v err=%v", ok, err)
	}
}
