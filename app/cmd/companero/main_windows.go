//go:build windows

package main

import (
	"log"
	"os"

	"golang.org/x/sys/windows"
)

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// attachParentProcess is ATTACH_PARENT_PROCESS: use the console that started us.
const attachParentProcess = ^uintptr(0)

// acquireSingleInstance takes the named mutex. first is false when another copy runs.
// The returned function releases the mutex.
func acquireSingleInstance() (release func(), first bool) {
	name, err := windows.UTF16PtrFromString(`Local\CompaneroWoW`)
	if err != nil {
		return func() {}, true
	}
	h, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		_ = windows.CloseHandle(h)
		return nil, false
	}
	if err != nil {
		log.Printf("single instance mutex: %v", err)
		return func() {}, true
	}
	return func() {
		_ = windows.ReleaseMutex(h)
		_ = windows.CloseHandle(h)
	}, true
}

// attachConsole connects stdout to the parent console, if there is one. A GUI-subsystem
// executable has no standard handles, so the console is opened explicitly.
func attachConsole() bool {
	r, _, _ := procAttachConsole.Call(attachParentProcess)
	if r == 0 {
		return false
	}
	f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	os.Stdout = f
	return true
}
