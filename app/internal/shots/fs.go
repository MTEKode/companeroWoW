// Package shots reads the data panels of the screenshots WoW saves (the screenshot channel of
// the SCREENSHOT CHANNEL CONTRACT): it watches the Screenshots folder of the game, waits until
// each new file is fully written, decodes the panel at its top-left into frames for the bridge,
// and deletes the file only when it was ours. Other screenshots are never touched.
package shots

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Info is what the watcher needs to know of a file.
type Info struct {
	Path    string
	Name    string
	Size    int64
	ModTime time.Time
}

// FS is the file system the watcher works on. Tests replace it with a fake; the app uses OSFS.
type FS interface {
	// Stat returns the file's size and modification time.
	Stat(path string) (Info, error)
	// List returns the files directly in dir.
	List(dir string) ([]Info, error)
	// ReadFile returns the whole file.
	ReadFile(path string) ([]byte, error)
	// Probe reports whether the file can be opened for reading now: a file the game is still
	// writing is not.
	Probe(path string) error
	// Remove deletes the file.
	Remove(path string) error
}

// OSFS is the file system of the disk.
type OSFS struct{}

// Stat implements FS.
func (OSFS) Stat(path string) (Info, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Info{}, err
	}
	return Info{Path: path, Name: fi.Name(), Size: fi.Size(), ModTime: fi.ModTime()}, nil
}

// List implements FS. Subfolders are skipped.
func (OSFS) List(dir string) ([]Info, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue // removed while listing
		}
		out = append(out, Info{Path: filepath.Join(dir, e.Name()), Name: e.Name(), Size: fi.Size(), ModTime: fi.ModTime()})
	}
	return out, nil
}

// ReadFile implements FS.
func (OSFS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// Probe implements FS: the file must open for reading.
func (OSFS) Probe(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return f.Close()
}

// Remove implements FS.
func (OSFS) Remove(path string) error { return os.Remove(path) }

// isImageName reports whether a file name has an image extension the watcher reads.
func isImageName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".tga", ".jpg", ".jpeg":
		return true
	}
	return false
}

// errNotPanel marks a screenshot with no data panel at its top-left: it is left alone.
var errNotPanel = errors.New("la captura no tiene panel de datos")
