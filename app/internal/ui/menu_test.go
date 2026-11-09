package ui

import "testing"

func TestRegisterMenuActionOrderAndIDs(t *testing.T) {
	resetMenuActions()
	t.Cleanup(resetMenuActions)

	var ran []string
	a := RegisterMenuAction("Primera", func() { ran = append(ran, "a") })
	b := RegisterMenuAction("Segunda", func() { ran = append(ran, "b") })
	if a != firstActionID || b != firstActionID+1 {
		t.Fatalf("ids = %d, %d; want %d, %d", a, b, firstActionID, firstActionID+1)
	}
	list := menuActions()
	if len(list) != 2 || list[0].Label != "Primera" || list[1].Label != "Segunda" {
		t.Fatalf("menu actions = %+v", list)
	}
	if !runMenuAction(b) || !runMenuAction(a) {
		t.Fatal("registered actions were not found")
	}
	if len(ran) != 2 || ran[0] != "b" || ran[1] != "a" {
		t.Errorf("ran %v, want [b a]", ran)
	}
}

func TestRegisterMenuActionRejectsEmpty(t *testing.T) {
	resetMenuActions()
	t.Cleanup(resetMenuActions)
	if id := RegisterMenuAction("", func() {}); id != 0 {
		t.Errorf("empty label accepted with id %d", id)
	}
	if id := RegisterMenuAction("Sin función", nil); id != 0 {
		t.Errorf("nil function accepted with id %d", id)
	}
	if n := len(menuActions()); n != 0 {
		t.Errorf("%d actions registered, want none", n)
	}
}

func TestRunMenuActionUnknownID(t *testing.T) {
	resetMenuActions()
	t.Cleanup(resetMenuActions)
	if runMenuAction(firstActionID) {
		t.Error("unknown id reported as run")
	}
}

// The snapshot is a copy: changing it does not change the registry.
func TestMenuActionsSnapshotIsCopy(t *testing.T) {
	resetMenuActions()
	t.Cleanup(resetMenuActions)
	RegisterMenuAction("Uno", func() {})
	list := menuActions()
	list[0].Label = "cambiado"
	if menuActions()[0].Label != "Uno" {
		t.Error("snapshot shares storage with the registry")
	}
}
