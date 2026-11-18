//go:build windows

package ui

import (
	"fmt"
	"runtime"
	"slices"
	"sync"
	"unsafe"

	"github.com/MTEKode/companeroWoW/app/internal/config"
	"golang.org/x/sys/windows"
)

// The profile screen: what the player wants from the companion (goals, role, playstyle notes,
// tone, hardcore) and whether answers are checked before they are shown. It opens from the
// settings screen and works on a draft there; Guardar on the settings screen applies it.

// profileClass is the window class of the profile screen.
const profileClass = "CompaneroWoWProfile"

// Control ids of the profile screen. The goals take one id each, in config.AllGoals order.
const (
	idProfGoal0 = 2030
	idProfTone  = 2036
)

// emSetLimitText caps the text of an edit control (in characters).
const emSetLimitText = 0x00C5

var (
	profileRegOnce sync.Once
	profileRegErr  error
	profileClsName []uint16
	profileCB      uintptr
	// profileCur is the open profile screen (UI thread only).
	profileCur *profileWin
)

// profileWin is the profile screen. Its controls are the logical rows of createControls.
type profileWin struct {
	hwnd  uintptr
	inst  uintptr
	owner *settingsWin
	scale float64

	goals                      [6]uintptr
	spec, notes                uintptr
	toneBrief, toneDetail      uintptr
	hardcore, verify           uintptr
	errLabel, okBtn, cancelBtn uintptr
}

// sc scales a logical size of the profile screen.
func (p *profileWin) sc(v int) int { return scaled(v, p.scale) }

// child creates a control of the profile screen at logical position x, y.
func (p *profileWin) child(class, text string, style uintptr, x, y, w, h int, id uintptr) uintptr {
	cls := utf16Of(class)
	txt := utf16Of(text)
	ctl := call(procCreateWindowExW, 0, ptrOf(&cls[0]), ptrOf(&txt[0]), style,
		uintptr(p.sc(x)), uintptr(p.sc(y)), uintptr(p.sc(w)), uintptr(p.sc(h)), p.hwnd, id, p.inst, 0)
	runtime.KeepAlive(cls)
	runtime.KeepAlive(txt)
	return ctl
}

func (p *profileWin) label(text string, x, y, w, h int) uintptr {
	return p.child("STATIC", text, wsChild|wsVisible|ssLeft, x, y, w, h, 0)
}

func (p *profileWin) edit(x, y, w, h int) uintptr {
	return p.child("EDIT", "", wsChild|wsVisible|wsTabStop|wsBorder|esAutoHScroll, x, y, w, h, 0)
}

func (p *profileWin) check(text string, x, y, w, h int, id uintptr) uintptr {
	return p.child("BUTTON", text, wsChild|wsVisible|wsTabStop|bsAutoCheckbox, x, y, w, h, id)
}

func (p *profileWin) radio(text string, x, y, w, h int, first bool, id uintptr) uintptr {
	style := uintptr(wsChild | wsVisible | wsTabStop | bsAutoRadio)
	if first {
		style |= wsGroup
	}
	return p.child("BUTTON", text, style, x, y, w, h, id)
}

// openProfile shows the profile screen for the settings draft, or brings the open one forward.
func (s *settingsWin) openProfile() {
	if s.profileDlg != nil {
		call(procShowWindow, s.profileDlg.hwnd, swShow)
		call(procSetForegroundWindow, s.profileDlg.hwnd)
		return
	}
	p, err := newProfileWin(s)
	if err != nil {
		s.setStatus("No pude abrir el perfil: "+err.Error(), colBad)
		return
	}
	s.profileDlg = p
}

// registerProfileClass registers the window class once; its background is the panel's.
func registerProfileClass(inst, bg uintptr) error {
	profileRegOnce.Do(func() {
		profileClsName = utf16Of(profileClass)
		profileCB = windows.NewCallback(profileProc)
		wc := wndClassEx{
			Size:       uint32(unsafe.Sizeof(wndClassEx{})),
			WndProc:    profileCB,
			Instance:   inst,
			Cursor:     call(procLoadCursorW, 0, idcArrow),
			Background: bg,
			ClassName:  &profileClsName[0],
		}
		if call(procRegisterClassExW, ptrOf(&wc)) == 0 {
			profileRegErr = fmt.Errorf("no pude registrar la clase de ventana del perfil: %w", windows.GetLastError())
		}
	})
	return profileRegErr
}

// newProfileWin creates the profile screen over the settings screen and fills it with the draft.
func newProfileWin(owner *settingsWin) (*profileWin, error) {
	if err := registerProfileClass(owner.owner.inst, owner.owner.brushes.bg); err != nil {
		return nil, err
	}
	p := &profileWin{inst: owner.owner.inst, owner: owner, scale: owner.owner.scale}
	style := uintptr(wsPopup | wsCaption | wsSysMenu | wsClipChildren)
	// The client area is 440 x 370 logical pixels; AdjustWindowRectEx adds the frame.
	rc := rect{Right: int32(p.sc(440)), Bottom: int32(p.sc(370))}
	call(procAdjustWindowRectEx, ptrOf(&rc), style, 0, wsExAppWindow)
	ow, oh := int(rc.Right-rc.Left), int(rc.Bottom-rc.Top)
	screen := screenRect()
	x := screen.X + (screen.W-ow)/2
	y := screen.Y + (screen.H-oh)/3
	title := utf16Of("Perfil y respuestas")
	hwnd := call(procCreateWindowExW, wsExAppWindow, ptrOf(&profileClsName[0]), ptrOf(&title[0]),
		style, uintptr(x), uintptr(y), uintptr(ow), uintptr(oh), owner.hwnd, 0, owner.owner.inst, 0)
	runtime.KeepAlive(title)
	if hwnd == 0 {
		return nil, fmt.Errorf("no pude crear la ventana: %w", windows.GetLastError())
	}
	p.hwnd = hwnd
	p.createControls()
	p.load(owner.profile, owner.verify)
	profileCur = p
	call(procShowWindow, hwnd, swShow)
	call(procSetForegroundWindow, hwnd)
	call(procSetFocus, p.spec)
	return p, nil
}

