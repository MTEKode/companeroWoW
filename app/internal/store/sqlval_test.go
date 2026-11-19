package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateReadOnlySQLAllowed(t *testing.T) {
	cases := []string{
		`SELECT * FROM v_personaje`,
		`select personaje, nivel from v_misiones where accesible = 1 order by nivel desc`,
		`SELECT personaje FROM v_combates;`, // one trailing semicolon is fine
		`WITH mejores AS (SELECT objeto FROM v_objetos WHERE nivel_objeto > 5) SELECT * FROM mejores`,
		`WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 5) SELECT x FROM c`,
		`WITH a AS (SELECT 1 AS n), b(m) AS (SELECT n FROM a) SELECT m FROM b`,
		`SELECT 'DROP TABLE quests; -- no es nada' AS texto`,          // ; and -- inside a string
		`SELECT 'quests', 'sqlite_master', 'PRAGMA' FROM v_personaje`, // names inside strings are data
		`SELECT replace(nombre, 'a', 'b') FROM v_personaje`,           // replace() as a function is fine
		`SELECT personaje FROM v_combates /* comentario ; DELETE */ LIMIT 2`,
		`SELECT json_group_array(nombre) FROM v_npcs`,
		`SELECT personaje FROM v_misiones WHERE estado = 'lista_para_entregar' -- fin`,
		`SELECT objeto FROM v_vendedores_objetos JOIN v_objetos ON 1 = 0`,
		`SELECT x FROM (SELECT personaje AS x FROM v_personaje)`,
		`SELECT nombre FROM v_personaje AS p WHERE p.nivel > 3`,
	}
	for _, q := range cases {
		if _, err := ValidateReadOnlySQL(q); err != nil {
			t.Errorf("refused a valid query %q: %v", q, err)
		}
	}
}

func TestValidateReadOnlySQLDenied(t *testing.T) {
	cases := map[string]string{
		"empty":                  ``,
		"only spaces":            "   \n\t ",
		"insert":                 `INSERT INTO maps (map_id, name) VALUES (1, 'x')`,
		"update":                 `UPDATE characters SET level = 99`,
		"delete":                 `DELETE FROM v_personaje`,
		"drop":                   `DROP VIEW v_personaje`,
		"create":                 `CREATE TABLE x (a)`,
		"pragma":                 `PRAGMA user_version = 99`,
		"attach":                 `ATTACH DATABASE 'x.db' AS x`,
		"vacuum":                 `VACUUM`,
		"sqlite_master":          `SELECT name FROM sqlite_master`,
		"sqlite_ident":           `SELECT * FROM sqlite_schema`,
		"pragma function":        `SELECT * FROM pragma_table_info('quests')`,
		"base table":             `SELECT * FROM quests`,
		"base table join":        `SELECT p.nombre FROM v_personaje p JOIN characters c ON c.char_key = p.personaje`,
		"cte of base":            `WITH q AS (SELECT * FROM quests) SELECT * FROM q`,
		"cte named as base":      `WITH quests AS (SELECT 1 AS x) SELECT x FROM quests`,
		"cte with view and base": `WITH a AS (SELECT * FROM v_personaje), b AS (SELECT * FROM npcs) SELECT * FROM a, b`,
		"messages":               `SELECT content FROM messages`,
		"chat history":           `SELECT * FROM messages WHERE role = 'user'`,
		"cache":                  `SELECT value FROM cache`,
		"feedback":               `SELECT answer FROM feedback`,
		"wft table":              `SELECT body FROM wft_pages`,
		"quoted base":            `SELECT * FROM "quests"`,
		"bracket base":           `SELECT * FROM [npcs]`,
		"backtick base":          "SELECT * FROM `maps`",
		"qualified base":         `SELECT * FROM main.quests`,
		"upper base":             `SELECT * FROM QUESTS`,
		"unknown table":          `SELECT * FROM no_existe`,
		"two statements":         `SELECT 1; SELECT 2`,
		"stacked write":          `SELECT 1; DELETE FROM quests`,
		"semicolon in middle":    `SELECT 1;SELECT * FROM v_personaje`,
		"comment hides nothing":  `SELECT 1 /* ; */ ; DROP TABLE quests`,
		"unterminated string":    `SELECT 'abc`,
		"unterminated comment":   `SELECT 1 /* abc`,
		"unicode lookalike":      `SELECT * FROM v_personajе`, // Cyrillic е
		"unicode space":          "SELECT * FROM v_personaje",
		"nul byte":               "SELECT 1\x00; DROP TABLE quests",
		"with insert":            `WITH x AS (SELECT 1) INSERT INTO maps SELECT * FROM x`,
		"with delete":            `WITH x AS (SELECT 1) DELETE FROM quests`,
		"replace statement":      `REPLACE INTO maps (map_id, name) VALUES (1, 'x')`,
		"into":                   `SELECT * INTO backup FROM v_personaje`,
		"subquery base":          `SELECT (SELECT COUNT(*) FROM quests)`,
		"in base":                `SELECT * FROM v_personaje WHERE personaje IN (SELECT char_key FROM characters)`,
		"not select":             `(SELECT 1)`,
		"explain":                `EXPLAIN SELECT * FROM v_personaje`,
		"load_extension":         `SELECT load_extension('x')`,
		"readfile":               `SELECT readfile('/etc/passwd')`,
		"too long":               `SELECT '` + strings.Repeat("a", maxSQLLen) + `'`,
		"bad char":               "SELECT 1 \\ 2",
		"quoted keyword":         `SELECT "insert" AS x FROM v_personaje`,
	}
	for name, q := range cases {
		if _, err := ValidateReadOnlySQL(q); err == nil {
			t.Errorf("%s: accepted %q", name, q)
		} else if !errors.Is(err, ErrNotReadOnly) {
			t.Errorf("%s: error does not wrap ErrNotReadOnly: %v", name, err)
		}
	}
}

