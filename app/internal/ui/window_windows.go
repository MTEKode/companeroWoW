//go:build windows

package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/MTEKode/companeroWoW/app/internal/assistant"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"golang.org/x/sys/windows"
)

const (
	windowClassName = "CompaneroWoWPanel"
	// panelAlpha is the window opacity (230 of 255, about 90 %).
	panelAlpha   = 230
	cornerRadius = 12
	statusPeriod = 2 * time.Second
	tipQueueSize = 16

	// Custom messages posted to the window from other goroutines.
	msgAnswer = wmApp + 1
	msgTip    = wmApp + 2
	msgStatus = wmApp + 3
	msgToggle = wmApp + 4
	msgCombat = wmApp + 5 // wParam 1 while the character is in combat
	msgTray   = wmApp + 6 // tray icon mouse events (lParam)
	msgWizard = wmApp + 7 // setup checks finished

	idCheck  = 2001
	idSend   = 2002
	idInput  = 2003
	idReason = 2004

	menuClear     = 1001
	menuQuit      = 1002
	menuSession   = 1003
	menuSessionAI = 1004
	menuSettings  = 1005
	menuWizard    = 1006
	// Tray menu only; the other tray items reuse the header menu's commands.
	trayToggle = 1010
	// menuOpenPanel opens the external panel from the tray menu in the game mode.
	menuOpenPanel = 1011

	// combatPeriod is how often the combat flag is read.
	combatPeriod = 500 * time.Millisecond
)

// Palette: dark brown background, gold headings and borders, parchment text.
var (
	colBG      = rgbColor(0x16, 0x11, 0x0C)
	colHeader  = rgbColor(0x2A, 0x1F, 0x14)
	colConv    = rgbColor(0x1B, 0x15, 0x0F)
	colInput   = rgbColor(0x2C, 0x22, 0x16)
	colButton  = rgbColor(0x3B, 0x2C, 0x1A)
	colGold    = rgbColor(0xC8, 0xA2, 0x4A)
	colHeading = rgbColor(0xFF, 0xD1, 0x00)
	colText    = rgbColor(0xE8, 0xDC, 0xC0)
	colMeta    = rgbColor(0x8C, 0x80, 0x70)
	colError   = rgbColor(0xE8, 0x7A, 0x5E)
	colTipBG   = rgbColor(0x3D, 0x2F, 0x12)
	colLink    = rgbColor(0x7F, 0xB8, 0xE6)
	colOK      = rgbColor(0x9F, 0xD0, 0x6A)
	colWarn    = rgbColor(0xE0, 0xB0, 0x50)
	colBad     = rgbColor(0xD0, 0x70, 0x60)
)

// current is the panel. The window procedures are plain functions, so they find the
// window through this variable. Only the UI thread reads or writes its fields, except
// the fields documented as safe for other goroutines.
var (
	current      *window
	origEditProc uintptr
	editProcCB   uintptr
)

type winBrushes struct {
	bg, header, conv, input, button, gold uintptr
}

type winFonts struct {
	ui, title, small uintptr
}

type window struct {
	hwnd, inst               uintptr
	conv, input, check, send uintptr
	reasonEdit               uintptr // the one-line reason box after a 👎
	origReasonProc           uintptr
	opts                     Options
	dpi                      uint32
	scale                    float64
	fonts                    winFonts
	brushes                  winBrushes
	state                    windowState

	// cfg is the configuration in use; the settings screen replaces it (UI thread only).
	cfg      config.Config
	cfgPath  string
	combat   combatHider
	feedback FeedbackSaver
	// reasonFor is the index of the answer whose reason is being typed, or -1.
	reasonFor int
	// wiz is the first-run wizard while it is open; nil otherwise.
	wiz      *wizardView
	wizCh    chan wizardResult
	settings *settingsWin
	hotkey   *hotkeyLoop
	tray     trayIcon
	// taskbarMsg is the "TaskbarCreated" broadcast; the tray icon is added again on it.
	taskbarMsg uint32

	quitting    bool // UI thread only
	busy        bool // UI thread only
	notice      string
	entries     []entry
	statusText  string
	statusColor uint32

	// latest is written by the status goroutine and read on the UI thread.
	latestMu sync.Mutex
	latest   Status

	// avoid is the WoW pixel strip the panel must not cover; written by refreshAvoid
	// from any goroutine, read under the same mutex.
	avoidMu sync.Mutex
	avoid   Rect
	avoidOK bool

	answers chan answer        // answers from the ask goroutine
	tips    chan assistant.Tip // tips from the tip goroutine

	// byUser is true while the panel is open because the user opened it (hotkey, tray,
	// balloon, menu); false when a tip opened it. The hide-in-combat rule reads it.
	byUser bool
	// badge counts the tips that came while the panel was closed; the tray icon shows it.
	badge tipBadge
	// pendingTip is a tip whose balloon waits for the end of the fight.
	pendingTip *assistant.Tip
	ctx        context.Context
	cancel     context.CancelFunc

	// askCancel cancels the question in flight; askGen identifies it, so that the answer of
	// a cancelled or superseded question is dropped. noticeAt is when notice was set.
	askCancel context.CancelFunc
	askGen    int
	noticeAt  time.Time
}

