//go:build windows

package ui

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"unsafe"

	"github.com/MTEKode/companeroWoW/app/internal/config"
	"golang.org/x/sys/windows"
)

// settingsClass is the window class of the settings screen.
const settingsClass = "CompaneroWoWSettings"

// Control ids of the settings screen. IDOK and IDCANCEL are the dialog's own: Enter presses
// Guardar and Escape closes the screen.
const (
	idSetUp     = 2010
	idSetDown   = 2011
	idSetBrowse = 2012
	idSetKey    = 2013
	idSetWizard = 2014
)

// Styles that are not in the panel's list.
const (
	esNumber      = 0x00002000
	wsExAppWindow = 0x00040000
	lbGetTextLen  = 0x018A
)

var (
	settingsRegOnce sync.Once
	settingsRegErr  error
	settingsClsName []uint16
	settingsCB      uintptr
	// settingsCur is the open settings screen (UI thread only). The message loop asks it
	// about dialog keys.
	settingsCur *settingsWin
)

// settingsWin is the settings screen: a small window of standard controls over the
// panel's configuration. It belongs to the window that opened it.
type settingsWin struct {
	hwnd  uintptr
	inst  uintptr
	owner *window
	scale float64

	hotkey, opacity, font, providers uintptr
	up, down, hide, proactive        uintptr
	startup, wowPath, browseBtn      uintptr
	keyLabel, keyFolder, status      uintptr
	wizard, cancel, saveBtn          uintptr

	statusColor uint32
}

// sc scales a logical size of the settings screen.
func (s *settingsWin) sc(v int) int { return scaled(v, s.scale) }

// openSettings shows the settings screen, or brings the open one forward.
func (w *window) openSettings() {
	if w.settings != nil {
		call(procShowWindow, w.settings.hwnd, swShow)
		call(procSetForegroundWindow, w.settings.hwnd)
		return
	}
	s, err := newSettingsWin(w)
	if err != nil {
		log.Printf("ui: ajustes: %v", err)
		w.setNotice("No pude abrir los ajustes: " + err.Error())
		w.refreshStatus()
		return
	}
	w.settings = s
	s.load(w.cfg)
}

// registerSettingsClass registers the window class once; its background is the panel's.
func registerSettingsClass(inst, bg uintptr) error {
	settingsRegOnce.Do(func() {
		settingsClsName = utf16Of(settingsClass)
		settingsCB = windows.NewCallback(settingsProc)
		wc := wndClassEx{
			Size:       uint32(unsafe.Sizeof(wndClassEx{})),
			WndProc:    settingsCB,
			Instance:   inst,
			Cursor:     call(procLoadCursorW, 0, idcArrow),
			Background: bg,
			ClassName:  &settingsClsName[0],
		}
		if call(procRegisterClassExW, ptrOf(&wc)) == 0 {
			settingsRegErr = fmt.Errorf("no pude registrar la clase de ventana: %w", windows.GetLastError())
		}
	})
	return settingsRegErr
}

// newSettingsWin creates the screen centred on the primary monitor and fills it.
func newSettingsWin(owner *window) (*settingsWin, error) {
	if err := registerSettingsClass(owner.inst, owner.brushes.bg); err != nil {
		return nil, err
	}
	s := &settingsWin{inst: owner.inst, owner: owner, scale: owner.scale}
	style := uintptr(wsPopup | wsCaption | wsSysMenu | wsClipChildren)
	// The client area is 440 x 470 logical pixels; AdjustWindowRectEx adds the frame.
	rc := rect{Right: int32(s.sc(440)), Bottom: int32(s.sc(470))}
	call(procAdjustWindowRectEx, ptrOf(&rc), style, 0, wsExAppWindow)
	ow, oh := int(rc.Right-rc.Left), int(rc.Bottom-rc.Top)
	screen := screenRect()
	x := screen.X + (screen.W-ow)/2
	y := screen.Y + (screen.H-oh)/3
	title := utf16Of("Ajustes de Compañero WoW")
	hwnd := call(procCreateWindowExW, wsExAppWindow, ptrOf(&settingsClsName[0]), ptrOf(&title[0]),
		style, uintptr(x), uintptr(y), uintptr(ow), uintptr(oh), 0, 0, owner.inst, 0)
	runtime.KeepAlive(title)
	if hwnd == 0 {
		return nil, fmt.Errorf("no pude crear la ventana: %w", windows.GetLastError())
	}
	s.hwnd = hwnd
	s.createControls()
	settingsCur = s
	call(procShowWindow, hwnd, swShow)
	call(procSetForegroundWindow, hwnd)
	call(procSetFocus, s.hotkey)
	return s, nil
}

