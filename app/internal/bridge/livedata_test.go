package bridge

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"
)

// chunksOf splits body into chunks of size cs, as the addon does.
func chunksOf(body []byte, cs int) [][]byte {
	var out [][]byte
	for i := 0; i < len(body); i += cs {
		end := i + cs
		if end > len(body) {
			end = len(body)
		}
		out = append(out, body[i:end])
	}
	if len(out) == 0 {
		out = [][]byte{nil}
	}
	return out
}

func chunkFrame(t uint8, id uint16, idx, count int, data []byte) FrameV3 {
	return FrameV3{MsgType: t, MsgID: id, ChunkIndex: uint16(idx), ChunkCount: uint16(count), Data: data}
}

func TestReassemblerOutOfOrderAndDuplicates(t *testing.T) {
	body := []byte(`{"quests":[1,2,3],"text":"` + strings.Repeat("x", 300) + `"}`)
	parts := chunksOf(body, 100)
	order := rand.New(rand.NewSource(4)).Perm(len(parts))
	re := NewReassembler()
	now := time.Unix(1000, 0)
	var got []byte
	done := 0
	for step, idx := range order {
		// Each chunk is delivered twice, the second copy must be harmless.
		for rep := 0; rep < 2; rep++ {
			typ, b, ok := re.Add(chunkFrame(MsgQuests, 7, idx, len(parts), parts[idx]), now.Add(time.Duration(step)*time.Millisecond))
			if ok {
				done++
				got = b
				if typ != MsgQuests {
					t.Fatalf("type %d, want %d", typ, MsgQuests)
				}
			}
		}
	}
	if done != 1 {
		t.Fatalf("message completed %d times, want once", done)
	}
	if string(got) != string(body) {
		t.Fatalf("reassembled %q, want %q", got, body)
	}
}

func TestReassemblerMissingChunkTimesOut(t *testing.T) {
	body := []byte(strings.Repeat("abcdefgh", 60))
	parts := chunksOf(body, 100)
	re := NewReassembler()
	now := time.Unix(2000, 0)
	// Chunk 1 never arrives.
	for _, i := range []int{0, 2, 3} {
		if _, _, ok := re.Add(chunkFrame(MsgBags, 9, i, len(parts), parts[i]), now); ok {
			t.Fatal("message complete with a missing chunk")
		}
	}
	re.Expire(now.Add(reassemblyTTL + time.Millisecond))
	// The partial is gone: the late chunk alone does not complete anything.
	if _, _, ok := re.Add(chunkFrame(MsgBags, 9, 1, len(parts), parts[1]), now.Add(reassemblyTTL+2*time.Millisecond)); ok {
		t.Fatal("a partial message survived its timeout")
	}
	// A full re-send of the message (the carousel) delivers it.
	var body2 []byte
	for i := range parts {
		if typ, b, ok := re.Add(chunkFrame(MsgBags, 10, i, len(parts), parts[i]), now.Add(time.Minute)); ok {
			if typ != MsgBags {
				t.Fatalf("type %d", typ)
			}
			body2 = b
		}
	}
	if string(body2) != string(body) {
		t.Fatalf("re-sent message = %q", body2)
	}
}

func TestReassemblerNewIDReplacesPartial(t *testing.T) {
	re := NewReassembler()
	now := time.Unix(3000, 0)
	re.Add(chunkFrame(MsgCombat, 1, 0, 2, []byte("old")), now)
	// The addon replaced the message: a new msgId with one chunk completes at once.
	typ, b, ok := re.Add(chunkFrame(MsgCombat, 2, 0, 1, []byte(`{"n":2}`)), now)
	if !ok || typ != MsgCombat || string(b) != `{"n":2}` {
		t.Fatalf("new id: ok %v type %d body %q", ok, typ, b)
	}
	// The old msgId's remaining chunk does not complete a message any more.
	if _, _, ok := re.Add(chunkFrame(MsgCombat, 1, 1, 2, []byte("x")), now); ok {
		t.Fatal("old partial completed after replacement")
	}
}

