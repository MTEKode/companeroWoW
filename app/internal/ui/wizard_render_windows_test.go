//go:build windows

package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// The window of the panel is excluded from screen captures (WDA_EXCLUDEFROMCAPTURE), so a
// PrintWindow of it is black. This test draws the wizard with the same paint code into an
// offscreen bitmap instead, using the real setup checks of this machine.
//
// It runs only when COMPANERO_RENDER_DIR names a folder, which receives wizard.png:
//
//	COMPANERO_RENDER_DIR=<folder> go test ./internal/ui -run TestRenderWizardPNG -v

var (
	procGetDCRender            = modUser32.NewProc("GetDC")
	procReleaseDCRender        = modUser32.NewProc("ReleaseDC")
	procCreateCompatibleDC     = modGDI32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = modGDI32.NewProc("CreateCompatibleBitmap")
	procDeleteDC               = modGDI32.NewProc("DeleteDC")
	procGetDIBits              = modGDI32.NewProc("GetDIBits")
)

// bitmapInfoHeader is the Win32 BITMAPINFOHEADER.
type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
}

func TestRenderWizardPNG(t *testing.T) {
	dir := os.Getenv("COMPANERO_RENDER_DIR")
	if dir == "" {
		t.Skip("set COMPANERO_RENDER_DIR to write the wizard image")
	}
	const cw, ch = 420, 560 // the default panel size at 96 DPI

	cfg := config.Defaults()
	w := &window{cfg: cfg, scale: 1, dpi: 96, reasonFor: -1}
	w.makeBrushes()
	w.makeFonts()
	// The folder of the executable, where the addon copy is looked for; the app's dist folder
	// when COMPANERO_RENDER_EXEDIR names it.
	exeDir := os.Getenv("COMPANERO_RENDER_EXEDIR")
	if exeDir == "" {
		exeDir = filepath.Dir(os.Args[0])
	}
	items, forever := runSetupChecks(osEnv{}, setupInput{
		WoWPath:      cfg.WoWPath,
		AgyCommand:   cfg.AntigravityCommand,
		CodexCommand: cfg.CodexCommand,
		KeyPresent:   keyPresent(cfg),
		ExeDir:       exeDir,
	})
	w.wiz = &wizardView{items: items, forever: forever}

	screen := call(procGetDCRender, 0)
	mem := call(procCreateCompatibleDC, screen)
	bmp := call(procCreateCompatibleBitmap, screen, cw, ch)
	old := call(procSelectObject, mem, bmp)
	call(procSetBkMode, mem, transparent) // the panel's paint sets this too
	fillRect(mem, Rect{W: cw, H: ch}, w.brushes.bg)
	w.paintWizard(mem, computeLayout(cw, ch, 1))

	bi := bitmapInfo{Header: bitmapInfoHeader{
		Size: 40, Width: cw, Height: -ch, Planes: 1, BitCount: 32,
	}}
	buf := make([]byte, cw*ch*4)
	call(procGetDIBits, mem, bmp, 0, ch, ptrOf(&buf[0]), ptrOf(&bi), 0)
	call(procSelectObject, mem, old)
	call(procDeleteObject, bmp)
	call(procDeleteDC, mem)
	call(procReleaseDCRender, 0, screen)

	img := image.NewRGBA(image.Rect(0, 0, cw, ch))
	for i := 0; i < cw*ch; i++ {
		img.Pix[4*i+0] = buf[4*i+2]
		img.Pix[4*i+1] = buf[4*i+1]
		img.Pix[4*i+2] = buf[4*i+0]
		img.Pix[4*i+3] = 0xFF
	}
	out := filepath.Join(dir, "wizard.png")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%d checks)", out, len(items))
}
