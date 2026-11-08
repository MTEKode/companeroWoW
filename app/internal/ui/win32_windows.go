//go:build windows

package ui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Thin bindings to the few Win32 calls the panel needs. Everything is plain syscall
// through x/sys; there is no cgo.

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modGDI32    = windows.NewLazySystemDLL("gdi32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")
	modShell32  = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW          = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW           = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW            = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow             = modUser32.NewProc("DestroyWindow")
	procShowWindow                = modUser32.NewProc("ShowWindow")
	procSetWindowPos              = modUser32.NewProc("SetWindowPos")
	procMoveWindow                = modUser32.NewProc("MoveWindow")
	procGetWindowRect             = modUser32.NewProc("GetWindowRect")
	procGetClientRect             = modUser32.NewProc("GetClientRect")
	procIsWindowVisible           = modUser32.NewProc("IsWindowVisible")
	procSetForegroundWindow       = modUser32.NewProc("SetForegroundWindow")
	procSetFocus                  = modUser32.NewProc("SetFocus")
	procReleaseCapture            = modUser32.NewProc("ReleaseCapture")
	procTranslateMessage          = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW          = modUser32.NewProc("DispatchMessageW")
	procPostMessageW              = modUser32.NewProc("PostMessageW")
	procPostQuitMessage           = modUser32.NewProc("PostQuitMessage")
	procSendMessageW              = modUser32.NewProc("SendMessageW")
	procSetLayeredWindowAttrs     = modUser32.NewProc("SetLayeredWindowAttributes")
	procSetWindowRgn              = modUser32.NewProc("SetWindowRgn")
	procBeginPaint                = modUser32.NewProc("BeginPaint")
	procEndPaint                  = modUser32.NewProc("EndPaint")
	procFillRect                  = modUser32.NewProc("FillRect")
	procFrameRect                 = modUser32.NewProc("FrameRect")
	procDrawTextW                 = modUser32.NewProc("DrawTextW")
	procInvalidateRect            = modUser32.NewProc("InvalidateRect")
	procGetDpiForWindow           = modUser32.NewProc("GetDpiForWindow")
	procGetDpiForSystem           = modUser32.NewProc("GetDpiForSystem")
	procSetProcessDpiAwarenessCtx = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware        = modUser32.NewProc("SetProcessDPIAware")
	procCreatePopupMenu           = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW               = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenu            = modUser32.NewProc("TrackPopupMenu")
	procDestroyMenu               = modUser32.NewProc("DestroyMenu")
	procGetCursorPos              = modUser32.NewProc("GetCursorPos")
	procGetSystemMetrics          = modUser32.NewProc("GetSystemMetrics")
	procFindWindowW               = modUser32.NewProc("FindWindowW")
	procMessageBoxW               = modUser32.NewProc("MessageBoxW")
	procLoadCursorW               = modUser32.NewProc("LoadCursorW")
	procEnableWindow              = modUser32.NewProc("EnableWindow")
	procGetWindowTextW            = modUser32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW      = modUser32.NewProc("GetWindowTextLengthW")
	procSetWindowTextW            = modUser32.NewProc("SetWindowTextW")
	procSetWindowLongPtrW         = modUser32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtrW         = modUser32.NewProc("GetWindowLongPtrW")
	procCallWindowProcW           = modUser32.NewProc("CallWindowProcW")
	procGetKeyState               = modUser32.NewProc("GetKeyState")

	procCreateFontIndirectW = modGDI32.NewProc("CreateFontIndirectW")
	procDeleteObject        = modGDI32.NewProc("DeleteObject")
	procCreateSolidBrush    = modGDI32.NewProc("CreateSolidBrush")
	procCreateRoundRectRgn  = modGDI32.NewProc("CreateRoundRectRgn")
	procSelectObject        = modGDI32.NewProc("SelectObject")
	procSetTextColor        = modGDI32.NewProc("SetTextColor")
	procSetBkColor          = modGDI32.NewProc("SetBkColor")
	procSetBkMode           = modGDI32.NewProc("SetBkMode")

	procGetModuleHandleW = modKernel32.NewProc("GetModuleHandleW")
	procLoadLibraryW     = modKernel32.NewProc("LoadLibraryW")

	procShellExecuteW = modShell32.NewProc("ShellExecuteW")
)