// createChild creates a control of the settings screen at logical position x, y.
func (s *settingsWin) createChild(class, text string, style uintptr, x, y, w, h int, id uintptr) uintptr {
	cls := utf16Of(class)
	txt := utf16Of(text)
	ctl := call(procCreateWindowExW, 0, ptrOf(&cls[0]), ptrOf(&txt[0]), style,
		uintptr(s.sc(x)), uintptr(s.sc(y)), uintptr(s.sc(w)), uintptr(s.sc(h)), s.hwnd, id, s.inst, 0)
	runtime.KeepAlive(cls)
	runtime.KeepAlive(txt)
	return ctl
}

func (s *settingsWin) label(text string, x, y, w, h int) uintptr {
	return s.createChild("STATIC", text, wsChild|wsVisible|ssLeft, x, y, w, h, 0)
}

func (s *settingsWin) edit(x, y, w, h int, extra uintptr) uintptr {
	return s.createChild("EDIT", "", wsChild|wsVisible|wsTabStop|wsBorder|esAutoHScroll|extra, x, y, w, h, 0)
}

func (s *settingsWin) button(text string, x, y, w, h int, extra uintptr, id uintptr) uintptr {
	return s.createChild("BUTTON", text, wsChild|wsVisible|wsTabStop|extra, x, y, w, h, id)
}

func (s *settingsWin) checkBox(text string, x, y, w, h int) uintptr {
	return s.createChild("BUTTON", text, wsChild|wsVisible|wsTabStop|bsAutoCheckbox, x, y, w, h, 0)
}

// createControls lays out the screen. The rows are in logical pixels.
func (s *settingsWin) createControls() {
	s.label("Atajo de teclado", 16, 16, 170, 20)
	s.hotkey = s.edit(200, 13, 220, 24, 0)
	setCueBanner(s.hotkey, "Ctrl+Shift+Space")

	s.label("Opacidad (60–100 %)", 16, 48, 180, 20)
	s.opacity = s.edit(200, 45, 80, 24, esNumber)

	s.label("Tamaño de letra (9–14 pt)", 16, 80, 180, 20)
	s.font = s.edit(200, 77, 80, 24, 0)

	s.label("Orden de proveedores", 16, 112, 180, 20)
	s.providers = s.createChild("LISTBOX", "", wsChild|wsVisible|wsTabStop|wsBorder|wsVScroll|lbsNotify,
		200, 110, 150, 70, 0)
	s.up = s.button("▲", 360, 110, 32, 32, 0, idSetUp)
	s.down = s.button("▼", 360, 148, 32, 32, 0, idSetDown)

	s.hide = s.checkBox("Ocultar el panel en combate", 16, 194, 400, 22)
	s.proactive = s.checkBox("Consejos proactivos", 16, 220, 400, 22)
	s.startup = s.checkBox("Iniciar con WoW (minimizado en la bandeja)", 16, 246, 400, 22)

	s.label("Carpeta de WoW (_forever_)", 16, 280, 400, 20)
	s.wowPath = s.edit(16, 302, 316, 24, 0)
	s.browseBtn = s.button("Examinar…", 340, 301, 84, 26, 0, idSetBrowse)

	s.keyLabel = s.label("", 16, 336, 240, 20)
	s.keyFolder = s.button("Abrir carpeta .env", 260, 332, 164, 26, 0, idSetKey)

	s.status = s.label("", 16, 370, 408, 44)

	s.wizard = s.button("Asistente de configuración…", 16, 424, 210, 28, 0, idSetWizard)
	s.cancel = s.button("Cancelar", 276, 424, 70, 28, 0, idCancel)
	s.saveBtn = s.button("Guardar", 354, 424, 70, 28, bsDefPushButton, idOK)

	for _, h := range []uintptr{s.hotkey, s.opacity, s.font, s.providers, s.up, s.down, s.hide,
		s.proactive, s.startup, s.wowPath, s.browseBtn, s.keyLabel, s.keyFolder, s.status,
		s.wizard, s.cancel, s.saveBtn} {
		call(procSendMessageW, h, wmSetFont, s.owner.fonts.ui, 1)
	}
	s.statusColor = colMeta
}

