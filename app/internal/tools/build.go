package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/capture"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/store"
	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

// CaptureFunc takes the screenshot for capturar_pantalla. Tests override it.
var CaptureFunc = capture.CaptureWoW

const (
	knowledgeDefaultLimit = 10
	npcMapDefaultLimit    = 30
	maxLimit              = 50
	maxNpcMapLimit        = 100
)

var (
	errNoGame = errors.New("los datos del juego no están disponibles en este momento")
	errNoKB   = errors.New("todavía no tengo conocimiento aprendido de WoW Forever. Escribe /compa sync en el juego para aprenderlo")
)

// Build returns the registry with every tool, backed by the game data service g and
// the web client ws. Either may be nil; the tools then answer that the data is not
// available instead of failing.
func Build(g *gamedata.Service, ws *websearch.Client) *Registry {
	r := NewRegistry()
	r.Add(contextoJugador(g))
	r.Add(guiaMision(g))
	r.Add(planProfesiones(g))
	r.Add(capturarPantalla())

	getters := []struct {
		name, label, desc string
		fn                func(*gamedata.Service) (any, error)
	}{
		{"estado_conexion", "el estado de la conexión", descEstado, (*gamedata.Service).Status},
		{"obtener_ubicacion", "la ubicación", descUbicacion, (*gamedata.Service).Location},
		{"obtener_personaje", "la ficha del personaje", descPersonaje, (*gamedata.Service).Character},
		{"obtener_misiones", "las misiones", descMisiones, (*gamedata.Service).Quests},
	}
	for _, d := range getters {
		r.Add(getter(g, d.name, d.label, d.desc, d.fn))
	}
	r.Add(obtenerMision(g))
	more := []struct {
		name, label, desc string
		fn                func(*gamedata.Service) (any, error)
	}{
		{"obtener_objetivo", "el objetivo", descObjetivo, (*gamedata.Service).Target},
		{"obtener_inventario", "el inventario", descInventario, (*gamedata.Service).Inventory},
		{"obtener_equipo", "el equipo", descEquipo, (*gamedata.Service).Equipment},
		{"obtener_grupo", "el grupo", descGrupo, (*gamedata.Service).Group},
		{"obtener_hechizos", "los hechizos", descHechizos, (*gamedata.Service).Spells},
		{"obtener_talentos", "los talentos", descTalentos, (*gamedata.Service).Talents},
		{"obtener_profesiones", "las profesiones", descProfesiones, (*gamedata.Service).Professions},
		{"obtener_reputaciones", "las reputaciones", descReputaciones, (*gamedata.Service).Reputations},
		{"obtener_dinero", "el dinero", descDinero, (*gamedata.Service).Money},
	}
	for _, d := range more {
		r.Add(getter(g, d.name, d.label, d.desc, d.fn))
	}
	r.Add(buscarConocimiento(g))
	r.Add(npcsEnMapa(g))
	r.Add(buscarWeb(ws))
	r.Add(leerPagina(ws))
	return r
}

// ---- tool constructors -----------------------------------------------------

func capturarPantalla() Def {
	return Def{
		Name:        "capturar_pantalla",
		Description: "Toma una captura de la ventana de World of Warcraft y la envía como imagen. Úsala cuando necesites ver lo que el jugador ve (la escena, textos, mapa o interfaz) y los datos de las demás herramientas no basten.",
		InputSchema: schema(nil),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			if CaptureFunc == nil {
				return Result{}, errors.New("la captura de pantalla no está disponible en este sistema")
			}
			png, err := CaptureFunc()
			if err != nil {
				return Result{}, fmt.Errorf("no pude capturar la ventana de WoW (%w). Abre el juego en modo ventana o sin bordes", err)
			}
			return Result{Content: []Content{
				{Type: "image", Data: png, MimeType: "image/png"},
				{Type: "text", Text: "Captura de la ventana de World of Warcraft adjunta (PNG)."},
			}}, nil
		},
	}
}

// getter builds a tool that returns the JSON of a parameterless gamedata getter.
func getter(g *gamedata.Service, name, label, desc string, fn func(*gamedata.Service) (any, error)) Def {
	return Def{
		Name:        name,
		Description: desc,
		InputSchema: schema(nil),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := fn(g)
			if err != nil {
				return Result{}, fmt.Errorf("no pude leer %s: %v", label, err)
			}
			return jsonResult(v)
		},
	}
}

type questArgs struct {
	QuestID *int64 `json:"quest_id"`
}

