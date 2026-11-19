package shots

import (
	"sort"
	"time"
)

// settler decides when a new file is fully written: its size and modification time have not
// changed for quiet, and the file opens for reading (WoW holds it open while it writes). A file
// that does not settle within maxWait is dropped.
type settler struct {
	quiet, maxWait time.Duration
	pend           map[string]*settleEntry
}

type settleEntry struct {
	known      bool
	size       int64
	modTime    time.Time
	stableFrom time.Time // when the size and time last changed
	first      time.Time // when the file was first noted
}

func newSettler(quiet, maxWait time.Duration) *settler {
	return &settler{quiet: quiet, maxWait: maxWait, pend: make(map[string]*settleEntry)}
}

// note records that path changed at now. A path already pending keeps its first time.
func (s *settler) note(path string, now time.Time) {
	if _, ok := s.pend[path]; !ok {
		s.pend[path] = &settleEntry{first: now}
	}
}

// pending is the number of files waiting to settle.
func (s *settler) pending() int { return len(s.pend) }

// ready returns, in name order, the pending files that are fully written at now, and forgets
// them. A file that vanished, or never settled within maxWait, is forgotten too.
func (s *settler) ready(now time.Time, fs FS) []string {
	var out []string
	for path, e := range s.pend {
		info, err := fs.Stat(path)
		if err != nil {
			delete(s.pend, path)
			continue
		}
		if !e.known || info.Size != e.size || !info.ModTime.Equal(e.modTime) {
			e.known = true
			e.size = info.Size
			e.modTime = info.ModTime
			e.stableFrom = now
		}
		if now.Sub(e.first) > s.maxWait {
			delete(s.pend, path)
			continue
		}
		if now.Sub(e.stableFrom) < s.quiet {
			continue
		}
		if fs.Probe(path) != nil {
			continue // still held open by the writer
		}
		delete(s.pend, path)
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
