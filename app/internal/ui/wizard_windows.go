//go:build windows

package ui

import (
	"path/filepath"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// wizardView is the first-run wizard shown in the panel in place of the conversation.
type wizardView struct {
	items     []checkItem
	forever   string // the _forever_ folder the checks found, "" when none
	checking  bool
	note      string // the result of the last button, one line
	noteColor uint32
}

// wizardResult is what a round of checks sends back to the UI thread.
type wizardResult struct {
	items   []checkItem
	forever string
}

// wizardInfo is the explanation under the checklist: the window mode WoW needs and the
// command that loads the data.
const wizardInfo = "En WoW, usa el modo «Sin bordes» (Opciones de vídeo → Modo de ventana): " +
	"la franja de píxeles de la esquina superior izquierda debe verse. " +
	"Luego escribe /compa sync en el juego para cargar tus datos."

// startWizard opens the wizard in the panel and runs the checks. It shows the panel
// without taking the focus when it was hidden.
func (w *window) startWizard() {
	if w.wiz != nil {
		w.show(true)
		return
	}
	w.closeReason()
	w.wiz = &wizardView{checking: true}
	w.setChatVisible(false)
	w.show(true)
	w.runChecks()
}

// endWizard returns the panel to the conversation.
func (w *window) endWizard() {
	w.wiz = nil
	w.setChatVisible(true)
	w.layoutChildren()
	w.refreshStatus()
	call(procSetFocus, w.input)
}

// setChatVisible shows or hides the conversation and the input row.
func (w *window) setChatVisible(on bool) {
	sw := uintptr(swHide)
	if on {
		sw = swShow
	}
	for _, h := range []uintptr{w.conv, w.input, w.check, w.send} {
		if h != 0 {
			call(procShowWindow, h, sw)
		}
	}
}

// runChecks runs the setup checks in the background; the result comes back through msgWizard.
// The checks only read files and run status commands (agy models, codex login status).
func (w *window) runChecks() {
	if w.wiz == nil {
		return
	}
	w.wiz.checking = true
	cfg := w.cfg
	in := setupInput{
		WoWPath:      cfg.WoWPath,
		AgyCommand:   cfg.AntigravityCommand,
		CodexCommand: cfg.CodexCommand,
		ExeDir:       w.opts.ExeDir,
	}
	go func() {
		in.KeyPresent = keyPresent(cfg)
		items, forever := runSetupChecks(osEnv{}, in)
		w.wizCh <- wizardResult{items: items, forever: forever}
		postMessage(w.hwnd, msgWizard, 0, 0)
	}()
}

// onWizardResults takes the newest result of the checks.
func (w *window) onWizardResults() {
	for {
		select {
		case r := <-w.wizCh:
			if w.wiz != nil {
				w.wiz.items = r.items
				w.wiz.forever = r.forever
				w.wiz.checking = false
			}
		default:
			w.invalidateConv()
			return
		}
	}
}

// invalidateConv repaints the conversation area, where the wizard is drawn.
func (w *window) invalidateConv() {
	rc := toRect(w.layout().Conv)
	call(procInvalidateRect, w.hwnd, ptrOf(&rc), 1)
}

// wizNote sets the one-line result under the checklist.
func (w *window) wizNote(text string, color uint32) {
	if w.wiz == nil {
		return
	}
	w.wiz.note = text
	w.wiz.noteColor = color
	w.invalidateConv()
}

// paintWizard draws the checklist, the explanation and the buttons in the conversation area.
func (w *window) paintWizard(hdc uintptr, l Layout) {
	wz := w.wiz
	area := wizardListArea(l.Conv, w.scale)
	pad := scaled(4, w.scale)
	lineH := scaled(20, w.scale)
	indent := scaled(22, w.scale)
	bottom := area.Y + area.H
	y := area.Y

	drawText(hdc, "Configuración de Compañero WoW", Rect{X: area.X, Y: y, W: area.W, H: lineH},
		dtLeft|dtVCenter|dtSingleLine|dtNoPrefix, w.fonts.title, colHeading)
	y += lineH + pad
	if wz.checking {
		drawText(hdc, "Comprobando…", Rect{X: area.X, Y: y, W: area.W, H: lineH},
			dtLeft|dtVCenter|dtSingleLine|dtNoPrefix, w.fonts.ui, colMeta)
		y += lineH + pad
	}
	for _, it := range wz.items {
		if y+lineH > bottom {
			break
		}
		mark, color := "❌", colBad
		if it.OK {
			mark, color = "✅", colOK
		}
		drawText(hdc, mark+"  "+it.Title, Rect{X: area.X, Y: y, W: area.W, H: lineH},
			dtLeft|dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix, w.fonts.ui, color)
		y += lineH
		h := measureText(hdc, it.Detail, area.W-indent, w.fonts.small)
		drawText(hdc, it.Detail, Rect{X: area.X + indent, Y: y, W: area.W - indent, H: h},
			dtLeft|dtWordBreak|dtNoPrefix, w.fonts.small, colMeta)
		y += h + 2*pad
	}
	if y < bottom {
		h := measureText(hdc, wizardInfo, area.W, w.fonts.small)
		if y+h <= bottom {
			drawText(hdc, wizardInfo, Rect{X: area.X, Y: y, W: area.W, H: h},
				dtLeft|dtWordBreak|dtNoPrefix, w.fonts.small, colMeta)
		}
	}
	if wz.note != "" {
		drawText(hdc, wz.note, Rect{X: area.X, Y: bottom - lineH, W: area.W, H: lineH},
			dtLeft|dtVCenter|dtEndEllipsis|dtSingleLine|dtNoPrefix, w.fonts.small, wz.noteColor)
	}

	for _, b := range wizardButtons(l.Conv, w.scale, wizardHasInstall(wz.items)) {
		color := uint32(colText)
		if !b.Enabled {
			color = colMeta
		}
		fillRect(hdc, b.Rect, w.brushes.button)
		frameRect(hdc, b.Rect, w.brushes.gold)
		drawText(hdc, b.Label, b.Rect, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix, w.fonts.ui, color)
	}
}

// measureText returns the height of text wrapped to width pixels.
func measureText(hdc uintptr, text string, width int, font uintptr) int {
	if width <= 0 {
		return 0
	}
	rc := rect{Right: int32(width)}
	t := utf16Of(text)
	call(procSelectObject, hdc, font)
	call(procDrawTextW, hdc, ptrOf(&t[0]), uintptr(len(t)-1), ptrOf(&rc), dtCalcRect|dtWordBreak|dtNoPrefix)
	return int(rc.Bottom - rc.Top)
}

// wizardClick runs the wizard button under (x, y), if any. It reports whether a button
// was hit.
func (w *window) wizardClick(x, y, cw, ch int) bool {
	l := computeLayout(cw, ch, w.scale)
	if !l.Conv.Contains(x, y) {
		return false
	}
	for _, b := range wizardButtons(l.Conv, w.scale, wizardHasInstall(w.wiz.items)) {
		if b.Rect.Contains(x, y) {
			if b.Enabled {
				w.wizardAction(b.Action)
			}
			return true
		}
	}
	return false
}

// wizardAction runs a wizard button.
func (w *window) wizardAction(action string) {
	switch action {
	case wizInstall:
		w.installAddonAction()
	case wizBrowse:
		w.browseAction()
	case wizRecheck:
		w.wizNote("", colMeta)
		w.runChecks()
	case wizDone:
		w.endWizard()
	}
}

// installAddonAction copies the addon into the _forever_ folder. It runs only on the click.
func (w *window) installAddonAction() {
	env := osEnv{}
	if w.wiz.forever == "" {
		w.wizNote("Primero indica la carpeta de WoW con «Buscar carpeta…».", colWarn)
		return
	}
	src := addonSource(env, w.opts.ExeDir)
	if src == "" {
		w.wizNote("No encuentro la carpeta del addon junto a la app.", colWarn)
		return
	}
	dst := addonDir(w.wiz.forever)
	if err := installAddon(src, dst); err != nil {
		w.wizNote("No pude instalar el addon: "+err.Error(), colBad)
		return
	}
	w.wizNote("Addon instalado en "+dst+". En WoW escribe /reload.", colOK)
	w.runChecks()
}

// browseAction lets the user point at the WoW folder, and keeps the _forever_ folder it
// contains.
func (w *window) browseAction() {
	dir, ok := browseFolder(w.hwnd, "Elige la carpeta _forever_ de World of Warcraft (o la carpeta World of Warcraft)")
	if !ok {
		return
	}
	env := osEnv{}
	var chosen string
	switch {
	case isForeverFolder(env, dir):
		chosen = dir
	case env.IsDir(filepath.Join(dir, foreverFolder)):
		chosen = filepath.Join(dir, foreverFolder)
	default:
		w.wizNote("Esa carpeta no es _forever_ ni contiene una carpeta _forever_.", colWarn)
		return
	}
	next := w.cfg
	next.WoWPath = chosen
	if err := w.saveConfig(next); err != nil {
		w.wizNote("No pude guardar la carpeta: "+err.Error(), colBad)
		return
	}
	w.wizNote("Carpeta guardada. Se usará al reiniciar la app.", colOK)
	w.runChecks()
}

// saveConfig writes next to config.json and makes it the configuration in use.
func (w *window) saveConfig(next config.Config) error {
	if w.cfgPath != "" {
		if err := next.Save(w.cfgPath); err != nil {
			return err
		}
	}
	w.cfg = next
	return nil
}

// keyPresent reports whether an NVIDIA key resolves for one of the API providers. The value
// is never returned.
func keyPresent(cfg config.Config) bool {
	for _, p := range cfg.APIProviders {
		if _, err := p.ResolveKey(); err == nil {
			return true
		}
	}
	return false
}
