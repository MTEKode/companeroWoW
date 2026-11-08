//go:build !windows

package ui

import "testing"

// Outside Windows the clipboard helper is a stub that reports the platform limit and
// never pretends to copy.
func TestCopyTextStubReportsUnavailable(t *testing.T) {
	if err := CopyText("/compa marca 1 2 3"); err == nil {
		t.Fatal("the stub must return an error")
	}
}
