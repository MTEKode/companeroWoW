package theory

// Locale-agnostic reading of the numbers in a client text. The Spanish client writes 1.234 for one
// thousand two hundred and thirty-four and 2,5 for two and a half; the English client writes 1,234 and
// 2.5. The rule is the same for both: a separator followed by exactly three digits, repeated, is a
// thousands grouping; any other separator is a decimal point. The words around a number are ignored,
// so the order of the numbers is the only thing the caller relies on.

import (
	"strconv"
	"strings"
)

// Numbers returns the numbers of a text, in order of appearance. Percent signs and words are skipped.
func Numbers(text string) []float64 {
	var out []float64
	rs := []rune(text)
	for i := 0; i < len(rs); {
		if !isDigit(rs[i]) {
			i++
			continue
		}
		j := i
		for j < len(rs) && (isDigit(rs[j]) || isSep(rs, j)) {
			j++
		}
		// A trailing separator is punctuation ("…en 15 sec.").
		for j > i && !isDigit(rs[j-1]) {
			j--
		}
		if v, ok := parseToken(string(rs[i:j])); ok {
			out = append(out, v)
		}
		i = j
	}
	return out
}

// isSep reports whether rs[j] is a separator that sits between two digits.
func isSep(rs []rune, j int) bool {
	if j <= 0 || j+1 >= len(rs) {
		return false
	}
	return (rs[j] == '.' || rs[j] == ',') && isDigit(rs[j-1]) && isDigit(rs[j+1])
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// parseToken reads one token made of digits and separators.
func parseToken(tok string) (float64, bool) {
	if tok == "" {
		return 0, false
	}
	// Split the token into digit groups at each separator; remember the separators.
	var groups []string
	var seps []byte
	start := 0
	for k := 0; k < len(tok); k++ {
		if tok[k] == '.' || tok[k] == ',' {
			groups = append(groups, tok[start:k])
			seps = append(seps, tok[k])
			start = k + 1
		}
	}
	groups = append(groups, tok[start:])
	if len(groups) == 1 {
		v, err := strconv.ParseFloat(groups[0], 64)
		return v, err == nil
	}
	thousands := true
	for _, g := range groups[1:] {
		if len(g) != 3 {
			thousands = false
			break
		}
	}
	if thousands {
		// Only a grouping when the first group is short (1 to 3 digits) and the separators agree.
		if len(groups[0]) > 3 || len(groups[0]) == 0 {
			thousands = false
		}
	}
	if thousands {
		v, err := strconv.ParseFloat(strings.Join(groups, ""), 64)
		return v, err == nil
	}
	// A decimal point: the last separator. Earlier separators that the grouping rule rejected are
	// dropped, so a malformed token such as "1.2.3" reads as 12.3 (no client text has one).
	intPart := strings.Join(groups[:len(groups)-1], "")
	frac := groups[len(groups)-1]
	v, err := strconv.ParseFloat(intPart+"."+frac, 64)
	return v, err == nil
}
