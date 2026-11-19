//go:build !windows

package shots

import (
	"context"
	"time"
)

// watchDir reports the changed files of dir by polling its listing. The screenshot channel runs on
// Windows, where the folder's change notifications are used instead (see watch_windows.go); this
// keeps the package buildable and testable elsewhere.
func watchDir(ctx context.Context, dir string, out chan<- string) error {
	seen := map[string]fileMark{}
	first := true
	for {
		infos, err := OSFS{}.List(dir)
		if err != nil {
			return err
		}
		for _, info := range infos {
			mark := fileMark{size: info.Size, modTime: info.ModTime}
			if prev, ok := seen[info.Path]; ok && prev == mark {
				continue
			}
			seen[info.Path] = mark
			if first {
				continue // the watcher's own first scan covers the files that exist now
			}
			select {
			case out <- info.Path:
			case <-ctx.Done():
				return nil
			}
		}
		first = false
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
	}
}
