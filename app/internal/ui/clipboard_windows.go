//go:build windows

package ui

import (
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procOpenClipboard    = modUser32.NewProc("OpenClipboard")
	procCloseClipboard   = modUser32.NewProc("CloseClipboard")
	procEmptyClipboard   = modUser32.NewProc("EmptyClipboard")
	procSetClipboardData = modUser32.NewProc("SetClipboardData")
	procGlobalAlloc      = modKernel32.NewProc("GlobalAlloc")
	procGlobalLock       = modKernel32.NewProc("GlobalLock")
	procGlobalUnlock     = modKernel32.NewProc("GlobalUnlock")
	procGlobalFree       = modKernel32.NewProc("GlobalFree")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	// clipboardTries and clipboardWait cover another application holding the clipboard.
	clipboardTries = 10
	clipboardWait  = 20 * time.Millisecond
)

// CopyText puts text on the clipboard as Unicode text (CF_UNICODETEXT). It only copies;
// nothing is typed or sent to the game.
func CopyText(text string) error {
	data, err := windows.UTF16FromString(text)
	if err != nil {
		return errors.New("el texto no se puede copiar")
	}
	h := call(procGlobalAlloc, gmemMoveable, uintptr(len(data)*2))
	if h == 0 {
		return errors.New("no hay memoria para copiar")
	}
	p := call(procGlobalLock, h)
	if p == 0 {
		call(procGlobalFree, h)
		return errors.New("no pude preparar el portapapeles")
	}
	copy(unsafe.Slice((*uint16)(fromUintptr(p)), len(data)), data)
	call(procGlobalUnlock, h)

	opened := false
	for i := 0; i < clipboardTries && !opened; i++ {
		opened = call(procOpenClipboard, 0) != 0
		if !opened {
			time.Sleep(clipboardWait)
		}
	}
	if !opened {
		call(procGlobalFree, h)
		return errors.New("el portapapeles está ocupado, inténtalo otra vez")
	}
	defer call(procCloseClipboard)
	call(procEmptyClipboard)
	if call(procSetClipboardData, cfUnicodeText, h) == 0 {
		call(procGlobalFree, h)
		return errors.New("el portapapeles no aceptó el texto")
	}
	// On success the system owns the memory now; it must not be freed here.
	return nil
}
