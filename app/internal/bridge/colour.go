package bridge

import "math"

// colourModel maps the colours the client shows to the levels the addon drew. Two shapes are
// fitted from the calibration colours and the better one (by RMS error in colour values) is used:
//
//   - linear: o = A e + b. A gain, a digital vibrance (a change between channels) and an
//     offset are all linear. The inverse A^-1 undoes them.
//   - power: o_c = exp(alpha_c) * e_c^gamma_c, per channel. A gamma is a power law; with
//     gamma = 1 this is a plain gain.
//
// A channel with a level of 0 or an observed 0 contributes nothing to a power fit (log of 0).
type colourModel struct {
	power bool
	// linear
	a   [3][3]float64
	b   [3]float64
	inv [3][3]float64
	// power
	alpha [3]float64
	gamma [3]float64
}

// acceptDistance is the squared distance (over the three channels) below which the rounded
// estimate of a colour is taken without a search: about one colour value of error per channel.
const acceptDistance = 3

// offsetResidualMax is the RMS error, in colour values, above which the linear model without an
// offset is refused and the offset is fitted too.
const offsetResidualMax = 6

// rowFit fits output channel r as a linear function of the known levels, o_r = a . e (+ b when
// offset), by least squares over the pairs whose output is below clipLevel (the known input is
// exact even when the output clips, so a clipped output is left out of its own row). Pairs with
// drop[k] set are left out too. It returns the coefficients and the number of pairs used.
func rowFit(known, seen [][3]byte, r int, offset bool, drop []bool) ([4]float64, int, bool) {
	var xtx [4][4]float64
	var xty [4]float64
	n := 0
	for k := range known {
		if seen[k][r] >= clipLevel || (drop != nil && drop[k]) {
			continue
		}
		n++
		x := [4]float64{float64(known[k][0]), float64(known[k][1]), float64(known[k][2]), 1}
		if !offset {
			x[3] = 0
		}
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				xtx[i][j] += x[i] * x[j]
			}
			xty[i] += x[i] * float64(seen[k][r])
		}
	}
	need := 3
	if offset {
		need = 4
	} else {
		xtx[3][3] = 1 // pins the unused offset to 0 without coupling the other terms
	}
	if n < need {
		return [4]float64{}, n, false
	}
	beta, ok := solve4(xtx, xty)
	return beta, n, ok
}

// fitRow is rowFit with the clamp rule: a zero output where the fit predicts no more than zero is
// the client's clamp at 0, which says only that the true value is at most zero. Such pairs are
// dropped and the row is fitted again.
func fitRow(known, seen [][3]byte, r int, offset bool) ([4]float64, bool) {
	beta, _, ok := rowFit(known, seen, r, offset, nil)
	if !ok {
		return [4]float64{}, false
	}
	drop := make([]bool, len(known))
	any := false
	for k := range known {
		if seen[k][r] == 0 && predictRow(beta, known[k], offset) <= 0 {
			drop[k], any = true, true
		}
	}
	if any {
		if b2, _, ok2 := rowFit(known, seen, r, offset, drop); ok2 {
			return b2, true
		}
	}
	return beta, true
}

// predictRow is the linear prediction of one output channel for the known levels e.
func predictRow(beta [4]float64, e [3]byte, offset bool) float64 {
	v := beta[0]*float64(e[0]) + beta[1]*float64(e[1]) + beta[2]*float64(e[2])
	if offset {
		v += beta[3]
	}
	return v
}

// fitLinearWith fits every output row of the linear model, with or without the offset.
func fitLinearWith(known, seen [][3]byte, offset bool) (colourModel, bool) {
	var m colourModel
	for r := 0; r < 3; r++ {
		beta, ok := fitRow(known, seen, r, offset)
		if !ok {
			return colourModel{}, false
		}
		m.a[r] = [3]float64{beta[0], beta[1], beta[2]}
		m.b[r] = beta[3]
	}
	inv, ok := invert3(m.a)
	if !ok {
		return colourModel{}, false
	}
	m.inv = inv
	return m, true
}

