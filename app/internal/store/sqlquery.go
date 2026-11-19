package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// QueryTimeout is how long a read-only query may run before it is interrupted.
const QueryTimeout = 2 * time.Second

// maxCellText bounds one text value in a result, so a long JSON column cannot flood the answer.
const maxCellText = 1000

// QueryResult is the outcome of a read-only query: the column names, the rows in column order, and
// whether more rows existed than the limit allowed.
type QueryResult struct {
	Columns   []string
	Rows      [][]any
	Truncated bool
}

// QueryReadOnly runs one read-only SELECT over the views. The statement is checked by
// ValidateReadOnlySQL, then runs on a second connection that is read-only (mode=ro and
// PRAGMA query_only), wrapped in SELECT * FROM (...) LIMIT n so the row cap holds whatever the
// statement is. limit <= 0 means DefaultQueryRows and anything above MaxQueryRows is cut to it.
// The run is cancelled after QueryTimeout, which interrupts SQLite.
func (s *Store) QueryReadOnly(ctx context.Context, query string, limit int) (QueryResult, error) {
	var res QueryResult
	body, _, err := checkReadOnly(query)
	if err != nil {
		return res, err
	}
	if limit <= 0 {
		limit = DefaultQueryRows
	}
	if limit > MaxQueryRows {
		limit = MaxQueryRows
	}
	db, err := s.readOnlyDB()
	if err != nil {
		return res, err
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, "SELECT * FROM (\n"+body+"\n) LIMIT ?", limit+1)
	if err != nil {
		return res, queryError(ctx, err)
	}
	defer rows.Close()
	res.Columns, err = rows.Columns()
	if err != nil {
		return res, queryError(ctx, err)
	}
	for rows.Next() {
		if len(res.Rows) == limit {
			res.Truncated = true
			break
		}
		vals := make([]any, len(res.Columns))
		ptrs := make([]any, len(vals))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return res, queryError(ctx, err)
		}
		for i, v := range vals {
			vals[i] = cellValue(v)
		}
		res.Rows = append(res.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return res, queryError(ctx, err)
	}
	if res.Rows == nil {
		res.Rows = [][]any{}
	}
	return res, nil
}

// queryError explains a failed query in Spanish. A cancelled context is the timeout.
func queryError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("la consulta tardó más de %d s y se canceló; acótala (filtros, menos columnas o un LIMIT)", int(QueryTimeout/time.Second))
	}
	if errors.Is(err, context.Canceled) {
		return errors.New("la consulta se canceló")
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no such column"):
		return fmt.Errorf("error de SQL: una columna no existe (%s). Consulta la documentación de las vistas (VIEWS.md)", msg)
	case strings.Contains(msg, "no such table"), strings.Contains(msg, "no such view"):
		return fmt.Errorf("error de SQL: la vista o tabla no existe (%s). Usa las vistas documentadas: %s", msg, strings.Join(viewNames, ", "))
	case strings.Contains(msg, "readonly"), strings.Contains(msg, "query_only"), strings.Contains(msg, "read-only"):
		return fmt.Errorf("error de SQL: la consulta intentó escribir y no está permitido (%s)", msg)
	}
	return fmt.Errorf("error de SQL: %s", msg)
}

// cellValue turns a scanned value into something JSON can carry: text stays text (cut to
// maxCellText), blobs become a short marker, numbers and nil stay as they are.
func cellValue(v any) any {
	switch t := v.(type) {
	case []byte:
		return fmt.Sprintf("<blob %d bytes>", len(t))
	case string:
		if len(t) > maxCellText {
			return t[:maxCellText] + "…"
		}
		return t
	case nil, int64, float64, bool:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// readOnlyDB returns the connection the queries run on, opened on first use. It is read-only
// at the driver (mode=ro) and at the connection (query_only), and it never runs migrations.
func (s *Store) readOnlyDB() (*sql.DB, error) {
	s.roMu.Lock()
	defer s.roMu.Unlock()
	if s.ro != nil {
		return s.ro, nil
	}
	if s.path == "" {
		return nil, errors.New("base de datos sin ruta: no hay consultas de solo lectura")
	}
	dsn := "file:" + s.path + "?mode=ro&_pragma=busy_timeout(2000)&_pragma=query_only(1)&_pragma=cache_size(-512)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir consulta de solo lectura: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("abrir consulta de solo lectura: %w", err)
	}
	s.ro = db
	return db, nil
}
