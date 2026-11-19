package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// SVImport is one import of a SavedVariables file into the store: which file, its content identity
// (sha256), size and modification time, the schema it had, and what the ingestion did.
type SVImport struct {
	Path       string
	SHA256     string
	Size       int64
	ModTime    time.Time
	Schema     int
	ImportedAt time.Time
	Stats      IngestStats
}

// ImportSavedVariables stores the data of a SavedVariables file (IngestSavedVars) and records the
// import: the file's path and content hash, its size, its schema, and the counts. The record is kept
// per (path, sha256): importing the same content again updates its counts and time. This is the
// marker that the file's data is in the store before the addon changes it (the rollout of the lean
// SavedVariables reads it, see LastSVImport).
func (s *Store) ImportSavedVariables(sv *bridge.SavedVars, now time.Time) (SVImport, error) {
	if sv == nil {
		return SVImport{}, errors.New("import saved variables: nil data")
	}
	st, err := s.IngestSavedVars(sv, now)
	if err != nil {
		return SVImport{}, err
	}
	imp := SVImport{
		Path:       sv.Path,
		SHA256:     sv.SHA256,
		Size:       sv.Size,
		ModTime:    sv.ModTime,
		Schema:     int(rawNum(sv.Raw, "schema")),
		ImportedAt: now,
		Stats:      st,
	}
	if _, err := s.db.Exec(`
		INSERT INTO sv_imports (path, sha256, size, mtime, schema, imported_at, applied, skipped, failed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path, sha256) DO UPDATE SET
			imported_at = excluded.imported_at, applied = excluded.applied,
			skipped = excluded.skipped, failed = excluded.failed`,
		imp.Path, imp.SHA256, imp.Size, imp.ModTime.Unix(), imp.Schema, now.Unix(),
		st.Applied, st.Skipped, st.Failed); err != nil {
		return imp, fmt.Errorf("record saved variables import: %w", err)
	}
	return imp, nil
}

// LastSVImport returns the most recent import recorded for path. ok is false when the file was never
// imported.
func (s *Store) LastSVImport(path string) (SVImport, bool, error) {
	var imp SVImport
	var mtime, imported int64
	err := s.db.QueryRow(`SELECT path, sha256, size, mtime, schema, imported_at, applied, skipped, failed
		FROM sv_imports WHERE path = ? ORDER BY imported_at DESC, id DESC LIMIT 1`, path).
		Scan(&imp.Path, &imp.SHA256, &imp.Size, &mtime, &imp.Schema, &imported,
			&imp.Stats.Applied, &imp.Stats.Skipped, &imp.Stats.Failed)
	if errors.Is(err, sql.ErrNoRows) {
		return SVImport{}, false, nil
	}
	if err != nil {
		return SVImport{}, false, fmt.Errorf("read saved variables import: %w", err)
	}
	imp.ModTime = time.Unix(mtime, 0)
	imp.ImportedAt = time.Unix(imported, 0)
	return imp, true, nil
}
