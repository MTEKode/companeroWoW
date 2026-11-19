package shots

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
)

// StateFile is the file, in the app's data folder, where the running app writes the screenshot
// status: --diag reads it, since it runs as a separate process.
const StateFile = "captura.json"

// WriteState writes st to path. The file is replaced in one step (a temporary file, then a rename),
// so a reader never sees half of it.
func WriteState(path string, st Status) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadState reads the status written by WriteState. ok is false when no app has written one.
func ReadState(path string) (st Status, ok bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Status{}, false, nil
	}
	if err != nil {
		return Status{}, false, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return Status{}, false, err
	}
	return st, true, nil
}

// CountImages returns how many screenshot files dir holds (any image format the watcher reads).
func CountImages(dir string) (int, error) {
	infos, err := OSFS{}.List(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, info := range infos {
		if isImageName(info.Name) {
			n++
		}
	}
	return n, nil
}
