package main

import (
	"context"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// gainNotifier is what the colour-gain notice needs from the game-mode controller.
type gainNotifier interface {
	Notify(text string) bool
}

// runGainNotice copies the colour-gain notice to the clipboard once per alert, while a question is
// pending (see gamemode.Controller.Notify). Each second it checks the reader's alert until ctx ends;
// with no question pending the copy is tried again on the next tick.
func runGainNotice(ctx context.Context, reader *bridge.Reader, n gainNotifier) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	sent := false
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			gain, alert := reader.ColourGainAlert(now)
			if !alert {
				sent = false
				continue
			}
			if !sent {
				sent = n.Notify(bridge.ColourGainText(gain))
			}
		}
	}
}
