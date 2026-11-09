package bridge

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// channelFixture is app/testdata/channel_v3_fixture.json, written by
// tools/lua/gen_channel_fixture.lua running the addon's own Channel.lua.
type channelFixture struct {
	N        int                        `json:"N"`
	Status   string                     `json:"status"`
	Frames   []string                   `json:"frames"`   // per block 6 hex digits (R G B), N blocks
	Messages map[string]json.RawMessage `json:"messages"` // expected JSON of each message type
}

func loadChannelFixture(t *testing.T) channelFixture {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/channel_v3_fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx channelFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return fx
}

// blocksOfHex decodes the colours of one frame: N blocks of 3 bytes each.
func blocksOfHex(t *testing.T, s string, n int) [][3]byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 3*n {
		t.Fatalf("frame colours: %d bytes (want %d), err %v", len(b), 3*n, err)
	}
	out := make([][3]byte, n)
	for i := range out {
		out[i] = [3]byte{b[3*i], b[3*i+1], b[3*i+2]}
	}
	return out
}

// TestChannelFixturePipeline runs the addon's frames, as drawn by Lua, through the screen
// reader at two pitches (4 px and 5.33 px, with colour shift and noise), reassembles the
// messages and checks each one against the JSON the addon encoded.
func TestChannelFixturePipeline(t *testing.T) {
	fx := loadChannelFixture(t)
	statusWant, err := hex.DecodeString(fx.Status)
	if err != nil || len(statusWant) != PayloadLen {
		t.Fatalf("fixture status: %v", err)
	}
	if len(fx.Frames) < 10 || len(fx.Messages) != 11 {
		t.Fatalf("fixture has %d frames and %d messages; regenerate it", len(fx.Frames), len(fx.Messages))
	}

	for _, p := range []float64{4, 5.33} {
		t.Run(fmt.Sprintf("pitch%.2f", p), func(t *testing.T) {
			ch := NewChannel()
			rng := rand.New(rand.NewSource(int64(p * 100)))
			width := int(math.Ceil(float64(fx.N)*p)) + 6
			now := time.Unix(7000, 0)
			for i, hx := range fx.Frames {
				blocks := blocksOfHex(t, hx, fx.N)
				img := renderV3(blocks, p, width, rng, observedDistortion, 2)
				f, _, err := DecodeFrameV3(img, 0)
				if err != nil {
					t.Fatalf("frame %d: %v", i, err)
				}
				if f.N != fx.N {
					t.Fatalf("frame %d: N = %d, want %d", i, f.N, fx.N)
				}
				if got := encodePayload(f.Status); !bytes.Equal(got[:], statusWant) {
					t.Fatalf("frame %d: status bytes differ from the fixture", i)
				}
				ch.Feed(f, now.Add(time.Duration(i)*200*time.Millisecond))
			}
			for key, raw := range fx.Messages {
				typ, err := strconv.Atoi(key)
				if err != nil {
					t.Fatalf("message key %q", key)
				}
				it, ok := ch.Data.Get(uint8(typ))
				if !ok {
					t.Errorf("message %s (%s) never arrived", key, MsgNames[uint8(typ)])
					continue
				}
				var want any
				if err := json.Unmarshal(raw, &want); err != nil {
					t.Fatalf("expected JSON for %s: %v", key, err)
				}
				if !reflect.DeepEqual(it.Value, want) {
					t.Errorf("message %s (%s): decoded value differs from the addon's JSON", key, MsgNames[uint8(typ)])
				}
			}
			if n, msg := ch.Data.DecodeErrors(); n != 0 {
				t.Errorf("decode errors: %d (%s)", n, msg)
			}
		})
	}
}

// TestChannelFixtureSpeed bounds the cost of decoding one 3440 px frame, which runs on every
// poll (the search runs only when the hint fails).
func TestChannelFixtureSpeed(t *testing.T) {
	fx := loadChannelFixture(t)
	blocks := blocksOfHex(t, fx.Frames[0], fx.N)
	img := renderV3(blocks, 4, fx.N*4+6, rand.New(rand.NewSource(1)), identity, 0)
	start := time.Now()
	if _, _, err := DecodeFrameV3(img, 0); err != nil {
		t.Fatal(err)
	}
	full := time.Since(start)
	start = time.Now()
	_, p, err := DecodeFrameV3(img, 4)
	if err != nil {
		t.Fatal(err)
	}
	hinted := time.Since(start)
	t.Logf("v3 decode at 3440 px: full search %v, with pitch hint %v (pitch %.3f)", full, hinted, p)
	if full > 500*time.Millisecond {
		t.Errorf("full search took %v", full)
	}
	if hinted > 100*time.Millisecond {
		t.Errorf("hinted decode took %v", hinted)
	}
}
