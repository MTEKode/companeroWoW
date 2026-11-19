package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/gamemode"
	"github.com/MTEKode/companeroWoW/app/internal/ui"
)

// testAnswerText is the sample answer of --test-answer: the markup the addon renders, with each
// kind of action line.
const testAnswerText = `**Ve a Entregar** (Puerto de Ventormenta)
📍 /compa marca 51.2 38.7 Entregar
⌨️ /compa barras aplicar 6:2054,7:1234
- Paso uno | paso dos
Fuente: wowforevertalents.com`

// runTestAnswer is the --test-answer debug flag: it puts a sample CWOW1 answer on the clipboard
// and plays the answer sound, the way a real answer does, then reports each step on the console
// and in the log. It does not start the app or touch the game.
func runTestAnswer(cfg *config.Config, dir string) int {
	attachConsole()
	block := gamemode.FormatAnswer(gamemode.Answer{
		QuestionID: 1,
		Provider:   "prueba",
		MapID:      85,
		Text:       testAnswerText,
	})
	ok := true
	report := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		log.Print("test-answer: " + msg)
		fmt.Fprintln(os.Stdout, "test-answer: "+msg)
	}

	report("bloque (%d caracteres):\n%s", len([]rune(block)), block)
	if err := ui.CopyText(block); err != nil {
		report("portapapeles: ERROR %v", err)
		ok = false
	} else {
		report("portapapeles: copiado (Ctrl+V en el marco de Compañero)")
	}

	ann := gamemode.NewSystemAnnouncer(cfg.AnswerSound, cfg.AnswerVolume)
	if how, err := ann.PlayReport(gamemode.CueReady); err != nil {
		report("sonido (%s, volumen %d): ERROR %v", cfg.AnswerSound, cfg.AnswerVolume, err)
		ok = false
	} else {
		report("sonido: %s, volumen %d", how, cfg.AnswerVolume)
	}
	report("log: %s", filepath.Join(dir, "companero.log"))
	if !ok {
		return 1
	}
	return 0
}
