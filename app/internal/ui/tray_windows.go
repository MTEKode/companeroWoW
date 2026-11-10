//go:build windows

package ui

import (
	"log"
	"runtime"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// smallIconSystemMetric is SM_CXSMICON, the width of a small icon at the current DPI.
const smallIconSystemMetric = 49

// trayIcon is the notification-area icon. Its tooltip, its badge and its icon are updated
// from the status goroutine as well, so the fields are guarded.
type trayIcon struct {
	mu        sync.Mutex
	added     bool
	icon      uintptr
	badgeIcon uintptr // the icon with the unread dot, made on first use
	badge     int     // unread tips; the badge icon shows while it is above zero
}

// setBadge sets the number of unread tips the icon shows. Safe from any goroutine.
func (t *trayIcon) setBadge(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.badge = n
}

// badgeCount is the number of unread tips. Safe from any goroutine.
func (t *trayIcon) badgeCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.badge
}

// badgeHandle returns the icon with the dot, creating it once. The caller holds t.mu. It is
// 0 when Windows refuses the icon; the plain icon is shown then.
func (t *trayIcon) badgeHandle() uintptr {
	if t.badgeIcon != 0 {
		return t.badgeIcon
	}
	size := int(int32(call(procGetSystemMetrics, smallIconSystemMetric)))
	if size < 16 {
		size = 16
	}
	if size > 32 {
		size = 32
	}
	img := badgeIconImage(size)
	t.badgeIcon = call(procCreateIconFromResource, ptrOf(&img[0]), uintptr(len(img)), 1, 0x00030000,
		uintptr(size), uintptr(size), 0)
	runtime.KeepAlive(img)
	if t.badgeIcon == 0 {
		log.Printf("ui: no pude crear el icono con el aviso: %v", windows.GetLastError())
	}
	return t.badgeIcon
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

// iconHandle returns the icon: the one with the unread dot while tips are unread, else the
// stock icon (loaded once).
func (t *trayIcon) iconHandle() uintptr {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.badge > 0 {
		if h := t.badgeHandle(); h != 0 {
			return h
		}
	}
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
	nid := w.trayData(trayTipWithBadge(Status{}, w.tray.badgeCount()))
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
// panel, a right click opens the tray menu, and a click on a balloon opens the panel.
func (w *window) onTrayMessage(mouse uint32) {
	switch mouse {
	case wmLButtonUpTray:
		w.toggle()
	case wmRButtonUpTray:
		w.trayMenu()
	case ninBalloonUserClick:
		w.show(true)
	}
}

// balloon shows a tray balloon with the title and the text. The sound plays the Windows
// notification sound; without it the balloon is silent. The system skips it during its quiet
// time. It reports whether Windows took the balloon.
func (w *window) balloon(title, text string, sound bool) bool {
	if w.hwnd == 0 {
		return false
	}
	nid := notifyIconData{
		CbSize: uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:   w.hwnd,
		UID:    trayUID,
		UFlags: nifInfo,
	}
	fillUTF16(nid.SzInfoTitle[:], title)
	fillUTF16(nid.SzInfo[:], text)
	flags := uint32(niifInfo | niifQuietTime)
	if !sound {
		flags |= niifNoSound
	}
	nid.DwInfoFlags = flags
	w.tray.mu.Lock()
	defer w.tray.mu.Unlock()
	if !w.tray.added {
		return false
	}
	ok := call(procShellNotifyIconW, nimModify, ptrOf(&nid)) != 0
	runtime.KeepAlive(nid)
	if ok {
		log.Printf("ui: aviso de la bandeja mostrado: %q (sonido %v)", title, sound)
	} else {
		log.Printf("ui: Windows no mostró el aviso de la bandeja: %v", windows.GetLastError())
	}
	return ok
}

// fillUTF16 writes s into dst as UTF-16, cut to leave the terminating zero.
func fillUTF16(dst []uint16, s string) {
	if len(dst) == 0 {
		return
	}
	u := utf16.Encode([]rune(s))
	if len(u) > len(dst)-1 {
		u = u[:len(dst)-1]
	}
	copy(dst, u)
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