// Run creates the panel and runs its message loop on the calling thread, which must be
// the main thread. It returns when the user chooses Salir. Closing the panel with × or
// Alt+F4 only hides it.
func Run(opts Options) error {
	runtime.LockOSThread()
	log.Printf("ui: iniciando el panel nativo")

	// Another component may have fixed the awareness already (the capture code does at
	// startup); then this call fails and the existing setting stands.
	if call(procSetProcessDpiAwarenessCtx, dpiContextPerMon2) != 0 {
		log.Printf("ui: DPI por monitor (v2) activado")
	} else {
		log.Printf("ui: DPI por monitor no cambiado (ya fijado por otro componente): %v", windows.GetLastError())
	}

	lib := utf16Of("Msftedit.dll")
	if call(procLoadLibraryW, ptrOf(&lib[0])) == 0 {
		return fmt.Errorf("no pude cargar Msftedit.dll (el control de texto de Windows): %w", windows.GetLastError())
	}
	log.Printf("ui: Msftedit.dll cargado")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &window{
		opts:      opts,
		ctx:       ctx,
		cancel:    cancel,
		answers:   make(chan answer, 1),
		tips:      make(chan assistant.Tip, tipQueueSize),
		cfg:       opts.Config,
		cfgPath:   opts.ConfigPath,
		combat:    newCombatHider(opts.Config.HideInCombat),
		feedback:  opts.Feedback,
		reasonFor: -1,
		wizCh:     make(chan wizardResult, 8),
	}
	RegisterQuestionActions()
	setQuestionSink(w.askFromMenu)
	defer setQuestionSink(nil)
	w.inst = call(procGetModuleHandleW, 0)
	w.setDPI(uint32(call(procGetDpiForSystem)))
	call(procCoInitializeEx, 0, coinitApartmentThreaded) // the folder browser needs COM

	if err := w.register(); err != nil {
		return err
	}
	log.Printf("ui: clase de ventana registrada")

	state, found := loadWindowState(opts.StatePath)
	if !found {
		state = defaultState(w.scale)
	}
	w.refreshAvoid()
	placed := w.placement(state, found)
	w.state = placed
	log.Printf("ui: posición inicial %d,%d (%dx%d); zona a no cubrir: %s", placed.X, placed.Y, placed.W, placed.H, w.avoidText())

	title := utf16Of(WindowTitle)
	class := utf16Of(windowClassName)
	current = w
	hwnd := call(procCreateWindowExW,
		wsExTopmost|wsExToolWindow|wsExLayered,
		ptrOf(&class[0]), ptrOf(&title[0]),
		wsPopup|wsThickFrame|wsClipChildren,
		uintptr(w.state.X), uintptr(w.state.Y), uintptr(w.state.W), uintptr(w.state.H),
		0, 0, w.inst, 0)
	runtime.KeepAlive(title)
	runtime.KeepAlive(class)
	if hwnd == 0 {
		current = nil
		return fmt.Errorf("no pude crear la ventana: %w", windows.GetLastError())
	}
	w.hwnd = hwnd
	w.setDPI(windowDPI(hwnd))
	// Re-send the frame so the window procedure removes the standard frame.
	call(procSetWindowPos, hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
	call(procSetLayeredWindowAttrs, hwnd, 0, panelAlpha, lwaAlpha)
	// Keep the panel out of screenshots, so a capture of the game does not show it. Older
	// Windows lack the flag; the panel then simply appears in captures.
	if call(procSetWindowDisplayAffinity, hwnd, wdaExcludeFromCapture) == 0 {
		log.Printf("ui: captura sin excluir el panel (Windows no lo admite)")
	}
	log.Printf("ui: ventana creada en %d,%d (%dx%d), %d ppp", w.state.X, w.state.Y, w.state.W, w.state.H, w.dpi)

	w.taskbarMsg = registerMessage("TaskbarCreated")
	w.makeBrushes()
	w.makeFonts()
	if err := w.createChildren(); err != nil {
		return err
	}
	log.Printf("ui: controles creados (conversación, campo, casilla, enviar)")

	w.loadHistory()
	w.layoutChildren()
	w.applyRegion()
	w.applyOpacity()
	w.renderAll()
	w.registerHotkey()
	w.addTray()
	if opts.Setup {
		w.startWizard()
	}
	// A panel started with --tray stays hidden: only the tray icon shows. In the game mode the
	// panel stays hidden as well; the first-run wizard (Setup) still opens it.
	if (!opts.StartHidden && !gameMode(w.cfg.UIMode)) || opts.Setup {
		w.show(true)
	}

	go w.statusLoop()
	go w.tipLoop()
	go w.combatLoop()
	log.Printf("ui: bucle de mensajes en marcha")

	var m winMsg
	for {
		r := int32(call(procGetMessageW, ptrOf(&m), 0, 0, 0))
		if r <= 0 {
			break
		}
		// The settings screen is a dialog: Tab moves between its fields, Enter presses
		// Guardar and Escape closes it.
		if s := w.settings; s != nil && call(procIsDialogMessageW, s.hwnd, ptrOf(&m)) != 0 {
			continue
		}
		call(procTranslateMessage, ptrOf(&m))
		call(procDispatchMessageW, ptrOf(&m))
	}
	cancel()
	w.removeTray()
	log.Printf("ui: panel cerrado")
	return nil
}

// registerMessage returns the id of a system-wide window message.
func registerMessage(name string) uint32 {
	s := utf16Of(name)
	id := uint32(call(procRegisterWindowMessageW, ptrOf(&s[0])))
	runtime.KeepAlive(s)
	return id
}

// combatLoop reads the combat flag twice a second and tells the UI thread about it. The
// hide-in-combat rules themselves live in combatHider.
func (w *window) combatLoop() {
	if w.opts.InCombat == nil {
		return
	}
	t := time.NewTicker(combatPeriod)
	defer t.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-t.C:
			var wp uintptr
			if w.opts.InCombat() {
				wp = 1
			}
			postMessage(w.hwnd, msgCombat, wp, 0)
		}
	}
}

// onCombat applies the combat flag: the panel is hidden when a fight starts while it is
// open, and shown again two seconds after the fight ends, but only when the user opened it.
// A tip that waited for the fight gets its balloon when the fight ends.
func (w *window) onCombat(inCombat bool) {
	switch w.combat.Observe(inCombat, isVisible(w.hwnd), w.byUser, time.Now()) {
	case CombatHide:
		log.Printf("ui: en combate: el panel se oculta")
		w.hide()
	case CombatRestore:
		log.Printf("ui: fin de combate: el panel vuelve")
		w.show(false)
	}
	if !inCombat && w.pendingTip != nil {
		tip := *w.pendingTip
		w.pendingTip = nil
		if !isVisible(w.hwnd) {
			w.balloonFor(tip)
		}
	}
}

