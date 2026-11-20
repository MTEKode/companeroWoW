package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
)

// Snapshot writes a consistent copy of the database at src into dst, which must not exist. src is opened
// read-only, so an app that keeps it open is not disturbed and nothing is migrated or written there. VACUUM
// INTO reads through the write-ahead log, so the copy holds every committed write, including the newest.
func Snapshot(src, dst string) error {
	if src == "" || dst == "" {
		return fmt.Errorf("snapshot: empty path")
	}
	dsn := "file:" + filepath.ToSlash(src) + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("snapshot: open %s: %w", src, err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`VACUUM INTO ?`, filepath.ToSlash(dst)); err != nil {
		return fmt.Errorf("snapshot: copy %s: %w", src, err)
	}
	return nil
}
