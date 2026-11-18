package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A config from before the interface modes gets uiMode "juego", the default answer sound and
// volume, and the file is written again so that the keys show up.
func TestLegacyConfigGetsGameModeDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{"hotkey":"Ctrl+Shift+Space","tipsMode":"bandeja","opacity":90,"fontSize":10,"mcpPort":47321}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.UIMode != UIModeGame || c.AnswerSound != AnswerVoice || c.AnswerVolume != 80 {
		t.Fatalf("defaults not applied: uiMode=%q answerSound=%q answerVolume=%d", c.UIMode, c.AnswerSound, c.AnswerVolume)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"uiMode": "juego"`, `"answerSound": "voz"`, `"answerVolume": 80`} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("config.json not rewritten with %s:\n%s", key, data)
		}
	}
}

func TestExplicitUIModeIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"uiMode":"ventana","answerSound":"pitido","answerVolume":35}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.UIMode != UIModeWindow || c.AnswerSound != AnswerBeep || c.AnswerVolume != 35 {
		t.Fatalf("explicit values lost: %+v", c)
	}
}

func TestInvalidGameModeValuesAreFixed(t *testing.T) {
	c := Defaults()
	c.UIMode = "flotante"
	c.AnswerSound = "campana"
	c.AnswerVolume = 250
	if !c.migrate() {
		t.Fatal("invalid values must be reported as changed")
	}
	if c.UIMode != UIModeGame || c.AnswerSound != AnswerVoice || c.AnswerVolume != MaxAnswerVolume {
		t.Fatalf("fixed values: %+v", c)
	}
	c.AnswerVolume = -4
	c.migrate()
	if c.AnswerVolume != MinAnswerVolume {
		t.Fatalf("negative volume = %d", c.AnswerVolume)
	}
}

func TestValidAnswerSettings(t *testing.T) {
	for _, s := range []string{AnswerVoice, AnswerBeep, AnswerNone} {
		if !ValidAnswerSound(s) {
			t.Fatalf("%q should be valid", s)
		}
	}
	if ValidAnswerSound("voz ") || ValidAnswerSound("") {
		t.Fatal("only the three values are valid")
	}
	if !ValidUIMode(UIModeGame) || !ValidUIMode(UIModeWindow) || ValidUIMode("panel") {
		t.Fatal("ValidUIMode")
	}
}
