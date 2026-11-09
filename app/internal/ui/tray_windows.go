//go:build windows

package ui

import (
	"log"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// trayIcon is the notification-area icon. Its tooltip is updated from the status goroutine,
// so the fields are guarded.
type trayIcon struct {
	mu    sync.Mutex
	added bool
	icon  uintptr
}

// stockIcon returns the application's standard icon, which the executable does not replace.
func stockIcon() uintptr {
	return call(procLoadIconW, 0, idiApplication)
}

// trayData builds the NOTIFYICONDATA of the panel's icon with the given tooltip.
func (w *window) trayData(tip string) notifyIconData {
	nid := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             w.hwnd,
		UID:              trayUID,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: msgTray,
		HIcon:            w.tray.iconHandle(),
	}
	t := utf16Of(tip)
	if len(t) > len(nid.SzTip)-1 {
		t = append(t[:len(nid.SzTip)-1], 0)
	}
	copy(nid.SzTip[:], t)
	return nid
}

// iconHandle returns the icon, loading it once.
func (t *trayIcon) iconHandle() uintptr {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.icon == 0 {
		t.icon = stockIcon()
	}
	return t.icon
}

// addTray adds the icon to the notification area. It is also called when Explorer restarts.
func (w *window) addTray() {
	if w.hwnd == 0 {
		return
	}
	nid := w.trayData(trayTip(Status{}))
	w.tray.mu.Lock()
	defer w.tray.mu.Unlock()
	if call(procShellNotifyIconW, nimAdd, ptrOf(&nid)) == 0 {
		// The icon may already exist (Explorer restarted); modify it instead.
		if call(procShellNotifyIconW, nimModify, ptrOf(&nid)) == 0 {
			log.Printf("ui: no pude añadir el icono de la bandeja: %v", windows.GetLastError())
			return
		}
	}
	w.tray.added = true
	log.Printf("ui: icono de la bandeja listo")
	runtime.KeepAlive(nid)
}

// updateTray changes the tooltip. It is safe from any goroutine.
func (w *window) updateTray(tip string) {
	// trayData takes the icon's own lock, so it is built before the lock below.
	nid := w.trayData(tip)
	w.tray.mu.Lock()
	defer w.tray.mu.Unlock()
	if !w.tray.added {
		return
	}
	call(procShellNotifyIconW, nimModify, ptrOf(&nid))
}

// removeTray takes the icon away; the app is quitting.
func (w *window) removeTray() {
	w.tray.mu.Lock()
	defer w.tray.mu.Unlock()
	if !w.tray.added {
		return
	}
	nid := notifyIconData{CbSize: uint32(unsafe.Sizeof(notifyIconData{})), HWnd: w.hwnd, UID: trayUID}
	call(procShellNotifyIconW, nimDelete, ptrOf(&nid))
	w.tray.added = false
}

// onTrayMessage handles the mouse events of the icon: a left click shows or hides the
// panel, a right click opens the tray menu.
func (w *window) onTrayMessage(mouse uint32) {
	switch mouse {
	case wmLButtonUpTray:
		w.toggle()
	case wmRButtonUpTray:
		w.trayMenu()
	}
}

// trayMenu is the menu of the tray icon.
func (w *window) trayMenu() {
	w.menuCommand(w.popup([]menuItem{
		{trayToggle, "Mostrar/ocultar"},
		{menuSettings, "Ajustes…"},
		{menuSession, "Resumen de sesión"},
		{0, ""},
		{menuQuit, "Salir"},
	}))
}
