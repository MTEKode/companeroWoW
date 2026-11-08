package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// logRotateEvery is how often the log size is checked while the app runs.
const logRotateEvery = time.Hour

// logFile is the log destination. When the file reaches logMaxBytes it becomes
// companero.log.1 (replacing the previous one) and a new companero.log is started. The
// check runs when the file is opened and then every logRotateEvery.
type logFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
	done chan struct{}
	once sync.Once
}

// openLogFile opens path for appending, rotating it first if it is already large.
func openLogFile(path string) (*logFile, error) {
	l := &logFile{path: path, done: make(chan struct{})}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.rotateLocked(); err != nil {
		return nil, err
	}
	go l.rotateLoop(logRotateEvery)
	return l, nil
}

// Write appends to the file; it is what the standard logger writes to.
func (l *logFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Write(p)
}

// Close stops the hourly check and closes the file.
func (l *logFile) Close() error {
	l.once.Do(func() { close(l.done) })
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}

func (l *logFile) rotateLoop(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-t.C:
			l.mu.Lock()
			if err := l.rotateLocked(); err != nil {
				// Logging here would write to the file being rotated; stderr is the only option.
				fmt.Fprintln(os.Stderr, "log rotation:", err)
			}
			l.mu.Unlock()
		}
	}
}

// rotateLocked rotates the file if it is large and makes sure it is open. The caller
// holds l.mu, or is the only user (openLogFile).
func (l *logFile) rotateLocked() error {
	if l.f != nil {
		fi, err := l.f.Stat()
		if err != nil {
			return fmt.Errorf("stat log %s: %w", l.path, err)
		}
		if fi.Size() < logMaxBytes {
			return nil
		}
		if err := l.f.Close(); err != nil {
			return fmt.Errorf("close log %s: %w", l.path, err)
		}
		l.f = nil
		if err := os.Rename(l.path, l.path+".1"); err != nil {
			// Keep appending to the old file rather than losing the log.
			if f, oerr := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); oerr == nil {
				l.f = f
			}
			return fmt.Errorf("rotate log: %w", err)
		}
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log %s: %w", l.path, err)
	}
	l.f = f
	return nil
}

// setupLog sends the standard log to companero.log in dir. See logFile for the rotation.
func setupLog(dir string) (io.Closer, error) {
	lf, err := openLogFile(filepath.Join(dir, "companero.log"))
	if err != nil {
		return nil, err
	}
	log.SetOutput(lf)
	log.SetFlags(log.LstdFlags)
	return lf, nil
}
