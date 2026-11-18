package bridge

import (
	"encoding/json"
	"sync"
	"time"
)

// Receiver tuning.
const (
	// reassemblyTTL drops a partial message that got no new chunk for this long.
	reassemblyTTL = 5 * time.Second
	// rateWindow is the span of the KB/s estimate.
	rateWindow = 5 * time.Second
	// freshAfter is how old the last frame may be and still count as a live channel.
	freshAfter = 3 * time.Second
)

// Reassembler collects the chunks of each message type into complete messages. A type has
// at most one message in flight: a chunk with a new msgId replaces the partial one (the
// addon replaces a queued message of the same type the same way). Duplicate and
// out-of-order chunks are fine. A partial message that gets no new chunk for TTL is dropped.
type Reassembler struct {
	TTL  time.Duration
	pend map[uint8]*partial
	done map[uint8]uint16 // msgId of the last complete message per type
}

type partial struct {
	id     uint16
	count  int
	chunks [][]byte
	have   []bool
	got    int
	last   time.Time
}

// NewReassembler returns an empty reassembler with the default TTL.
func NewReassembler() *Reassembler {
	return &Reassembler{
		TTL:  reassemblyTTL,
		pend: make(map[uint8]*partial),
		done: make(map[uint8]uint16),
	}
}

// Add takes the chunk of one frame. When it completes a message it returns the message
// type and body (the concatenated chunks) and true.
func (r *Reassembler) Add(f FrameV3, now time.Time) (uint8, []byte, bool) {
	r.Expire(now)
	t := f.MsgType
	count := int(f.ChunkCount)
	idx := int(f.ChunkIndex)
	if t == 0 || count == 0 || count > MaxChunkCount || idx >= count {
		return 0, nil, false
	}
	if last, ok := r.done[t]; ok && last == f.MsgID {
		if p := r.pend[t]; p == nil || p.id != f.MsgID {
			return 0, nil, false // a repeat of a message already delivered
		}
	}
	p := r.pend[t]
	if p == nil || p.id != f.MsgID || p.count != count {
		p = &partial{id: f.MsgID, count: count, chunks: make([][]byte, count), have: make([]bool, count)}
		r.pend[t] = p
	}
	p.last = now
	if !p.have[idx] {
		p.have[idx] = true
		p.chunks[idx] = append([]byte(nil), f.Data...)
		p.got++
	}
	if p.got < p.count {
		return 0, nil, false
	}
	var body []byte
	for _, c := range p.chunks {
		body = append(body, c...)
	}
	delete(r.pend, t)
	r.done[t] = f.MsgID
	return t, body, true
}

// Expire drops the partial messages that have been silent for TTL.
func (r *Reassembler) Expire(now time.Time) {
	for t, p := range r.pend {
		if now.Sub(p.last) > r.TTL {
			delete(r.pend, t)
		}
	}
}

// LiveItem is the latest complete message of one type.
type LiveItem struct {
	Type  uint8
	Name  string
	Value any       // the decoded JSON (map, slice or scalar)
	Bytes int       // size of the JSON body
	At    time.Time // when it arrived
	Count int64     // messages of this type received since start
}

// LiveData holds the latest message of each type, the frame statistics and the decode
// errors. It is safe for concurrent use: the poll goroutine writes, the UI reads.
type LiveData struct {
	mu        sync.RWMutex
	items     map[uint8]LiveItem
	samples   []rateSample // chunk bytes received, for the KB/s estimate
	frames    int64
	lastFrame time.Time
	decodeErr int64
	lastErr   string
}

type rateSample struct {
	at    time.Time
	bytes int
}

// NewLiveData returns an empty store.
func NewLiveData() *LiveData {
	return &LiveData{items: make(map[uint8]LiveItem)}
}

// Put stores a complete message body (JSON) of type t, received at now.
func (d *LiveData) Put(t uint8, body []byte, now time.Time) error {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		d.mu.Lock()
		d.decodeErr++
		d.lastErr = err.Error()
		d.mu.Unlock()
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	prev := d.items[t]
	d.items[t] = LiveItem{
		Type:  t,
		Name:  MsgNames[t],
		Value: v,
		Bytes: len(body),
		At:    now,
		Count: prev.Count + 1,
	}
	return nil
}

// Get returns the latest message of type t.
func (d *LiveData) Get(t uint8) (LiveItem, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	it, ok := d.items[t]
	return it, ok
}

// All returns a copy of the latest message of every type seen so far.
func (d *LiveData) All() map[uint8]LiveItem {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make(map[uint8]LiveItem, len(d.items))
	for k, v := range d.items {
		out[k] = v
	}
	return out
}