// load fills the screen from the configuration in use.
func (s *settingsWin) load(cfg config.Config) {
	setWindowText(s.hotkey, cfg.Hotkey)
	setWindowText(s.opacity, strconv.Itoa(cfg.Opacity))
	setWindowText(s.font, strconv.FormatFloat(cfg.FontSize, 'f', -1, 64))
	s.fillProviders(cfg.ProviderOrder)
	setCheck(s.hide, cfg.HideInCombat)
	setCheck(s.proactive, cfg.Proactive)
	setCheck(s.startup, cfg.StartWithWoW)
	setWindowText(s.wowPath, cfg.WoWPath)
	s.refreshKey(cfg)
	s.setStatus("Cambia lo que quieras y pulsa Guardar.", colMeta)
}

// refreshKey shows whether the NVIDIA key is configured. The key itself is never read out.
func (s *settingsWin) refreshKey(cfg config.Config) {
	setWindowText(s.keyLabel, keyStatusText(keyPresent(cfg)))
}

// setStatus shows a line under the settings.
func (s *settingsWin) setStatus(text string, color uint32) {
	s.statusColor = color
	setWindowText(s.status, text)
	call(procInvalidateRect, s.status, 0, 1)
}

func (s *settingsWin) fillProviders(order []string) {
	call(procSendMessageW, s.providers, lbResetContent, 0, 0)
	for _, name := range order {
		t := utf16Of(name)
		call(procSendMessageW, s.providers, lbAddString, 0, ptrOf(&t[0]))
		runtime.KeepAlive(t)
	}
	call(procSendMessageW, s.providers, lbSetCurSel, 0, 0)
}

// readProviders returns the providers in the list's order.
func (s *settingsWin) readProviders() []string {
	n := int(call(procSendMessageW, s.providers, lbGetCount, 0, 0))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		l := int(call(procSendMessageW, s.providers, lbGetTextLen, uintptr(i), 0))
		buf := make([]uint16, l+1)
		call(procSendMessageW, s.providers, lbGetText, uintptr(i), ptrOf(&buf[0]))
		out = append(out, windows.UTF16ToString(buf))
	}
	return out
}

// moveProvider moves the selected provider one place up (-1) or down (+1).
func (s *settingsWin) moveProvider(dir int) {
	sel := int(int32(call(procSendMessageW, s.providers, lbGetCurSel, 0, 0)))
	if sel < 0 {
		return
	}
	list, idx := moveItem(s.readProviders(), sel, dir)
	s.fillProviders(list)
	call(procSendMessageW, s.providers, lbSetCurSel, uintptr(idx), 0)
}

// save reads the screen, checks it and hands it to the panel to apply.
func (s *settingsWin) save() {
	form := settingsForm{
		Hotkey:        getWindowText(s.hotkey),
		Opacity:       getWindowText(s.opacity),
		FontSize:      getWindowText(s.font),
		ProviderOrder: s.readProviders(),
		HideInCombat:  getCheck(s.hide),
		Proactive:     getCheck(s.proactive),
		StartWithWoW:  getCheck(s.startup),
		WoWPath:       getWindowText(s.wowPath),
	}
	next, err := form.apply(s.owner.cfg)
	if err != nil {
		s.setStatus(err.Error(), colBad)
		return
	}
	msg, err := s.owner.applySettings(next)
	if err != nil {
		s.setStatus(err.Error(), colBad)
		return
	}
	setWindowText(s.hotkey, next.Hotkey)
	s.refreshKey(s.owner.cfg)
	s.setStatus(msg, colOK)
}

// browse picks the WoW folder; a folder that holds _forever_ is taken as its parent.
func (s *settingsWin) browse() {
	dir, ok := browseFolder(s.hwnd, "Elige la carpeta _forever_ de World of Warcraft (o la carpeta World of Warcraft)")
	if !ok {
		return
	}
	if fi, err := os.Stat(filepath.Join(dir, foreverFolder)); err == nil && fi.IsDir() {
		dir = filepath.Join(dir, foreverFolder)
	}
	setWindowText(s.wowPath, dir)
}

// openEnvFolder opens the folder of the .env file (the first one that exists, else the
// data folder), where the NVIDIA key goes.
func (s *settingsWin) openEnvFolder() {
	dir := envFolder(s.owner.opts.DataDir)
	if dir == "" {
		s.setStatus("No sé qué carpeta abrir.", colBad)
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.setStatus("No pude crear la carpeta: "+err.Error(), colBad)
		return
	}
	op := utf16Of("open")
	target := utf16Of(dir)
	r := call(procShellExecuteW, s.hwnd, ptrOf(&op[0]), ptrOf(&target[0]), 0, 0, swShowNormal)
	runtime.KeepAlive(op)
	runtime.KeepAlive(target)
	if r <= 32 {
		s.setStatus("No pude abrir la carpeta.", colBad)
	}
}

