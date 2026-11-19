package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MTEKode/companeroWoW/app/internal/tools"
)

type consultaOut struct {
	Columnas []string `json:"columnas"`
	Filas    [][]any  `json:"filas"`
	N        int      `json:"n"`
	Truncado bool     `json:"truncado"`
	Aviso    string   `json:"aviso"`
}

func callSQL(t *testing.T, reg *tools.Registry, args map[string]any) (consultaOut, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Call(context.Background(), "consultar_db", raw)
	if err != nil {
		return consultaOut{}, err
	}
	if res.IsError {
		return consultaOut{}, errors.New(textOf(t, res))
	}
	var out consultaOut
	if err := json.Unmarshal([]byte(textOf(t, res)), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, textOf(t, res))
	}
	return out, nil
}

func TestConsultarDBReturnsColumnsAndRows(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	out, err := callSQL(t, reg, map[string]any{"sql": `SELECT titulo, nivel FROM v_misiones_conocidas ORDER BY mision_id`})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out.Columnas) != 2 || out.Columnas[0] != "titulo" || out.Columnas[1] != "nivel" {
		t.Fatalf("columns = %v", out.Columnas)
	}
	if out.N == 0 || out.N != len(out.Filas) {
		t.Fatalf("n = %d, rows = %d", out.N, len(out.Filas))
	}
	if out.Truncado {
		t.Errorf("a short result is marked truncated")
	}
}

func TestConsultarDBLimitAndTruncation(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	cte := `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 10) SELECT x FROM c`
	out, err := callSQL(t, reg, map[string]any{"sql": cte, "limite": 3})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if out.N != 3 || !out.Truncado || out.Aviso == "" {
		t.Fatalf("limite 3: n=%d truncado=%v aviso=%q", out.N, out.Truncado, out.Aviso)
	}
	out, err = callSQL(t, reg, map[string]any{"sql": cte})
	if err != nil || out.N != 10 || out.Truncado {
		t.Fatalf("default limit: n=%d truncado=%v err=%v", out.N, out.Truncado, err)
	}
}

func TestConsultarDBOutputIsCompact(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	raw, _ := json.Marshal(map[string]any{"sql": `SELECT 1 AS uno`})
	res, err := reg.Call(context.Background(), "consultar_db", raw)
	if err != nil {
		t.Fatal(err)
	}
	text := textOf(t, res)
	if strings.Contains(text, "\n") {
		t.Fatalf("result is indented, want one compact line: %q", text)
	}
}

func TestConsultarDBRefusesBaseTablesAndWrites(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	for _, q := range []string{
		`SELECT * FROM quests`,
		`SELECT content FROM messages`,
		`DELETE FROM v_personaje`,
		`SELECT name FROM sqlite_master`,
		`SELECT 1; SELECT 2`,
	} {
		_, err := callSQL(t, reg, map[string]any{"sql": q})
		if err == nil {
			t.Errorf("accepted %q", q)
			continue
		}
		if !strings.Contains(err.Error(), "consulta no permitida") && !strings.Contains(err.Error(), "solo") {
			t.Errorf("refusal of %q is not explained: %v", q, err)
		}
	}
}

func TestConsultarDBArgumentErrors(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	if _, err := callSQL(t, reg, map[string]any{"limite": 5}); err == nil || !strings.Contains(err.Error(), "sql") {
		t.Errorf("missing sql: %v", err)
	}
	if _, err := callSQL(t, reg, map[string]any{"sql": "SELECT 1 AS uno", "limite": 0}); err == nil {
		t.Errorf("limite 0 accepted")
	}
	if _, err := callSQL(t, reg, map[string]any{"sql": "SELECT 1 AS uno", "limite": "muchas"}); err == nil {
		t.Errorf("limite as text accepted")
	}
}

func TestConsultarDBAcceptsAliasedArgument(t *testing.T) {
	reg := tools.Build(fixtureService(t), nil)
	out, err := callSQL(t, reg, map[string]any{"consulta": "SELECT 7 AS siete"})
	if err != nil || out.N != 1 {
		t.Fatalf("consulta alias: %+v %v", out, err)
	}
}
