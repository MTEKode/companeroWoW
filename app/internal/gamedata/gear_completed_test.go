package gamedata

import "testing"

// completedQuests comes as the addon's text (state, export) or as the list of ids (live channel, store).
func TestCompletedQuestsReadsTextAndList(t *testing.T) {
	text := gearData{Completed: map[int64]bool{}}
	text.readCompleted(map[string]any{"completedQuests": "1001,1002, 1003"})
	if !text.CompletedKnown || !text.Completed[1002] || !text.Completed[1003] || text.Completed[1004] {
		t.Errorf("text form = known %v, completed %v", text.CompletedKnown, text.Completed)
	}
	list := gearData{Completed: map[int64]bool{}}
	list.readCompleted(map[string]any{"completedQuests": []any{float64(2001), float64(2002)}})
	if !list.CompletedKnown || !list.Completed[2001] || !list.Completed[2002] {
		t.Errorf("list form = known %v, completed %v", list.CompletedKnown, list.Completed)
	}
	none := gearData{Completed: map[int64]bool{}}
	none.readCompleted(map[string]any{})
	if none.CompletedKnown || len(none.Notes) != 1 || none.reasons["completed"] != "" {
		t.Errorf("missing = known %v, notes %v", none.CompletedKnown, none.Notes)
	}
}
