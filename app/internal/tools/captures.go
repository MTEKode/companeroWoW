package tools

import "sync"

// Captures keeps the tool calls made while one question is answered, so that the answer
// verifier can check the answer against the data the tools actually gave. The CLI providers
// call the tools over MCP, where no question identity travels with the call, so a capture
// takes every call made while it is open. That can include the calls of a proactive tip that
// ran at the same time; it only widens the evidence, so it can hide a violation but never
// invent one.
type Captures struct {
	mu   sync.Mutex
	open map[*Capture]struct{}
}

// Capture is one open window of calls. End closes it and returns what it saw.
type Capture struct {
	owner *Captures
	mu    sync.Mutex
	calls []CallRecord
	done  bool
}

// NewCaptures returns an empty set of captures.
func NewCaptures() *Captures {
	return &Captures{open: make(map[*Capture]struct{})}
}

// Begin opens a capture. Every call made from now until End is recorded in it.
func (c *Captures) Begin() *Capture {
	cp := &Capture{owner: c}
	c.mu.Lock()
	c.open[cp] = struct{}{}
	c.mu.Unlock()
	return cp
}

// Observe records a finished call in every open capture. It is a registry observer; see
// Registry.AddObserver.
func (c *Captures) Observe(rec CallRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for cp := range c.open {
		cp.mu.Lock()
		cp.calls = append(cp.calls, rec)
		cp.mu.Unlock()
	}
}

// End closes the capture and returns the calls it recorded, in the order they finished. A
// second End returns nil.
func (cp *Capture) End() []CallRecord {
	if cp == nil || cp.owner == nil {
		return nil
	}
	cp.owner.mu.Lock()
	delete(cp.owner.open, cp)
	cp.owner.mu.Unlock()
	cp.mu.Lock()
	defer cp.mu.Unlock()
	if cp.done {
		return nil
	}
	cp.done = true
	out := cp.calls
	cp.calls = nil
	return out
}