func TestValidateExplainsInSpanish(t *testing.T) {
	_, err := ValidateReadOnlySQL(`SELECT * FROM quests`)
	if err == nil || !strings.Contains(err.Error(), "usa las vistas") {
		t.Fatalf("base-table refusal does not point to the views: %v", err)
	}
	_, err = ValidateReadOnlySQL(`DELETE FROM v_personaje`)
	if err == nil || !strings.Contains(err.Error(), "solo lee") {
		t.Fatalf("write refusal is not explained: %v", err)
	}
}

func TestQueryReadOnlyRowCap(t *testing.T) {
	s := openTestStore(t)
	cte := `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 1000) SELECT x FROM c`
	res, err := s.QueryReadOnly(context.Background(), cte, 0)
	if err != nil {
		t.Fatalf("default limit: %v", err)
	}
	if len(res.Rows) != DefaultQueryRows || !res.Truncated {
		t.Fatalf("default: %d rows, truncated=%v; want %d and true", len(res.Rows), res.Truncated, DefaultQueryRows)
	}
	res, err = s.QueryReadOnly(context.Background(), cte, 100000)
	if err != nil {
		t.Fatalf("max limit: %v", err)
	}
	if len(res.Rows) != MaxQueryRows || !res.Truncated {
		t.Fatalf("max: %d rows, truncated=%v; want %d and true", len(res.Rows), res.Truncated, MaxQueryRows)
	}
	res, err = s.QueryReadOnly(context.Background(), `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 3) SELECT x FROM c`, 10)
	if err != nil || len(res.Rows) != 3 || res.Truncated {
		t.Fatalf("small result: %+v %v", res, err)
	}
	if len(res.Columns) != 1 || res.Columns[0] != "x" {
		t.Fatalf("columns = %v, want [x]", res.Columns)
	}
	if res.Rows[2][0] != int64(3) {
		t.Fatalf("last row = %v, want 3", res.Rows[2][0])
	}
}

func TestQueryReadOnlyEmptyResultIsNotNil(t *testing.T) {
	s := openTestStore(t)
	res, err := s.QueryReadOnly(context.Background(), `SELECT personaje FROM v_personaje`, 0)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if res.Rows == nil || len(res.Rows) != 0 || res.Truncated {
		t.Fatalf("empty result: %+v", res)
	}
}

func TestQueryReadOnlyTimeout(t *testing.T) {
	s := openTestStore(t)
	start := time.Now()
	_, err := s.QueryReadOnly(context.Background(),
		`WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT count(*) FROM c`, 0)
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "tardó más de 2 s") {
		t.Fatalf("expected a timeout, got %v after %v", err, elapsed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("the timeout did not interrupt the query: %v", elapsed)
	}
	// The connection is usable afterwards.
	if _, err := s.QueryReadOnly(context.Background(), `SELECT 1 AS uno`, 0); err != nil {
		t.Fatalf("query after timeout: %v", err)
	}
}

func TestQueryReadOnlyCallerCancel(t *testing.T) {
	s := openTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	_, err := s.QueryReadOnly(ctx,
		`WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT count(*) FROM c`, 0)
	if err == nil {
		t.Fatal("a cancelled query returned no error")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("cancel was not honoured: %v", time.Since(start))
	}
}

func TestReadOnlyConnectionRefusesWrites(t *testing.T) {
	s := openTestStore(t)
	db, err := s.readOnlyDB()
	if err != nil {
		t.Fatalf("read-only connection: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO maps (map_id, name) VALUES (1, 'x')`,
		`DELETE FROM characters`,
		`CREATE TABLE hack (a)`,
		`PRAGMA journal_mode = DELETE`,
	} {
		if _, err := db.Exec(stmt); err == nil {
			t.Errorf("the read-only connection ran %q", stmt)
		}
	}
	// The writable connection still works.
	if _, err := s.db.Exec(`INSERT INTO maps (map_id, name) VALUES (1, 'ok')`); err != nil {
		t.Fatalf("writable connection: %v", err)
	}
}

func TestQueryReadOnlyCellsAreCut(t *testing.T) {
	s := openTestStore(t)
	long := strings.Repeat("x", maxCellText+50)
	res, err := s.QueryReadOnly(context.Background(), `SELECT '`+long+`' AS t, X'0102' AS b`, 0)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	got, _ := res.Rows[0][0].(string)
	if len(got) > maxCellText+len("…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("long text not cut: %d bytes", len(got))
	}
	if res.Rows[0][1] != "<blob 2 bytes>" {
		t.Fatalf("blob = %v", res.Rows[0][1])
	}
}
