package gamemode

import (
	"encoding/binary"
	"testing"
)

func TestIsSpanishReadsTheLCID(t *testing.T) {
	for _, lang := range []string{"C0A", "c0a", "80A", "A", "C0A;409", " 0C0A "} {
		if !IsSpanish(lang) {
			t.Errorf("IsSpanish(%q) = false, want true", lang)
		}
	}
	for _, lang := range []string{"409", "9", "411", "", "xyz", "409;C0A"} {
		if IsSpanish(lang) {
			t.Errorf("IsSpanish(%q) = true, want false", lang)
		}
	}
}

// The Spanish voice is chosen even when another voice is installed first.
func TestChooseVoicePrefersSpanish(t *testing.T) {
	voices := []VoiceToken{
		{ID: "en", Name: "Zira", Language: "409"},
		{ID: "es", Name: "Helena", Language: "C0A"},
	}
	c, ok := ChooseVoice(voices)
	if !ok || !c.Spanish || c.Token.ID != "es" {
		t.Fatalf("choice = %+v ok=%v, want the Spanish voice", c, ok)
	}
	if got := c.Required(); got != "Language=C0A" {
		t.Fatalf("Required() = %q, want Language=C0A", got)
	}
}

// Without a Spanish voice the first installed voice speaks the phrase, with no language required.
func TestChooseVoiceFallsBackToAnyVoice(t *testing.T) {
	voices := []VoiceToken{{ID: "en", Name: "Zira", Language: "409"}}
	c, ok := ChooseVoice(voices)
	if !ok || c.Spanish || c.Token.ID != "en" {
		t.Fatalf("choice = %+v ok=%v, want the English voice", c, ok)
	}
	if got := c.Required(); got != "" {
		t.Fatalf("Required() = %q for a non-Spanish voice, want empty", got)
	}
}

// With no voice installed there is no choice: the announcer plays the tone.
func TestChooseVoiceWithNoVoices(t *testing.T) {
	if _, ok := ChooseVoice(nil); ok {
		t.Fatal("a choice with no voices installed")
	}
}

func TestDescribeNamesTheVoice(t *testing.T) {
	c, _ := ChooseVoice([]VoiceToken{{ID: "es", Name: "Helena", Language: "C0A"}})
	if got := c.Describe(); got != "Helena (C0A)" {
		t.Fatalf("Describe() = %q", got)
	}
}

// The tone WAV reads back as the format it was made with, and its data is the samples.
func TestParseWAVReadsTheToneFormat(t *testing.T) {
	wav := ToneWAV(CueReady, 80)
	f, pcm, err := parseWAV(wav)
	if err != nil {
		t.Fatalf("parseWAV: %v", err)
	}
	if f.Channels != 1 || f.Rate != toneRate || f.Bits != 16 {
		t.Fatalf("format = %+v, want mono 22050 Hz 16-bit", f)
	}
	if len(pcm) != len(wav)-44 || binary.LittleEndian.Uint32(wav[40:44]) != uint32(len(pcm)) {
		t.Fatalf("data is %d bytes, file has %d after the header", len(pcm), len(wav)-44)
	}
}

func TestParseWAVRejectsOtherFiles(t *testing.T) {
	for _, bad := range [][]byte{nil, []byte("RIFF0000WAVX"), []byte("not a wave file at all")} {
		if _, _, err := parseWAV(bad); err == nil {
			t.Errorf("parseWAV(%q) accepted a file that is not a WAV", bad)
		}
	}
}

// A voice that fails leaves the tone to say the answer is there; the reason is what the log shows.
func TestPlayReportSaysHowTheAnswerWasAnnounced(t *testing.T) {
	sp, pl := &fakeSpeaker{}, &fakePlayer{}
	a := NewAnnouncer(sp, pl, "voz", 80)
	a.logf = quietLog
	if how, err := a.PlayReport(CueReady); err != nil || how != "voz (Respuesta lista)" {
		t.Fatalf("voice: how %q err %v", how, err)
	}
	sp.err = errNoVoicesForTest
	if how, err := a.PlayReport(CueReady); err != nil || how != "pitido" {
		t.Fatalf("failed voice: how %q err %v, want the tone", how, err)
	}
	if len(pl.wavs) != 1 {
		t.Fatalf("the tone was played %d times, want 1", len(pl.wavs))
	}
}

var errNoVoicesForTest = errTest("sin voz")

type errTest string

func (e errTest) Error() string { return string(e) }
