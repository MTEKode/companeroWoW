package shots

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Options configure a Watcher. Only Dir and Sink are required.
type Options struct {
	// Dir is the Screenshots folder of the game.
	Dir string
	// Since is the app start: screenshots last written before it are not read.
	Since time.Time
	// Sink takes the frames of each panel.
	Sink Sink
	// FS is the file system (default: the disk).
	FS FS
	// Watch reports the paths of changed files in dir to out until ctx ends. It returns an error
	// when the folder cannot be watched (default: the folder's change notifications).
	Watch func(ctx context.Context, dir string, out chan<- string) error
	// Logf receives one line per screenshot read and each failure (default: none).
	Logf func(format string, args ...any)
	// Now is the clock (default time.Now).
	Now func() time.Time
	// Quiet is how long a file's size and time must hold still before it is read (default 500 ms).
	Quiet time.Duration
	// MaxWait drops a file that has not settled after this long (default 30 s).
	MaxWait time.Duration
	// Rescan lists the folder this often, in case a change notification was lost (default 3 s).
	Rescan time.Duration
	// Tick is how often the pending files are checked (default 200 ms).
	Tick time.Duration
	// RetryWatch is the wait before the folder is watched again after a failure (default 5 s).
	RetryWatch time.Duration
	// OnChange, when set, receives the status after each read, failure and change of the watch.
	OnChange func(Status)
}

// Status is what --diag and the status line show about the screenshot channel.
type Status struct {
	Dir         string
	Watching    bool      // the folder's change notifications are on
	Since       time.Time // screenshots written before this are ignored
	Shots       int       // screenshots that carried a panel
	Ignored     int       // screenshots read and left alone: no panel
	Last        ShotInfo  // the last panel read
	LastError   string    // the last failure ("" when none)
	LastErrorAt time.Time // when LastError happened
}

// ShotInfo describes one panel read.
type ShotInfo struct {
	File     string
	At       time.Time
	ShotID   uint16
	Rows     int
	Bytes    int
	Elapsed  time.Duration
	Complete bool
	Deleted  bool
}

// fileMark identifies a file version: a screenshot already read or left alone is not read again.
type fileMark struct {
	size    int64
	modTime time.Time
}

// Watcher reads the panels of the screenshots that appear in its folder.
type Watcher struct {
	opt  Options
	set  *settler
	seen map[string]fileMark

	mu sync.Mutex
	st Status
}

// New returns a Watcher with the defaults filled in. Run starts it.
func New(opt Options) *Watcher {
	if opt.FS == nil {
		opt.FS = OSFS{}
	}
	if opt.Watch == nil {
		opt.Watch = watchDir
	}
	if opt.Logf == nil {
		opt.Logf = func(string, ...any) {}
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Quiet <= 0 {
		opt.Quiet = 500 * time.Millisecond
	}
	if opt.MaxWait <= 0 {
		opt.MaxWait = 30 * time.Second
	}
	if opt.Rescan <= 0 {
		opt.Rescan = 3 * time.Second
	}
	if opt.Tick <= 0 {
		opt.Tick = 200 * time.Millisecond
	}
	if opt.RetryWatch <= 0 {
		opt.RetryWatch = 5 * time.Second
	}
	return &Watcher{
		opt:  opt,
		set:  newSettler(opt.Quiet, opt.MaxWait),
		seen: make(map[string]fileMark),
		st:   Status{Dir: opt.Dir, Since: opt.Since},
	}
}

// Status returns a copy of the watcher's status.
func (w *Watcher) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.st
}

// Run watches the folder until ctx ends. It is the only goroutine that reads screenshots.
func (w *Watcher) Run(ctx context.Context) {
	names := make(chan string, 128)
	go w.watchLoop(ctx, names)
	tick := time.NewTicker(w.opt.Tick)
	defer tick.Stop()
	rescan := time.NewTicker(w.opt.Rescan)
	defer rescan.Stop()
	w.scan()
	for {
		select {
		case <-ctx.Done():
			return
		case path := <-names:
			if info, err := w.opt.FS.Stat(path); err == nil {
				w.consider(info)
			}
		case <-rescan.C:
			w.scan()
		case <-tick.C:
			w.settle()
		}
	}
}