func obtenerMision(g *gamedata.Service) Def {
	return Def{
		Name:        "obtener_mision",
		Description: descMision,
		InputSchema: schema(map[string]any{
			"quest_id": intProp("Número de la misión (quest_id), por ejemplo 90001.", 1),
		}, "quest_id"),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			var a questArgs
			if err := decode(args, &a); err != nil {
				return Result{}, err
			}
			if a.QuestID == nil || *a.QuestID <= 0 {
				return Result{}, errors.New("falta quest_id: indica el número de la misión, por ejemplo 90001")
			}
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.Quest(*a.QuestID)
			if err != nil {
				return Result{}, fmt.Errorf("no pude leer la misión %d: %v", *a.QuestID, err)
			}
			return jsonResult(v)
		},
	}
}

type knowledgeArgs struct {
	Texto  string `json:"texto"`
	Limite *int   `json:"limite"`
}

func buscarConocimiento(g *gamedata.Service) Def {
	return Def{
		Name:        "buscar_conocimiento",
		Description: descConocimiento,
		InputSchema: schema(map[string]any{
			"texto":  strProp("Nombre o palabra clave a buscar (misión, NPC, zona)."),
			"limite": intProp("Máximo de resultados por tipo (por defecto 10).", 1),
		}, "texto"),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			var a knowledgeArgs
			if err := decode(args, &a); err != nil {
				return Result{}, err
			}
			texto := strings.TrimSpace(a.Texto)
			if texto == "" {
				return Result{}, errors.New("falta texto: escribe el nombre o la palabra clave a buscar")
			}
			if g == nil {
				return Result{}, errNoGame
			}
			st := g.Store()
			if st == nil {
				return Result{}, errNoKB
			}
			limite := clampLimit(a.Limite, knowledgeDefaultLimit, maxLimit)
			quests, err := st.SearchQuests(texto, limite)
			if err != nil {
				return Result{}, fmt.Errorf("no pude buscar misiones: %v", err)
			}
			npcs, err := st.SearchNpcs(texto, limite)
			if err != nil {
				return Result{}, fmt.Errorf("no pude buscar NPCs: %v", err)
			}
			questOut := make([]any, 0, len(quests))
			for _, q := range quests {
				v, err := g.Quest(q.QuestID)
				if err != nil {
					return Result{}, fmt.Errorf("no pude leer la misión %d: %v", q.QuestID, err)
				}
				questOut = append(questOut, v)
			}
			npcOut := make([]any, 0, len(npcs))
			for _, n := range npcs {
				npcOut = append(npcOut, npcMap(n))
			}
			out := map[string]any{
				"texto":    texto,
				"total":    len(questOut) + len(npcOut),
				"misiones": questOut,
				"npcs":     npcOut,
			}
			if len(questOut)+len(npcOut) == 0 {
				out["nota"] = "No hay nada sobre \"" + texto + "\" en el conocimiento aprendido. Prueba con buscar_web."
			}
			return jsonResult(out)
		},
	}
}

type npcMapArgs struct {
	MapID  *int `json:"map_id"`
	Limite *int `json:"limite"`
}

func npcsEnMapa(g *gamedata.Service) Def {
	return Def{
		Name:        "npcs_en_mapa",
		Description: descNpcsMapa,
		InputSchema: schema(map[string]any{
			"map_id": intProp("Número del mapa. Si se omite, se usa el mapa actual del jugador.", 1),
			"limite": intProp("Máximo de NPCs a devolver (por defecto 30).", 1),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			var a npcMapArgs
			if err := decode(args, &a); err != nil {
				return Result{}, err
			}
			if g == nil {
				return Result{}, errNoGame
			}
			mapID, err := resolveMap(g, a.MapID)
			if err != nil {
				return Result{}, err
			}
			st := g.Store()
			if st == nil {
				return Result{}, errNoKB
			}
			limite := clampLimit(a.Limite, npcMapDefaultLimit, maxNpcMapLimit)
			npcs, err := st.NpcsOnMap(mapID, limite)
			if err != nil {
				return Result{}, fmt.Errorf("no pude listar los NPCs del mapa %d: %v", mapID, err)
			}
			zona, err := st.MapName(mapID)
			if err != nil {
				return Result{}, fmt.Errorf("no pude leer el nombre del mapa %d: %v", mapID, err)
			}
			list := make([]any, 0, len(npcs))
			for _, n := range npcs {
				list = append(list, npcMap(n))
			}
			out := map[string]any{
				"mapId": mapID,
				"zona":  zona,
				"total": len(list),
				"npcs":  list,
			}
			if len(list) == 0 {
				out["nota"] = fmt.Sprintf("No tengo NPCs guardados en el mapa %d. Escribe /compa sync en el juego para aprenderlos.", mapID)
			}
			return jsonResult(out)
		},
	}
}

type webArgs struct {
	Consulta string `json:"consulta"`
	Sitio    string `json:"sitio"`
}

func buscarWeb(ws *websearch.Client) Def {
	return Def{
		Name:        "buscar_web",
		Description: descBuscarWeb,
		InputSchema: schema(map[string]any{
			"consulta": strProp("Qué buscar, en español o inglés (por ejemplo el nombre de una misión o un objeto)."),
			"sitio":    strProp("Opcional: un único sitio de la lista permitida (por ejemplo warcraft.wiki.gg) en el que buscar."),
		}, "consulta"),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			var a webArgs
			if err := decode(args, &a); err != nil {
				return Result{}, err
			}
			consulta := strings.TrimSpace(a.Consulta)
			if consulta == "" {
				return Result{}, errors.New("falta consulta: escribe qué buscar")
			}
			if ws == nil {
				return Result{}, errors.New("la búsqueda web no está configurada")
			}
			hits, err := ws.Search(ctx, consulta, a.Sitio)
			if err != nil {
				return Result{}, fmt.Errorf("no pude buscar en la web: %v", err)
			}
			if hits == nil {
				hits = []websearch.Hit{}
			}
			out := map[string]any{
				"consulta":   consulta,
				"sitio":      strings.TrimSpace(a.Sitio),
				"total":      len(hits),
				"resultados": hits,
			}
			if len(hits) == 0 {
				out["nota"] = "Sin resultados en las fuentes de WoW Forever. Reformula la consulta."
			}
			return jsonResult(out)
		},
	}
}

