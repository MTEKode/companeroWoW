package tools

import (
	"encoding/json"
	"testing"
)

func TestParseArgsRejectsNonObject(t *testing.T) {
	if _, err := parseArgs(json.RawMessage(`"hola"`)); err == nil {
		t.Fatal("a JSON string is not an argument object")
	}
	for _, empty := range []string{"", "null", "  "} {
		a, err := parseArgs(json.RawMessage(empty))
		if err != nil || len(a) != 0 {
			t.Errorf("parseArgs(%q) = %v, %v; want empty object", empty, a, err)
		}
	}
}

func TestNormKeyIgnoresCaseAndSeparators(t *testing.T) {
	for _, k := range []string{"quest_id", "questID", "questId", "QUEST-ID", " quest id "} {
		if got := normKey(k); got != "questid" {
			t.Errorf("normKey(%q) = %q, want questid", k, got)
		}
	}
}

func TestTextAcceptsNumbersAndCanonicalWins(t *testing.T) {
	a, _ := parseArgs(json.RawMessage(`{"query":"Herbalism","consulta":"Herboristería","texto":42}`))
	if got, ok := a.text(aliasText...); !ok || got != "Herboristería" {
		t.Errorf("text = %q, %v; want canonical consulta", got, ok)
	}
	a, _ = parseArgs(json.RawMessage(`{"texto":42}`))
	if got, ok := a.text(aliasText...); !ok || got != "42" {
		t.Errorf("number as text = %q, %v", got, ok)
	}
	a, _ = parseArgs(json.RawMessage(`{"consulta":"   "}`))
	if _, ok := a.text(aliasText...); ok {
		t.Error("blank text must count as absent")
	}
}

func TestIntegerAcceptsNumbersAndNumericText(t *testing.T) {
	cases := map[string]int64{
		`{"quest_id":90001}`:    90001,
		`{"quest_id":"90001"}`:  90001,
		`{"quest_id":"#95999"}`: 95999,
		`{"quest_id":90001.0}`:  90001,
	}
	for in, want := range cases {
		a, _ := parseArgs(json.RawMessage(in))
		got, ok, err := a.integer(aliasQuestID...)
		if err != nil || !ok || got != want {
			t.Errorf("integer(%s) = %d, %v, %v; want %d", in, got, ok, err, want)
		}
	}
	a, _ := parseArgs(json.RawMessage(`{"quest_id":"ninguno"}`))
	if _, ok, err := a.integer(aliasQuestID...); !ok || err == nil {
		t.Errorf("text without a number must be an error, got ok=%v err=%v", ok, err)
	}
	a, _ = parseArgs(json.RawMessage(`{"quest_id":null}`))
	if _, ok, _ := a.integer(aliasQuestID...); ok {
		t.Error("null must count as absent")
	}
}

func TestIntPtrDropsBadLimit(t *testing.T) {
	a, _ := parseArgs(json.RawMessage(`{"limite":"muchos"}`))
	if p := a.intPtr(aliasLimit...); p != nil {
		t.Errorf("bad limit = %d, want nil", *p)
	}
	a, _ = parseArgs(json.RawMessage(`{"max":"7"}`))
	if p := a.intPtr(aliasLimit...); p == nil || *p != 7 {
		t.Errorf("max=\"7\" = %v, want 7", p)
	}
}
