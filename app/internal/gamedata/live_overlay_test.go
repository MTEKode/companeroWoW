package gamedata

import (
	"reflect"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// liveService returns a service whose reader has an empty live channel.
func liveService(t *testing.T) (*Service, *bridge.Channel) {
	t.Helper()
	r := bridge.NewReader(time.Second)
	return NewService(r, nil, nil), r.Channel()
}

func TestOverlayReplacesNewerBlocksOnly(t *testing.T) {
	g, live := liveService(t)
	saved := time.Unix(1000, 0)
	oldInv := map[string]any{"items": []any{map[string]any{"id": float64(1)}}}
	base := map[string]any{"inventory": oldInv, "quests": []any{}}
	ch := charEntry{key: "A-R", data: base, updatedAt: saved}

	if err := live.Data.Put(bridge.MsgBags, []byte(`{"inventory":{"items":[{"id":2}]}}`), saved.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := live.Data.Put(bridge.MsgQuests, []byte(`{"quests":[{"questID":9}]}`), saved.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	got := g.overlayLive(ch)
	inv, _ := got.data["inventory"].(map[string]any)
	if !reflect.DeepEqual(inv, map[string]any{"items": []any{map[string]any{"id": float64(2)}}}) {
		t.Fatalf("inventory = %v, want the live block", inv)
	}
	if _, ok := got.data["quests"].([]any); !ok || len(got.data["quests"].([]any)) != 0 {
		t.Fatalf("an older live block replaced the saved quests: %v", got.data["quests"])
	}
	// The SavedVariables map is not modified.
	if !reflect.DeepEqual(base["inventory"], oldInv) {
		t.Fatal("overlay modified the SavedVariables map")
	}
}

func TestOverlayCompletedQuestsDeltaAndCombats(t *testing.T) {
	g, live := liveService(t)
	saved := time.Unix(2000, 0)
	ch := charEntry{key: "A-R", updatedAt: saved, data: map[string]any{
		"completedQuests": []any{float64(1), float64(2)},
		"combats":         []any{map[string]any{"id": float64(5)}},
	}}
	later := saved.Add(time.Minute)
	if err := live.Data.Put(bridge.MsgCompletedQuests, []byte(`{"add":[3,2]}`), later); err != nil {
		t.Fatal(err)
	}
	if err := live.Data.Put(bridge.MsgCombat, []byte(`{"combat":{"id":6,"zone":"Bosque"}}`), later); err != nil {
		t.Fatal(err)
	}
	got := g.overlayLive(ch)
	if want := []any{float64(1), float64(2), float64(3)}; !reflect.DeepEqual(got.data["completedQuests"], want) {
		t.Fatalf("completedQuests = %v, want %v", got.data["completedQuests"], want)
	}
	combats, _ := got.data["combats"].([]any)
	if len(combats) != 2 || combats[0].(map[string]any)["id"] != float64(6) {
		t.Fatalf("combats = %v", combats)
	}
}

func TestOverlayWithoutLiveChannelIsUnchanged(t *testing.T) {
	g := NewService(nil, nil, nil)
	ch := charEntry{key: "A-R", data: map[string]any{"x": 1.0}, updatedAt: time.Unix(1, 0)}
	if got := g.overlayLive(ch); !reflect.DeepEqual(got, ch) {
		t.Fatalf("overlay changed the entry without a live channel: %+v", got)
	}
}

func TestChannelFreshnessWithoutReader(t *testing.T) {
	g := NewService(nil, nil, nil)
	text, ok, types := g.ChannelFreshness(time.Now())
	if ok || text == "" || types != nil {
		t.Fatalf("freshness without reader: %q ok %v types %v", text, ok, types)
	}
}
