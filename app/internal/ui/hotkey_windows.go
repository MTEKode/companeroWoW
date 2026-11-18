//go:build windows

package ui

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procRegisterHotKey     = windows.NewLazySystemDLL("user32.dll").NewProc("RegisterHotKey")
	procUnregisterHotKey   = windows.NewLazySystemDLL("user32.dll").NewProc("UnregisterHotKey")
	procGetMessageW        = windows.NewLazySystemDLL("user32.dll").NewProc("GetMessageW")
	procPeekMessageW       = windows.NewLazySystemDLL("user32.dll").NewProc("PeekMessageW")
	procPostThreadMessageW = windows.NewLazySystemDLL("user32.dll").NewProc("PostThreadMessageW")
	procGetCurrentThreadId = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCurrentThreadId")
)

const (
	wmHotkey     = 0x0312
	modNoRepeat  = 0x4000
	hotkeyIDBase = 0xC0A1
	// wmHotkeyChange asks the hotkey thread to replace its registration (see Change).
	wmHotkeyChange = wmApp + 10
	pmNoRemove     = 0x0000
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

// hotkeyLoop owns the global shortcut. RegisterHotKey ties a hotkey to the thread that
// registered it, so one thread runs the message loop for the whole life of the app, and
// Change asks that thread to swap the registration.
type hotkeyLoop struct {
	ready   chan struct{} // closed once the thread has its message queue
	tid     uint32
	mu      sync.Mutex
	pending *hotkeyChange
}

type hotkeyChange struct {
	hk   Hotkey
	errc chan error
}

// startHotkey registers hk on a dedicated thread and calls onPress for every press. It
// returns the loop and the registration error (for example when another application already
// owns the shortcut). The loop runs even then, so Change can still succeed later.
func startHotkey(hk Hotkey, onPress func()) (*hotkeyLoop, error) {
	h := &hotkeyLoop{ready: make(chan struct{})}
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		var m winMsg
		// Creates the thread's message queue, so that PostThreadMessage can reach it.
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, wmUser, wmUser, pmNoRemove)
		h.tid = uint32(callNoArgs(procGetCurrentThreadId))
		current := hk
		err := register(current)
		errc <- err
		close(h.ready)
		if err == nil {
			defer func() { procUnregisterHotKey.Call(0, hotkeyIDBase) }()
		}

		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(ret) <= 0 {
				return
			}
			switch {
			case m.Message == wmHotkeyChange && m.HWnd == 0:
				h.mu.Lock()
				req := h.pending
				h.pending = nil
				h.mu.Unlock()
				if req != nil {
					req.errc <- replaceHotkey(&current, req.hk)
				}
			case m.Message == wmHotkey && m.WParam == hotkeyIDBase:
				onPress()
			}
		}
	}()
	return h, <-errc
}

// callNoArgs calls a procedure with no arguments and returns its result.
func callNoArgs(p *windows.LazyProc) uintptr {
	r, _, _ := p.Call()
	return r
}

// register installs hk on the current thread.
func register(hk Hotkey) error {
	r, _, callErr := procRegisterHotKey.Call(0, hotkeyIDBase, uintptr(hk.Mods|modNoRepeat), uintptr(hk.VK))
	if r == 0 {
		return fmt.Errorf("RegisterHotKey: %v", callErr)
	}
	return nil
}

// replaceHotkey swaps the registration to next. When next is refused, the previous one is
// registered again and the error says so.
func replaceHotkey(current *Hotkey, next Hotkey) error {
	procUnregisterHotKey.Call(0, hotkeyIDBase)
	if err := register(next); err != nil {
		if rerr := register(*current); rerr != nil {
			return errors.Join(err, fmt.Errorf("no pude volver a registrar %s: %w", current.String(), rerr))
		}
		return err
	}
	*current = next
	return nil
}

// Close unregisters the shortcut and ends the thread's message loop.
func (h *hotkeyLoop) Close() {
	if h == nil {
		return
	}
	<-h.ready
	procPostThreadMessageW.Call(uintptr(h.tid), wmQuit, 0, 0)
}

// Change replaces the shortcut with hk and reports whether the system accepted it. On
// failure the previous shortcut stays registered. It must not be called from the thread
// that delivers the presses (the UI thread waits for it, so that is safe: the loop runs on
// its own thread).
func (h *hotkeyLoop) Change(hk Hotkey) error {
	if h == nil {
		return errors.New("el atajo no está activo")
	}
	<-h.ready
	req := &hotkeyChange{hk: hk, errc: make(chan error, 1)}
	h.mu.Lock()
	h.pending = req
	h.mu.Unlock()
	if r, _, err := procPostThreadMessageW.Call(uintptr(h.tid), wmHotkeyChange, 0, 0); r == 0 {
		return fmt.Errorf("PostThreadMessage: %v", err)
	}
	return <-req.errc
}