// fitPower fits o_c = exp(alpha_c) * e_c^gamma by least squares in the log domain. The gamma is
// one value for all channels (a display gamma is a single setting), so the three channels give
// nine points for it; each channel has its own intercept, which carries the gain. Only the pairs
// with a level above 0 and an unclipped observation above 0 count.
func fitPower(known, seen [][3]byte) (colourModel, bool) {
	var m colourModel
	m.power = true
	pt := func(k, c int) (float64, float64, bool) {
		e := float64(known[k][c])
		o := float64(seen[k][c])
		if e <= 0 || o < 1 || o >= clipLevel {
			return 0, 0, false
		}
		return math.Log(e), math.Log(o), true
	}
	var cnt, mx, my [3]float64
	for c := 0; c < 3; c++ {
		for k := range known {
			if x, y, ok := pt(k, c); ok {
				cnt[c]++
				mx[c] += x
				my[c] += y
			}
		}
		if cnt[c] < 2 {
			return colourModel{}, false
		}
		mx[c] /= cnt[c]
		my[c] /= cnt[c]
	}
	var sxx, sxy float64
	for c := 0; c < 3; c++ {
		for k := range known {
			if x, y, ok := pt(k, c); ok {
				sxx += (x - mx[c]) * (x - mx[c])
				sxy += (x - mx[c]) * (y - my[c])
			}
		}
	}
	if sxx < 1e-12 {
		return colourModel{}, false
	}
	g := sxy / sxx
	if g < 0.2 || g > 5 {
		return colourModel{}, false
	}
	for c := 0; c < 3; c++ {
		m.gamma[c] = g
		m.alpha[c] = my[c] - g*mx[c]
	}
	return m, true
}

// nibbles returns the nibble of each channel of an observed colour: the grid point whose
// predicted colour is nearest to it. The power model is separable, so each channel takes its
// nearest level. The linear model mixes the channels (vibrance) and clamps at 0 and 255, so the
// whole triple is searched: the 16 x 16 x 16 nibble colours, each predicted through the model
// and clamped as the client clamps it. The inverse of the linear model is not used: once a
// channel is clamped it would corrupt the other two.
func (m colourModel) nibbles(o [3]byte) [3]byte {
	var lv [16]float64
	for n := range lv {
		lv[n] = float64(NibbleValueV31(byte(n)))
	}
	if m.power {
		var out [3]byte
		for c := 0; c < 3; c++ {
			best, bestD := 0, math.Inf(1)
			for n := 0; n < 16; n++ {
				p := 0.0
				if lv[n] > 0 {
					p = m.forwardLevel(c, lv[n])
				}
				if d := math.Abs(float64(o[c]) - p); d < bestD {
					best, bestD = n, d
				}
			}
			out[c] = byte(best)
		}
		return out
	}
	// A colour with no channel near the clamps is well inside the model: the inverse of the
	// linear model puts the nibbles within one step, so the 27 neighbours of that estimate
	// are searched. A clamped colour is searched in full.
	// The rounded inverse estimate is taken when the model, clamped as the client clamps, explains
	// the colour to within noise: this is the usual case, and it is one prediction.
	e := m.inverseLevels(o)
	var centre [3]int
	for c := 0; c < 3; c++ {
		centre[c] = clampNibble(int(math.Round(e[c] * 15 / nibbleTopV31)))
	}
	if m.clampedDistance(o, centre) <= acceptDistance {
		return [3]byte{byte(centre[0]), byte(centre[1]), byte(centre[2])}
	}
	// Otherwise each channel is searched at its neighbours of the estimate, or in full when it is
	// clamped (its estimate is then unknown).
	lo, hi := [3]int{0, 0, 0}, [3]int{15, 15, 15}
	for c := 0; c < 3; c++ {
		if o[c] <= 2 || o[c] >= 253 {
			continue
		}
		lo[c], hi[c] = clampNibble(centre[c]-1), clampNibble(centre[c]+1)
	}
	// The prediction of output r is the sum of three per-level terms (a_rc * level) plus b_r:
	// the terms are tabled once, so each candidate is a few additions.
	var term [3][3][16]float64 // term[r][c][n] = a[r][c] * lv[n]
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			for n := 0; n < 16; n++ {
				term[r][c][n] = m.a[r][c] * lv[n]
			}
		}
	}
	var best [3]byte
	bestD := math.Inf(1)
	for r := lo[0]; r <= hi[0]; r++ {
		for g := lo[1]; g <= hi[1]; g++ {
			for b := lo[2]; b <= hi[2]; b++ {
				var d float64
				for c := 0; c < 3; c++ {
					p := term[c][0][r] + term[c][1][g] + term[c][2][b] + m.b[c]
					v := math.Max(0, math.Min(255, p))
					d += (float64(o[c]) - v) * (float64(o[c]) - v)
				}
				if d < bestD {
					best, bestD = [3]byte{byte(r), byte(g), byte(b)}, d
				}
			}
		}
	}
	return best
}