// applyOpacity sets the panel's opacity from the configuration.
func (w *window) applyOpacity() {
	if w.hwnd == 0 {
		return
	}
	call(procSetLayeredWindowAttrs, w.hwnd, 0, uintptr(opacityAlpha(w.cfg.Opacity)), lwaAlpha)
}

// Activate brings an already running panel to the front. It reports whether one was found.
func Activate() bool {
	title := utf16Of(WindowTitle)
	h := call(procFindWindowW, 0, ptrOf(&title[0]))
	runtime.KeepAlive(title)
	if h == 0 {
		return false
	}
	call(procShowWindow, h, swRestore)
	call(procSetForegroundWindow, h)
	return true
}

// register registers the window class, once per process.
func (w *window) register() error {
	cls := utf16Of(windowClassName)
	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   windows.NewCallback(wndProc),
		Instance:  w.inst,
		ClassName: &cls[0],
	}
	atom := call(procRegisterClassExW, ptrOf(&wc))
	runtime.KeepAlive(cls)
	if atom == 0 {
		return fmt.Errorf("no pude registrar la clase de ventana: %w", windows.GetLastError())
	}
	return nil
}

func (w *window) setDPI(dpi uint32) {
	if dpi == 0 {
		dpi = 96
	}
	w.dpi = dpi
	w.scale = float64(dpi) / 96
}

func windowDPI(hwnd uintptr) uint32 {
	if d := uint32(call(procGetDpiForWindow, hwnd)); d != 0 {
		return d
	}
	return 96
}

// screenRect returns the primary monitor in pixels.
func screenRect() Rect {
	return Rect{
		W: int(call(procGetSystemMetrics, smCxScreen)),
		H: int(call(procGetSystemMetrics, smCyScreen)),
	}
}

// placement returns where the panel goes at startup: the saved place when it is on
// screen, otherwise the right side of the screen, vertically centred. Either way it is
// moved clear of the WoW pixel strip.
func (w *window) placement(s windowState, found bool) windowState {
	screen := screenRect()
	avoid, _ := w.currentAvoid()
	saved := Rect{X: s.X, Y: s.Y, W: s.W, H: s.H}
	var r Rect
	if !found || (s.X == 0 && s.Y == 0) || insideRect(saved, screen) != saved {
		r = defaultPlacement(s.W, s.H, screen, avoid, scaled(24, w.scale))
	} else {
		r = clearOf(saved, avoid, screen)
	}
	return windowState{X: r.X, Y: r.Y, W: r.W, H: r.H}
}

// refreshAvoid asks for the WoW strip again. It is safe from any goroutine.
func (w *window) refreshAvoid() {
	if w.opts.Avoid == nil {
		return
	}
	r, ok := w.opts.Avoid()
	w.avoidMu.Lock()
	w.avoid, w.avoidOK = r, ok
	w.avoidMu.Unlock()
}

// currentAvoid returns the last known WoW strip. ok is false when WoW was not found, in
// which case nothing needs to be kept clear.
func (w *window) currentAvoid() (Rect, bool) {
	w.avoidMu.Lock()
	defer w.avoidMu.Unlock()
	return w.avoid, w.avoidOK
}

func (w *window) avoidText() string {
	r, ok := w.currentAvoid()
	if !ok {
		return "ninguna (WoW no encontrado)"
	}
	return fmt.Sprintf("%d,%d %dx%d", r.X, r.Y, r.W, r.H)
}

// enforceAvoid moves the panel out of the WoW strip when it overlaps it. UI thread only.
func (w *window) enforceAvoid() {
	avoid, ok := w.currentAvoid()
	if !ok || w.hwnd == 0 {
		return
	}
	var rc rect
	call(procGetWindowRect, w.hwnd, ptrOf(&rc))
	cur := fromRect(rc)
	next := clearOf(cur, avoid, screenRect())
	if next.X != cur.X || next.Y != cur.Y {
		call(procSetWindowPos, w.hwnd, 0, uintptr(next.X), uintptr(next.Y), 0, 0,
			swpNoSize|swpNoZOrder|swpNoActivate)
		log.Printf("ui: el panel se movió fuera de la franja de píxeles de WoW")
	}
}

// clampDrag keeps the rectangle proposed by WM_MOVING or WM_SIZING clear of the strip.
// The message's rectangle is changed in place. UI thread only.
func (w *window) clampDrag(lp uintptr) {
	avoid, ok := w.currentAvoid()
	if !ok {
		return
	}
	rc := (*rect)(fromUintptr(lp))
	*rc = toRect(clearOf(fromRect(*rc), avoid, screenRect()))
}

func (w *window) makeBrushes() {
	solid := func(c uint32) uintptr { return call(procCreateSolidBrush, uintptr(c)) }
	w.brushes = winBrushes{
		bg:     solid(colBG),
		header: solid(colHeader),
		conv:   solid(colConv),
		input:  solid(colInput),
		button: solid(colButton),
		gold:   solid(colGold),
	}
}

// makeFonts creates the GDI fonts for the current DPI, replacing any earlier ones.
func (w *window) makeFonts() {
	for _, f := range []uintptr{w.fonts.ui, w.fonts.title, w.fonts.small} {
		if f != 0 {
			call(procDeleteObject, f)
		}
	}
	// The configured size is the body text; the title is one point larger and the status
	// line 1.5 points smaller, as they were at the default of 10 points.
	pt := w.cfg.FontSize
	if pt <= 0 {
		pt = config.Defaults().FontSize
	}
	w.fonts = winFonts{
		ui:    newFont("Segoe UI", pt, w.dpi, fwNormal),
		title: newFont("Segoe UI", pt+1, w.dpi, fwSemibold),
		small: newFont("Segoe UI", pt-1.5, w.dpi, fwNormal),
	}
}

