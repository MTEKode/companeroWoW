package gamemode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"log"
	"math"
	"sync"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// Cue is what the announcement says: an answer is ready, or it failed.
type Cue int

const (
	// CueReady is the announcement of a finished answer ("Respuesta lista").
	CueReady Cue = iota
	// CueError is the announcement of an answer that could not be given.
	CueError
)

// Spoken phrases of the voice setting (Spanish).
const (
	phraseReady = "Respuesta lista"
	phraseError = "No pude responder"
)

// Speaker says a short phrase aloud at a volume from 0 to 100. The Windows implementation uses
// the speech API (SAPI); it returns when the phrase has been said.
type Speaker interface {
	Speak(text string, volume int) error
}

// Player plays a WAV file held in memory and returns when it has finished.
type Player interface {
	PlayWAV(wav []byte) error
}

// Clipboard puts text on the clipboard as plain Unicode text. The Windows implementation is
// ui.CopyText; it only copies, nothing is typed into the game.
type Clipboard interface {
	SetText(text string) error
}

// ClipboardFunc adapts a plain function (such as ui.CopyText) to Clipboard.
type ClipboardFunc func(text string) error

// SetText implements Clipboard.
func (f ClipboardFunc) SetText(text string) error { return f(text) }

// Sounder announces a cue. The controller uses it through this interface, so tests can count the
// announcements without any sound.
type Sounder interface {
	Play(cue Cue) error
}

// Announcer plays the cues as the answer sound setting says: the spoken phrase (voz), a tone
// (pitido) or nothing (ninguno). A failed voice falls back to the tone, so the player still hears
// that the answer is there. Set changes the setting while the app runs.
type Announcer struct {
	mu      sync.Mutex
	mode    string
	volume  int
	speaker Speaker
	player  Player
	logf    func(format string, args ...any)
}

// NewAnnouncer returns an announcer over the given speaker and player. Either may be nil: a
// missing speaker makes the voice fall back to the tone, a missing player makes the tone silent.
func NewAnnouncer(speaker Speaker, player Player, mode string, volume int) *Announcer {
	a := &Announcer{speaker: speaker, player: player, logf: log.Printf}
	a.Set(mode, volume)
	return a
}

// Set changes the sound setting: the mode (config.AnswerVoice, AnswerBeep or AnswerNone) and the
// volume in percent. An unknown mode is taken as voice, the default.
func (a *Announcer) Set(mode string, volume int) {
	if !config.ValidAnswerSound(mode) {
		mode = config.AnswerVoice
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mode = mode
	a.volume = min(max(volume, config.MinAnswerVolume), config.MaxAnswerVolume)
}

// Play announces cue with the current setting. It returns an error only when no sound could be
// played at all; a silent setting or a zero volume is not an error.
func (a *Announcer) Play(cue Cue) error {
	a.mu.Lock()
	mode, volume := a.mode, a.volume
	a.mu.Unlock()
	if mode == config.AnswerNone || volume == 0 {
		return nil
	}
	if mode == config.AnswerVoice && a.speaker != nil {
		err := a.speaker.Speak(phraseFor(cue), volume)
		if err == nil {
			return nil
		}
		a.logf("gamemode: la voz no sonó, sigo con el pitido: %v", err)
	}
	if a.player == nil {
		return errors.New("gamemode: no hay forma de reproducir el sonido")
	}
	return a.player.PlayWAV(ToneWAV(cue, volume))
}

// phraseFor is the spoken text of a cue.
func phraseFor(cue Cue) string {
	if cue == CueError {
		return phraseError
	}
	return phraseReady
}

// Tone synthesis. The tones are built in memory, so the app carries no sound files.
const (
	toneRate    = 22050
	toneFadeSec = 0.006
)

// tone is one note of a cue: a frequency and a length in seconds; a zero frequency is a gap.
type tone struct {
	hz  float64
	sec float64
}

// cueTones are the notes of each cue: two rising beeps for a ready answer, a falling pair for an
// error.
var cueTones = map[Cue][]tone{
	CueReady: {{880, 0.11}, {0, 0.03}, {1320, 0.15}},
	CueError: {{330, 0.18}, {0, 0.04}, {220, 0.26}},
}

// ToneWAV returns the WAV file (16-bit mono PCM) of a cue at the given volume (percent).
func ToneWAV(cue Cue, volume int) []byte {
	amp := 0.35 * float64(min(max(volume, 0), 100)) / 100 * math.MaxInt16
	var pcm []int16
	for _, n := range cueTones[cue] {
		count := int(n.sec * toneRate)
		fade := int(math.Round(toneFadeSec * toneRate))
		for i := 0; i < count; i++ {
			v := 0.0
			if n.hz > 0 {
				v = math.Sin(2*math.Pi*n.hz*float64(i)/toneRate) * amp * envelope(i, count, fade)
			}
			pcm = append(pcm, int16(v))
		}
	}
	return wavBytes(pcm)
}

// envelope fades a note in and out over fade samples so that it does not click.
func envelope(i, count, fade int) float64 {
	switch {
	case fade <= 0:
		return 1
	case i < fade:
		return float64(i) / float64(fade)
	case count-i <= fade:
		return float64(count-i) / float64(fade)
	}
	return 1
}

// wavBytes wraps 16-bit mono samples in a RIFF/WAVE container.
func wavBytes(pcm []int16) []byte {
	var data bytes.Buffer
	_ = binary.Write(&data, binary.LittleEndian, pcm)
	var out bytes.Buffer
	dataLen := uint32(data.Len())
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, 36+dataLen)
	out.WriteString("WAVE")
	out.WriteString("fmt ")
	_ = binary.Write(&out, binary.LittleEndian, uint32(16)) // fmt chunk size
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))  // PCM
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))  // mono
	_ = binary.Write(&out, binary.LittleEndian, uint32(toneRate))
	_ = binary.Write(&out, binary.LittleEndian, uint32(toneRate*2))
	_ = binary.Write(&out, binary.LittleEndian, uint16(2))  // block align
	_ = binary.Write(&out, binary.LittleEndian, uint16(16)) // bits per sample
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, dataLen)
	out.Write(data.Bytes())
	return out.Bytes()
}
