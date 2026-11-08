package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
)

// Composite tools answer a whole question in one call. They are listed first so that the
// model sees them before the single-purpose getters.

const (
	descContexto = "Resume en una sola llamada lo que hace falta para una pregunta de situación: zona, coordenadas, personaje, misión rastreada con su siguiente paso y comando de marca, objetivo, profesiones y frescura de los datos. Úsala primero en cualquier pregunta de «¿dónde estoy?», «¿qué hago?» o «¿qué me conviene?»."

	descGuia = "Guía de una misión: objetivos con su progreso, dónde aceptarla y entregarla, el objetivo más cercano con sus coordenadas y el comando /compa marca listo para copiar. Sin quest_id usa la misión rastreada. Úsala para decir el siguiente paso de una misión."

	descPlan = "Plan de profesiones: nivel y máximo de cada profesión, si conviene entrenar el siguiente rango ya y los entrenadores conocidos de cada una con sus coordenadas. Úsala para preguntas sobre cuándo o dónde entrenar una profesión."
)

func contextoJugador(g *gamedata.Service) Def {
	return Def{
		Name:        "contexto_jugador",
		Description: descContexto,
		InputSchema: schema(nil),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.PlayerContext()
			if err != nil {
				return Result{}, fmt.Errorf("no pude reunir el contexto del jugador: %v", err)
			}
			return jsonResult(v)
		},
	}
}

type guideArgs struct {
	QuestID *int64 `json:"quest_id"`
}

func guiaMision(g *gamedata.Service) Def {
	return Def{
		Name:        "guia_mision",
		Description: descGuia,
		InputSchema: schema(map[string]any{
			"quest_id": intProp("Número de la misión. Si se omite, se usa la misión rastreada.", 1),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			var a guideArgs
			if err := decode(args, &a); err != nil {
				return Result{}, err
			}
			if a.QuestID != nil && *a.QuestID < 0 {
				return Result{}, errors.New("quest_id debe ser un número positivo")
			}
			if g == nil {
				return Result{}, errNoGame
			}
			var id int64
			if a.QuestID != nil {
				id = *a.QuestID
			}
			v, err := g.QuestGuide(id)
			if err != nil {
				return Result{}, fmt.Errorf("no pude preparar la guía de la misión: %v", err)
			}
			return jsonResult(v)
		},
	}
}

func planProfesiones(g *gamedata.Service) Def {
	return Def{
		Name:        "plan_profesiones",
		Description: descPlan,
		InputSchema: schema(nil),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.ProfessionPlan()
			if err != nil {
				return Result{}, fmt.Errorf("no pude preparar el plan de profesiones: %v", err)
			}
			return jsonResult(v)
		},
	}
}