// createChildren creates the conversation, the input, the checkbox and the send button,
// and subclasses the input so that Enter sends and Shift+Enter starts a new line.
func (w *window) createChildren() error {
	conv, err := createConversation(w.hwnd, w.inst)
	if err != nil {
		return err
	}
	w.conv = conv
	w.input = w.child("EDIT", "", wsChild|wsVisible|wsVScroll|wsTabStop|esMultiline|esAutoVScroll, idInput)
	w.check = w.child("BUTTON", "Incluir captura", wsChild|wsVisible|wsTabStop|bsAutoCheckbox, idCheck)
	w.send = w.child("BUTTON", "Enviar", wsChild|wsVisible|wsTabStop|bsOwnerDraw, idSend)
	if w.input == 0 || w.check == 0 || w.send == 0 {
		return errors.New("no pude crear los controles del panel")
	}
	editProcCB = windows.NewCallback(editProc)
	origEditProc = call(procGetWindowLongPtrW, w.input, gwlpWndProc)
	call(procSetWindowLongPtrW, w.input, gwlpWndProc, editProcCB)
	// The reason box is created hidden; it shows in the feedback area after a 👎.
	w.reasonEdit = w.child("EDIT", "", wsChild|wsTabStop|esAutoHScroll|wsBorder, idReason)
	if w.reasonEdit != 0 {
		reasonProcCB = windows.NewCallback(reasonProc)
		w.origReasonProc = call(procGetWindowLongPtrW, w.reasonEdit, gwlpWndProc)
		call(procSetWindowLongPtrW, w.reasonEdit, gwlpWndProc, reasonProcCB)
		setCueBanner(w.reasonEdit, reasonPlaceholder)
	}
	w.applyFonts()
	return nil
}

// setCueBanner shows a grey hint in an empty edit control.
func setCueBanner(h uintptr, text string) {
	t := utf16Of(text)
	call(procSendMessageW, h, emSetCueBanner, 0, ptrOf(&t[0]))
	runtime.KeepAlive(t)
}

// child creates a child control with text and a numeric ID.
func (w *window) child(class, text string, style uintptr, id uintptr) uintptr {
	cls := utf16Of(class)
	txt := utf16Of(text)
	h := call(procCreateWindowExW, 0, ptrOf(&cls[0]), ptrOf(&txt[0]), style,
		0, 0, 0, 0, w.hwnd, id, w.inst, 0)
	runtime.KeepAlive(cls)
	runtime.KeepAlive(txt)
	return h
}

// applyFonts gives the controls the UI font.
func (w *window) applyFonts() {
	for _, h := range []uintptr{w.input, w.check, w.send, w.reasonEdit} {
		if h != 0 {
			call(procSendMessageW, h, wmSetFont, w.fonts.ui, 1)
		}
	}
}

// layoutChildren places the controls for the current client size.
func (w *window) layoutChildren() {
	if w.conv == 0 || w.input == 0 {
		return
	}
	cw, ch := w.clientSize()
	l := computeLayout(cw, ch, w.scale)
	move := func(h uintptr, r Rect) {
		call(procMoveWindow, h, uintptr(r.X), uintptr(r.Y), uintptr(r.W), uintptr(r.H), 1)
	}
	move(w.conv, l.Conv)
	move(w.input, l.Input)
	move(w.check, l.Check)
	move(w.send, l.Send)
	if w.reasonEdit != 0 {
		move(w.reasonEdit, l.Feedback)
	}
	call(procInvalidateRect, w.hwnd, 0, 1)
}

// applyRegion rounds the corners of the panel.
func (w *window) applyRegion() {
	cw, ch := w.clientSize()
	d := uintptr(scaled(cornerRadius, w.scale) * 2)
	rgn := call(procCreateRoundRectRgn, 0, 0, uintptr(cw+1), uintptr(ch+1), d, d)
	call(procSetWindowRgn, w.hwnd, rgn, 1)
}

func (w *window) clientSize() (int, int) {
	var r rect
	call(procGetClientRect, w.hwnd, ptrOf(&r))
	return int(r.Right - r.Left), int(r.Bottom - r.Top)
}

// wndProc is the window procedure of the panel.
func wndProc(hwnd, msg, wp, lp uintptr) uintptr {
	w := current
	if w == nil {
		return call(procDefWindowProcW, hwnd, msg, wp, lp)
	}
	switch msg {
	case wmNcCalcSize:
		if wp != 0 {
			// The whole window is client area: no standard frame is drawn.
			return 0
		}
	case wmGetMinMaxInfo:
		mm := (*minMaxInfo)(fromUintptr(lp))
		mm.MinTrackSize = point{X: int32(scaled(minWidth, w.scale)), Y: int32(scaled(minHeight, w.scale))}
		return 0
	case wmNcHitTest:
		return w.hitTest(lp)
	case wmSize:
		if wp != sizeMinimized && w.conv != 0 {
			w.layoutChildren()
			w.applyRegion()
		}
		return 0
	case wmPaint:
		w.paint()
		return 0
	case wmEraseBkgnd:
		return 1
	case wmCtlColorEdit:
		return w.ctlColor(wp, colText, colInput, w.brushes.input)
	case wmCtlColorBtn, wmCtlColorStat:
		call(procSetBkMode, wp, transparent)
		return w.ctlColor(wp, colText, colBG, w.brushes.bg)
	case wmDrawItem:
		if di := (*drawItemStruct)(fromUintptr(lp)); di.CtlID == idSend {
			w.drawSend(di)
			return 1
		}
	case wmCommand:
		if lp == w.send {
			if w.busy {
				w.cancelAsk()
			} else {
				w.submit()
			}
			return 0
		}
		if lp == 0 {
			w.menuCommand(loWordCmd(wp))
			return 0
		}
	case wmNotify:
		nh := (*nmhdr)(fromUintptr(lp))
		if nh.HWndFrom == w.conv && nh.Code == enLink {
			if lk := (*enLinkMsg)(fromUintptr(lp)); lk.Msg == wmLButtonUp {
				w.linkClicked(w.linkText(lk.Range))
			}
		}
		return 0
	case wmLButtonDown:
		w.onLButtonDown(loWord(lp), hiWord(lp))
		return 0
	case wmRButtonUp:
		if hiWord(lp) < scaled(headerHeight, w.scale) {
			w.showMenu()
		}
		return 0
	case wmMoving, wmSizing:
		w.clampDrag(lp)
		return 1
	case wmExitSizeMove:
		w.saveGeometry()
		return 0
	case wmDpiChanged:
		w.onDPIChanged(wp, lp)
		return 0
	case wmClose:
		if !w.quitting {
			w.hide()
			return 0
		}
	case wmDestroy:
		call(procPostQuitMessage, 0)
		return 0
	case msgAnswer:
		w.drainAnswers()
		return 0
	case msgTip:
		w.drainTips()
		return 0
	case msgStatus:
		w.expireNotice()
		w.enforceAvoid()
		w.refreshStatus()
		return 0
	case msgToggle:
		w.hotkeyPressed()
		return 0
	case msgCombat:
		w.onCombat(wp != 0)
		return 0
	case msgTray:
		w.onTrayMessage(uint32(lp & 0xFFFF))
		return 0
	case msgWizard:
		w.onWizardResults()
		return 0
	}
	if msg != 0 && uint32(msg) == w.taskbarMsg {
		// Explorer restarted and dropped the tray icon: add it again.
		w.addTray()
		return 0
	}
	return call(procDefWindowProcW, hwnd, msg, wp, lp)
}

