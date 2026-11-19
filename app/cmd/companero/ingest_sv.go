package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// runIngestSV stores what the SavedVariables file holds in the app's database and exits, without
// opening the window. The rollout runs it once before the lean addon loads: the addon then moves the
// big blocks of the file into its outbox, and the app must already have them. The import is recorded
// with the file's path, size and content hash (store.ImportSavedVariables), so the result can be
// checked with the last import of that file.
func runIngestSV(cfg *config.Config, dir string) int {
	path, err := cfg.SavedVariablesPath()
	if err != nil {
		log.Printf("ingest-sv: %v", err)
		fmt.Fprintln(os.Stderr, "ingest-sv:", err)
		return 1
	}
	sv, err := bridge.LoadSavedVariables(path)
	if err != nil {
		log.Printf("ingest-sv: %v", err)
		fmt.Fprintln(os.Stderr, "ingest-sv:", err)
		return 1
	}
	st, err := store.Open(filepath.Join(dir, "companero.db"))
	if err != nil {
		log.Printf("ingest-sv: %v", err)
		fmt.Fprintln(os.Stderr, "ingest-sv:", err)
		return 1
	}
	defer st.Close()
	imp, err := st.ImportSavedVariables(sv, time.Now())
	if err != nil {
		log.Printf("ingest-sv: %v", err)
		fmt.Fprintln(os.Stderr, "ingest-sv:", err)
		return 1
	}
	line := fmt.Sprintf("ingest-sv: %s (esquema %d, %d bytes, sha256 %s): %d aplicados, %d ya guardados, %d sin guardar",
		imp.Path, imp.Schema, imp.Size, imp.SHA256, imp.Stats.Applied, imp.Stats.Skipped, imp.Stats.Failed)
	log.Print(line)
	fmt.Println(line)
	if imp.Stats.Failed > 0 {
		return 1
	}
	return 0
}
