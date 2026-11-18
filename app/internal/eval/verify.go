package eval

import (
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// The answer verifier runs the grounding checks of the evaluation on every answer before it is
// shown: the coordinates, the quoted names and the /compa barras aplicar commands must come
// from the tool outputs or the player context. The functions here are the exported part; the
// assistant calls them and does the repair round.

// Violation is one claim of an answer that the evidence does not back.
type Violation struct {
	Check  string `json:"check"`
	Detail string `json:"detail"`
	// Claims is how many items of the claim are unbacked (two wrong coordinates are two claims).
	Claims int `json:"afirmaciones"`
}

// minQuotedRunes is the shortest quoted name the verifier checks. A word such as «Ya» or
// «ahora» in quotes is style, not data.
const minQuotedRunes = 4

// Grounding returns the violations of answer against ev: coordinates, quoted names and barras
// commands that the evidence does not hold. An empty result means the answer is grounded.
// Each violation counts its own unbacked items in Claims.
func Grounding(answer string, ev Evidence) []Violation {
	var out []Violation
	if n, detail := missingCoordinates(answer, ev); n > 0 {
		out = append(out, Violation{Check: "coordenadas", Detail: detail, Claims: n})
	}
	if n, detail := missingNames(answer, ev); n > 0 {
		out = append(out, Violation{Check: "nombres", Detail: detail, Claims: n})
	}
	if strings.Contains(answer, barsApply) {
		if c := barCommandCheck(answer, ev); !c.OK {
			out = append(out, Violation{Check: "comando_barras", Detail: c.Detail, Claims: strings.Count(c.Detail, "; ") + 1})
		}
	}
	return out
}

// missingCoordinates counts the "(x, y)" pairs and marca coordinates of the answer that the
// evidence does not hold, and names them.
func missingCoordinates(answer string, ev Evidence) (int, string) {
	nums := evidenceNumbers(ev.text())
	var missing []string
	for _, p := range answerCoordinates(answer) {
		if !pairGrounded(p, nums) {
			missing = append(missing, p.text)
		}
	}
	if len(missing) == 0 {
		return 0, ""
	}
	return len(missing), "coordenadas sin respaldo en las herramientas o el contexto: " + strings.Join(missing, ", ")
}

// missingNames counts the quoted names of the answer that the evidence does not hold. Short
// quoted words are style, not data, and are not checked.
func missingNames(answer string, ev Evidence) (int, string) {
	haystack := strings.ToLower(ev.text())
	var missing []string
	for _, m := range quotedRE.FindAllStringSubmatch(answer, -1) {
		name := strings.TrimSpace(m[1])
		if utf8.RuneCountInString(name) < minQuotedRunes {
			continue
		}
		if !strings.Contains(haystack, strings.ToLower(name)) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return 0, ""
	}
	return len(missing), "nombres entre comillas sin respaldo en las herramientas: " + strings.Join(missing, ", ")
}

// EvidenceFor builds the evidence of one answer from the text of the tool outputs it used and
// the player context block sent with the question.
func EvidenceFor(outputs []string, injected string) Evidence {
	return Evidence{Output: strings.Join(outputs, "\n"), Injected: injected}
}

// VerifierStats counts what the verifier did, since the assistant started (or since the last
// reset). An answer is one question; a violation is an answer with at least one unbacked claim.
type VerifierStats struct {
	Answers    int `json:"respuestas"`
	Violations int `json:"conDatosSinRespaldo"`
	// Claims is the number of unbacked claims found in all answers.
	Claims int `json:"afirmacionesSinRespaldo"`
	// Repaired counts answers that the repair round made grounded.
	Repaired int `json:"reparadas"`
	// Flagged counts answers shown with the warning footer.
	Flagged int `json:"conAviso"`
}

// StatsKeeper is a concurrency-safe VerifierStats.
type StatsKeeper struct {
	mu sync.Mutex
	s  VerifierStats
}

// Add adds the counts of one answer: its violations (claims), whether it was repaired and
// whether it was flagged.
func (k *StatsKeeper) Add(answers, claims int, repaired, flagged bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.s.Answers += answers
	if claims > 0 {
		k.s.Violations++
		k.s.Claims += claims
	}
	if repaired {
		k.s.Repaired++
	}
	if flagged {
		k.s.Flagged++
	}
}

// Snapshot returns the current counts.
func (k *StatsKeeper) Snapshot() VerifierStats {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.s
}

// verifyReporter is implemented by an Asker that runs the verifier. The evaluation reads the
// counts before and after each question to report what the verifier did with it.
type verifyReporter interface {
	VerifyStats() VerifierStats
}

// clauseRE is only used to keep a violation's detail short in a footer.
var clauseRE = regexp.MustCompile(`\s+`)

// FooterDetail is the short text of the violations for the warning footer.
func FooterDetail(vs []Violation) string {
	parts := make([]string, 0, len(vs))
	for _, v := range vs {
		parts = append(parts, clauseRE.ReplaceAllString(v.Detail, " "))
	}
	return strings.Join(parts, "; ")
}

// Claims is the number of unbacked items in a set of violations.
func Claims(vs []Violation) int {
	n := 0
	for _, v := range vs {
		n += v.Claims
	}
	return n
}
