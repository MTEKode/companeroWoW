package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

// Tools for rotation advice, the combat review and the action bars. They read the addon's combats
// and actionBars blocks (contract: COMBAT REVIEW + ACTION BARS) and the spellbook.

// Aliases of the arguments, canonical name first.
var (
	aliasCuantos   = []string{"n", "cuantos", "cuántos", "numero", "número", "cantidad", "limite", "limit"}
	aliasUltimo    = []string{"ultimo", "último", "last", "solo_ultimo", "ultimo_combate"}
	aliasMuertes   = []string{"muertes", "deaths", "solo_muertes", "muerte"}
	aliasAsign     = []string{"asignaciones", "barras", "distribucion", "distribución", "colocacion", "colocación"}
	aliasRanura    = []string{"ranura", "slot", "casilla"}
	aliasHechizo   = []string{"hechizo", "spell", "nombre", "name", "spell_name"}
	aliasHechizoID = []string{"id", "spell_id", "spellid", "spellID", "hechizo_id"}
)

const (
	descAnalizarRotacion = "Devuelve lo que hace falta para armar una rotación: la clase, el nivel, la especialización (la que guardó el addon o, si no, el árbol con más puntos), los talentos y los hechizos que el personaje conoce con su rango y etiquetas aproximadas (daño, curación, defensivo, control, utilidad). Construye la rotación solo con esos hechizos. Para fuentes de la comunidad, busca en inglés con buscar_web usando busquedaWeb (por ejemplo «Priest leveling rotation Forever») y cita la fuente. Úsala para «revisa mi rotación» o «qué hechizos uso»."

	descRevisarCombates = "Revisa los últimos combates grabados (hasta 10, el más reciente primero): duración, daño por segundo, qué hechizos aportan más y su porcentaje, de dónde viene el daño recibido y, en las muertes, quién te mató y los últimos golpes. Trae pistas calculadas sobre la ventana completa: hechizos que no aparecen en ningún combate, aportes bajos, combates largos con poco daño, un enemigo que domina el daño recibido y los defensivos conocidos que no aparecen en la muerte. Úsala para «qué estoy haciendo mal», «por qué he muerto» o «cómo fue el último combate». Argumentos: ultimo (true para solo el combate más reciente), muertes (true para solo los combates en los que moriste) y n (cuántos mirar, de 1 a 10; por defecto 10)."

	descConsejoBarras = "Lee las barras de acción guardadas: qué hechizo, objeto o macro hay en cada ranura y con qué tecla, las teclas lejanas (fuera de 1 a 6, Q, E, R, F y SHIFT-1 a SHIFT-4), los hechizos repetidos, los que no están puestos (usados en combate o defensivos, de curación o de control) y una propuesta de ranuras libres con su comando. Úsala para «colócame las barras» o «qué tengo en las barras». Si decides una distribución, pásala a generar_comando_barras para obtener el comando que el jugador pega en el juego."

	descGenerarComandoBarras = "Convierte una distribución propuesta en el comando /compa barras aplicar que el jugador pega en el juego. Comprueba que cada hechizo está en su libro de hechizos, que cada ranura va de 1 a 120 y que no se repite, y que cada comando cabe en 255 caracteres (máximo 40 pares). Úsala después de consejo_barras con las ranuras que has decidido. Argumentos: asignaciones, una lista de objetos {ranura, hechizo} (nombre del hechizo) o {ranura, id} (id del hechizo)."
)

func analizarRotacion(g *gamedata.Service, ws *websearch.Client) Def {
	return Def{
		Name:        "analizar_rotacion",
		Description: descAnalizarRotacion,
		InputSchema: schema(nil),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.Rotation()
			if err != nil {
				return Result{}, fmt.Errorf("no pude preparar la rotación: %v", err)
			}
			// The class's trainer spells up to the character's level, so the rotation uses what the class learns now.
			if ws != nil {
				if class, level := wftCharacter(g); class != "" && level > 0 {
					if extra, err := wftLearnableAt(ctx, ws, class, level); err == nil {
						return withWFT(v, extra)
					}
				}
			}
			return jsonResult(v)
		},
	}
}

