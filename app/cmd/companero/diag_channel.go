package main

import (
	"context"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// diagChannelWait is how long --diag listens to the live channel before it reports.
const diagChannelWait = 2500 * time.Millisecond

// channelDiag is the live channel part of --diag: the status line and, per message type,
// how old the latest message is.
type channelDiag struct {
	Status string      `json:"status"`
	Live   bool        `json:"live"`
	KBps   float64     `json:"kbPerSec"`
	Frames int64       `json:"frames"`
	Types  []typeDiag `json:"types"`
}

// typeDiag is the freshness of one message type.
type typeDiag struct {
	Type   uint8   `json:"type"`
	Name   string  `json:"name"`
	AgeSec float64 `json:"ageSec"`
	Count  int64   `json:"count"`
}

// diagChannel reads the pixel channel for wait and reports what arrived. It runs its own
// reader, so it works without the window; with WoW closed it reports no data.
func diagChannel(wait time.Duration) *channelDiag {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	r := bridge.NewReader(250 * time.Millisecond)
	r.Start(ctx)
	<-ctx.Done()
	ch := r.Channel()
	now := time.Now()
	text, live := ch.Status(now)
	out := &channelDiag{
		Status: text,
		Live:   live,
		KBps:   ch.Data.Rate(now) / 1024,
		Frames: ch.Data.Frames(),
		Types:  []typeDiag{},
	}
	for _, f := range ch.Freshness(now) {
		out.Types = append(out.Types, typeDiag{
			Type:   f.Type,
			Name:   f.Name,
			AgeSec: f.Age.Seconds(),
			Count:  f.Count,
		})
	}
	return out
}