// watchLoop keeps the folder's change notifications on, and starts them again after a failure.
func (w *Watcher) watchLoop(ctx context.Context, names chan<- string) {
	for {
		w.setWatching(true)
		err := w.opt.Watch(ctx, w.opt.Dir, names)
		w.setWatching(false)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("el aviso de cambios terminó")
		}
		w.fail("vigilar la carpeta de capturas: %v", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.opt.RetryWatch):
		}
	}
}

// scan lists the folder and considers every image in it (the rescan of the change notifications).
func (w *Watcher) scan() {
	infos, err := w.opt.FS.List(w.opt.Dir)
	if err != nil {
		w.fail("listar la carpeta de capturas: %v", err)
		return
	}
	for _, info := range infos {
		w.consider(info)
	}
}

// consider notes an image file for reading, unless it is an old file or this version was read.
func (w *Watcher) consider(info Info) {
	if !isImageName(info.Name) {
		return
	}
	mark := fileMark{size: info.Size, modTime: info.ModTime}
	if prev, ok := w.seen[info.Path]; ok && prev == mark {
		return
	}
	if info.ModTime.Before(w.opt.Since) {
		w.seen[info.Path] = mark
		return
	}
	w.set.note(info.Path, w.opt.Now())
}

// settle reads the files that are fully written.
func (w *Watcher) settle() {
	now := w.opt.Now()
	for _, path := range w.set.ready(now, w.opt.FS) {
		info, statErr := w.opt.FS.Stat(path)
		res, err := Process(w.opt.FS, w.opt.Sink, path, now)
		if statErr == nil {
			w.seen[path] = fileMark{size: info.Size, modTime: info.ModTime}
		}
		switch {
		case err == nil:
			w.record(res, now)
		case errors.Is(err, errNotPanel):
			w.mu.Lock()
			w.st.Ignored++
			w.mu.Unlock()
			w.opt.Logf("captura: %s no tiene panel de datos, se deja", path)
			w.changed()
		default:
			w.fail("%v", err)
		}
	}
}

// record is the log and the status of a panel read.
func (w *Watcher) record(res Result, now time.Time) {
	info := ShotInfo{
		File: res.File, At: now, ShotID: res.ShotID, Rows: res.Rows, Bytes: res.Bytes,
		Elapsed: res.Elapsed, Complete: res.Complete, Deleted: res.Deleted,
	}
	w.mu.Lock()
	w.st.Shots++
	w.st.Last = info
	w.mu.Unlock()
	state := "completa"
	if !res.Complete {
		state = fmt.Sprintf("incompleta: %v", res.Stop)
	}
	w.opt.Logf("captura: %s id %d: %d filas, %d bytes, %d ms, %s",
		res.File, res.ShotID, res.Rows, res.Bytes, res.Elapsed.Milliseconds(), state)
	w.changed()
}

// changed passes the status to OnChange, outside the lock.
func (w *Watcher) changed() {
	if w.opt.OnChange != nil {
		w.opt.OnChange(w.Status())
	}
}

// fail records an error for the status. The log gets it once for each new message.
func (w *Watcher) fail(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	w.mu.Lock()
	changed := w.st.LastError != msg
	w.st.LastError = msg
	w.st.LastErrorAt = w.opt.Now()
	w.mu.Unlock()
	if changed {
		w.opt.Logf("captura: %s", msg)
	}
	w.changed()
}

func (w *Watcher) setWatching(on bool) {
	w.mu.Lock()
	w.st.Watching = on
	w.mu.Unlock()
	w.changed()
}
