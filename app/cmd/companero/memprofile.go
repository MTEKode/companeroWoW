package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"time"
)

// memProfileIdle is how long the app runs before --profile-mem writes its heap profile. It is
// measured from the start, so the profile shows the warm, idle app.
const memProfileIdle = 30 * time.Second

// memProfileRate is the heap sampling rate used with --profile-mem (default is 512 KiB).
// A finer rate costs little and shows the small retainers too.
const memProfileRate = 4096

// startMemProfile writes a heap profile to path after memProfileIdle and a MemStats summary
// to path+".txt". It runs once and stops when ctx ends. Use `go tool pprof -top <path>`.
func startMemProfile(ctx context.Context, path string) {
	runtime.MemProfileRate = memProfileRate
	go func() {
		t := time.NewTimer(memProfileIdle)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := writeMemProfile(path); err != nil {
			log.Printf("profile-mem: %v", err)
			return
		}
		log.Printf("profile-mem: perfil de memoria escrito en %s", path)
	}()
}

// writeMemProfile collects garbage, then writes the heap profile and the summary.
func writeMemProfile(path string) error {
	runtime.GC()
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
		_ = f.Close()
		return fmt.Errorf("write heap profile: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return os.WriteFile(path+".txt", []byte(memStatsText(time.Now())), 0o644)
}

// memStatsText formats the runtime's memory counters: what the Go heap holds and what the
// runtime has reserved from the OS, which is what the process's private bytes follow.
func memStatsText(now time.Time) string {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return fmt.Sprintf(`time            %s
heap_alloc      %d  (live objects)
heap_inuse      %d  (spans in use)
heap_idle       %d
heap_released   %d  (returned to the OS)
heap_sys        %d
stack_inuse     %d
mspan_inuse     %d
mcache_inuse    %d
gc_sys          %d
other_sys       %d
sys             %d  (total obtained from the OS)
num_gc          %d
gc_percent      %d
`,
		now.Format(time.RFC3339),
		ms.HeapAlloc, ms.HeapInuse, ms.HeapIdle, ms.HeapReleased, ms.HeapSys,
		ms.StackInuse, ms.MSpanInuse, ms.MCacheInuse, ms.GCSys, ms.OtherSys, ms.Sys,
		ms.NumGC, gcPercent)
}