// procSetWindowDisplayAffinity hides the panel from screen captures.
var procSetWindowDisplayAffinity = modUser32.NewProc("SetWindowDisplayAffinity")

// wdaExcludeFromCapture keeps the window out of screen captures (Windows 10 2004 and later).
const wdaExcludeFromCapture = 0x00000011

// Window messages.
const (
	wmNull          = 0x0000
	wmMove          = 0x0003
	wmSize          = 0x0005
	wmClose         = 0x0010
	wmDestroy       = 0x0002
	wmPaint         = 0x000F
	wmSetText       = 0x000C
	wmGetTextLength = 0x000E
	wmEraseBkgnd    = 0x0014
	wmSetFont       = 0x0030
	wmGetMinMaxInfo = 0x0024
	wmDrawItem      = 0x002B
	wmNotify        = 0x004E
	wmKeyDown       = 0x0100
	wmChar          = 0x0102
	wmCommand       = 0x0111
	wmCtlColorEdit  = 0x0133
	wmCtlColorBtn   = 0x0135
	wmCtlColorStat  = 0x0138
	wmNcCalcSize    = 0x0083
	wmNcHitTest     = 0x0084
	wmNcLButtonDown = 0x00A1
	wmKeyDownAlt    = 0x0104
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmRButtonUp     = 0x0205
	wmExitSizeMove  = 0x0232
	wmMoving        = 0x0216
	wmSizing        = 0x0214
	wmDpiChanged    = 0x02E0
	wmApp           = 0x8000
	wmUser          = 0x0400
)

// Edit, button and RichEdit messages.
const (
	emSetSel          = 0x00B1
	emScrollCaret     = 0x00B7
	emScroll          = 0x00B5
	emReplaceSel      = 0x00C2
	emSetReadOnly     = 0x00CF
	bmGetCheck        = 0x00F0
	bmSetCheck        = 0x00F1
	emSetBkgndColor   = wmUser + 67
	emSetCharFormat   = wmUser + 68
	emSetEventMask    = wmUser + 69
	emGetTextRange    = wmUser + 75
	enLink            = 0x070B
	enmLink           = 0x04000000
	cfmBold           = 0x00000001
	cfmItalic         = 0x00000002
	cfmUnderline      = 0x00000004
	cfmLink           = 0x00000020
	cfmSize           = 0x80000000
	cfmColor          = 0x40000000
	cfmFace           = 0x20000000
	cfmBackColor      = 0x04000000
	cfeBold           = 0x00000001
	cfeUnderline      = 0x00000004
	cfeLink           = 0x00000020
	scfSelection      = 0x0001
	sbBottom          = 7
	bstChecked        = 1
	bsAutoCheckbox    = 0x00000003
	bsOwnerDraw       = 0x0000000B
	esMultiline       = 0x0004
	esAutoVScroll     = 0x0040
	esReadOnly        = 0x0800
	esNoHideSel       = 0x0100
	wsPopup           = 0x80000000
	wsChild           = 0x40000000
	wsVisible         = 0x10000000
	wsVScroll         = 0x00200000
	wsTabStop         = 0x00010000
	wsClipChildren    = 0x02000000
	wsThickFrame      = 0x00040000
	wsExTopmost       = 0x00000008
	wsExToolWindow    = 0x00000080
	wsExLayered       = 0x00080000
	lwaAlpha          = 0x00000002
	swHide            = 0
	swShowNormal      = 1
	swShowNoActivate  = 4
	swShow            = 5
	swRestore         = 9
	swpNoSize         = 0x0001
	swpNoMove         = 0x0002
	swpNoZOrder       = 0x0004
	swpNoActivate     = 0x0010
	swpFrameChanged   = 0x0020
	hwndTopmost       = ^uintptr(0) // (HWND)-1
	hwndNoTopmost     = ^uintptr(1) // (HWND)-2
	htCaption         = 2
	smCxScreen        = 0
	smCyScreen        = 1
	mfString          = 0x00000000
	mfSeparator       = 0x00000800
	tpmRightButton    = 0x0002
	tpmReturnCmd      = 0x0100
	dtLeft            = 0x0000
	dtCenter          = 0x0001
	dtRight           = 0x0002
	dtVCenter         = 0x0004
	dtSingleLine      = 0x0020
	dtEndEllipsis     = 0x8000
	dtNoPrefix        = 0x0800
	transparent       = 1
	fwNormal          = 400
	fwSemibold        = 600
	defaultCharset    = 1
	clearTypeQuality  = 5
	gwlpWndProc       = ^uintptr(3) // -4
	cwUseDefault      = 0x80000000
	dpiContextPerMon2 = ^uintptr(3) // -4 (DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2)
	idcArrow          = 32512
	vkReturn          = 0x0D
	vkEscape          = 0x1B
	vkShift           = 0x10
	odsDisabled       = 0x0004
	mbOK              = 0x00000000
	mbIconError       = 0x00000010
	sizeMinimized     = 1
)