// loWordCmd returns the command identifier of a WM_COMMAND.
func loWordCmd(wp uintptr) int {
	return int(wp & 0xFFFF)
}

// editProc is the subclassed input: Enter sends, Shift+Enter inserts a line break, Escape
// cancels the question in flight or, when none is, hides the panel.
func editProc(hwnd, msg, wp, lp uintptr) uintptr {
	if w := current; w != nil {
		switch msg {
		case wmKeyDown:
			if wp == vkReturn && !shiftDown() {
				w.submit()
				return 0
			}
			if wp == vkEscape {
				if w.busy {
					w.cancelAsk()
				} else {
					w.hide()
				}
				return 0
			}
		case wmChar:
			// The key was handled on WM_KEYDOWN; do not insert the character.
			if (wp == vkReturn && !shiftDown()) || wp == vkEscape {
				return 0
			}
		}
	}
	return call(procCallWindowProcW, origEditProc, hwnd, msg, wp, lp)
}

// hitTest resolves WM_NCHITTEST: the edges resize, everything else is client area.
func (w *window) hitTest(lp uintptr) uintptr {
	var r rect
	call(procGetWindowRect, w.hwnd, ptrOf(&r))
	x := loWord(lp) - int(r.Left)
	y := hiWord(lp) - int(r.Top)
	cw, ch := int(r.Right-r.Left), int(r.Bottom-r.Top)
	// The header buttons sit inside the top resize band; they must stay clickable.
	if _, ok := hitHeaderButton(x, y, cw, w.scale); ok {
		return htClient
	}
	return uintptr(hitEdge(x, y, cw, ch, scaled(gripWidth, w.scale)))
}

func (w *window) ctlColor(hdc uintptr, text, bg uint32, brush uintptr) uintptr {
	call(procSetTextColor, hdc, uintptr(text))
	call(procSetBkColor, hdc, uintptr(bg))
	return brush
}

// onLButtonDown handles the header buttons, and starts moving the window when the header
// is dragged. The system does the moving, so the panel stays smooth.
func (w *window) onLButtonDown(x, y int) {
	cw, ch := w.clientSize()
	if w.wiz != nil && w.wizardClick(x, y, cw, ch) {
		return
	}
	if w.wiz == nil && w.feedbackClick(x, y, cw, ch) {
		return
	}
	if btn, ok := hitHeaderButton(x, y, cw, w.scale); ok {
		switch btn.Action {
		case actionClear:
			w.clearHistory()
		case actionHide, actionClose:
			w.hide()
		}
		return
	}
	if y < scaled(headerHeight, w.scale) {
		call(procReleaseCapture)
		call(procSendMessageW, w.hwnd, wmNcLButtonDown, htCaption, 0)
	}
}

// menuItem is one line of a popup menu; a zero id draws a separator.
type menuItem struct {
	id    int
	label string
}

// popup shows the items at the cursor and returns the chosen command id, or 0.
func (w *window) popup(items []menuItem) int {
	menu := call(procCreatePopupMenu)
	if menu == 0 {
		return 0
	}
	defer call(procDestroyMenu, menu)
	var keep [][]uint16
	for _, it := range items {
		if it.id == 0 {
			call(procAppendMenuW, menu, mfSeparator, 0, 0)
			continue
		}
		s := utf16Of(it.label)
		keep = append(keep, s)
		call(procAppendMenuW, menu, mfString, uintptr(it.id), ptrOf(&s[0]))
	}
	var pt point
	call(procGetCursorPos, ptrOf(&pt))
	call(procSetForegroundWindow, w.hwnd)
	cmd := call(procTrackPopupMenu, menu, tpmRightButton|tpmReturnCmd,
		uintptr(pt.X), uintptr(pt.Y), 0, w.hwnd, 0)
	runtime.KeepAlive(keep)
	// The documented way to make a tray menu go away when the user clicks elsewhere.
	postMessage(w.hwnd, wmNull, 0, 0)
	return int(cmd)
}

// showMenu opens the header context menu: the session, the settings, the wizard, the
// history, the actions other components registered, and Salir.
func (w *window) showMenu() {
	items := []menuItem{
		{menuSession, "Resumen de sesión"},
		{menuSessionAI, "Analizar sesión con IA"},
		{0, ""},
		{menuSettings, "Ajustes…"},
		{menuWizard, "Asistente de configuración…"},
		{0, ""},
		{menuClear, "Limpiar historial"},
	}
	if acts := menuActions(); len(acts) > 0 {
		items = append(items, menuItem{0, ""})
		for _, a := range acts {
			items = append(items, menuItem{a.ID, a.Label})
		}
	}
	items = append(items, menuItem{0, ""}, menuItem{menuQuit, "Salir"})
	w.menuCommand(w.popup(items))
}

