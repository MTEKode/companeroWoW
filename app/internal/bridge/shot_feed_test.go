package bridge

import (
	"testing"
	"time"
)

// A reader that is disabled never captures, whatever it is asked, and a frame fed from a
// screenshot reaches the channel and the status exactly like a live read.
func TestDisabledReaderNeverActivates(t *testing.T) {
	r := NewReader(time.Second)
	r.Disable()
	r.SetActive(true)
	r.SetWatch(true)
	if r.Active() || r.Watching() || !r.Disabled() {
		t.Fatalf("active %v, watching %v, disabled %v after Disable", r.Active(), r.Watching(), r.Disabled())
	}
}

func TestFeedFrameIngestsLikeALiveRead(t *testing.T) {
	r := NewReader(time.Second)
	r.Disable()
	body := []byte(`{"level":60}`)
	now := time.Now()
	f, err := BuildFrameV5(LiveState{Level: 60, Money: 5}, 1, 64, 9, &ChunkV3{Type: MsgCharacter, ID: 4, Count: 1, Data: body})
	if err != nil {
		t.Fatal(err)
	}
	fr, err := ParseFrameV5(f)
	if err != nil {
		t.Fatal(err)
	}
	r.FeedFrame(fr, now)

	st, ok := r.Latest()
	if !ok || st.Level != 60 || st.Money != 5 {
		t.Fatalf("Latest = %+v, %v", st, ok)
	}
	if got := r.Channel().Source(); got != SourceShot {
		t.Fatalf("source = %q, want %q", got, SourceShot)
	}
	if it, ok := r.Channel().Data.Get(MsgCharacter); !ok || it.Count != 1 {
		t.Fatalf("the character message did not arrive: %+v, %v", it, ok)
	}
	if got := r.Stats().Source; got != SourceShot {
		t.Fatalf("stats source = %q", got)
	}
}