func revisarCombates(g *gamedata.Service) Def {
	return Def{
		Name:        "revisar_combates",
		Description: descRevisarCombates,
		InputSchema: schema(map[string]any{
			"ultimo":  boolProp("true para revisar solo el combate más reciente."),
			"muertes": boolProp("true para revisar solo los combates en los que moriste."),
			"n":       intProp("Cuántos combates recientes mirar, de 1 a 10. Por defecto 10.", 1),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			ultimo := a.flag(aliasUltimo...)
			muertes := a.flag(aliasMuertes...)
			n := 0
			if p := a.intPtr(aliasCuantos...); p != nil {
				n = *p
			}
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.CombatReview(n, ultimo, muertes)
			if err != nil {
				return Result{}, fmt.Errorf("no pude revisar los combates: %v", err)
			}
			return jsonResult(v)
		},
	}
}

func consejoBarras(g *gamedata.Service) Def {
	return Def{
		Name:        "consejo_barras",
		Description: descConsejoBarras,
		InputSchema: schema(nil),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.BarsAdvice()
			if err != nil {
				return Result{}, fmt.Errorf("no pude leer las barras: %v", err)
			}
			return jsonResult(v)
		},
	}
}

func generarComandoBarras(g *gamedata.Service) Def {
	return Def{
		Name:        "generar_comando_barras",
		Description: descGenerarComandoBarras,
		InputSchema: schema(map[string]any{
			"asignaciones": map[string]any{
				"type":        "array",
				"description": "Ranuras con su hechizo: [{\"ranura\": 6, \"hechizo\": \"Heal\"}].",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"ranura":  intProp("Número de la ranura, de 1 a 120.", 1),
						"hechizo": strProp("Nombre del hechizo, tal como aparece en su libro."),
						"id":      intProp("Id del hechizo, si se conoce.", 1),
					},
					"required": []string{"ranura"},
				},
			},
		}, "asignaciones"),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			reqs, err := barRequests(a)
			if err != nil {
				return Result{}, err
			}
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.BarCommands(reqs)
			if err != nil {
				return Result{}, fmt.Errorf("no pude generar el comando: %v", err)
			}
			return jsonResult(v)
		},
	}
}

// barRequests reads the asignaciones list leniently: each item needs a slot and either a spell name
// or a spell id.
func barRequests(a argValues) ([]gamedata.BarRequest, error) {
	raw, ok := a.raw(aliasAsign...)
	if !ok {
		return nil, errors.New("falta asignaciones: indica la ranura y el hechizo de cada barra")
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, errors.New("asignaciones debe ser una lista de objetos {ranura, hechizo}")
	}
	if len(items) == 0 {
		return nil, errors.New("asignaciones está vacía")
	}
	out := make([]gamedata.BarRequest, 0, len(items))
	for i, item := range items {
		m := argValues(item)
		slot, ok, err := m.integer(aliasRanura...)
		if err != nil || !ok || slot < 1 {
			return nil, fmt.Errorf("asignación %d: la ranura debe ser un número de 1 a 120", i+1)
		}
		name, _ := m.text(aliasHechizo...)
		id, hasID, ierr := m.integer(aliasHechizoID...)
		if ierr != nil {
			return nil, fmt.Errorf("asignación %d: el id del hechizo debe ser un número", i+1)
		}
		if name == "" && (!hasID || id <= 0) {
			return nil, fmt.Errorf("asignación %d: falta el hechizo (nombre o id)", i+1)
		}
		req := gamedata.BarRequest{Slot: slot, Spell: name}
		if hasID && id > 0 {
			req.SpellID = id
		}
		out = append(out, req)
	}
	return out, nil
}

// flag reads a boolean argument. Text such as "true" or "sí" counts as true.
func (a argValues) flag(aliases ...string) bool {
	v, ok := a.raw(aliases...)
	if !ok {
		return false
	}
	var b bool
	if json.Unmarshal(v, &b) == nil {
		return b
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "sí", "si", "yes", "1":
			return true
		}
		return false
	}
	var f float64
	if json.Unmarshal(v, &f) == nil {
		return f != 0
	}
	return false
}

// boolProp is the JSON Schema of a boolean argument.
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
