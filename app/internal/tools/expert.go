package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
)

// Expert tools: the leveling pace, the value of the quests and the verdict on an enemy. They
// answer the questions a player asks while playing; the numbers are estimates where the
// description says so.

// profileSource returns the player's profile for the tools that rank things. It is set once the
// assistant exists and read by every tool call, so it is held atomically.
var profileSource atomic.Pointer[func() config.Profile]

// SetProfileSource connects the player's profile to the tools that rank things. Nil means no
// profile: the tools then use their defaults.
func SetProfileSource(f func() config.Profile) {
	if f == nil {
		profileSource.Store(nil)
		return
	}
	profileSource.Store(&f)
}

// profile reads the player's profile, or the zero profile when there is no source.
func profile() config.Profile {
	f := profileSource.Load()
	if f == nil {
		return config.Profile{}
	}
	return (*f)()
}

var (
	aliasNivelObjetivo = []string{"nivel", "nivel_objetivo", "objetivo", "target", "level", "meta"}
	aliasCriterio      = []string{"criterio", "criteria", "ordenar", "por"}
	aliasNombreEnemigo = []string{"nombre", "name", "enemigo", "mob", "npc", "texto", "consulta"}
	aliasNpcID         = []string{"npc_id", "npcid", "id"}
	aliasGrupo         = []string{"grupo", "group", "cantidad", "numero", "número"}
)

const (
	descRitmo = "Calcula el ritmo de subida: XP por hora de la sesión actual, de las últimas tres sesiones y de los últimos 30 minutos; cuánto falta para el siguiente nivel y, con nivel, para ese nivel; y en qué se va el tiempo (combate, viaje o parado), estimado con las banderas en vivo. Úsala para «¿cuánto me falta para subir?», «¿cuánto tardo en el nivel X?» o «¿qué ritmo llevo?»."

	descValor = "Valora las misiones accesibles (en el registro, las que puede aceptar ya): XP por minuto estimado de cada una, con su tiempo de viaje y de combate, y una ruta por el mismo mapa. El XP es el que el addon vio en la misión cuando se puede; si no, se estima por nivel («estimado»). Los minutos son estimados. Con criterio «oro» ordena por oro por minuto. Argumentos: ventana (ahora, pronto o ambas; por defecto ahora), zona (opcional) y criterio (xp u oro; por defecto según el perfil del jugador)."

	descEnemigo = "Dice si el personaje puede con un enemigo: nivel y clasificación, daño recibido por segundo y duración de los combates grabados contra él (observado), muertes que causó, el tiempo para matarlo del modelo de combate si lo hay, y un veredicto con sus motivos para uno, dos o tres a la vez. Sin nombre ni npc_id usa el objetivo seleccionado. Úsala para «¿puedo con este élite?», «¿y con dos?» o «¿qué tal es X?»."
)

// ritmoSubida answers ritmo_subida.
func ritmoSubida(g *gamedata.Service) Def {
	return Def{
		Name:        "ritmo_subida",
		Description: descRitmo,
		InputSchema: schema(map[string]any{
			"nivel": intProp("Opcional: el nivel al que quieres llegar; da el tiempo estimado hasta él.", 1),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			target, ok, ierr := a.integer(aliasNivelObjetivo...)
			if ierr != nil {
				return Result{}, errors.New("nivel debe ser un número entero")
			}
			if !ok {
				target = 0
			}
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.LevelingPace(int(target))
			if err != nil {
				return Result{}, fmt.Errorf("no pude calcular el ritmo de subida: %v", err)
			}
			return jsonResult(v)
		},
	}
}

// valorMisiones answers valor_misiones. The profile sets the default criterion: a player whose
// goals name gold and not leveling is ranked by gold per minute.
func valorMisiones(g *gamedata.Service) Def {
	return Def{
		Name:        "valor_misiones",
		Description: descValor,
		InputSchema: schema(map[string]any{
			"ventana":  strProp("ahora, pronto o ambas. Por defecto ahora."),
			"zona":     strProp("Opcional: solo las misiones de esta zona, en castellano tal como aparece en el juego."),
			"criterio": strProp("xp u oro. Por defecto según el perfil del jugador."),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			ventana, _ := a.text(aliasVentana...)
			zona, _ := a.text(aliasZona...)
			criterio, _ := a.text(aliasCriterio...)
			if g == nil {
				return Result{}, errNoGame
			}
			p := profile()
			opts := gamedata.QuestValueOptions{Ventana: ventana, Zona: zona}
			switch {
			case criterio != "":
				c := strings.ToLower(strings.TrimSpace(criterio))
				opts.Oro = c == "oro" || c == "gold"
			case p.HasGoal(config.GoalGold) && !p.HasGoal(config.GoalLevel):
				opts.Oro = true
			}
			v, err := g.QuestValue(opts)
			if err != nil {
				return Result{}, fmt.Errorf("no pude valorar las misiones: %v", err)
			}
			return jsonResult(v)
		},
	}
}

// infoEnemigo answers info_enemigo.
func infoEnemigo(g *gamedata.Service) Def {
	return Def{
		Name:        "info_enemigo",
		Description: descEnemigo,
		InputSchema: schema(map[string]any{
			"nombre": strProp("Opcional: nombre del enemigo o del NPC."),
			"npc_id": intProp("Opcional: el npcID del enemigo, si lo conoces.", 1),
			"grupo":  intProp("Opcional: cuántos enemigos de este tipo vendrían a la vez (1 a 3). Por defecto 1.", 1),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			nombre, _ := a.text(aliasNombreEnemigo...)
			id, ok, ierr := a.integer(aliasNpcID...)
			if ierr != nil || (ok && id < 0) {
				return Result{}, errors.New("npc_id debe ser un número positivo")
			}
			grupo, gok, gerr := a.integer(aliasGrupo...)
			if gerr != nil || (gok && (grupo < 1 || grupo > 3)) {
				return Result{}, errors.New("grupo debe ser 1, 2 o 3")
			}
			if !gok {
				grupo = 1
			}
			if g == nil {
				return Result{}, errNoGame
			}
			v, err := g.MobInfo(gamedata.MobQuery{Name: nombre, NpcID: id, Group: int(grupo)})
			if err != nil {
				return Result{}, fmt.Errorf("no pude revisar el enemigo: %v", err)
			}
			return jsonResult(v)
		},
	}
}
