package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// Argument names of consultar_db, canonical first.
var (
	aliasSQL    = []string{"sql", "consulta", "query", "consulta_sql", "select"}
	aliasLimite = []string{"limite", "limit", "filas", "max_filas", "limite_filas"}
)

// descConsultarDB is the tool description: how to use the tool, then the schema doc of the views
// (store/VIEWS.md), so the model sees every view and column it may query.
func descConsultarDB() string {
	return "Consulta de solo lectura sobre los datos que la app ha guardado de WoW Forever y del personaje: " +
		"misiones, PNJ, vendedores, instructores, vuelos, objetos, consumibles, combates, muertes, sesiones, " +
		"equipo, candidatos de equipo, macros, barras y mazmorras. Úsala para listas, filtros, historial y " +
		"comparaciones que las demás herramientas no cubren. Escribe una sola sentencia SELECT (o WITH ... SELECT) " +
		"que lea solo de las vistas v_* de abajo; la respuesta trae las columnas y las filas en JSON. " +
		"Por defecto devuelve 100 filas (limite, máximo 500) y tiene 2 segundos.\n\n" +
		store.ViewsDoc()
}

// consultarDB is the read-only SQL tool over the views (store.QueryReadOnly).
func consultarDB(g *gamedata.Service) Def {
	return Def{
		Name:        "consultar_db",
		Description: descConsultarDB(),
		InputSchema: schema(map[string]any{
			"sql":    strProp("Una sentencia SELECT o WITH ... SELECT sobre las vistas v_* (ver la descripción). Solo lectura."),
			"limite": intProp("Filas a devolver: por defecto 100, máximo 500.", 1),
		}, "sql"),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			sqlText, _ := a.text(aliasSQL...)
			if sqlText == "" {
				return Result{}, errors.New("falta la consulta: indica una sentencia SELECT en el campo sql")
			}
			limit, ok, lerr := a.integer(aliasLimite...)
			if lerr != nil || (ok && limit < 1) {
				return Result{}, errors.New("limite debe ser un número de filas entre 1 y 500")
			}
			if g == nil || g.Store() == nil {
				return Result{}, errNoGame
			}
			res, err := g.Store().QueryReadOnly(ctx, sqlText, int(limit))
			if err != nil {
				return Result{}, err
			}
			out := map[string]any{
				"columnas": res.Columns,
				"filas":    res.Rows,
				"n":        len(res.Rows),
				"truncado": res.Truncated,
			}
			if res.Truncated {
				out["aviso"] = fmt.Sprintf("hay más filas que las devueltas: acota la consulta con WHERE o pide un limite mayor (máximo %d)", store.MaxQueryRows)
			}
			b, err := json.Marshal(out)
			if err != nil {
				return Result{}, fmt.Errorf("no pude dar formato a la respuesta: %v", err)
			}
			return textResult(string(b)), nil
		},
	}
}
