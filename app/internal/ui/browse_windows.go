//go:build windows

package ui

import (
	"runtime"

	"golang.org/x/sys/windows"
)

// browseFolder shows the system's folder picker, owned by owner, and returns the chosen
// folder. ok is false when the user cancels. The picker is modal: the panel waits for it.
func browseFolder(owner uintptr, title string) (dir string, ok bool) {
	display := make([]uint16, maxPathUnits)
	t := utf16Of(title)
	bi := browseInfo{
		HWndOwner:      owner,
		PszDisplayName: &display[0],
		LpszTitle:      &t[0],
		UlFlags:        bifReturnOnlyFSDirs | bifNewDialogStyle | bifNoNewFolderButton,
	}
	pidl := call(procSHBrowseForFolderW, ptrOf(&bi))
	runtime.KeepAlive(display)
	runtime.KeepAlive(t)
	if pidl == 0 {
		return "", false
	}
	buf := make([]uint16, maxPathUnits)
	got := call(procSHGetPathFromIDListW, pidl, ptrOf(&buf[0]))
	call(procCoTaskMemFree, pidl)
	if got == 0 {
		return "", false
	}
	return windows.UTF16ToString(buf), true
}
