package gamedata

import (
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// Live overlay: the messages of the v3 data channel (bridge.Channel) laid over the
// SavedVariables data of the active character, where they are newer than it. The
// SavedVariables map is never modified: the overlay works on a shallow copy.
//
// Block names follow the addon's Channel.lua: each message type carries the same
// section names the SavedVariables character uses, so most of them are copied as they
// are. completedQuests (6) and combat (7) are lists and are merged; knowledge (9) is not
// overlaid, because its store is filled from SavedVariables only (see store.ImportKnowledge).

// LiveChannel returns the data channel of the pixel reader, or nil when there is none.
func (g *Service) LiveChannel() *bridge.Channel {
	if g.reader == nil {
		return nil
	}
	return g.reader.Channel()
}

// overlayLive returns ch with the live blocks newer than its SavedVariables data laid over.
// Without a live channel, or with nothing newer, ch is returned unchanged.
func (g *Service) overlayLive(ch charEntry) charEntry {
	live := g.LiveChannel()
	if live == nil {
		return ch
	}
	items := live.Data.All()
	if len(items) == 0 {
		return ch
	}
	merged := make(map[string]any, len(ch.data)+8)
	for k, v := range ch.data {
		merged[k] = v
	}
	changed := false
	for t, it := range items {
		if t == bridge.MsgKnowledge || t == bridge.MsgMeta || t == bridge.MsgSession {
			continue
		}
		if !ch.updatedAt.IsZero() && !it.At.After(ch.updatedAt) {
			continue
		}
		if applyLiveBlock(merged, t, it.Value) {
			changed = true
		}
	}
	if !changed {
		return ch
	}
	ch.data = merged
	return ch
}

// applyLiveBlock writes one message into merged. It reports whether anything changed.
func applyLiveBlock(merged map[string]any, t uint8, v any) bool {
	body, ok := v.(map[string]any)
	switch t {
	case bridge.MsgCompletedQuests:
		return applyCompletedQuests(merged, v)
	case bridge.MsgSheet:
		// The character sheet replaces the saved one as a whole (it is newer than the saved one when this runs).
		if !ok || len(body) == 0 {
			return false
		}
		merged["sheet"] = body
		return true
	case bridge.MsgCombat:
		if !ok {
			return false
		}
		rec, _ := body["combat"].(map[string]any)
		if rec == nil {
			return false
		}
		merged["combats"] = prependCombat(merged["combats"], rec)
		return true
	case bridge.MsgCharacter, bridge.MsgQuests, bridge.MsgBags, bridge.MsgEquipment,
		bridge.MsgProfessions, bridge.MsgActionBars, bridge.MsgHeavy:
		if !ok {
			return false
		}
		for k, val := range body {
			merged[k] = val
		}
		return len(body) > 0
	}
	return false
}

// applyCompletedQuests takes either the full list of ids or {"add": [...]} (ids completed
// since the last full list). The list in merged is replaced, never edited in place.
func applyCompletedQuests(merged map[string]any, v any) bool {
	switch x := v.(type) {
	case []any:
		merged["completedQuests"] = x
		return true
	case map[string]any:
		add, _ := x["add"].([]any)
		if len(add) == 0 {
			return false
		}
		old, _ := merged["completedQuests"].([]any)
		seen := make(map[any]bool, len(old)+len(add))
		out := make([]any, 0, len(old)+len(add))
		for _, id := range append(append([]any{}, old...), add...) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		merged["completedQuests"] = out
		return true
	}
	return false
}

// prependCombat puts rec at the front of the combat list, replacing a record with the same
// id, and returns a new list.
func prependCombat(list any, rec map[string]any) []any {
	old, _ := list.([]any)
	out := make([]any, 0, len(old)+1)
	out = append(out, rec)
	id := rec["id"]
	for _, r := range old {
		if m, ok := r.(map[string]any); ok && id != nil && m["id"] == id {
			continue
		}
		out = append(out, r)
	}
	const keep = 10 // the addon keeps the last 10 encounters
	if len(out) > keep {
		out = out[:keep]
	}
	return out
}

// ChannelFreshness reports the live channel's state at now, for the status bar and --diag.
func (g *Service) ChannelFreshness(now time.Time) (string, bool, []bridge.TypeFreshness) {
	live := g.LiveChannel()
	if live == nil {
		return "Canal en vivo: no disponible", false, nil
	}
	text, ok := live.Status(now)
	return text, ok, live.Freshness(now)
}