func TestReassemblerIgnoresIdleAndBadChunks(t *testing.T) {
	re := NewReassembler()
	now := time.Unix(4000, 0)
	if _, _, ok := re.Add(FrameV3{}, now); ok {
		t.Fatal("idle frame completed a message")
	}
	if _, _, ok := re.Add(chunkFrame(MsgQuests, 1, 5, 2, []byte("x")), now); ok {
		t.Fatal("chunk index past the count was accepted")
	}
	if _, _, ok := re.Add(chunkFrame(MsgQuests, 1, 0, MaxChunkCount+1, []byte("x")), now); ok {
		t.Fatal("chunk count over the limit was accepted")
	}
}

func TestLiveDataPutGetAndBadJSON(t *testing.T) {
	d := NewLiveData()
	now := time.Unix(5000, 0)
	if err := d.Put(MsgBags, []byte(`{"items":[{"id":1,"n":5}]}`), now); err != nil {
		t.Fatal(err)
	}
	it, ok := d.Get(MsgBags)
	if !ok || it.Name != "bags" || it.Count != 1 || it.Bytes == 0 {
		t.Fatalf("item %+v ok %v", it, ok)
	}
	want := map[string]any{"items": []any{map[string]any{"id": float64(1), "n": float64(5)}}}
	if !reflect.DeepEqual(it.Value, want) {
		t.Fatalf("value %v, want %v", it.Value, want)
	}
	if err := d.Put(MsgBags, []byte(`{"items":`), now); err == nil {
		t.Fatal("broken JSON accepted")
	}
	if n, msg := d.DecodeErrors(); n != 1 || msg == "" {
		t.Fatalf("decode errors %d %q", n, msg)
	}
	if got, _ := d.Get(MsgBags); got.Count != 1 {
		t.Fatal("a bad body replaced the good one")
	}
}

func TestChannelFeedsRateStatusAndFreshness(t *testing.T) {
	c := NewChannel()
	now := time.Unix(6000, 0)
	if s, ok := c.Status(now); ok || !strings.Contains(s, "sin datos") {
		t.Fatalf("before any frame: %q ok %v", s, ok)
	}
	body := []byte(`{"money":12345,"level":60}`)
	parts := chunksOf(body, 10)
	for i := range parts {
		f := chunkFrame(MsgCharacter, 3, i, len(parts), parts[i])
		c.Feed(f, now.Add(time.Duration(i)*200*time.Millisecond))
	}
	// Idle frames keep the channel fresh.
	for i := 0; i < 10; i++ {
		c.Feed(FrameV3{}, now.Add(2*time.Second+time.Duration(i)*200*time.Millisecond))
	}
	at := now.Add(2 * time.Second)
	s, ok := c.Status(at)
	if !ok || !strings.HasPrefix(s, "Canal en vivo: OK (") || !strings.HasSuffix(s, "KB/s)") {
		t.Fatalf("status %q ok %v", s, ok)
	}
	if _, ok := c.Status(at.Add(10 * time.Second)); ok {
		t.Fatal("a channel with no frames for 10 s still reads as live")
	}
	fr := c.Freshness(at)
	if len(fr) != 1 || fr[0].Name != "character" || fr[0].Count != 1 || fr[0].Age < 0 {
		t.Fatalf("freshness %+v", fr)
	}
	if it, _ := c.Data.Get(MsgCharacter); it.Value.(map[string]any)["level"] != float64(60) {
		t.Fatalf("stored value %v", it.Value)
	}
}

// TestJSONBodyRoundtrip checks that the store reads what a JSON encoder writes.
func TestJSONBodyRoundtrip(t *testing.T) {
	in := map[string]any{"a": []any{1.0, "two", true}, "b": map[string]any{"c": nil}}
	body, _ := json.Marshal(in)
	d := NewLiveData()
	if err := d.Put(MsgMeta, body, time.Now()); err != nil {
		t.Fatal(err)
	}
	it, _ := d.Get(MsgMeta)
	if !reflect.DeepEqual(it.Value, in) {
		t.Fatalf("value %v, want %v", it.Value, in)
	}
}
