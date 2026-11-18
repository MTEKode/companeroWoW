package gamedata

import (
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// shareService returns a Service whose reader has a live channel, with nothing received yet,
// and that channel.
func shareService(t *testing.T) (*Service, *bridge.Channel) {
	t.Helper()
	r := bridge.NewReader(250 * time.Millisecond)
	return NewService(r, nil, nil), r.Channel()
}

func putType(t *testing.T, ch *bridge.Channel, typ uint8, body string, at time.Time) {
	t.Helper()
	if err := ch.Data.Put(typ, []byte(body), at); err != nil {
		t.Fatalf("put type %d: %v", typ, err)
	}
}

func TestFreeBagSlotsComeFromTheCharacterMessage(t *testing.T) {
	g, ch := shareService(t)
	if got := g.freeBagSlots(); got != 0 {
		t.Fatalf("freeBagSlots with no character message = %v, want 0", got)
	}
	putType(t, ch, bridge.MsgCharacter, `{"level": 12, "freeBagSlots": 14}`, time.Now())
	if got := g.freeBagSlots(); got != 14 {
		t.Fatalf("freeBagSlots = %v, want 14", got)
	}
}

// An addon that does not send freeBagSlots yet falls back to 0.
func TestFreeBagSlotsFallBackToZero(t *testing.T) {
	g, ch := shareService(t)
	putType(t, ch, bridge.MsgCharacter, `{"level": 12}`, time.Now())
	if got := g.freeBagSlots(); got != 0 {
		t.Fatalf("freeBagSlots without the field = %v, want 0", got)
	}
}

func TestKeyAgeIsTheOldestKeyType(t *testing.T) {
	g, ch := shareService(t)
	now := time.Now()
	if _, ok := g.KeyAge(now); ok {
		t.Fatal("KeyAge known with no key types")
	}
	putType(t, ch, bridge.MsgCharacter, `{}`, now.Add(-10*time.Second))
	putType(t, ch, bridge.MsgSheet, `{}`, now.Add(-30*time.Second))
	putType(t, ch, bridge.MsgQuests, `{}`, now.Add(-5*time.Second))
	if _, ok := g.KeyAge(now); ok {
		t.Fatal("KeyAge known while equipment is missing")
	}
	putType(t, ch, bridge.MsgEquipment, `{}`, now.Add(-20*time.Second))
	age, ok := g.KeyAge(now)
	if !ok || age < 29*time.Second || age > 31*time.Second {
		t.Fatalf("KeyAge = %v, %v; want about 30s", age, ok)
	}
}

func TestShareRemainingIsZeroWithoutStrip(t *testing.T) {
	g, _ := shareService(t)
	if got := g.ShareRemaining(); got != 0 {
		t.Fatalf("ShareRemaining = %d, want 0", got)
	}
}
