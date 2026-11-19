package bridge

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Colour gain. The client's display settings (brightness, gamma, digital vibrance, HDR) scale
// the colours of the strip. A gain up to about x2.2 is decoded (protocol v3.1 keeps its
// levels below 110 for that); a gain outside [gainMin, gainMax], or a calibration colour that
// clips at 255, is not decoded and is reported as a ColourGainError, so that the player is told
// what to change instead of seeing nothing.
const (
	// ColourGainAfter is how long the gain error must persist before the player is told.
	ColourGainAfter = 2 * time.Second

	gainMin = 0.5
	gainMax = 2.5
	// clipLevel is the value from which a channel counts as clipped (255 minus the noise).
	clipLevel = 250
	// gainResidualMax is the largest RMS error, in colour values, of a per-channel gain fit
	// that still counts as a gain. Larger residuals mean the strip is not a scaled calibration
	// (a different layout, or a cross-channel change), and the gain is not reported.
	gainResidualMax = 16
)

// ErrColourGain is the error of a strip whose colours the client has altered beyond what the
// decoder reads (see ColourGainError). Use errors.Is to test it.
var ErrColourGain = errors.New("bridge: pixel colours are altered (gain outside [0.5, 2.5] or clipped)")

// ColourGainError reports a strip whose calibration colours show a gain outside the decodable
// range, or clipping. Gain is the estimated gain (1 = unaltered); Clipped says that a calibration
// level reads 255 and the top of the range is lost.
type ColourGainError struct {
	Gain    float64
	Clipped bool
}

func (e *ColourGainError) Error() string {
	return fmt.Sprintf("bridge: pixel colours altered: gain x%.2f, clipped %t", e.Gain, e.Clipped)
}

// Is makes errors.Is(err, ErrColourGain) true for a ColourGainError.
func (e *ColourGainError) Is(target error) bool { return target == ErrColourGain }

// ColourGainOf returns the estimated gain when err is (or wraps) a ColourGainError.
func ColourGainOf(err error) (float64, bool) {
	var g *ColourGainError
	if errors.As(err, &g) {
		return g.Gain, true
	}
	return 0, false
}

// ColourGainText is the notice for the player about a colour gain, in Spanish: the status bar and
// the clipboard notice in the game mode both say it.
func ColourGainText(gain float64) string {
	return fmt.Sprintf("Los colores de la pantalla están alterados (brillo/gamma/intensidad digital/HDR ≈ ×%.1f). "+
		"Vuelve a valores normales o desactiva el HDR y la intensidad digital.", gain)
}

// gainFit is the per-channel gain the calibration colours show: a least-squares line through
// the origin over the unclipped points, with the RMS residual of that line.
type gainFit struct {
	gain    float64
	clipped bool // a calibration level above 0 reads at or above clipLevel
	ok      bool // enough points, and the residual is small enough to be a gain
}

// fitGain compares the seen colours of the calibration blocks with their known colours. Only
// the levels strictly between 0 and clipLevel are used: 0 says nothing about a gain, and 255
// (magic, white) reads 255 at any gain of at least one.
func fitGain(known, seen [][3]byte) gainFit {
	var sxy, sxx float64
	var es, os []float64
	var g gainFit
	for k := range known {
		for c := 0; c < 3; c++ {
			e := float64(known[k][c])
			if e <= 0 || e >= clipLevel {
				continue
			}
			o := float64(seen[k][c])
			if o >= clipLevel {
				g.clipped = true
				continue
			}
			es = append(es, e)
			os = append(os, o)
			sxy += e * o
			sxx += e * e
		}
	}
	if len(es) < 2 || sxx == 0 {
		return g
	}
	g.gain = sxy / sxx
	var rss float64
	for i := range es {
		r := os[i] - g.gain*es[i]
		rss += r * r
	}
	g.ok = math.Sqrt(rss/float64(len(es))) <= gainResidualMax
	return g
}

// gainError returns a ColourGainError when the seen calibration colours show a gain outside
// [gainMin, gainMax] or clipping, and nil otherwise.
func gainError(known, seen [][3]byte) error {
	g := fitGain(known, seen)
	if !g.ok || !(g.clipped || g.gain < gainMin || g.gain > gainMax) {
		return nil
	}
	return &ColourGainError{Gain: g.gain, Clipped: g.clipped}
}
