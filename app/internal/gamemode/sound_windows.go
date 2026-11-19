//go:build windows

package gamemode

import (
	"errors"
	"fmt"
	"log"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// NewSystemAnnouncer returns the announcer that uses the Windows speech voice and the Windows
// sound player (waveOut).
func NewSystemAnnouncer(mode string, volume int) *Announcer {
	return NewAnnouncer(sapiSpeaker{}, waveOutPlayer{}, mode, volume)
}

var (
	modOle32  = windows.NewLazySystemDLL("ole32.dll")
	modWinmm  = windows.NewLazySystemDLL("winmm.dll")
	procCoIni = modOle32.NewProc("CoInitializeEx")
	procCoUni = modOle32.NewProc("CoUninitialize")
	procCoCre = modOle32.NewProc("CoCreateInstance")
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
	spfIsXML                = 0x10 // the text is SAPI XML: the volume and voice tags apply
	rpcEChangedMode         = 0x80010106
)

// ISpVoice vtable slots (IUnknown: 0 QueryInterface, 2 Release; ISpVoice: 20 Speak).
const (
	vtblRelease = 2
	vtblSpeak   = 20
)

// errNoVoices is the speaker's error when Windows has no speech voice installed. The announcer
// then plays the tone.
var errNoVoices = errors.New("no hay voces instaladas en Windows (SAPI)")

// sapiSpeaker speaks through SAPI's ISpVoice on its own thread, so the COM apartment belongs to
// that thread only. It picks the voice from the installed ones (see ChooseVoice).
type sapiSpeaker struct{}

// Speak says text at the given volume (percent). It uses the first installed Spanish voice; when
// that fails it uses the default voice. It returns when the phrase has been said, or with an error
// when no voice is installed or none can speak.
func (sapiSpeaker) Speak(text string, volume int) error {
	choice, ok := ChooseVoice(installedVoices())
	if !ok {
		return errNoVoices
	}
	err := speakAsync(text, volume, choice.Required())
	if err != nil && choice.Required() != "" {
		log.Printf("gamemode: la voz %s no pudo hablar (%v); pruebo la voz predeterminada", choice.Describe(), err)
		if err2 := speakAsync(text, volume, ""); err2 == nil {
			log.Printf("gamemode: aviso con la voz predeterminada de Windows")
			return nil
		}
		return err
	}
	if err == nil {
		log.Printf("gamemode: voz elegida: %s", choice.Describe())
	}
	return err
}

// speakAsync speaks on a thread of its own (see sapiSpeaker).
func speakAsync(text string, volume int, required string) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		done <- speakOnThread(voiceXML(text, volume, required))
	}()
	return <-done
}

// voiceXML is the SAPI XML of a phrase: the volume, and the voice when one is required by language.
func voiceXML(text string, volume int, required string) string {
	body := escapeXML(text)
	if required != "" {
		body = `<voice required="` + required + `">` + body + `</voice>`
	}
	return fmt.Sprintf(`<volume level="%d">%s</volume>`, min(max(volume, 0), 100), body)
}

// speakOnThread does the COM calls of one phrase. The voice object is released before returning.
func speakOnThread(xml string) error {
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

// installedVoices lists the speech voices registered for SAPI (HKLM and HKCU). The OneCore voices
// are not listed: the SAPI voice object does not use them.
func installedVoices() []VoiceToken {
	var out []VoiceToken
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		out = append(out, voicesUnder(root, `SOFTWARE\Microsoft\Speech\Voices\Tokens`)...)
	}
	return out
}

