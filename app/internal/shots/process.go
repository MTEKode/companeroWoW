package shots

import (
	"fmt"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// Sink takes the frames of a screenshot. The app's reader implements it (Reader.FeedFrame).
type Sink interface {
	FeedFrame(f bridge.FrameV3, now time.Time)
}

// Result is what one screenshot gave.
type Result struct {
	File     string
	ShotID   uint16
	Rows     int           // rows read, the end row included
	Bytes    int           // chunk bytes carried by the rows
	Complete bool          // the end row was read and its count matches
	Stop     error         // why the rows ended before a complete panel (nil when Complete)
	Elapsed  time.Duration // time spent reading and feeding the screenshot
	Deleted  bool          // the file was ours and is gone
}

// Process reads one settled screenshot. When its top-left holds a data panel, the rows go to the
// sink and the file is deleted; the rows are fed before the delete, so a failed delete loses
// nothing. A screenshot without a panel gives an error wrapping errNotPanel and is not touched.
func Process(fs FS, sink Sink, path string, now time.Time) (Result, error) {
	start := time.Now()
	res := Result{File: path}
	data, err := fs.ReadFile(path)
	if err != nil {
		return res, fmt.Errorf("leer %s: %w", path, err)
	}
	img, err := LoadImage(path, data)
	if err != nil {
		return res, err
	}
	panel, err := bridge.DecodePanel(img)
	if err != nil {
		return res, fmt.Errorf("%w: %v", errNotPanel, err)
	}
	res.ShotID = panel.ShotID
	res.Rows = len(panel.Frames)
	res.Complete = panel.Complete
	res.Stop = panel.Stop
	for _, f := range panel.Frames {
		res.Bytes += len(f.Data)
		sink.FeedFrame(f, now)
	}
	if err := fs.Remove(path); err != nil {
		res.Elapsed = time.Since(start)
		return res, fmt.Errorf("borrar %s: %w", path, err)
	}
	res.Deleted = true
	res.Elapsed = time.Since(start)
	return res, nil
}
