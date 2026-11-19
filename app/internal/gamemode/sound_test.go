package gamemode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

type fakeSpeaker struct {
	mu     sync.Mutex
	said   []string
	levels []int
	err    error
}

func (f *fakeSpeaker) Speak(text string, volume int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.said = append(f.said, text)
	f.levels = append(f.levels, volume)
	return f.err
}

type fakePlayer struct {
	mu   sync.Mutex
	wavs [][]byte
	err  error
}

func (f *fakePlayer) PlayWAV(wav []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wavs = append(f.wavs, wav)
	return f.err
}

func quietLog(string, ...any) {}

func TestToneWAVIsAValidWave(t *testing.T) {
	for _, cue := range []Cue{CueReady, CueError} {
		wav := ToneWAV(cue, 80)
		if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || string(wav[12:16]) != "fmt " {
			t.Fatalf("cue %d: not a RIFF/WAVE file", cue)
		}
		if rate := binary.LittleEndian.Uint32(wav[24:28]); rate != toneRate {
			t.Fatalf("cue %d: sample rate %d", cue, rate)
		}
		if string(wav[36:40]) != "data" {
			t.Fatalf("cue %d: no data chunk", cue)
		}
		dataLen := int(binary.LittleEndian.Uint32(wav[40:44]))
		if dataLen != len(wav)-44 || int(binary.LittleEndian.Uint32(wav[4:8])) != len(wav)-8 {
			t.Fatalf("cue %d: chunk sizes do not match the file", cue)
		}
		peak := 0
		for i := 44; i+1 < len(wav); i += 2 {
			v := int(int16(binary.LittleEndian.Uint16(wav[i:])))
			peak = max(peak, abs(v))
		}
		// 60 % of full scale at 100 %: 80 % of that is 48 %, and the tone must still be heard.
		if float64(peak) < 0.4*32767 || float64(peak) > 0.5*32767 {
			t.Fatalf("cue %d: peak %d out of range for volume 80", cue, peak)
		}
	}
}

func TestToneWAVVolumeScalesTheTone(t *testing.T) {
	loud := ToneWAV(CueReady, 100)
	soft := ToneWAV(CueReady, 20)
	if bytes.Equal(loud, soft) {
		t.Fatal("volume must change the samples")
	}
	if len(loud) != len(soft) {
		t.Fatal("volume must not change the length")
	}
}

func TestAnnouncerVoiceSaysThePhrase(t *testing.T) {
	sp, pl := &fakeSpeaker{}, &fakePlayer{}
	a := NewAnnouncer(sp, pl, config.AnswerVoice, 65)
	a.logf = quietLog
	if err := a.Play(CueReady); err != nil {
		t.Fatal(err)
	}
	if len(sp.said) != 1 || sp.said[0] != "Respuesta lista" || sp.levels[0] != 65 {
		t.Fatalf("said %v at %v", sp.said, sp.levels)
	}
	if len(pl.wavs) != 0 {
		t.Fatal("the tone must not play when the voice worked")
	}
	if err := a.Play(CueError); err != nil || sp.said[1] != "No pude responder" {
		t.Fatalf("error phrase: %v (%v)", sp.said, err)
	}
}

func TestAnnouncerFallsBackToTone(t *testing.T) {
	sp := &fakeSpeaker{err: errors.New("sin voz")}
	pl := &fakePlayer{}
	a := NewAnnouncer(sp, pl, config.AnswerVoice, 80)
	a.logf = quietLog
	if err := a.Play(CueReady); err != nil {
		t.Fatal(err)
	}
	if len(pl.wavs) != 1 {
		t.Fatalf("the tone should play when the voice fails, got %d plays", len(pl.wavs))
	}
}

func TestAnnouncerBeepAndNone(t *testing.T) {
	sp, pl := &fakeSpeaker{}, &fakePlayer{}
	a := NewAnnouncer(sp, pl, config.AnswerBeep, 80)
	if err := a.Play(CueReady); err != nil {
		t.Fatal(err)
	}
	if len(sp.said) != 0 || len(pl.wavs) != 1 {
		t.Fatalf("pitido: said %v, plays %d", sp.said, len(pl.wavs))
	}

	a.Set(config.AnswerNone, 80)
	if err := a.Play(CueReady); err != nil {
		t.Fatal(err)
	}
	if len(pl.wavs) != 1 || len(sp.said) != 0 {
		t.Fatal("ninguno must play nothing")
	}
}

func TestAnnouncerZeroVolumeIsSilent(t *testing.T) {
	sp, pl := &fakeSpeaker{}, &fakePlayer{}
	a := NewAnnouncer(sp, pl, config.AnswerVoice, 0)
	if err := a.Play(CueReady); err != nil {
		t.Fatal(err)
	}
	if len(sp.said) != 0 || len(pl.wavs) != 0 {
		t.Fatal("volume 0 must be silent")
	}
}

func TestAnnouncerSetTakesEffectAndValidates(t *testing.T) {
	sp, pl := &fakeSpeaker{}, &fakePlayer{}
	a := NewAnnouncer(sp, pl, config.AnswerNone, 10)
	a.Set("desconocido", 150) // an unknown mode is the voice; the volume is capped
	if err := a.Play(CueReady); err != nil {
		t.Fatal(err)
	}
	if len(sp.said) != 1 || sp.levels[0] != config.MaxAnswerVolume {
		t.Fatalf("after Set: said %v at %v", sp.said, sp.levels)
	}
}

func TestAnnouncerWithoutPlayerReportsIt(t *testing.T) {
	a := NewAnnouncer(nil, nil, config.AnswerBeep, 80)
	if err := a.Play(CueReady); err == nil {
		t.Fatal("no way to play must be an error")
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