// rect is the Win32 RECT.
type rect struct{ Left, Top, Right, Bottom int32 }

// point is the Win32 POINT.
type point struct{ X, Y int32 }

// paintStruct is the Win32 PAINTSTRUCT (72 bytes on 64-bit Windows).
type paintStruct struct {
	HDC       uintptr
	Erase     int32
	Paint     rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

// wndClassEx is the Win32 WNDCLASSEXW.
type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

// minMaxInfo is the Win32 MINMAXINFO.
type minMaxInfo struct {
	Reserved     point
	MaxSize      point
	MaxPosition  point
	MinTrackSize point
	MaxTrackSize point
}

// drawItemStruct is the Win32 DRAWITEMSTRUCT.
type drawItemStruct struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HWndItem   uintptr
	HDC        uintptr
	RcItem     rect
	ItemData   uintptr
}

// nmhdr is the Win32 NMHDR.
type nmhdr struct {
	HWndFrom uintptr
	IDFrom   uintptr
	Code     uint32
}

// charRange is the Win32 CHARRANGE.
type charRange struct{ Min, Max int32 }

// enLinkMsg is the Win32 ENLINK notification.
type enLinkMsg struct {
	Hdr    nmhdr
	Msg    uint32
	_      uint32
	WParam uintptr
	LParam uintptr
	Range  charRange
}

// textRange is the Win32 TEXTRANGEW.
type textRange struct {
	Range charRange
	Text  *uint16
}

// charFormat2 is the Win32 CHARFORMAT2W (116 bytes).
type charFormat2 struct {
	CbSize          uint32
	DwMask          uint32
	DwEffects       uint32
	YHeight         int32
	YOffset         int32
	CrTextColor     uint32
	BCharSet        uint8
	BPitchAndFamily uint8
	SzFaceName      [32]uint16
	WWeight         uint16
	SSpacing        int16
	CrBackColor     uint32
	LCID            uint32
	DwReserved      uint32
	SStyle          int16
	WKerning        uint16
	BUnderlineType  uint8
	BAnimation      uint8
	BRevAuthor      uint8
	BReserved1      uint8
}

// logFont is the Win32 LOGFONTW (92 bytes).
type logFont struct {
	Height, Width, Escapement, Orientation, Weight int32
	Italic, Underline, StrikeOut, CharSet          uint8
	OutPrecision, ClipPrecision, Quality, Pitch    uint8
	FaceName                                       [32]uint16
}

// call invokes a procedure and returns its result. Errors are not checked here; the
// callers check the result value where it matters.
func call(p *windows.LazyProc, args ...uintptr) uintptr {
	r, _, _ := p.Call(args...)
	return r
}

