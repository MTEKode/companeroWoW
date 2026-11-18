package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

// Tools that answer from the gear, quest-window and dungeon blocks the addon exports: what to
// equip, which quests to take, which dungeon to run and where an item comes from.

// Aliases of the arguments, canonical name first.
var (
	aliasVentana = []string{"ventana", "window", "plazo", "cuando"}
	aliasSlot    = []string{"slot", "ranura", "slot_id", "slotid"}
	aliasZona    = []string{"zona", "zone", "lugar", "area"}
	aliasNombre  = []string{"nombre", "name", "mazmorra", "dungeon", "objeto", "item", "texto", "consulta"}
	aliasItemID  = []string{"item_id", "itemid", "id"}
)

const (
	descAnalizarEquipo = "Analiza qué ranuras del equipo conviene mejorar, con qué objeto y de qué misión o mazmorra, solo con lo que el personaje puede obtener: ya («ahora») o pronto (hasta 3 niveles más). Devuelve por ranura el objeto actual, cada mejora con su diferencia de nivel de objeto y de stats, y su origen con dónde aceptarla o entrar y el comando /compa marca. Trae la clase y la especialización: tú decides qué stats priorizar, pero solo nombra los objetos que aparecen aquí. Úsala para «analiza mi equipo» o «qué me conviene equiparme». Argumentos: ventana (ahora, pronto o ambas; por defecto ambas) y slot (opcional: cabeza, pecho, anillo, mano derecha…)."

	descMisionesDisponibles = "Lista las misiones que el personaje puede hacer ya («ahora») o pronto, agrupadas por zona y ordenadas por distancia cuando está en el mismo mapa. Cada misión trae dónde aceptarla con su comando /compa marca, lo que faltaría si hay prerrequisitos y sus recompensas, marcando las que son mejoras de equipo. Úsala para «¿qué misiones puedo hacer?» o para elegir zona. Argumentos: ventana (ahora, pronto o ambas; por defecto ambas) y zona (opcional)."

	descInfoMazmorra = "Información de una mazmorra de su rango de nivel: nivel, entrada con su comando /compa marca, misiones relacionadas y si el personaje puede hacerlas, y cada jefe con el botín que sería mejora para él. Sin nombre, sugiere las mazmorras para su nivel. Úsala para «¿qué mazmorra me conviene?» o «¿qué saco de la mazmorra X?»."

	descBuscarObjeto = "Busca un objeto por nombre o por item_id: de dónde sale (misión o jefe de mazmorra, con su estado), su nivel de objeto y stats, si su clase lo puede usar, si es mejora para su equipo y si lo lleva en las bolsas. Úsala para «¿dónde consigo X?» o «¿me sirve X?»."
)

func analizarEquipo(g *gamedata.Service, ws *websearch.Client) Def {
	return Def{
		Name:        "analizar_equipo",
		Description: descAnalizarEquipo,
		InputSchema: schema(map[string]any{
			"ventana": strProp("ahora, pronto o ambas. Por defecto ambas."),
			"slot":    strProp("Opcional: una ranura (cabeza, pecho, anillo, mano derecha, a distancia…) o su número (1 a 19)."),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			ventana, _ := a.text(aliasVentana...)
			slot, _ := a.text(aliasSlot...)
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.GearAnalysis(ventana, slot)
			if err != nil {
				return Result{}, err
			}
			// The simulated ranking of the candidates (the class model, when there is one).
			extras := map[string]any{}
			teo, _ := theoryCandidatesSummary(ctx, g, ws)
			extras["teoria"] = teo
			// The class's armour and weapon proficiencies from the site, to cross-check the gear.
			if class, _ := wftCharacter(g); class != "" && ws != nil {
				if extra, err := wftProficiencies(ctx, ws, class); err == nil {
					return withWFT(withExtras(v, extras), extra)
				}
			}
			return jsonResult(withExtras(v, extras))
		},
	}
}

func misionesDisponibles(g *gamedata.Service) Def {
	return Def{
		Name:        "misiones_disponibles",
		Description: descMisionesDisponibles,
		InputSchema: schema(map[string]any{
			"ventana": strProp("ahora, pronto o ambas. Por defecto ambas."),
			"zona":    strProp("Opcional: solo las misiones de esta zona, en castellano tal como aparece en el juego."),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			ventana, _ := a.text(aliasVentana...)
			zona, _ := a.text(aliasZona...)
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.AvailableQuests(ventana, zona)
			if err != nil {
				return Result{}, err
			}
			return jsonResult(v)
		},
	}
}

func infoMazmorra(g *gamedata.Service, ws *websearch.Client) Def {
	return Def{
		Name:        "info_mazmorra",
		Description: descInfoMazmorra,
		InputSchema: schema(map[string]any{
			"nombre": strProp("Nombre de la mazmorra, en castellano o en inglés. Si se omite, sugiere las mazmorras para su nivel."),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			nombre, _ := a.text(aliasNombre...)
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.DungeonInfo(nombre)
			if err != nil {
				return Result{}, err
			}
			// The site's group size, boss order and zones for the dungeon, labelled as its source.
			if nombre != "" && ws != nil {
				if extra, err := wftDungeon(ctx, ws, nombre); err == nil && extra != nil {
					return withWFT(v, extra)
				}
			}
			return jsonResult(v)
		},
	}
}

func buscarObjeto(g *gamedata.Service) Def {
	return Def{
		Name:        "buscar_objeto",
		Description: descBuscarObjeto,
		InputSchema: schema(map[string]any{
			"nombre":  strProp("Nombre del objeto, o parte de él."),
			"item_id": intProp("Número del objeto (item_id), si se conoce.", 1),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			nombre, _ := a.text(aliasNombre...)
			id, ok, ierr := a.integer(aliasItemID...)
			if ierr != nil || (ok && id < 0) {
				return Result{}, errors.New("item_id debe ser un número positivo")
			}
			if nombre == "" && id <= 0 {
				return Result{}, errors.New("falta nombre o item_id: indica el nombre del objeto o su número")
			}
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.ItemLookup(nombre, id)
			if err != nil {
				return Result{}, fmt.Errorf("no pude buscar el objeto: %v", err)
			}
			return jsonResult(v)
		},
	}
}