// menuCommand runs the menu item with the given command id: a header menu item, a tray
// menu item or an action registered with RegisterMenuAction.
func (w *window) menuCommand(id int) {
	switch id {
	case menuSession:
		w.showSession(false)
	case menuSessionAI:
		w.showSession(true)
	case menuClear:
		w.clearHistory()
	case menuSettings:
		w.openSettings()
	case menuWizard:
		w.startWizard()
	case trayToggle:
		w.toggle()
	case menuOpenPanel:
		w.show(true)
	case menuQuit:
		w.quit()
	case 0:
	default:
		if !runMenuAction(id) {
			log.Printf("ui: orden de menú desconocida %d", id)
		}
	}
}

// showSession puts the session summary in the conversation. With withAI, it then asks the
// assistant for two tips on it.
func (w *window) showSession(withAI bool) {
	if w.opts.Backend == nil {
		w.add(entry{Kind: entryError, Text: "El asistente no está disponible."})
		return
	}
	text, err := w.opts.Backend.SessionSummary()
	if err != nil {
		log.Printf("ui: resumen de sesión: %v", err)
		w.add(entry{Kind: entryError, Text: "No pude preparar el resumen de la sesión. El detalle está en companero.log."})
		return
	}
	w.add(entry{Kind: entryInfo, Text: text})
	if withAI {
		if w.busy {
			w.setNotice("Espera a que termine la pregunta actual")
		} else {
			w.start(sessionAIDisplay, sessionAIPrompt(text), false)
			return
		}
	}
	w.refreshStatus()
}

// drawSend paints the owner-drawn send button in the panel palette.
func (w *window) drawSend(di *drawItemStruct) {
	r := fromRect(di.RcItem)
	color := uint32(colText)
	if di.ItemState&odsDisabled != 0 {
		color = colMeta
	}
	fillRect(di.HDC, r, w.brushes.button)
	frameRect(di.HDC, r, w.brushes.gold)
	label := "Enviar"
	if w.busy {
		label = "Cancelar"
	}
	drawText(di.HDC, label, r, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix, w.fonts.ui, color)
}

// paint draws the background, the header with its buttons, the frame and the status bar.
// The controls paint themselves.
func (w *window) paint() {
	var ps paintStruct
	hdc := call(procBeginPaint, w.hwnd, ptrOf(&ps))
	defer call(procEndPaint, w.hwnd, ptrOf(&ps))
	call(procSetBkMode, hdc, transparent)

	cw, ch := w.clientSize()
	l := computeLayout(cw, ch, w.scale)
	fillRect(hdc, Rect{W: cw, H: ch}, w.brushes.bg)
	fillRect(hdc, l.Header, w.brushes.header)
	fillRect(hdc, Rect{Y: l.Header.H - 1, W: cw, H: 1}, w.brushes.gold)
	drawText(hdc, "Compañero", Rect{X: scaled(padding, w.scale), W: cw / 2, H: l.Header.H},
		dtLeft|dtVCenter|dtSingleLine|dtNoPrefix, w.fonts.title, colHeading)
	for _, b := range headerButtons(cw, w.scale) {
		fillRect(hdc, b.Rect, w.brushes.button)
		frameRect(hdc, b.Rect, w.brushes.gold)
		drawText(hdc, b.Label, b.Rect, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix, w.fonts.ui, colText)
	}
	if w.wiz != nil {
		w.paintWizard(hdc, l)
	} else {
		w.paintFeedback(hdc, l)
	}
	drawText(hdc, w.statusText, l.Status,
		dtLeft|dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix, w.fonts.small, w.statusColor)
	frameRect(hdc, Rect{W: cw, H: ch}, w.brushes.gold)
}

// onDPIChanged adopts the size and DPI the system suggests when the panel moves to a
// monitor with another scale.
func (w *window) onDPIChanged(wp, lp uintptr) {
	w.setDPI(uint32(wp & 0xFFFF))
	s := (*rect)(fromUintptr(lp))
	w.makeFonts()
	w.applyFonts()
	call(procSetWindowPos, w.hwnd, 0, uintptr(s.Left), uintptr(s.Top),
		uintptr(s.Right-s.Left), uintptr(s.Bottom-s.Top), swpNoZOrder|swpNoActivate)
	log.Printf("ui: DPI cambiado a %d", w.dpi)
	w.renderAll()
	w.refreshStatus()
}