// createControls lays out the profile screen. The rows are in logical pixels.
func (p *profileWin) createControls() {
	p.label("¿Qué quieres de Compañero?", 16, 14, 400, 20)
	labels := goalsLabels()
	for i, text := range labels {
		col, row := i/3, i%3
		x := 24 + col*200
		y := 40 + row*22
		p.goals[i] = p.check(text, x, y, 190, 22, uintptr(idProfGoal0+i))
	}

	p.label("Rol preferido (opcional)", 16, 120, 200, 20)
	p.spec = p.edit(16, 142, 404, 24)
	setCueBanner(p.spec, "Por ejemplo: DPS mêlée, tanque, sanador")

	p.label(fmt.Sprintf("Estilo de juego (hasta %d caracteres)", config.MaxProfileNotes), 16, 176, 404, 20)
	p.notes = p.edit(16, 198, 404, 24)
	call(procSendMessageW, p.notes, emSetLimitText, uintptr(config.MaxProfileNotes), 0)

	p.label("Tono de las respuestas:", 16, 232, 170, 20)
	p.toneBrief = p.radio("Breve", 190, 232, 90, 22, true, idProfTone)
	p.toneDetail = p.radio("Detallado", 290, 232, 130, 22, false, idProfTone+1)

	p.hardcore = p.check("Personaje hardcore (una sola vida)", 16, 262, 404, 22, 0)
	p.verify = p.check("Verificar las respuestas antes de mostrarlas", 16, 288, 404, 22, 0)

	p.errLabel = p.label("", 16, 318, 404, 20)

	p.cancelBtn = p.child("BUTTON", "Cancelar", wsChild|wsVisible|wsTabStop, 276, 336, 70, 28, idCancel)
	p.okBtn = p.child("BUTTON", "Aceptar", wsChild|wsVisible|wsTabStop|bsDefPushButton, 354, 336, 70, 28, idOK)

	for _, h := range append(p.goals[:], p.spec, p.notes, p.toneBrief, p.toneDetail, p.hardcore,
		p.verify, p.errLabel, p.okBtn, p.cancelBtn) {
		call(procSendMessageW, h, wmSetFont, p.owner.owner.fonts.ui, 1)
	}
}

// load fills the screen from the draft.
func (p *profileWin) load(prof config.Profile, verify bool) {
	f := formFromProfile(prof, verify)
	for i, g := range config.AllGoals {
		setCheck(p.goals[i], slices.Contains(f.Goals, g))
	}
	setWindowText(p.spec, f.Spec)
	setWindowText(p.notes, f.Notes)
	setCheck(p.toneBrief, f.Tone == config.ToneBrief)
	setCheck(p.toneDetail, f.Tone == config.ToneDetailed)
	setCheck(p.hardcore, f.Hardcore)
	setCheck(p.verify, f.Verify)
}

// read returns the screen's values as a form.
func (p *profileWin) read() profileForm {
	f := profileForm{
		Spec:     getWindowText(p.spec),
		Notes:    getWindowText(p.notes),
		Tone:     config.ToneBrief,
		Hardcore: getCheck(p.hardcore),
		Verify:   getCheck(p.verify),
	}
	if getCheck(p.toneDetail) {
		f.Tone = config.ToneDetailed
	}
	for i, g := range config.AllGoals {
		if getCheck(p.goals[i]) {
			f.Goals = append(f.Goals, g)
		}
	}
	return f
}

// accept checks the screen and, when it is valid, puts the values in the settings draft.
func (p *profileWin) accept() {
	f := p.read()
	prof, err := f.profile()
	if err != nil {
		setWindowText(p.errLabel, err.Error())
		call(procInvalidateRect, p.errLabel, 0, 1)
		return
	}
	p.owner.profile = prof
	p.owner.verify = f.Verify
	p.owner.updateProfileLabel()
	p.owner.setStatus("Perfil listo: pulsa Guardar en Ajustes para aplicarlo.", colMeta)
	call(procDestroyWindow, p.hwnd)
}

// profileProc is the window procedure of the profile screen.
func profileProc(hwnd, msg, wp, lp uintptr) uintptr {
	p := profileCur
	if p == nil || p.hwnd != hwnd {
		return call(procDefWindowProcW, hwnd, msg, wp, lp)
	}
	switch msg {
	case wmCommand:
		switch loWordCmd(wp) {
		case idOK:
			p.accept()
		case idCancel:
			call(procDestroyWindow, hwnd)
		}
		return 0
	case wmCtlColorEdit, wmCtlColorList:
		return p.owner.ctlColor(wp, colText, colInput, p.owner.owner.brushes.input)
	case wmCtlColorBtn, wmCtlColorStat:
		text := uint32(colText)
		if lp == p.errLabel {
			text = colBad
		}
		call(procSetBkMode, wp, transparent)
		return p.owner.ctlColor(wp, text, colBG, p.owner.owner.brushes.bg)
	case wmDestroy:
		if p.owner.profileDlg == p {
			p.owner.profileDlg = nil
		}
		profileCur = nil
		return 0
	}
	return call(procDefWindowProcW, hwnd, msg, wp, lp)
}

// updateProfileLabel shows the draft's summary on the settings screen.
func (s *settingsWin) updateProfileLabel() {
	setWindowText(s.profileLabel, profileSummary(s.profile))
}
