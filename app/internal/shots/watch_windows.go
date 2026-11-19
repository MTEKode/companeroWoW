//go:build windows

package shots

import (
	"context"
	"encoding/binary"
	"path/filepath"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// notifyFilter selects the changes that matter: a file created or renamed in, and a file being
// written (its size and last write time move while WoW saves it).
const notifyFilter = windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_SIZE | windows.FILE_NOTIFY_CHANGE_LAST_WRITE

// watchDir reports the paths of the files that change in dir, using ReadDirectoryChangesW, until
// ctx ends. It returns an error when the folder cannot be watched (missing, access denied).
func watchDir(ctx context.Context, dir string, out chan<- string) error {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(ev)

	buf := make([]byte, 64*1024)
	for {
		if ctx.Err() != nil {
			return nil
		}
		var ov windows.Overlapped
		ov.HEvent = ev
		if err := windows.ResetEvent(ev); err != nil {
			return err
		}
		var ret uint32
		if err := windows.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)), false, notifyFilter, &ret, &ov, 0); err != nil {
			return err
		}
		// Wait in slices, so a cancelled context ends the watch within half a second.
		for {
			if ctx.Err() != nil {
				windows.CancelIo(h)
				return nil
			}
			st, err := windows.WaitForSingleObject(ev, 500)
			if err != nil {
				return err
			}
			if st == windows.WAIT_OBJECT_0 {
				break
			}
		}
		var n uint32
		if err := windows.GetOverlappedResult(h, &ov, &n, false); err != nil {
			return err
		}
		if n == 0 {
			continue // the buffer overflowed: the watcher's rescan finds the files
		}
		if !sendNames(ctx, dir, buf[:n], out) {
			return nil
		}
	}
}

// sendNames decodes the FILE_NOTIFY_INFORMATION records in b and sends each file's path. It
// reports false when ctx ended first.
func sendNames(ctx context.Context, dir string, b []byte, out chan<- string) bool {
	off := 0
	for {
		next := int(binary.LittleEndian.Uint32(b[off:]))
		nameLen := int(binary.LittleEndian.Uint32(b[off+8:]))
		raw := b[off+12 : off+12+nameLen]
		u := make([]uint16, nameLen/2)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(raw[2*i:])
		}
		path := filepath.Join(dir, string(utf16.Decode(u)))
		select {
		case out <- path:
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
			// the watcher is busy: the rescan catches the file
		}
		if next == 0 {
			return true
		}
		off += next
	}
}
