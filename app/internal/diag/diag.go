// Package diag holds the switches of the app's diagnostic log. The log always gets the lines that
// say what the game-mode reader and the answers did (activations, captures per second, questions,
// clipboard, sound). --trace, or trace in config.json, adds the detail of every capture and frame.
package diag

import (
	"log"
	"sync/atomic"
)

var tracing atomic.Bool

// SetTrace turns the detailed trace lines on or off (--trace, or trace in config.json).
func SetTrace(on bool) { tracing.Store(on) }

// Tracing reports whether the detailed trace lines are written.
func Tracing() bool { return tracing.Load() }

// Tracef writes a trace line when tracing is on. Trace lines start with "trace:" so that they can be
// filtered out of companero.log.
func Tracef(format string, args ...any) {
	if tracing.Load() {
		log.Printf("trace: "+format, args...)
	}
}