// show makes the panel visible. The focus moves to the input only when focus is true, so
// that tips never take the keyboard from the game. focus means the user opened the panel:
// then the unread tips are marked as read.
func (w *window) show(focus bool) {
	w.refreshAvoid()
	w.enforceAvoid()
	if focus {
		call(procShowWindow, w.hwnd, swShow)
		call(procSetWindowPos, w.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize)
		call(procSetForegroundWindow, w.hwnd)
		call(procSetFocus, w.input)
		w.byUser = true
		w.markRead()
	} else {
		call(procShowWindow, w.hwnd, swShowNoActivate)
		call(procSetWindowPos, w.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	}
	go w.pollStatus()
}

// markRead runs when the user opens the panel: the badge clears and the conversation scrolls
// to the first tip that came while the panel was closed. A balloon still waiting is dropped.
func (w *window) markRead() {
	w.pendingTip = nil
	first, unread := w.badge.firstUnread()
	if !unread {
		return
	}
	w.badge.clear()
	w.syncBadge()
	off := unreadOffset(w.entries, first)
	call(procSendMessageW, w.conv, emSetSel, uintptr(off), uintptr(off))
	call(procSendMessageW, w.conv, emScrollCaret, 0, 0)
}

// syncBadge shows the unread count on the tray icon and in its tooltip.
func (w *window) syncBadge() {
	n := w.badge.count()
	w.tray.setBadge(n)
	w.latestMu.Lock()
	s := w.latest
	w.latestMu.Unlock()
	w.updateTray(trayTipWithBadge(s, n))
}

// deliverTip routes a proactive tip by the tips mode: the conversation always gets it (unless
// the tips are off), the panel opens only in panel mode, and in bandeja mode the tray badge
// and a balloon announce it.
func (w *window) deliverTip(tip assistant.Tip) {
	route := routeTip(w.cfg.TipsMode, isVisible(w.hwnd), w.combat.InCombat())
	if gameMode(w.cfg.UIMode) {
		route = gameTipRoute(route)
	}
	if !route.Conversation {
		log.Printf("ui: consejo descartado (consejos desactivados): %s", tip.Title)
		return
	}
	w.add(entry{Kind: entryTip, Title: tip.Title, Text: tip.Text})
	if route.ShowPanel {
		w.byUser = false
		w.show(false)
	}
	if route.Badge {
		w.badge.add(len(w.entries) - 1)
		w.syncBadge()
	}
	if route.Balloon {
		w.balloonFor(tip)
	}
	if route.Defer {
		t := tip
		w.pendingTip = &t
	}
}

// balloonFor shows the tray balloon of a tip, as the sound setting says.
func (w *window) balloonFor(tip assistant.Tip) {
	title, text := balloonText(tip)
	if !w.balloon(title, text, w.cfg.TipsSound) {
		log.Printf("ui: no pude mostrar el aviso del consejo: %s", tip.Title)
	}
}

func (w *window) hide() {
	w.saveGeometry()
	call(procShowWindow, w.hwnd, swHide)
}

// toggle is the window mode's hotkey action: hide when shown, show with focus otherwise.
func (w *window) toggle() {
	if isVisible(w.hwnd) {
		w.hide()
		return
	}
	w.show(true)
}

// hotkeyPressed is the global shortcut. In the game mode it only activates the reading of the
// game channel and never shows the panel; in the window mode it toggles the panel.
func (w *window) hotkeyPressed() {
	if hotkeyFor(w.cfg.UIMode) == hotkeyActivate {
		log.Printf("ui: atajo en modo juego: el panel no se abre; la lectura del canal sigue activa")
		return
	}
	w.toggle()
}

// quit closes the panel for good; the message loop ends afterwards.
func (w *window) quit() {
	w.quitting = true
	w.saveGeometry()
	log.Printf("ui: Salir desde el panel")
	if w.settings != nil {
		call(procDestroyWindow, w.settings.hwnd)
	}
	call(procDestroyWindow, w.hwnd)
}

// saveGeometry remembers the position and size of the panel.
func (w *window) saveGeometry() {
	var r rect
	if call(procGetWindowRect, w.hwnd, ptrOf(&r)) == 0 {
		return
	}
	s := windowState{X: int(r.Left), Y: int(r.Top), W: int(r.Right - r.Left), H: int(r.Bottom - r.Top)}
	if s.W < scaled(minWidth, w.scale) || s.H < scaled(minHeight, w.scale) {
		return
	}
	w.state = s
	if err := saveWindowState(w.opts.StatePath, s); err != nil {
		log.Printf("ui: guardar la posición: %v", err)
	}
}

// registerHotkey installs the global shortcut. Its thread posts a message to the panel,
// so the panel is only touched from the UI thread.
func (w *window) registerHotkey() {
	hk, err := ParseHotkey(w.opts.Hotkey)
	if err != nil {
		w.setNotice(fmt.Sprintf("Atajo no válido en config.json: %v", err))
		log.Printf("ui: atajo no válido: %v", err)
		return
	}
	hwnd := w.hwnd
	loop, err := startHotkey(hk, func() { postMessage(hwnd, msgToggle, 0, 0) })
	w.hotkey = loop
	if err != nil {
		w.setNotice(fmt.Sprintf("No pude registrar el atajo %s: quizá ya lo usa otra aplicación. Puedes cambiarlo en config.json.", hk))
		log.Printf("ui: atajo %s: %v", hk, err)
		return
	}
	log.Printf("ui: atajo %s registrado", hk)
}

func (w *window) loadHistory() {
	if w.opts.Backend == nil {
		return
	}
	msgs, err := w.opts.Backend.History(60)
	if err != nil {
		log.Printf("ui: historial: %v", err)
		w.entries = append(w.entries, entry{Kind: entryInfo, Text: "No pude leer el historial: " + err.Error()})
		return
	}
	for _, m := range toAnswerMsgs(msgs) {
		w.entries = append(w.entries, entryFromMsg(m))
	}
	for i := range w.entries {
		if w.entries[i].Kind == entryAssistant {
			w.entries[i].Question = questionFor(w.entries, i)
		}
	}
	log.Printf("ui: historial cargado (%d mensajes)", len(msgs))
}

func (w *window) clearHistory() {
	if w.opts.Backend != nil {
		if err := w.opts.Backend.ClearHistory(); err != nil {
			log.Printf("ui: borrar historial: %v", err)
			w.setNotice("No pude borrar el historial")
			w.refreshStatus()
			return
		}
	}
	w.entries = nil
	w.badge.clear()
	w.syncBadge()
	w.renderAll()
	w.refreshStatus()
}

// add appends an entry to the conversation.
func (w *window) add(e entry) {
	w.entries = append(w.entries, e)
	w.appendRuns(entryRuns(e))
}

// submit sends the question in the input. The answer comes back through msgAnswer.
func (w *window) submit() {
	if w.busy {
		return
	}
	text := strings.TrimSpace(strings.ReplaceAll(getWindowText(w.input), "\r\n", "\n"))
	if text == "" {
		return
	}
	withShot := call(procSendMessageW, w.check, bmGetCheck, 0, 0) == bstChecked
	setWindowText(w.input, "")
	w.start(text, text, withShot)
}

// askFromMenu sends a question chosen in the header menu. It reports whether the question was
// sent; while another question is in flight it only says so, and the question is not sent.
func (w *window) askFromMenu(question string) bool {
	if w.opts.Backend == nil {
		w.add(entry{Kind: entryError, Text: "El asistente no está disponible."})
		return false
	}
	if w.busy {
		w.setNotice("Espera a que termine la pregunta actual")
		return false
	}
	w.start(question, question, false)
	return true
}

// start sends question to the assistant and shows display as the user's message. The
// answer comes back through msgAnswer.
func (w *window) start(display, question string, withShot bool) {
	w.add(entry{Kind: entryUser, Text: display})
	w.busy = true
	w.askGen++
	ctx, cancel := context.WithCancel(w.ctx)
	w.askCancel = cancel
	call(procInvalidateRect, w.send, 0, 1)
	w.refreshStatus()
	go w.ask(ctx, w.askGen, question, withShot)
}

// cancelAsk stops the question in flight: the button and Escape do this while the
// assistant is thinking. The panel is free again at once; the answer of the cancelled
// question, if it still arrives, is dropped.
func (w *window) cancelAsk() {
	if !w.busy {
		return
	}
	if w.askCancel != nil {
		w.askCancel()
		w.askCancel = nil
	}
	w.askGen++
	w.busy = false
	w.setNotice("Pregunta cancelada")
	call(procInvalidateRect, w.send, 0, 1)
	w.refreshStatus()
}

// ask runs the question in the background and hands the answer to the UI thread.
func (w *window) ask(ctx context.Context, gen int, text string, withShot bool) {
	e := w.answer(ctx, text, withShot)
	w.answers <- answer{gen: gen, e: e}
	postMessage(w.hwnd, msgAnswer, 0, 0)
}

// answer is one question's result, tagged with the question's generation.
type answer struct {
	gen int
	e   entry
}

func (w *window) answer(ctx context.Context, text string, withShot bool) entry {
	if w.opts.Backend == nil {
		return entry{Kind: entryError, Text: askError(errors.New("el asistente no está disponible"))}
	}
	resp, err := w.opts.Backend.Ask(ctx, text, withShot)
	if err != nil {
		return entry{Kind: entryError, Text: askError(err)}
	}
	return entry{Kind: entryAssistant, Text: resp.Text, Provider: resp.Provider, At: time.Now().Format("15:04"), Question: text}
}

// drainAnswers shows the answer that the ask goroutine delivered.
func (w *window) drainAnswers() {
	select {
	case a := <-w.answers:
		if a.gen != w.askGen {
			return // the question was cancelled or superseded
		}
		w.busy = false
		if w.askCancel != nil {
			w.askCancel()
			w.askCancel = nil
		}
		w.add(a.e)
		call(procInvalidateRect, w.send, 0, 1)
		w.refreshStatus()
	default:
	}
}

// setNotice shows a transient message in the status line; it clears after noticeTTL.
func (w *window) setNotice(text string) {
	w.notice = text
	w.noticeAt = time.Now()
}

// expireNotice clears the transient message once it has been shown for noticeTTL.
func (w *window) expireNotice() {
	if w.notice != "" && noticeExpired(w.noticeAt, time.Now()) {
		w.notice = ""
		w.noticeAt = time.Time{}
	}
}

// drainTips delivers the queued proactive tips (see deliverTip).
func (w *window) drainTips() {
	for {
		select {
		case tip := <-w.tips:
			w.deliverTip(tip)
		default:
			return
		}
	}
}

// linkClicked copies a clicked map mark to the clipboard, or opens any other link.
func (w *window) linkClicked(text string) {
	if cmd, ok := CommandFromLink(text); ok {
		if err := CopyText(cmd); err != nil {
			w.setNotice("No pude copiar la marca: " + err.Error())
		} else {
			w.setNotice(CopiedNotice)
		}
		w.refreshStatus()
		return
	}
	w.openLink(text)
}

// openLink opens an http or https link in the default browser.
func (w *window) openLink(raw string) {
	u, err := safeURL(raw)
	if err != nil {
		log.Printf("ui: enlace rechazado: %v", err)
		return
	}
	op := utf16Of("open")
	target := utf16Of(u)
	r := call(procShellExecuteW, w.hwnd, ptrOf(&op[0]), ptrOf(&target[0]), 0, 0, swShowNormal)
	runtime.KeepAlive(op)
	runtime.KeepAlive(target)
	if r <= 32 {
		w.setNotice("No pude abrir el enlace en el navegador")
		w.refreshStatus()
	}
}

// refreshStatus repaints the status bar from the latest status and the busy flag.
func (w *window) refreshStatus() {
	w.latestMu.Lock()
	s := w.latest
	w.latestMu.Unlock()
	w.statusText = formatStatus(s, w.notice, w.busy)
	w.statusColor = statusColour(s, w.busy)
	cw, ch := w.clientSize()
	rc := toRect(computeLayout(cw, ch, w.scale).Status)
	call(procInvalidateRect, w.hwnd, ptrOf(&rc), 1)
}

// statusColour picks the colour of the status line from the bridge state.
func statusColour(s Status, busy bool) uint32 {
	switch {
	case busy:
		return colHeading
	case s.Bridge == "connected":
		return colOK
	case s.Bridge == "no_strip":
		return colWarn
	case s.Bridge == "":
		return colMeta
	}
	return colBad
}

// statusLoop asks for a fresh status every two seconds while the panel is visible. A hidden
// panel (the tray icon only) refreshes the tooltip every ten seconds instead.
func (w *window) statusLoop() {
	t := time.NewTicker(statusPeriod)
	defer t.Stop()
	const hiddenEvery = 5 // ticks: 10 s
	n := 0
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-t.C:
			n++
			if isVisible(w.hwnd) || n%hiddenEvery == 0 {
				w.pollStatus()
			}
		}
	}
}

// pollStatus reads the status from the callback (any goroutine) and asks the UI thread
// to show it. The tray tooltip follows the same status.
func (w *window) pollStatus() {
	w.refreshAvoid()
	if w.opts.Status == nil {
		return
	}
	s := w.opts.Status()
	w.latestMu.Lock()
	w.latest = s
	w.latestMu.Unlock()
	w.updateTray(trayTipWithBadge(s, w.tray.badgeCount()))
	postMessage(w.hwnd, msgStatus, 0, 0)
}

// tipLoop queues proactive tips for the UI thread.
func (w *window) tipLoop() {
	if w.opts.Tips == nil {
		return
	}
	for tip := range w.opts.Tips {
		select {
		case w.tips <- tip:
			postMessage(w.hwnd, msgTip, 0, 0)
		default:
			log.Printf("ui: consejo descartado (cola llena): %s", tip.Title)
		}
	}
}
