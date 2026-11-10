//go:build windows

package ui

import "golang.org/x/sys/windows"

// Bindings for the settings screen, the wizard, the tray icon and the feedback buttons.
// They sit beside win32_windows.go, which holds the panel's own.

var (
	modShell32Ux = windows.NewLazySystemDLL("shell32.dll")
	modOle32     = windows.NewLazySystemDLL("ole32.dll")

	procShellNotifyIconW       = modShell32Ux.NewProc("Shell_NotifyIconW")
	procSHBrowseForFolderW     = modShell32Ux.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW   = modShell32Ux.NewProc("SHGetPathFromIDListW")
	procCoTaskMemFree          = modOle32.NewProc("CoTaskMemFree")
	procCoInitializeEx         = modOle32.NewProc("CoInitializeEx")
	procLoadIconW              = modUser32.NewProc("LoadIconW")
	procRegisterWindowMessageW = modUser32.NewProc("RegisterWindowMessageW")
	procAdjustWindowRectEx     = modUser32.NewProc("AdjustWindowRectEx")
	procIsDialogMessageW       = modUser32.NewProc("IsDialogMessageW")
	procCreateIconFromResource = modUser32.NewProc("CreateIconFromResourceEx")
)

// Shell notification (the tray icon).
const (
	nimAdd        = 0x00000000
	nimModify     = 0x00000001
	nimDelete     = 0x00000002
	nifMessage    = 0x00000001
	nifIcon       = 0x00000002
	nifTip        = 0x00000004
	nifInfo       = 0x00000010 // balloon text and title
	niifInfo      = 0x00000001 // the balloon's icon: information
	niifNoSound   = 0x00000010
	niifQuietTime = 0x00000080 // no balloon during the system's quiet time (Focus assist)
	// ninBalloonUserClick is the callback message when the user clicks the balloon.
	ninBalloonUserClick = 0x0405
	trayUID             = 1
	idiApplication      = 32512
)

// Messages used by the tray icon and the wizard.
const (
	wmLButtonUpTray = 0x0202
	wmRButtonUpTray = 0x0205
)

// Controls and styles of the settings screen.
const (
	bsDefPushButton         = 0x00000001
	bsAutoRadio             = 0x00000009
	wsGroup                 = 0x00020000
	bsPushButton            = 0x00000000
	esAutoHScroll           = 0x00000080
	wsBorder                = 0x00800000
	wsCaption               = 0x00C00000
	wsSysMenu               = 0x00080000
	lbsNotify               = 0x00000001
	ssLeft                  = 0x00000000
	lbGetCount              = 0x018B
	lbGetText               = 0x0189
	lbGetCurSel             = 0x0188
	lbSetCurSel             = 0x0186
	lbResetContent          = 0x0184
	lbAddString             = 0x0180
	wmCtlColorList          = 0x0134
	emSetCueBanner          = 0x1501
	wmSetIcon               = 0x0080
	iconSmall               = 0
	iconBig                 = 1
	dtCalcRect              = 0x00000400
	dtWordBreak             = 0x00000010
	dtEditControl           = 0x00002000
	idOK                    = 1
	idCancel                = 2
	bnClicked               = 0
	coinitApartmentThreaded = 0x2
	bifReturnOnlyFSDirs     = 0x00000001
	bifNewDialogStyle       = 0x00000040
	bifNoNewFolderButton    = 0x00000200
	maxPathUnits            = 260
	swpNoZOrderUx           = 0x0004
)

// browseInfo is the Win32 BROWSEINFOW used by SHBrowseForFolder.
type browseInfo struct {
	HWndOwner      uintptr
	PidlRoot       uintptr
	PszDisplayName *uint16
	LpszTitle      *uint16
	UlFlags        uint32
	Lpfn           uintptr
	LParam         uintptr
	IImage         int32
}

// notifyIconData is the Win32 NOTIFYICONDATAW (976 bytes on 64-bit Windows, the layout of
// Windows 2000 and later).
type notifyIconData struct {
	CbSize            uint32
	HWnd              uintptr
	UID               uint32
	UFlags            uint32
	UCallbackMessage  uint32
	HIcon             uintptr
	SzTip             [128]uint16
	DwState           uint32
	DwStateMask       uint32
	SzInfo            [256]uint16
	UTimeoutOrVersion uint32
	SzInfoTitle       [64]uint16
	DwInfoFlags       uint32
	GuidItem          windows.GUID
	HBalloonIcon      uintptr
}
