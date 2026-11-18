//go:build windows

package ui

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procSetWindowsHookExW   = windows.NewLazySystemDLL("user32.dll").NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = windows.NewLazySystemDLL("user32.dll").NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = windows.NewLazySystemDLL("user32.dll").NewProc("CallNextHookEx")
)

const (
	whKeyboardLL    = 13
	wmKeyDownKey    = 0x0100 // WM_KEYDOWN (wmKeyDown in win32_windows.go is the same value)
	wmSysKeyDownKey = 0x0104
	wmKeyUp         = 0x0101
	wmSysKeyUp      = 0x0105
	wmQuit          = 0x0012
	msgHookFired    = wmApp + 11 // posted to the hook thread by the callback; the thread then calls onPress
	hookStopWaitMs  = 1000
)

// kbdllHookStruct mirrors KBDLLHOOKSTRUCT: only the virtual-key code is used.
type kbdllHookStruct struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// keyHook is the panel key as a low-level keyboard hook (WH_KEYBOARD_LL). The hook only
// observes: every key is passed on with CallNextHookEx, and nothing is changed or injected.
// Its thread runs the message loop that the hook needs; the callback does the matching and
// posts a message, and onPress runs on the thread after that.
type keyHook struct {
	ready   chan struct{} // closed once the hook is installed (or failed)
	done    chan struct{} // closed when the thread has unhooked and ended
	tid     uint32
	onPress func()

	mu    sync.Mutex
	combo Hotkey

	// hook-thread state, touched only from the callback
	matcher comboMatcher
	deb     debouncer
	hook    uintptr
	err     error
}

// currentHook is the hook the callback reports to; one hook runs at a time.
var currentHook atomic.Pointer[keyHook]

var (
	hookProcOnce sync.Once
	hookProc     uintptr
)

// startKeyHook installs the hook for hk on a dedicated thread and calls onPress for each
// accepted press. The error is the installation failure, if any; the returned listener is
// nil then.
func startKeyHook(hk Hotkey, onPress func()) (*keyHook, error) {
	hookProcOnce.Do(func() { hookProc = windows.NewCallback(keyHookProc) })
	h := &keyHook{
		ready:   make(chan struct{}),
		done:    make(chan struct{}),
		combo:   hk,
		onPress: onPress,
		deb:     debouncer{window: debounceWindow},
	}
	go h.run()
	<-h.ready
	if h.err != nil {
		return nil, h.err
	}
	return h, nil
}

func (h *keyHook) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(h.done)

	var m winMsg
	// Creates the thread's message queue, so that PostThreadMessage can reach it.
	procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, wmUser, wmUser, pmNoRemove)
	h.tid = uint32(callNoArgs(procGetCurrentThreadId))

	mod, _, _ := procGetModuleHandleW.Call(0)
	hook, _, callErr := procSetWindowsHookExW.Call(whKeyboardLL, hookProc, mod, 0)
	if hook == 0 {
		h.err = fmt.Errorf("SetWindowsHookExW: %v", callErr)
		close(h.ready)
		return
	}
	h.hook = hook
	currentHook.Store(h)
	close(h.ready)

	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 { // WM_QUIT (Close) or an error
			break
		}
		if m.Message == msgHookFired && m.HWnd == 0 {
			h.onPress()
		}
	}
	currentHook.CompareAndSwap(h, nil)
	procUnhookWindowsHookEx.Call(h.hook)
}

// keyHookProc is the WH_KEYBOARD_LL callback. It must return quickly: it only matches the
// key and posts a message. Every call passes the key on.
func keyHookProc(nCode int32, wParam, lParam uintptr) uintptr {
	if nCode >= 0 {
		if h := currentHook.Load(); h != nil {
			h.observe(wParam, lParam)
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return r
}

// observe runs in the callback: it reads the key event and posts a message on a match.
func (h *keyHook) observe(wParam, lParam uintptr) {
	var down bool
	switch wParam {
	case wmKeyDownKey, wmSysKeyDownKey:
		down = true
	case wmKeyUp, wmSysKeyUp:
	default:
		return
	}
	kb := (*kbdllHookStruct)(ptrFromUintptr(lParam))
	h.mu.Lock()
	combo := h.combo
	h.mu.Unlock()
	if !h.matcher.feed(keyEvent{VK: kb.VkCode, Down: down}, combo) {
		return
	}
	if !h.deb.allow(time.Now()) {
		return
	}
	procPostThreadMessageW.Call(uintptr(h.tid), msgHookFired, 0, 0)
}

// ptrFromUintptr turns the lParam of a hook callback back into a pointer. The memory is
// the system's KBDLLHOOKSTRUCT for the duration of the call, not Go heap memory.
func ptrFromUintptr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

// Change makes hk the panel key. The hook cannot refuse a combo, so it never fails while the
// hook is running.
func (h *keyHook) Change(hk Hotkey) error {
	if h == nil {
		return errors.New("el atajo no está activo")
	}
	h.mu.Lock()
	h.combo = hk
	h.mu.Unlock()
	return nil
}

// Close unhooks the keyboard and ends the hook thread. It waits briefly for the thread.
func (h *keyHook) Close() {
	if h == nil {
		return
	}
	<-h.ready
	procPostThreadMessageW.Call(uintptr(h.tid), wmQuit, 0, 0)
	select {
	case <-h.done:
	case <-time.After(hookStopWaitMs * time.Millisecond):
	}
}
