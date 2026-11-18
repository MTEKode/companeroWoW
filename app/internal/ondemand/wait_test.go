package ondemand

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// fakeFeed is the game side as the test sets it: the status freshness and the arrival times.
type fakeFeed struct {
	fresh bool
	at    map[uint8]time.Time
}

func (f *fakeFeed) StatusFresh() bool { return f.fresh }

func (f *fakeFeed) MessageAt(t uint8) (time.Time, bool) {
	at, ok := f.at[t]
	return at, ok
}

var t0 = time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)

// waitRig runs a Waiter on a fake clock. hook runs at each simulated poll and may change the feed.
type waitRig struct {
	now   time.Time
	feed  *fakeFeed
	hook  func(now time.Time, f *fakeFeed)
	calls int
}

func (r *waitRig) waiter() *Waiter {
	return &Waiter{
		Feed: r.feed,
		Now:  func() time.Time { return r.now },
		Sleep: func(ctx context.Context, d time.Duration) error {
			r.calls++
			if err := ctx.Err(); err != nil {
				return err
			}
			r.now = r.now.Add(d)
			if r.hook != nil {
				r.hook(r.now, r.feed)
			}
			return nil
		},
	}
}

func allKeys(at time.Time) map[uint8]time.Time {
	m := map[uint8]time.Time{}
	for _, k := range KeyMessages {
		m[k] = at
	}
	return m
}

func TestReadinessNeedsStatusAndAllKeys(t *testing.T) {
	activated := t0
	feed := &fakeFeed{at: allKeys(t0.Add(time.Second))}
	have, ready := Readiness(t0.Add(2*time.Second), activated, false, feed.MessageAt)
	if have != len(KeyMessages) || ready {
		t.Fatalf("no status payload: have=%d ready=%v, want all keys but not ready", have, ready)
	}
	feed.fresh = true
	if _, ready := Readiness(t0.Add(2*time.Second), activated, true, feed.MessageAt); !ready {
		t.Fatal("status fresh and all keys since activation must be ready")
	}
}

func TestReadinessCountsOnlyRecentOldMessages(t *testing.T) {
	// Received before the activation: counts only while it is under RecentWindow old.
	activated := t0.Add(time.Minute)
	now := t0.Add(time.Minute + 10*time.Second)
	feed := &fakeFeed{fresh: true, at: map[uint8]time.Time{
		bridge.MsgCharacter: t0.Add(50 * time.Second), // 20 s old: counts
		bridge.MsgSheet:     t0,                       // 70 s old: does not
	}}
	have, ready := Readiness(now, activated, true, feed.MessageAt)
	if have != 1 || ready {
		t.Fatalf("have=%d ready=%v, want 1 and not ready", have, ready)
	}
}

func TestWaitReturnsAsSoonAsDataIsFresh(t *testing.T) {
	feed := &fakeFeed{}
	r := &waitRig{now: t0.Add(time.Minute), feed: feed}
	activated := r.now
	// The status comes after 1 s; the key messages after 2 s, one by one.
	r.hook = func(now time.Time, f *fakeFeed) {
		if now.Sub(activated) >= time.Second {
			f.fresh = true
		}
		for i, k := range KeyMessages {
			if now.Sub(activated) >= time.Duration(i+1)*time.Duration(400)*time.Millisecond {
				if f.at == nil {
					f.at = map[uint8]time.Time{}
				}
				f.at[k] = now
			}
		}
	}
	var progress []int
	stale, err := r.waiter().Wait(context.Background(), activated, 8*time.Second, func(h, total int) {
		if total != len(KeyMessages) {
			t.Errorf("total = %d, want %d", total, len(KeyMessages))
		}
		progress = append(progress, h)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("ready data must have no stale blocks, got %+v", stale)
	}
	if got := r.now.Sub(activated); got > 2500*time.Millisecond {
		t.Errorf("waited %v; the data was ready after about 2 s", got)
	}
	if len(progress) < 2 || progress[len(progress)-1] != len(KeyMessages) {
		t.Errorf("progress = %v, want steps up to %d", progress, len(KeyMessages))
	}
}

func TestWaitTimesOutWithAgesOfStaleBlocks(t *testing.T) {
	// The status never comes; the character is 3 min old, the other keys never arrived.
	feed := &fakeFeed{at: map[uint8]time.Time{bridge.MsgCharacter: t0.Add(-3 * time.Minute)}}
	r := &waitRig{now: t0, feed: feed}
	activated := t0
	stale, err := r.waiter().Wait(context.Background(), activated, 8*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.now.Sub(t0); got != 8*time.Second {
		t.Errorf("waited %v, want exactly the 8 s timeout", got)
	}
	note := StaleNote(stale)
	if !strings.Contains(note, "personaje (datos de hace 3 min)") {
		t.Errorf("note lacks the aged character block: %q", note)
	}
	if !strings.Contains(note, "misiones (sin datos del juego)") {
		t.Errorf("note lacks the missing quest block: %q", note)
	}
}

func TestWaitCancelledReturnsContextError(t *testing.T) {
	feed := &fakeFeed{}
	r := &waitRig{now: t0, feed: feed}
	ctx, cancel := context.WithCancel(context.Background())
	r.hook = func(time.Time, *fakeFeed) { cancel() }
	_, err := r.waiter().Wait(ctx, t0, 8*time.Second, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestStaleNoteEmptyWhenFresh(t *testing.T) {
	if got := StaleNote(nil); got != "" {
		t.Fatalf("StaleNote(nil) = %q, want empty", got)
	}
}

func TestHumanAgeSpanish(t *testing.T) {
	cases := map[time.Duration]string{
		10 * time.Second:   "menos de 1 min",
		3 * time.Minute:    "3 min",
		2 * time.Hour:      "2 h",
		50 * time.Hour:     "2 d",
		90 * time.Minute:   "1 h",
		59*time.Minute + 2: "59 min",
	}
	for d, want := range cases {
		if got := humanAge(d); got != want {
			t.Errorf("humanAge(%v) = %q, want %q", d, got, want)
		}
	}
}

// While the addon streams its share window a question waits up to 20 s; otherwise at most 3 s,
// and a shorter configured wait is kept.
func TestWaitForShareWindow(t *testing.T) {
	cases := []struct {
		name       string
		remaining  int
		configured time.Duration
		want       time.Duration
	}{
		{"share window running", 12, 8 * time.Second, 20 * time.Second},
		{"share window running, short config", 1, time.Second, 20 * time.Second},
		{"no share window, default config", 0, 8 * time.Second, 3 * time.Second},
		{"no share window, shorter config kept", 0, 2 * time.Second, 2 * time.Second},
		{"no share window, unset config", 0, 0, 3 * time.Second},
	}
	for _, tc := range cases {
		if got := WaitFor(tc.remaining, tc.configured); got != tc.want {
			t.Errorf("%s: WaitFor(%d, %v) = %v, want %v", tc.name, tc.remaining, tc.configured, got, tc.want)
		}
	}
}