// envFolder is the folder that holds the first existing .env file, or dataDir.
func envFolder(dataDir string) string {
	for _, p := range config.DotenvPaths() {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return filepath.Dir(p)
		}
	}
	return dataDir
}

// applySettings applies next: the hotkey, the start-with-WoW entry, the file and the
// appearance. Each step that can fail is undone when a later one fails, so the screen,
// the file and the running panel agree. The returned line says what took effect now and
// what waits for a restart.
func (w *window) applySettings(next config.Config) (string, error) {
	old := w.cfg
	live, restart := settingsChanges(old, next)

	hotkeyChanged := old.Hotkey != next.Hotkey
	if hotkeyChanged {
		hk, err := ParseHotkey(next.Hotkey)
		if err != nil {
			return "", err
		}
		if w.hotkey == nil {
			return "", errors.New("el atajo no está activo")
		}
		if err := w.hotkey.Change(hk); err != nil {
			log.Printf("ui: atajo %s: %v", hk, err)
			return "", fmt.Errorf("No pude registrar el atajo %s: quizá ya lo usa otra aplicación. Sigue activo %s.", hk, old.Hotkey)
		}
	}
	undoHotkey := func() {
		if !hotkeyChanged || w.hotkey == nil {
			return
		}
		if prev, err := ParseHotkey(old.Hotkey); err == nil {
			_ = w.hotkey.Change(prev)
		}
	}
	startChanged := old.StartWithWoW != next.StartWithWoW
	if startChanged {
		if err := SetStartWithWoW(next.StartWithWoW); err != nil {
			undoHotkey()
			return "", err
		}
	}
	if err := w.saveConfig(next); err != nil {
		undoHotkey()
		if startChanged {
			_ = SetStartWithWoW(old.StartWithWoW)
		}
		return "", fmt.Errorf("No pude guardar config.json: %w", err)
	}

	w.combat.enabled = next.HideInCombat
	w.applyOpacity()
	if old.FontSize != next.FontSize {
		w.makeFonts()
		w.applyFonts()
	}
	w.layoutChildren()
	w.refreshStatus()
	log.Printf("ui: ajustes guardados (ahora: %v; al reiniciar: %v)", live, restart)
	return saveMessage(live, restart), nil
}

// settingsProc is the window procedure of the settings screen.
func settingsProc(hwnd, msg, wp, lp uintptr) uintptr {
	s := settingsCur
	if s == nil || s.hwnd != hwnd {
		return call(procDefWindowProcW, hwnd, msg, wp, lp)
	}
	switch msg {
	case wmCommand:
		switch loWordCmd(wp) {
		case idOK:
			s.save()
		case idCancel:
			call(procDestroyWindow, hwnd)
		case idSetUp:
			s.moveProvider(-1)
		case idSetDown:
			s.moveProvider(1)
		case idSetBrowse:
			s.browse()
		case idSetKey:
			s.openEnvFolder()
		case idSetWizard:
			s.owner.startWizard()
		}
		return 0
	case wmCtlColorEdit, wmCtlColorList:
		return s.ctlColor(wp, colText, colInput, s.owner.brushes.input)
	case wmCtlColorBtn, wmCtlColorStat:
		text := uint32(colText)
		if lp == s.status {
			text = s.statusColor
		}
		call(procSetBkMode, wp, transparent)
		return s.ctlColor(wp, text, colBG, s.owner.brushes.bg)
	case wmDestroy:
		if s.owner.settings == s {
			s.owner.settings = nil
		}
		settingsCur = nil
		return 0
	}
	return call(procDefWindowProcW, hwnd, msg, wp, lp)
}

func (s *settingsWin) ctlColor(hdc uintptr, text, bg uint32, brush uintptr) uintptr {
	call(procSetTextColor, hdc, uintptr(text))
	call(procSetBkColor, hdc, uintptr(bg))
	return brush
}

func getCheck(h uintptr) bool {
	return call(procSendMessageW, h, bmGetCheck, 0, 0) == bstChecked
}

func setCheck(h uintptr, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	call(procSendMessageW, h, bmSetCheck, v, 0)
}