// ptrOf returns the address of v for a Win32 pointer argument.
func ptrOf[T any](v *T) uintptr {
	return uintptr(unsafe.Pointer(v))
}

// fromUintptr turns a pointer received as a message parameter back into a pointer. The
// memory belongs to the system (a notification struct), not to the Go heap.
func fromUintptr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

// loWord and hiWord return the signed 16-bit halves of a message parameter, which is how
// mouse coordinates arrive.
func loWord(v uintptr) int { return int(int16(uint16(v & 0xFFFF))) }
func hiWord(v uintptr) int { return int(int16(uint16((v >> 16) & 0xFFFF))) }

// utf16Of converts s to a NUL-terminated UTF-16 slice.
func utf16Of(s string) []uint16 {
	return windows.StringToUTF16(s)
}

// rgbColor builds a COLORREF (0x00BBGGRR).
func rgbColor(r, g, b uint8) uint32 {
	return uint32(r) | uint32(g)<<8 | uint32(b)<<16
}

// toRect converts a Rect to a RECT.
func toRect(r Rect) rect {
	return rect{Left: int32(r.X), Top: int32(r.Y), Right: int32(r.X + r.W), Bottom: int32(r.Y + r.H)}
}

// fromRect converts a RECT to a Rect.
func fromRect(r rect) Rect {
	return Rect{X: int(r.Left), Y: int(r.Top), W: int(r.Right - r.Left), H: int(r.Bottom - r.Top)}
}

// fillRect paints r with brush.
func fillRect(hdc uintptr, r Rect, brush uintptr) {
	rc := toRect(r)
	call(procFillRect, hdc, ptrOf(&rc), brush)
}

// frameRect paints a one-pixel frame inside r with brush.
func frameRect(hdc uintptr, r Rect, brush uintptr) {
	rc := toRect(r)
	call(procFrameRect, hdc, ptrOf(&rc), brush)
}

// drawText draws one line of text in r with the given font and colour.
func drawText(hdc uintptr, text string, r Rect, flags uint32, font uintptr, color uint32) {
	rc := toRect(r)
	t := utf16Of(text)
	call(procSelectObject, hdc, font)
	call(procSetTextColor, hdc, uintptr(color))
	call(procDrawTextW, hdc, ptrOf(&t[0]), uintptr(len(t)-1), ptrOf(&rc), uintptr(flags))
}

// newFont creates a Segoe UI (or other face) font of pt points at dpi.
func newFont(face string, pt float64, dpi uint32, weight int32) uintptr {
	lf := logFont{
		Height:  -int32(pt*float64(dpi)/72 + 0.5),
		Weight:  weight,
		CharSet: defaultCharset,
		Quality: clearTypeQuality,
	}
	copy(lf.FaceName[:31], utf16Of(face))
	return call(procCreateFontIndirectW, ptrOf(&lf))
}

// getWindowText returns the text of a control.
func getWindowText(h uintptr) string {
	n := call(procGetWindowTextLengthW, h)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	call(procGetWindowTextW, h, ptrOf(&buf[0]), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}

// setWindowText sets the text of a control.
func setWindowText(h uintptr, s string) {
	call(procSetWindowTextW, h, ptrOf(&utf16Of(s)[0]))
}

// postMessage queues a message for a window; it is safe from any goroutine.
func postMessage(h uintptr, msg uint32, wp, lp uintptr) {
	call(procPostMessageW, h, uintptr(msg), wp, lp)
}

// isVisible reports whether a window is shown. Safe from any goroutine.
func isVisible(h uintptr) bool {
	return call(procIsWindowVisible, h) != 0
}

// shiftDown reports whether a Shift key is held.
func shiftDown() bool {
	return int16(call(procGetKeyState, vkShift)) < 0
}

// errorBox shows a Spanish message box with an error icon.
func errorBox(text, caption string) {
	t := utf16Of(text)
	c := utf16Of(caption)
	call(procMessageBoxW, 0, ptrOf(&t[0]), ptrOf(&c[0]), mbOK|mbIconError)
}
