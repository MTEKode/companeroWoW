package bridge

import (
	"encoding/json"
	"time"
)

// CachedItem is one message type as the on-demand cache file keeps it: the latest complete
// message with the time it arrived. The file is written by the app (package ondemand); this
// file only converts between the store and that form.
type CachedItem struct {
	Type  uint8           `json:"type"`
	Name  string          `json:"name"`
	At    time.Time       `json:"at"`
	Count int64           `json:"count"`
	Bytes int             `json:"bytes"`
	Value json.RawMessage `json:"value"`
}

// Export returns the latest message of every type, as CachedItem, in type order. A message
// whose value cannot be encoded is left out.
func (d *LiveData) Export() []CachedItem {
	all := d.All()
	out := make([]CachedItem, 0, len(all))
	for t := uint8(1); t <= MsgSession; t++ {
		it, ok := all[t]
		if !ok {
			continue
		}
		raw, err := json.Marshal(it.Value)
		if err != nil {
			continue
		}
		out = append(out, CachedItem{Type: t, Name: it.Name, At: it.At, Count: it.Count, Bytes: it.Bytes, Value: raw})
	}
	return out
}

// Import loads cached messages into the store. A type that already holds a message at least
// as new as the cached one is kept. Items with an unknown type or a value that does not decode
// are skipped. It returns how many were loaded.
func (d *LiveData) Import(items []CachedItem) int {
	n := 0
	for _, c := range items {
		if c.Type == 0 || c.At.IsZero() || len(c.Value) == 0 {
			continue
		}
		var v any
		if err := json.Unmarshal(c.Value, &v); err != nil {
			continue
		}
		d.mu.Lock()
		if cur, ok := d.items[c.Type]; ok && !cur.At.Before(c.At) {
			d.mu.Unlock()
			continue
		}
		d.items[c.Type] = LiveItem{
			Type:  c.Type,
			Name:  MsgNames[c.Type],
			Value: v,
			Bytes: c.Bytes,
			At:    c.At,
			Count: c.Count,
		}
		d.mu.Unlock()
		n++
	}
	return n
}
