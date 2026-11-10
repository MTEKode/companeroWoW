package assistant

import (
	"context"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// gateBuffer is the size of the filtered channel, the same as the bridge's.
const gateBuffer = 16

// Gate forwards the bridge events to the returned channel while enabled reports true, and
// drops them while it reports false. It is how the proactive tips are switched off and on
// without restarting the tip loop: the loop keeps running and sees no events while the tips
// are off. Forwarding never blocks; a full output drops the event, as the bridge does. The
// output closes when in closes or ctx ends.
func Gate(ctx context.Context, in <-chan bridge.LiveState, enabled func() bool) <-chan bridge.LiveState {
	out := make(chan bridge.LiveState, gateBuffer)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-in:
				if !ok {
					return
				}
				if enabled != nil && !enabled() {
					continue
				}
				select {
				case out <- ev:
				default:
				}
			}
		}
	}()
	return out
}
