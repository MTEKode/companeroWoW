package gamedata

import (
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// Share on key: the addon is idle until the player asks for its data, with the panel key
// (or the WoW binding "Compañero: compartir datos"). It then streams for a share window and
// the status payload's shareRemainingSec counts it down. The reader is switched on by the
// panel (see ondemand); these helpers only read the share state for the status bar and the wait.

// keyTypes are the message types a share sends first: character, sheet, quests, equipment.
var keyTypes = []uint8{bridge.MsgCharacter, bridge.MsgSheet, bridge.MsgQuests, bridge.MsgEquipment}

// ShareRemaining is the countdown of the addon's share window in seconds, from the status
// payload; 0 when the addon is at rest or the strip is not read.
func (g *Service) ShareRemaining() int {
	st, ok := g.Live()
	if !ok {
		return 0
	}
	return int(st.ShareRemainingSec)
}

// KeyAge is the age at now of the oldest key data type. ok is false while one of them has
// not arrived.
func (g *Service) KeyAge(now time.Time) (age time.Duration, ok bool) {
	ch := g.LiveChannel()
	if ch == nil {
		return 0, false
	}
	for _, t := range keyTypes {
		it, found := ch.Data.Get(t)
		if !found {
			return 0, false
		}
		if a := now.Sub(it.At); a > age {
			age = a
		}
	}
	return age, true
}

// freeBagSlots is the free bag slots of the live character message (freeBagSlots); 0 when
// the addon does not send it.
func (g *Service) freeBagSlots() float64 {
	ch := g.LiveChannel()
	if ch == nil {
		return 0
	}
	it, ok := ch.Data.Get(bridge.MsgCharacter)
	if !ok {
		return 0
	}
	m, _ := it.Value.(map[string]any)
	n, _ := m["freeBagSlots"].(float64)
	return n
}
