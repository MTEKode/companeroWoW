package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
)

// World tools: travel by the measured flight paths, where to buy and train (with the cost),
// quest chains and consumables. They answer from the world block the addon exports (account-wide).

// Aliases of the arguments of the world tools, canonical name first.
var (
	aliasDestino  = []string{"destino", "destination", "lugar", "donde", "zona", "npc", "mision", "texto", "consulta", "nombre", "name"}
	aliasPeleas   = []string{"peleas", "pulls", "combates", "enemigos", "cantidad", "n"}
	aliasItemName = []string{"nombre", "name", "objeto", "item", "texto", "consulta"}
)

const (
	descRutaViaje = "Calcula cómo llegar a un destino desde donde estás: a pie o con los vuelos (taxi) que el addon ha medido, con el tiempo total, el coste en cobre, los pasos («toma vuelo en X hacia Y») y la marca del primer paso. Acepta zona, NPC, misión (por su nombre), un nodo de vuelo o coordenadas («mapID x y» o «x y» en el mapa actual, escala 0-100). Los vuelos son medidos; caminar es estimado. Úsala para «¿cómo llego a X?» o «¿me conviene el vuelo?». Argumentos: destino."

	descDondeComprar = "Dónde comprar un objeto: los vendedores que el addon ha visto con ese objeto, el precio, el tiempo de llegar desde donde estás y su marca. Si no hay datos de stock, dice qué vendedores del mapa conoce. Úsala para «¿dónde compro X?» o «¿cuánto cuesta X?». Argumentos: nombre o item_id."

	descCosteEntrenar = "Cuánto cuesta entrenar: el coste de lo que puedes aprender ahora y del siguiente nivel o rango, con el entrenador y dónde está, y si te alcanza el oro. Sin argumentos, los hechizos de tu clase; con profesion, los de esa profesión (su rango actual); con nombre, un servicio concreto. Úsala para «¿cuánto me cuesta entrenar?» o «¿cuánto me cuesta Sastrería?». Argumentos: profesion y nombre (opcionales)."

	descCadenaMision = "Cadena de una misión: qué misiones van antes (y cuáles le faltan al personaje), qué desbloquea después, el estado de cada una (hecha, en registro o pendiente) y un orden sugerido. Viene de Questie y del orden que el addon vio al aceptar y entregar. Úsala para «¿qué misiones hago antes de X?» o «¿qué viene después?». Argumentos: quest_id o nombre."

	descConsumibles = "Plan de consumibles: lo que lleva el personaje (comida, bebida, pociones, vendajes), qué restaura más por cobre y por segundo, dónde comprarlo, qué vendajes y comida permiten sus profesiones de Primeros auxilios y Cocina, y cuántos llevar para N peleas según el daño recibido en las peleas que ha ganado. Úsala para «¿qué me conviene llevar?», «¿cuántos vendajes necesito?» o «¿dónde compro comida?». Argumentos: peleas (por defecto 5)."
)

// rutaViaje answers ruta_viaje.
func rutaViaje(g *gamedata.Service) Def {
	return Def{
		Name:        "ruta_viaje",
		Description: descRutaViaje,
		InputSchema: schema(map[string]any{
			"destino": strProp("Zona, NPC, misión o nodo de vuelo; o coordenadas («mapID x y» o «x y», escala 0-100)."),
		}, "destino"),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			destino, _ := a.text(aliasDestino...)
			if destino == "" {
				return Result{}, errors.New("falta el destino: di una zona, un NPC, una misión o unas coordenadas")
			}
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.RouteTo(destino)
			if err != nil {
				return Result{}, fmt.Errorf("no pude calcular la ruta: %v", err)
			}
			return jsonResult(v)
		},
	}
}

// dondeComprar answers donde_comprar.
func dondeComprar(g *gamedata.Service) Def {
	return Def{
		Name:        "donde_comprar",
		Description: descDondeComprar,
		InputSchema: schema(map[string]any{
			"nombre":  strProp("Nombre del objeto, o parte de él."),
			"item_id": intProp("Número del objeto (item_id), si se conoce.", 1),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			nombre, _ := a.text(aliasItemName...)
			id, ok, ierr := a.integer(aliasItemID...)
			if ierr != nil || (ok && id < 0) {
				return Result{}, errors.New("item_id debe ser un número positivo")
			}
			if nombre == "" && id <= 0 {
				return Result{}, errors.New("falta el nombre o el item_id del objeto")
			}
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.BuyOptions(nombre, id)
			if err != nil {
				return Result{}, err
			}
			return jsonResult(v)
		},
	}
}

// costeEntrenamiento answers coste_entrenamiento.
func costeEntrenamiento(g *gamedata.Service) Def {
	return Def{
		Name:        "coste_entrenamiento",
		Description: descCosteEntrenar,
		InputSchema: schema(map[string]any{
			"profesion": strProp("Opcional: la profesión (en castellano o en inglés), por ejemplo Sastrería o Tailoring."),
			"nombre":    strProp("Opcional: el nombre de un servicio o hechizo de entrenador."),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			profesion, _ := a.text(aliasProfesion...)
			nombre, _ := a.text(aliasNombre...)
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.TrainingCost(profesion, nombre)
			if err != nil {
				return Result{}, fmt.Errorf("no pude calcular el coste de entrenar: %v", err)
			}
			return jsonResult(v)
		},
	}
}

// cadenaMision answers cadena_mision.
func cadenaMision(g *gamedata.Service) Def {
	return Def{
		Name:        "cadena_mision",
		Description: descCadenaMision,
		InputSchema: schema(map[string]any{
			"quest_id": intProp("Número de la misión (quest_id), si se conoce.", 1),
			"nombre":   strProp("Nombre de la misión, si no se conoce el número."),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			id, ok, ierr := a.integer(aliasQuestID...)
			if ierr != nil || (ok && id < 0) {
				return Result{}, errors.New("quest_id debe ser un número positivo")
			}
			nombre, _ := a.text(aliasNombre...)
			if id <= 0 && nombre == "" {
				return Result{}, errors.New("falta quest_id o nombre de la misión")
			}
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.ChainOf(id, nombre)
			if err != nil {
				return Result{}, err
			}
			return jsonResult(v)
		},
	}
}

// consumibles answers consumibles.
func consumibles(g *gamedata.Service) Def {
	return Def{
		Name:        "consumibles",
		Description: descConsumibles,
		InputSchema: schema(map[string]any{
			"peleas": intProp("Cuántas peleas vas a hacer (por defecto 5, máximo 50).", 1),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			n, ok, ierr := a.integer(aliasPeleas...)
			if ierr != nil || (ok && n < 0) {
				return Result{}, errors.New("peleas debe ser un número positivo")
			}
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.ConsumablesPlan(int(n))
			if err != nil {
				return Result{}, fmt.Errorf("no pude preparar el plan de consumibles: %v", err)
			}
			return jsonResult(v)
		},
	}
}

// addWorldTools registers the world tools. The data is read through g, which may be nil.
func addWorldTools(r *Registry, g *gamedata.Service) {
	r.Add(rutaViaje(g))
	r.Add(dondeComprar(g))
	r.Add(costeEntrenamiento(g))
	r.Add(cadenaMision(g))
	r.Add(consumibles(g))
}
