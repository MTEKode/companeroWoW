package eval

import (
	"context"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
)

func TestGroundingCountsEachUnbackedCoordinate(t *testing.T) {
	ev := EvidenceFor([]string{`{"x": 51.3, "y": 38.5}`}, "")
	if vs := Grounding("Ve a (51.3, 38.5).", ev); len(vs) != 0 {
		t.Fatalf("a grounded place must pass: %+v", vs)
	}
	vs := Grounding("Ve a (12.3, 45.6) y luego a (70.1, 80.2).", ev)
	if len(vs) != 1 || vs[0].Check != "coordenadas" {
		t.Fatalf("violations = %+v, want one coordinates violation", vs)
	}
	if vs[0].Claims != 2 || Claims(vs) != 2 {
		t.Errorf("two unbacked places must count as two claims: %+v", vs[0])
	}
	if !strings.Contains(vs[0].Detail, "(12.3, 45.6)") || !strings.Contains(vs[0].Detail, "(70.1, 80.2)") {
		t.Errorf("detail must name the places: %q", vs[0].Detail)
	}
}

func TestGroundingFindsUnbackedQuotedNamesButNotShortWords(t *testing.T) {
	ev := EvidenceFor([]string{`{"nombre": "Thrall"}`}, "")
	if vs := Grounding("Habla con «Thrall» (anota «Ya»).", ev); len(vs) != 0 {
		t.Fatalf("a quoted name in the tools and a short word must pass: %+v", vs)
	}
	vs := Grounding("Habla con «Garrosh Hellscream».", ev)
	if len(vs) != 1 || vs[0].Check != "nombres" || vs[0].Claims != 1 {
		t.Fatalf("violations = %+v", vs)
	}
}

func TestGroundingChecksBarCommandsOnlyWhenPresent(t *testing.T) {
	ev := EvidenceFor([]string{`{"id": 585, "nombre": "Dolor"}`}, "")
	if vs := Grounding("Sin barras hoy.", ev); len(vs) != 0 {
		t.Fatalf("an answer without commands has nothing to check: %+v", vs)
	}
	if vs := Grounding("⌨️ /compa barras aplicar 1:585", ev); len(vs) != 0 {
		t.Fatalf("a command with a listed spell passes: %+v", vs)
	}
	vs := Grounding("⌨️ /compa barras aplicar 1:999", ev)
	if len(vs) != 1 || vs[0].Check != "comando_barras" {
		t.Fatalf("an unlisted spell must be a violation: %+v", vs)
	}
}

func TestGroundingUsesTheInjectedContextToo(t *testing.T) {
	ev := EvidenceFor(nil, `{"ubicacion":{"x":51.2,"y":38.7}}`)
	if vs := Grounding("Estás en (51.2, 38.7).", ev); len(vs) != 0 {
		t.Errorf("the player context backs the place: %+v", vs)
	}
}

func TestStatsKeeperCountsAnswersViolationsAndRepairs(t *testing.T) {
	var k StatsKeeper
	k.Add(1, 0, false, false)
	k.Add(1, 2, true, false)
	k.Add(1, 1, false, true)
	s := k.Snapshot()
	if s.Answers != 3 || s.Violations != 2 || s.Claims != 3 || s.Repaired != 1 || s.Flagged != 1 {
		t.Errorf("stats = %+v", s)
	}
}

// verifyingAsker is an Asker that also reports verifier counts; each answer adds one violation
// with two claims, and every second answer is repaired.
type verifyingAsker struct {
	stats StatsKeeper
	n     int
}

func (v *verifyingAsker) Answer(ctx context.Context, q string, shot bool) (ai.Response, error) {
	v.n++
	if v.n%2 == 1 {
		v.stats.Add(1, 2, false, true)
	} else {
		v.stats.Add(1, 2, true, false)
	}
	return ai.Response{Text: "Respuesta " + q, Provider: "fake"}, nil
}

func (v *verifyingAsker) VerifyStats() VerifierStats { return v.stats.Snapshot() }

func TestRunReportsWhatTheVerifierDidDuringTheRun(t *testing.T) {
	asker := &verifyingAsker{}
	// Some earlier verifier work must not show up in this run's report.
	asker.stats.Add(5, 9, true, true)
	qs := Questions[:4]
	rep := Run(context.Background(), asker, nil, qs, "", "test")
	if rep.Verifier == nil {
		t.Fatal("a verifying asker must give the report its stats")
	}
	v := rep.Verifier
	if v.Answers != 4 || v.Violations != 4 || v.Claims != 8 || v.Repaired != 2 || v.Flagged != 2 {
		t.Errorf("run stats = %+v", v)
	}
	if rep.Records[0].Claims != 2 || rep.Records[1].Repaired != true {
		t.Errorf("per answer: %+v / %+v", rep.Records[0], rep.Records[1])
	}
	md := Markdown(rep)
	if !strings.Contains(md, "Verificador: 4 respuestas") {
		t.Errorf("the report must show the verifier statistics:\n%s", md[:200])
	}
}

func TestRunWithoutVerifierHasNoStats(t *testing.T) {
	rep := Run(context.Background(), &fakeAsker{}, nil, Questions[:1], "", "test")
	if rep.Verifier != nil {
		t.Errorf("an asker without the verifier must leave the stats out: %+v", rep.Verifier)
	}
	if strings.Contains(Markdown(rep), "Verificador:") {
		t.Error("no verifier line without stats")
	}
}
