package ui

// Wizard button actions.
const (
	wizInstall = "install"
	wizBrowse  = "browse"
	wizRecheck = "recheck"
	wizDone    = "done"
)

// wizardButton is one button under the wizard's checklist.
type wizardButton struct {
	Action  string
	Label   string
	Rect    Rect
	Enabled bool
}

// wizardButtons lays out the wizard buttons in two rows at the bottom of area: install and
// browse on the first, recheck and done on the second. Install is enabled only when the
// checklist offers it.
func wizardButtons(area Rect, scale float64, installEnabled bool) []wizardButton {
	gapPx := scaled(gap, scale)
	rowH := scaled(rowHeight, scale)
	half := (area.W - gapPx) / 2
	rightW := area.W - half - gapPx
	y2 := area.Y + area.H - rowH
	y1 := y2 - gapPx - rowH
	left := area.X
	right := area.X + half + gapPx
	return []wizardButton{
		{Action: wizInstall, Label: "Instalar addon", Rect: Rect{X: left, Y: y1, W: half, H: rowH}, Enabled: installEnabled},
		{Action: wizBrowse, Label: "Buscar carpeta…", Rect: Rect{X: right, Y: y1, W: rightW, H: rowH}, Enabled: true},
		{Action: wizRecheck, Label: "Comprobar de nuevo", Rect: Rect{X: left, Y: y2, W: half, H: rowH}, Enabled: true},
		{Action: wizDone, Label: "Terminar", Rect: Rect{X: right, Y: y2, W: rightW, H: rowH}, Enabled: true},
	}
}

// wizardListArea is the part of the conversation area above the buttons.
func wizardListArea(conv Rect, scale float64) Rect {
	gapPx := scaled(gap, scale)
	rows := 2*scaled(rowHeight, scale) + gapPx
	return Rect{X: conv.X, Y: conv.Y, W: conv.W, H: conv.H - rows - gapPx}
}

// wizardHasInstall reports whether any item offers the addon install.
func wizardHasInstall(items []checkItem) bool {
	for _, it := range items {
		if it.Action == "install-addon" {
			return true
		}
	}
	return false
}