type pageArgs struct {
	URL string `json:"url"`
}

func leerPagina(ws *websearch.Client) Def {
	return Def{
		Name:        "leer_pagina",
		Description: descLeerPagina,
		InputSchema: schema(map[string]any{
			"url": strProp("Dirección completa de la página, tal como la devolvió buscar_web."),
		}, "url"),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			var a pageArgs
			if err := decode(args, &a); err != nil {
				return Result{}, err
			}
			u := strings.TrimSpace(a.URL)
			if u == "" {
				return Result{}, errors.New("falta url: indica la dirección de la página")
			}
			if ws == nil {
				return Result{}, errors.New("la lectura web no está configurada")
			}
			text, err := ws.Fetch(ctx, u)
			if err != nil {
				return Result{}, fmt.Errorf("no pude leer la página: %v", err)
			}
			return textResult(text), nil
		},
	}
}

// ---- helpers ---------------------------------------------------------------

// resolveMap returns the requested map, else the live map, else the map of the
// SavedVariables location.
func resolveMap(g *gamedata.Service, requested *int) (int, error) {
	if requested != nil {
		if *requested <= 0 {
			return 0, errors.New("map_id debe ser un número positivo")
		}
		return *requested, nil
	}
	if live, ok := g.Live(); ok && live.MapID != 0 {
		return int(live.MapID), nil
	}
	if loc, err := g.Location(); err == nil {
		if m, ok := loc.(map[string]any); ok && m["available"] == true {
			if id, ok := m["mapId"].(int); ok && id != 0 {
				return id, nil
			}
		}
	}
	return 0, errors.New("no sé en qué mapa está el jugador: indica map_id o abre WoW en modo ventana. " + gamedata.HintLive)
}

func npcMap(n store.KNpc) map[string]any {
	return map[string]any{
		"npcId":        n.NpcID,
		"name":         n.Name,
		"mapId":        n.MapID,
		"x":            n.X,
		"y":            n.Y,
		"isQuestGiver": n.IsQuestGiver,
		"isVendor":     n.IsVendor,
		"isTrainer":    n.IsTrainer,
		"trainerType":  n.TrainerType,
		"reaction":     n.Reaction,
	}
}

func clampLimit(p *int, def, max int) int {
	if p == nil || *p < 1 {
		return def
	}
	if *p > max {
		return max
	}
	return *p
}

// decode unmarshals tool arguments; empty or null arguments leave v at its zero value.
func decode(args json.RawMessage, v any) error {
	trimmed := bytes.TrimSpace(args)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if err := json.Unmarshal(trimmed, v); err != nil {
		return fmt.Errorf("argumentos no válidos: %v", err)
	}
	return nil
}

func jsonResult(v any) (Result, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("no pude dar formato a la respuesta: %v", err)
	}
	return textResult(string(b)), nil
}

// schema builds a JSON Schema object with the given properties and required names.
func schema(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func intProp(desc string, min int) map[string]any {
	return map[string]any{"type": "integer", "description": desc, "minimum": min}
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
