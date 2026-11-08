package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
)

// Tools that answer from the blocks the addon exports from other addons (WhatsTraining,
// RXPGuides, SkillUpForever) and from the app's own session tracker.

// Aliases of the profession arguments, canonical name first.
var (
	aliasProfesion = []string{"profesion", "profession", "oficio", "nombre", "name"}
	aliasSkillLine = []string{"skill_line_id", "skilllineid", "skill_line", "skillline"}
)

func queEntrenar(g *gamedata.Service) Def {
	return Def{
		Name:        "que_entrenar",
		Description: descEntrenar,
		InputSchema: schema(nil),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.TrainingPlan()
			if err != nil {
				return Result{}, fmt.Errorf("no pude preparar el entrenamiento: %v", err)
			}
			return jsonResult(v)
		},
	}
}

func pasoGuia(g *gamedata.Service) Def {
	return Def{
		Name:        "paso_guia",
		Description: descPasoGuia,
		InputSchema: schema(nil),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.Guide()
			if err != nil {
				return Result{}, fmt.Errorf("no pude leer el paso de la guía: %v", err)
			}
			return jsonResult(v)
		},
	}
}

func rutaProfesion(g *gamedata.Service) Def {
	return Def{
		Name:        "ruta_profesion",
		Description: descRutaProfesion,
		InputSchema: schema(map[string]any{
			"profesion":     strProp("Nombre de la profesión, en castellano o en inglés (por ejemplo Sastrería o Tailoring). Si se omite, devuelve todas."),
			"skill_line_id": intProp("Número de la línea de habilidad de la profesión (por ejemplo 197 para Sastrería).", 1),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			nombre, _ := a.text(aliasProfesion...)
			id, ok, ierr := a.integer(aliasSkillLine...)
			if ierr != nil || (ok && id < 0) {
				return Result{}, errors.New("skill_line_id debe ser un número positivo")
			}
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.ProfessionRoute(nombre, id)
			if err != nil {
				return Result{}, fmt.Errorf("no pude preparar la ruta de la profesión: %v", err)
			}
			return jsonResult(v)
		},
	}
}

func resumenSesion(g *gamedata.Service) Def {
	return Def{
		Name:        "resumen_sesion",
		Description: descResumenSesion,
		InputSchema: schema(nil),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.SessionSummary()
			if err != nil {
				return Result{}, fmt.Errorf("no pude preparar el resumen de la sesión: %v", err)
			}
			return jsonResult(v)
		},
	}
}
