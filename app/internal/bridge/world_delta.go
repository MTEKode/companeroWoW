package bridge

// The world block (msgType 16) is sent as changed parts only, so the store keeps one document and
// lays every delta over it. MergeDelta does that merge; LiveData.Put calls it for MsgWorld.

// MergeDelta returns base with over laid on top: nested objects are merged key by key, and every
// other value (lists, numbers, text, booleans) is replaced by the one in over. Neither input is
// modified: the result shares unchanged subtrees with base, which is safe because nothing edits them.
func MergeDelta(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		ov, isMap := v.(map[string]any)
		bv, baseMap := out[k].(map[string]any)
		if isMap && baseMap {
			out[k] = MergeDelta(bv, ov)
			continue
		}
		out[k] = v
	}
	return out
}

// mergeWorldValue is the document after delta: the delta merged over the previous document when both
// are objects, else the delta itself (the first message, or a message that is not an object).
func mergeWorldValue(prev, delta any) any {
	pm, okPrev := prev.(map[string]any)
	dm, okDelta := delta.(map[string]any)
	if !okPrev || !okDelta {
		return delta
	}
	return MergeDelta(pm, dm)
}