// clampedDistance is the squared distance between the observed colour and the colour the model
// predicts for the nibbles n, with the prediction clamped to 0..255 as the client clamps it.
func (m colourModel) clampedDistance(o [3]byte, n [3]int) float64 {
	var lv [3]float64
	for c := 0; c < 3; c++ {
		lv[c] = float64(NibbleValueV31(byte(n[c])))
	}
	pred := m.forward(lv)
	var d float64
	for c := 0; c < 3; c++ {
		v := math.Max(0, math.Min(255, pred[c]))
		d += (float64(o[c]) - v) * (float64(o[c]) - v)
	}
	return d
}

// inverseLevels maps an observed colour back to its levels through the inverse of the linear
// model (not clamped). It is used only to place the search.
func (m colourModel) inverseLevels(o [3]byte) [3]float64 {
	var v [3]float64
	for r := 0; r < 3; r++ {
		v[r] = float64(o[r]) - m.b[r]
	}
	var e [3]float64
	for r := 0; r < 3; r++ {
		e[r] = m.inv[r][0]*v[0] + m.inv[r][1]*v[1] + m.inv[r][2]*v[2]
	}
	return e
}

// modelRMS is the RMS error, in colour values, of m over the unclipped observations of known/seen.
func modelRMS(m colourModel, known, seen [][3]byte) float64 {
	var rss float64
	var n int
	for k := range known {
		e := [3]float64{float64(known[k][0]), float64(known[k][1]), float64(known[k][2])}
		pred := m.forward(e)
		for c := 0; c < 3; c++ {
			if seen[k][c] >= clipLevel {
				continue
			}
			d := float64(seen[k][c]) - pred[c]
			if seen[k][c] == 0 && pred[c] <= 0 {
				d = 0 // the clamp at 0 is what the model predicts
			}
			rss += d * d
			n++
		}
	}
	if n == 0 {
		return math.Inf(1)
	}
	return math.Sqrt(rss / float64(n))
}

// fitColourModel fits the colour model from the pairs (known e, seen o). The linear model is
// tried first without an offset (a gain or a saturation change needs none, and fewer unknowns
// keep the noise of the calibration out of the nibbles), with the offset only when the residual
// is above offsetResidualMax. The power model is fitted too; the one with the smaller RMS wins,
// the linear one on a tie.
func fitColourModel(known, seen [][3]byte) (colourModel, bool) {
	if len(known) < 3 || len(known) != len(seen) {
		return colourModel{}, false
	}
	var lin colourModel
	linOK := false
	if m, ok := fitLinearWith(known, seen, false); ok && modelRMS(m, known, seen) <= offsetResidualMax {
		lin, linOK = m, true
	} else if m, ok := fitLinearWith(known, seen, true); ok {
		lin, linOK = m, true
	}
	pw, pwOK := fitPower(known, seen)
	switch {
	case linOK && pwOK:
		if modelRMS(pw, known, seen) < modelRMS(lin, known, seen)-0.2 {
			return pw, true
		}
		return lin, true
	case linOK:
		return lin, true
	case pwOK:
		return pw, true
	}
	return colourModel{}, false
}

// forwardLevel is the power model's colour of channel c at level e.
func (m colourModel) forwardLevel(c int, e float64) float64 {
	return math.Exp(m.alpha[c]) * math.Pow(e, m.gamma[c])
}

// forward maps levels to the colours the client shows for them (before clamping).
func (m colourModel) forward(e [3]float64) [3]float64 {
	var o [3]float64
	if m.power {
		for c := 0; c < 3; c++ {
			if e[c] > 0 {
				o[c] = m.forwardLevel(c, e[c])
			}
		}
		return o
	}
	for r := 0; r < 3; r++ {
		o[r] = m.a[r][0]*e[0] + m.a[r][1]*e[1] + m.a[r][2]*e[2] + m.b[r]
	}
	return o
}

// clampNibble limits a nibble index to 0..15.
func clampNibble(n int) int {
	return int(math.Max(0, math.Min(15, float64(n))))
}
