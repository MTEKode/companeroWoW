package ui

import "sync"

// MenuAction is an item added to the panel's header menu by RegisterMenuAction.
type MenuAction struct {
	ID    int
	Label string
	Run   func()
}

// firstActionID is the first command id of a registered action. The built-in items use
// lower ids, so the two never collide.
const firstActionID = 3000

var actions = struct {
	sync.Mutex
	list []MenuAction
}{}

// RegisterMenuAction adds an item to the end of the panel's header menu (before Salir)
// and returns its command id, or 0 when label is empty or fn is nil. Register before Run
// or at any time after; the menu reads the list each time it opens.
//
// fn runs on the UI thread when the user chooses the item: keep it quick, and start a
// goroutine for anything slow. It must not block on the panel itself.
func RegisterMenuAction(label string, fn func()) int {
	if label == "" || fn == nil {
		return 0
	}
	actions.Lock()
	defer actions.Unlock()
	id := firstActionID + len(actions.list)
	actions.list = append(actions.list, MenuAction{ID: id, Label: label, Run: fn})
	return id
}

// menuActions returns the registered actions in registration order.
func menuActions() []MenuAction {
	actions.Lock()
	defer actions.Unlock()
	out := make([]MenuAction, len(actions.list))
	copy(out, actions.list)
	return out
}

// runMenuAction runs the action with the given id. It reports whether one was found.
func runMenuAction(id int) bool {
	for _, a := range menuActions() {
		if a.ID == id {
			a.Run()
			return true
		}
	}
	return false
}

// resetMenuActions removes every registered action; tests use it.
func resetMenuActions() {
	actions.Lock()
	defer actions.Unlock()
	actions.list = nil
}
