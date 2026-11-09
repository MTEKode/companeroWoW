package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// feedbackExport is the JSON written by --export-feedback: every 👍/👎 with the question,
// the answer, the provider and the skill version, and the totals.
type feedbackExport struct {
	ExportedAt time.Time        `json:"exportedAt"`
	Version    string           `json:"version"`
	Count      int              `json:"count"`
	Up         int              `json:"up"`
	Down       int              `json:"down"`
	Items      []store.Feedback `json:"items"`
}

// buildFeedbackExport counts the ratings and wraps them for the file.
func buildFeedbackExport(items []store.Feedback, now time.Time, appVersion string) feedbackExport {
	if items == nil {
		items = []store.Feedback{}
	}
	exp := feedbackExport{ExportedAt: now, Version: appVersion, Count: len(items), Items: items}
	for _, f := range items {
		if f.Rating > 0 {
			exp.Up++
		} else {
			exp.Down++
		}
	}
	return exp
}

// runExportFeedback writes the ratings to path and returns the exit code. It reads the
// database only, so it works while the app runs.
func runExportFeedback(dir, path string) int {
	st, err := store.Open(filepath.Join(dir, "companero.db"))
	if err != nil {
		log.Printf("feedback: %v", err)
		return 1
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Printf("feedback: cerrar la base: %v", err)
		}
	}()
	items, err := st.ListFeedback()
	if err != nil {
		log.Printf("feedback: %v", err)
		return 1
	}
	exp := buildFeedbackExport(items, time.Now(), version)
	data, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		log.Printf("feedback: %v", err)
		return 1
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("feedback: escribir %s: %v", path, err)
		return 1
	}
	if attachConsole() {
		fmt.Printf("Valoraciones: %d (%d arriba, %d abajo)\nArchivo: %s\n", exp.Count, exp.Up, exp.Down, path)
	}
	return 0
}