// noteFrame records one frame and the chunk bytes it carried.
func (d *LiveData) noteFrame(now time.Time, chunkBytes int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.frames++
	d.lastFrame = now
	if chunkBytes > 0 {
		d.samples = append(d.samples, rateSample{at: now, bytes: chunkBytes})
	}
	d.pruneLocked(now)
}

func (d *LiveData) pruneLocked(now time.Time) {
	i := 0
	for i < len(d.samples) && now.Sub(d.samples[i].at) > rateWindow {
		i++
	}
	if i > 0 {
		d.samples = append(d.samples[:0], d.samples[i:]...)
	}
}

// Rate is the chunk payload rate in bytes per second over the last rateWindow.
func (d *LiveData) Rate(now time.Time) float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pruneLocked(now)
	total := 0
	for _, s := range d.samples {
		total += s.bytes
	}
	return float64(total) / rateWindow.Seconds()
}

// LastFrame is when the last frame was decoded (zero if none).
func (d *LiveData) LastFrame() time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastFrame
}

// Frames is the number of v3 frames decoded so far.
func (d *LiveData) Frames() int64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.frames
}

// DecodeErrors returns the number of messages whose JSON did not parse, and the last error.
func (d *LiveData) DecodeErrors() (int64, string) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.decodeErr, d.lastErr
}

// Where the strip was read from, as the status line names it.
const (
	SourceScreen = "pantalla"
	SourceWindow = "captura de ventana"
)

// Channel is the v3 receiver: frames in, complete messages out into LiveData.
type Channel struct {
	re   *Reassembler
	Data *LiveData

	srcMu  sync.RWMutex
	source string // SourceScreen or SourceWindow; empty until the first read

	subMu sync.RWMutex
	subs  []MessageFunc
}

// MessageFunc receives each complete message: its type, its JSON body and when it arrived.
// It runs on the poll goroutine, so it must return quickly.
type MessageFunc func(t uint8, body []byte, at time.Time)

// OnMessage registers f to be called with every complete message, after it is stored. The
// messages of the question type (MsgQuestion) reach the game-mode answerer this way.
func (c *Channel) OnMessage(f MessageFunc) {
	if f == nil {
		return
	}
	c.subMu.Lock()
	c.subs = append(c.subs, f)
	c.subMu.Unlock()
}

// NewChannel returns a receiver with an empty store.
func NewChannel() *Channel {
	return &Channel{re: NewReassembler(), Data: NewLiveData()}
}

// SetSource records where the strip is being read from: SourceScreen or SourceWindow.
func (c *Channel) SetSource(src string) {
	c.srcMu.Lock()
	c.source = src
	c.srcMu.Unlock()
}

// Source returns where the strip is read from (SourceScreen or SourceWindow), or "".
func (c *Channel) Source() string {
	c.srcMu.RLock()
	defer c.srcMu.RUnlock()
	return c.source
}

// Feed takes one decoded v3 frame read at now.
func (c *Channel) Feed(f FrameV3, now time.Time) {
	c.Data.noteFrame(now, len(f.Data))
	t, body, ok := c.re.Add(f, now)
	if ok {
		_ = c.Data.Put(t, body, now) // a bad body is counted in DecodeErrors
		c.subMu.RLock()
		subs := c.subs
		c.subMu.RUnlock()
		for _, sub := range subs {
			sub(t, body, now)
		}
	}
}

// TypeFreshness is how old the latest message of one type is.
type TypeFreshness struct {
	Type  uint8
	Name  string
	Age   time.Duration
	Count int64
}

// Freshness lists every message type seen so far, in type order, with its age at now.
func (c *Channel) Freshness(now time.Time) []TypeFreshness {
	all := c.Data.All()
	var out []TypeFreshness
	for t := uint8(1); t <= MsgQuestion; t++ {
		it, ok := all[t]
		if !ok {
			continue
		}
		out = append(out, TypeFreshness{Type: t, Name: MsgNames[t], Age: now.Sub(it.At), Count: it.Count})
	}
	return out
}

// Status is the one-line state for the status bar: "Canal en vivo: OK (captura de ventana)"
// or "(pantalla)" while frames arrive, otherwise why it is not live. ok is true only when
// frames are fresh. The rate is in Rate and --diag.
func (c *Channel) Status(now time.Time) (string, bool) {
	last := c.Data.LastFrame()
	if last.IsZero() || now.Sub(last) > freshAfter {
		return "Canal en vivo: sin datos (WoW cerrado, sin CompaneroWoW v3 o sin /reload)", false
	}
	src := c.Source()
	if src == "" {
		src = SourceScreen
	}
	return "Canal en vivo: OK (" + src + ")", true
}
