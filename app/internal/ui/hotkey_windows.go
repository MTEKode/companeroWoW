//go:build windows

package ui

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procRegisterHotKey = windows.NewLazySystemDLL("user32.dll").NewProc("RegisterHotKey")
	procGetMessageW    = windows.NewLazySystemDLL("user32.dll").NewProc("GetMessageW")
)

const (
	wmHotkey     = 0x0312
	modNoRepeat  = 0x4000
	hotkeyIDBase = 0xC0A1
)

// winMsg mirrors the Win32 MSG structure (64-bit layout).
type winMsg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	PtX     int32
	PtY     int32
}

// startHotkey registers hk as a global hotkey on a dedicated OS thread that runs its own
// message loop, and calls onPress for every press. It returns the registration error
// (for example when another application already owns the shortcut); the loop is then
// not started.
func startHotkey(hk Hotkey, onPress func()) error {
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		r, _, callErr := procRegisterHotKey.Call(0, hotkeyIDBase, uintptr(hk.Mods|modNoRepeat), uintptr(hk.VK))
		if r == 0 {
			errc <- fmt.Errorf("RegisterHotKey: %v", callErr)
			return
		}
		errc <- nil

		var m winMsg
		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(ret) <= 0 {
				return
			}
			if m.Message == wmHotkey && m.WParam == hotkeyIDBase {
				onPress()
			}
		}
	}()
	return <-errc
}