// voicesUnder reads the voice tokens under one registry key: each subkey is a voice, its
// Attributes subkey holds the Name and the Language.
func voicesUnder(root registry.Key, path string) []VoiceToken {
	k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	var out []VoiceToken
	for _, n := range names {
		v := VoiceToken{ID: n, Name: n}
		if ak, err := registry.OpenKey(root, path+`\`+n+`\Attributes`, registry.QUERY_VALUE); err == nil {
			if s, _, err := ak.GetStringValue("Name"); err == nil && s != "" {
				v.Name = s
			}
			if s, _, err := ak.GetStringValue("Language"); err == nil {
				v.Language = s
			}
			ak.Close()
		}
		out = append(out, v)
	}
	return out
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

// waveOut functions and structures (mmsystem.h).
var (
	procWaveOutOpen          = modWinmm.NewProc("waveOutOpen")
	procWaveOutPrepareHeader = modWinmm.NewProc("waveOutPrepareHeader")
	procWaveOutWrite         = modWinmm.NewProc("waveOutWrite")
	procWaveOutUnprepare     = modWinmm.NewProc("waveOutUnprepareHeader")
	procWaveOutReset         = modWinmm.NewProc("waveOutReset")
	procWaveOutClose         = modWinmm.NewProc("waveOutClose")
)

const (
	waveMapper     = 0xFFFFFFFF // WAVE_MAPPER: the default output device
	wavePCM        = 1          // WAVE_FORMAT_PCM
	callbackNull   = 0
	whdrDone       = 0x00000001 // WHDR_DONE
	waveDrainSlack = 2 * time.Second
)

// waveFormatEx is WAVEFORMATEX (PCM: cbSize is not used).
type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	CbSize         uint16
}

// waveHdr is WAVEHDR. The Data pointer refers to the WAV buffer, which the player keeps alive until
// the driver has finished with it.
type waveHdr struct {
	Data          uintptr
	BufferLength  uint32
	BytesRecorded uint32
	User          uintptr
	Flags         uint32
	Loops         uint32
	Next          uintptr
	Reserved      uintptr
}

// waveOutPlayer plays a WAV held in memory through waveOut, on the default output device, and
// returns when the sound has finished.
type waveOutPlayer struct{}

// PlayWAV plays the WAV bytes (PCM) at the levels they were made with.
func (waveOutPlayer) PlayWAV(wav []byte) error {
	f, pcm, err := parseWAV(wav)
	if err != nil {
		return err
	}
	align := f.Channels * f.Bits / 8
	format := waveFormatEx{
		FormatTag:      wavePCM,
		Channels:       f.Channels,
		SamplesPerSec:  f.Rate,
		AvgBytesPerSec: f.Rate * uint32(align),
		BlockAlign:     align,
		BitsPerSample:  f.Bits,
	}
	var hwo uintptr
	if r, _, _ := procWaveOutOpen.Call(uintptr(unsafe.Pointer(&hwo)), waveMapper,
		uintptr(unsafe.Pointer(&format)), 0, 0, callbackNull); r != 0 {
		return fmt.Errorf("waveOutOpen: código %d", r)
	}
	defer procWaveOutClose.Call(hwo)

	hdr := waveHdr{Data: uintptr(unsafe.Pointer(&pcm[0])), BufferLength: uint32(len(pcm))}
	hdrSize := unsafe.Sizeof(hdr)
	if r, _, _ := procWaveOutPrepareHeader.Call(hwo, uintptr(unsafe.Pointer(&hdr)), hdrSize); r != 0 {
		return fmt.Errorf("waveOutPrepareHeader: código %d", r)
	}
	defer procWaveOutUnprepare.Call(hwo, uintptr(unsafe.Pointer(&hdr)), hdrSize)

	if r, _, _ := procWaveOutWrite.Call(hwo, uintptr(unsafe.Pointer(&hdr)), hdrSize); r != 0 {
		return fmt.Errorf("waveOutWrite: código %d", r)
	}
	length := time.Duration(len(pcm)) * time.Second / time.Duration(f.Rate*uint32(align))
	deadline := time.Now().Add(length + waveDrainSlack)
	for atomic.LoadUint32(&hdr.Flags)&whdrDone == 0 { // the driver sets the flag; read it each time
		if time.Now().After(deadline) {
			procWaveOutReset.Call(hwo)
			return errors.New("waveOut no terminó el sonido a tiempo")
		}
		time.Sleep(5 * time.Millisecond)
	}
	runtime.KeepAlive(pcm)
	return nil
}
