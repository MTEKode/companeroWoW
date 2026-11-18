//go:build !windows

package gamemode

import "errors"

// NewSystemAnnouncer returns an announcer with no voice and no player: the sound is for Windows
// only, so the announcements are silent (and logged as errors) elsewhere.
func NewSystemAnnouncer(mode string, volume int) *Announcer {
	return NewAnnouncer(unsupportedSpeaker{}, unsupportedPlayer{}, mode, volume)
}

type unsupportedSpeaker struct{}

func (unsupportedSpeaker) Speak(string, int) error {
	return errors.New("la voz solo está disponible en Windows")
}

type unsupportedPlayer struct{}

func (unsupportedPlayer) PlayWAV([]byte) error {
	return errors.New("el sonido solo está disponible en Windows")
}
