//go:build windows

package gamemode

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// NewSystemAnnouncer returns the announcer that uses the Windows speech voice and the Windows
// sound player.
func NewSystemAnnouncer(mode string, volume int) *Announcer {
	return NewAnnouncer(sapiSpeaker{}, winmmPlayer{}, mode, volume)
}

var (
	modOle32  = windows.NewLazySystemDLL("ole32.dll")
	modWinmm  = windows.NewLazySystemDLL("winmm.dll")
	procCoIni = modOle32.NewProc("CoInitializeEx")
	procCoUni = modOle32.NewProc("CoUninitialize")
	procCoCre = modOle32.NewProc("CoCreateInstance")
	procPlayS = modWinmm.NewProc("PlaySoundW")
)

// COM and SAPI identifiers.
var (
	// CLSID_SpVoice: the SAPI 5 voice object.
	clsidSpVoice = windows.GUID{Data1: 0x96749377, Data2: 0x3391, Data3: 0x11D2, Data4: [8]byte{0x9E, 0xE3, 0x00, 0xC0, 0x4F, 0x79, 0x73, 0x96}}
	// IID_ISpVoice.
	iidSpVoice = windows.GUID{Data1: 0x6C44DF74, Data2: 0x72B9, Data3: 0x4992, Data4: [8]byte{0xA1, 0xEC, 0xEF, 0x99, 0x6E, 0x04, 0x22, 0xD4}}
)

const (
	coinitApartmentThreaded = 0x2
	clsctxAll               = 0x17 // INPROC_SERVER | INPROC_HANDLER | LOCAL_SERVER
	spfIsXML                = 0x10 // the text is SAPI XML: the volume tag sets the level
	sndMemory               = 0x0004
	sndNoDefault            = 0x0002
	rpcEChangedMode         = 0x80010106
)

// ISpVoice vtable slots (IUnknown: 0 QueryInterface, 2 Release; ISpVoice: 20 Speak).
const (
	vtblRelease = 2
	vtblSpeak   = 20
)

// sapiSpeaker speaks through SAPI's ISpVoice on its own thread, so the COM apartment belongs to
// that thread only.
type sapiSpeaker struct{}

// Speak says text with the default voice at the given volume (percent). It returns when the
// phrase has been said, or with an error when the voice is not available.
func (sapiSpeaker) Speak(text string, volume int) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		done <- speakOnThread(text, volume)
	}()
	return <-done
}

// speakOnThread does the COM calls of one phrase. The voice object is released before returning.
func speakOnThread(text string, volume int) error {
	hr, _, _ := procCoIni.Call(0, coinitApartmentThreaded)
	if int32(hr) < 0 && uint32(hr) != rpcEChangedMode {
		return fmt.Errorf("COM no inició: %#x", uint32(hr))
	}
	// S_OK and S_FALSE both add a reference that CoUninitialize removes; a mode change by another
	// component is left as it is.
	if hr == 0 || hr == 1 {
		defer procCoUni.Call()
	}
	var obj unsafe.Pointer
	hr, _, _ = procCoCre.Call(
		uintptr(unsafe.Pointer(&clsidSpVoice)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidSpVoice)), uintptr(unsafe.Pointer(&obj)))
	if int32(hr) < 0 || obj == nil {
		return fmt.Errorf("no hay voz de Windows (SAPI): %#x", uint32(hr))
	}
	this := uintptr(obj)
	vt := (*[64]uintptr)(*(*unsafe.Pointer)(obj))
	defer syscall.SyscallN(vt[vtblRelease], this)

	xml := fmt.Sprintf(`<volume level="%d">%s</volume>`, min(max(volume, 0), 100), escapeXML(text))
	pw, err := windows.UTF16PtrFromString(xml)
	if err != nil {
		return errors.New("texto de voz no válido")
	}
	r, _, _ := syscall.SyscallN(vt[vtblSpeak], this, uintptr(unsafe.Pointer(pw)), spfIsXML, 0)
	runtime.KeepAlive(pw)
	runtime.KeepAlive(obj)
	if int32(r) < 0 {
		return fmt.Errorf("la voz no pudo hablar: %#x", uint32(r))
	}
	return nil
}

// escapeXML escapes the characters that SAPI's XML reads as markup.
func escapeXML(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			out = append(out, "&amp;"...)
		case '<':
			out = append(out, "&lt;"...)
		case '>':
			out = append(out, "&gt;"...)
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

// winmmPlayer plays a WAV held in memory with PlaySound, and waits for it to finish.
type winmmPlayer struct{}

// PlayWAV plays the WAV bytes. The buffer must stay valid while the sound plays, which the
// synchronous call guarantees.
func (winmmPlayer) PlayWAV(wav []byte) error {
	if len(wav) == 0 {
		return errors.New("sonido vacío")
	}
	ok, _, err := procPlayS.Call(uintptr(unsafe.Pointer(&wav[0])), 0, sndMemory|sndNoDefault)
	runtime.KeepAlive(wav)
	if ok == 0 {
		return fmt.Errorf("PlaySound no sonó: %v", err)
	}
	return nil
}
