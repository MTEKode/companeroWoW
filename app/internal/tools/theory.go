package tools

// The theorycraft tools: deterministic numbers from the character sheet and the game's formulas. The
// language model explains what they return; it does not compute these numbers itself. A class is
// supported when the theory package has a model for it (today: Warrior).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/theory"
	"github.com/MTEKode/companeroWoW/app/internal/theory/warrior"
	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

const (
	descCalcularDPS = "Calcula el daño por segundo, las muertes por hora y la ira de tu personaje con un cálculo propio (simulación de peleas con las fórmulas del juego), no con guías. Sin argumentos usa tu hoja y tus talentos actuales. Con argumentos calcula un cambio: talentos (nombre → rango), rotacion (lista de hechizos por prioridad: mortal_strike, bloodthirst, overpower, rend, hs, execute, slam, cleave, whirlwind, death_wish), hs_sobre (ira mínima para el golpe heroico) o cambios de stats (fuerza, agilidad, poder_ataque, critico, precision, habilidad_arma, daño_arma, velocidad_arma, aguante, armadura). Argumentos opcionales: peleas, nivel_enemigo (fijo; por defecto nivel ±2) y elite. Úsala para «¿cuánto daño hago?», «¿qué pasa si cambio X?» o para explicar un cálculo."

	descPesosEstadisticas = "Calcula el peso de cada stat para tu personaje con su equipo actual: cuántas muertes por hora (y daño por segundo) da una unidad de fuerza, agilidad, poder de ataque, crítico, precisión, habilidad, daño o velocidad del arma y armadura, y cuánto vale cada una frente al poder de ataque. Úsala para «¿qué stat me conviene?» o «¿qué priorizo?». Argumentos opcionales: peleas, nivel_enemigo."

	descCompararObjetos = "Puntúa objetos candidatos (los que el addon ha visto que puedes usar, con su misión o mazmorra, y los equipables de tus bolsas) por lo que cambiarían tu rendimiento en una simulación: muertes por hora y daño por segundo frente a tu equipo actual, no por el nivel de objeto. Las armas se comparan por daño y velocidad. Argumentos opcionales: nombre o item_id (uno), ranura (cabeza, pecho, anillo…). Sin argumentos compara todos. Úsala para «¿me conviene X?» o para ordenar mejoras."

	descOptimizarTalentos = "Busca cómo gastar los puntos de talento que te quedan y los de los próximos niveles (uno por nivel) para más muertes por hora en tu nivel, respetando las reglas del árbol (filas, requisitos y rangos). Devuelve las mejores opciones con el orden de los puntos y los números de cada una. Úsala para «¿en qué gasto los talentos?» o «¿qué build me conviene para subir?». Argumentos opcionales: puntos (cuántos añadir, por defecto 10), haz (cuántas opciones mantener, por defecto 5), peleas."

	descOptimizarRotacion = "Compara las prioridades de hechizos que puedes lanzar con tu talento actual (y los umbrales de ira del golpe heroico) y devuelve las mejores por muertes por hora. Úsala para «¿cómo ordeno mis hechizos?» o «¿cuándo uso el golpe heroico?». Argumentos opcionales: peleas."
)

// theoryClasses are the classes the engine models. Other classes get the data-only answer.
var theoryClasses = map[string]theory.Class{
	"WARRIOR": warrior.New(),
}

// theoryUnsupported is the answer when the class has no model yet.
func theoryUnsupported(class string) map[string]any {
	return map[string]any{
		"disponible": false,
		"motivo":     fmt.Sprintf("todavía no tengo un modelo de cálculo para la clase %s: solo guerrero; responde con los datos de la hoja sin recomendar valores", class),
	}
}

// theoryData reads the talent tree and the ability ranks of a class from the site.
func theoryData(ctx context.Context, ws *websearch.Client, class string) (theory.Data, error) {
	if ws == nil {
		return theory.Data{}, errors.New("no tengo conexión a wowforevertalents.com: no puedo leer el árbol de talentos")
	}
	talents, err := ws.WFTClassTalents(ctx, class)
	if err != nil {
		return theory.Data{}, fmt.Errorf("no pude leer los talentos de %s en wowforevertalents.com: %v", class, err)
	}
	abil, err := ws.WFTClassAbilities(ctx, class)
	if err != nil {
		return theory.Data{}, fmt.Errorf("no pude leer los hechizos de %s en wowforevertalents.com: %v", class, err)
	}
	return theoryDataFrom(talents, abil), nil
}

// theoryDataFrom converts the site's feeds into the engine's data. It is also used by the tests with
// the saved fixtures.
func theoryDataFrom(talents websearch.WFTClassTalents, abil websearch.WFTAbilities) theory.Data {
	d := theory.Data{Abilities: map[string][]theory.AbilityRank{}}
	for _, t := range talents.Talentos {
		td := theory.TalentDef{
			Slug: t.Slug, Name: t.Nombre, Tree: t.Arbol, Row: t.Fila, Max: t.Maximo,
			Need: t.Necesita, Prereq: t.Requisito, Status: t.Estado, RankText: map[int]string{},
		}
		for _, r := range t.Rangos {
			td.RankText[r.Rango] = r.Texto
		}
		d.Talents = append(d.Talents, td)
	}
	for _, school := range abil.Escuelas {
		for _, sp := range school.Hechizos {
			for i, r := range sp.Rangos {
				rank := i + 1
				d.Abilities[sp.Nombre] = append(d.Abilities[sp.Nombre], theory.AbilityRank{
					Rank: rank, Level: r.Nivel, Cost: r.Coste, Text: r.Texto,
				})
			}
		}
	}
	return d
}

// theorySheet reads the active character's sheet. The hint tells the player how to export it.
func theorySheet(g *gamedata.Service) (theory.Sheet, error) {
	if g == nil {
		return theory.Sheet{}, errNoGame
	}
	raw, _, ok := g.SheetJSON()
	if !ok {
		return theory.Sheet{}, errors.New("No tengo la hoja de tu personaje: escribe /compa sync en el juego (la versión del addon que exporta la hoja)")
	}
	sh, err := theory.ParseSheet(raw)
	if err != nil {
		return theory.Sheet{}, fmt.Errorf("la hoja de tu personaje no se puede leer: %v", err)
	}
	return sh, nil
}

// theoryClass finds the model of the sheet's class. ok is false when the class has no model.
func theoryClass(sh theory.Sheet) (theory.Class, bool) {
	c, ok := theoryClasses[strings.ToUpper(sh.Class)]
	return c, ok
}

// calibrationLine says whether the model matches the character's sheet: "modelo calibrado ✓", or the
// mismatch the tools show. The check runs once per exported sheet and its line is stored, so later calls
// read it back. ok is false when there is no sheet, no model for the class, or no site data yet.
func calibrationLine(ctx context.Context, g *gamedata.Service, ws *websearch.Client) (string, bool) {
	if g == nil {
		return "", false
	}
	key, ok := g.CalibrationKey()
	if !ok {
		return "", false
	}
	if line, ok := g.StoredCalibration(key); ok {
		return line, true
	}
	cls, in, err := theoryFor(ctx, g, ws)
	if err != nil || len(in.Data.Talents) == 0 {
		return "", false // no site data: nothing to compare against, and nothing is stored
	}
	line := "modelo calibrado ✓"
	if msg := theory.Mismatch(cls.Calibrate(in)); msg != "" {
		line = msg
	}
	g.StoreCalibration(key, line)
	return line, true
}

// theoryFor loads the sheet, the class model and the site data the calculations share.
func theoryFor(ctx context.Context, g *gamedata.Service, ws *websearch.Client) (theory.Class, theory.Input, error) {
	sh, err := theorySheet(g)
	if err != nil {
		return nil, theory.Input{}, err
	}
	cls, ok := theoryClass(sh)
	if !ok {
		return nil, theory.Input{}, fmt.Errorf("todavía no tengo un modelo de cálculo para %s: solo guerrero", sh.Class)
	}
	data, err := theoryData(ctx, ws, strings.ToLower(sh.Class))
	if err != nil {
		return nil, theory.Input{}, err
	}
	return cls, theory.Input{Sheet: sh, Data: data}, nil
}

// theoryArgs are the optional simulation settings the tools accept.
func theoryOptions(a argValues) theory.Options {
	var o theory.Options
	if n := a.intPtr("peleas", "fights", "n_peleas"); n != nil && *n > 0 {
		o.Fights = min(*n, 2000)
	}
	if n := a.intPtr("nivel_enemigo", "mob_level", "nivel_mob"); n != nil && *n > 0 {
		o.MobLevel = *n
	}
	if n := a.intPtr("objetivos", "enemigos", "targets"); n != nil && *n > 0 {
		o.Targets = min(*n, 2)
	}
	o.Elite = a.flag("elite")
	return o
}

// theoryObjetivos is the schema of the number of enemies pulled together (1 or 2).
func theoryObjetivos() map[string]any {
	return intProp("Enemigos juntos en la pelea: 1 (por defecto) o 2 (Barrido, Torbellino y Hendedura pegan a los dos).", 1)
}

// theoryDelta reads the stat changes the player or the model asked for.
func theoryDelta(a argValues) theory.Delta {
	num := func(aliases ...string) float64 {
		raw, ok := a.raw(aliases...)
		if !ok {
			return 0
		}
		var v float64
		if json.Unmarshal(raw, &v) == nil {
			return v
		}
		return 0
	}
	return theory.Delta{
		Str:       num("fuerza", "str"),
		Agi:       num("agilidad", "agi"),
		Sta:       num("aguante", "stamina", "sta"),
		AP:        num("poder_ataque", "ap"),
		Crit:      num("critico", "crítico", "crit"),
		Hit:       num("precision", "precisión", "hit"),
		Skill:     num("habilidad_arma", "skill"),
		Expertise: num("pericia", "expertise"),
		Armor:     num("armadura", "armor"),
		MHAvg:     num("daño_arma", "dano_arma", "weapon_damage"),
		MHSpeed:   num("velocidad_arma", "weapon_speed"),
	}
}

// theoryBuild reads the talents and the rotation the player asked for; nil fields keep the sheet's.
func theoryBuild(a argValues) (theory.Build, error) {
	var b theory.Build
	if raw, ok := a.raw("talentos", "talents"); ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &b.Talents); err != nil {
			return b, errors.New("talentos debe ser un objeto: nombre del talento → rango")
		}
	}
	if raw, ok := a.raw("rotacion", "prioridad", "rotation"); ok && string(raw) != "null" {
		var list []string
		if json.Unmarshal(raw, &list) != nil {
			var one string
			if json.Unmarshal(raw, &one) == nil {
				for _, p := range strings.Split(one, ",") {
					list = append(list, strings.TrimSpace(p))
				}
			}
		}
		for i := range list {
			list[i] = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(list[i]), " ", "_"))
		}
		b.Rotation = list
	}
	if v := a.intPtr("hs_sobre", "golpe_heroico_con_ira"); v != nil {
		b.Params = map[string]float64{"hs_sobre": float64(*v)}
	}
	if v := a.intPtr("cambios_postura", "postura"); v != nil && *v > 0 {
		if b.Params == nil {
			b.Params = map[string]float64{}
		}
		b.Params["cambios_postura"] = float64(min(*v, 20))
	}
	b.Delta = theoryDelta(a)
	return b, nil
}

// calcularDPS answers calcular_dps.
func calcularDPS(g *gamedata.Service, ws *websearch.Client) Def {
	return Def{
		Name:        "calcular_dps",
		Description: descCalcularDPS,
		InputSchema: schema(map[string]any{
			"talentos":        map[string]any{"type": "object", "description": "Talentos (nombre → rango) para el cálculo; sin él, los de la hoja."},
			"rotacion":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Hechizos por prioridad."},
			"hs_sobre":        intProp("Ira mínima para encolar el golpe heroico.", 0),
			"cambios_postura": intProp("Cambios de postura por pelea (la ira se queda en lo que retiene Táctica).", 0),
			"fuerza":          map[string]any{"type": "number", "description": "Cambio de fuerza."},
			"agilidad":        map[string]any{"type": "number", "description": "Cambio de agilidad."},
			"poder_ataque":    map[string]any{"type": "number", "description": "Cambio de poder de ataque."},
			"critico":         map[string]any{"type": "number", "description": "Cambio de crítico (puntos)."},
			"precision":       map[string]any{"type": "number", "description": "Cambio de precisión (puntos)."},
			"habilidad_arma":  map[string]any{"type": "number", "description": "Cambio de habilidad con arma."},
			"daño_arma":       map[string]any{"type": "number", "description": "Cambio del daño medio del arma."},
			"velocidad_arma":  map[string]any{"type": "number", "description": "Cambio de la velocidad del arma (negativo = más rápida)."},
			"peleas":          intProp("Peleas simuladas (por defecto 400).", 1),
			"nivel_enemigo":   intProp("Nivel fijo del enemigo (por defecto nivel ±2).", 1),
			"objetivos":       theoryObjetivos(),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			cls, in, err := theoryFor(ctx, g, ws)
			if err != nil {
				return Result{}, err
			}
			build, err := theoryBuild(a)
			if err != nil {
				return Result{}, err
			}
			in.Build = build
			in.Opt = theoryOptions(a)
			rep, err := cls.Evaluate(in)
			if err != nil {
				return Result{}, err
			}
			out := map[string]any{"disponible": true, "resultado": rep}
			if msg := theory.Mismatch(rep.Calibracion); msg != "" {
				out["calibracion"] = msg
			}
			return jsonResult(out)
		},
	}
}

// pesosEstadisticas answers pesos_estadisticas.
func pesosEstadisticas(g *gamedata.Service, ws *websearch.Client) Def {
	return Def{
		Name:        "pesos_estadisticas",
		Description: descPesosEstadisticas,
		InputSchema: schema(map[string]any{
			"peleas":        intProp("Peleas simuladas (por defecto 400).", 1),
			"nivel_enemigo": intProp("Nivel fijo del enemigo (por defecto nivel ±2).", 1),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			cls, in, err := theoryFor(ctx, g, ws)
			if err != nil {
				return Result{}, err
			}
			in.Opt = theoryOptions(a)
			w, err := cls.Weights(in)
			if err != nil {
				return Result{}, err
			}
			return jsonResult(map[string]any{"disponible": true, "resultado": w})
		},
	}
}

// compararObjetos answers comparar_objetos.
func compararObjetos(g *gamedata.Service, ws *websearch.Client) Def {
	return Def{
		Name:        "comparar_objetos",
		Description: descCompararObjetos,
		InputSchema: schema(map[string]any{
			"nombre":        strProp("Nombre del objeto (opcional)."),
			"item_id":       intProp("Id del objeto (opcional).", 1),
			"ranura":        strProp("Ranura: cabeza, pecho, anillo, mano derecha… (opcional)."),
			"peleas":        intProp("Peleas simuladas (por defecto 400).", 1),
			"nivel_enemigo": intProp("Nivel fijo del enemigo (por defecto nivel ±2).", 1),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			cls, in, err := theoryFor(ctx, g, ws)
			if err != nil {
				return Result{}, err
			}
			items, left, ok := g.TheoryCandidates()
			if !ok {
				return Result{}, errors.New("No tengo las mejoras candidatas: escribe /compa sync en el juego")
			}
			name, _ := a.text(aliasNombre...)
			slot, _ := a.text(aliasSlot...)
			id := a.intPtr(aliasItemID...)
			var pick []theory.Item
			for _, it := range items {
				if name != "" && !strings.Contains(foldText(it.Name), foldText(name)) {
					continue
				}
				if id != nil && it.ItemID != int64(*id) {
					continue
				}
				if slot != "" && !strings.Contains(foldText(it.Slot), foldText(slot)) {
					continue
				}
				pick = append(pick, it)
			}
			in.Opt = theoryOptions(a)
			rep, err := cls.CompareItems(in, pick)
			if err != nil {
				return Result{}, err
			}
			out := map[string]any{"disponible": true, "resultado": rep}
			if len(left) > 0 {
				out["noUsables"] = left
			}
			if len(pick) == 0 {
				out["aviso"] = "No encontré candidatos con esos datos: prueba sin filtros o con otro nombre"
			}
			return jsonResult(out)
		},
	}
}

// optimizarTalentos answers optimizar_talentos.
func optimizarTalentos(g *gamedata.Service, ws *websearch.Client) Def {
	return Def{
		Name:        "optimizar_talentos",
		Description: descOptimizarTalentos,
		InputSchema: schema(map[string]any{
			"puntos":    intProp("Puntos que añadir (por defecto 10, uno por nivel).", 1),
			"haz":       intProp("Opciones que mantener en cada paso (por defecto 5).", 1),
			"objetivos": theoryObjetivos(),
			"peleas":    intProp("Peleas por opción final (por defecto 400).", 1),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			cls, in, err := theoryFor(ctx, g, ws)
			if err != nil {
				return Result{}, err
			}
			search := theory.TalentSearch{Opts: theoryOptions(a)}
			if v := a.intPtr("puntos", "points"); v != nil && *v > 0 {
				search.Points = min(*v, 40)
			}
			if v := a.intPtr("haz", "beam"); v != nil && *v > 0 {
				search.Beam = min(*v, 20)
			}
			plan, err := cls.OptimizeTalents(in, search)
			if err != nil {
				return Result{}, err
			}
			out := map[string]any{"disponible": true, "resultado": plan}
			if msg := theory.Mismatch(plan.Actual.Calibracion); msg != "" {
				out["calibracion"] = msg
			}
			return jsonResult(out)
		},
	}
}

// optimizarRotacion answers optimizar_rotacion.
func optimizarRotacion(g *gamedata.Service, ws *websearch.Client) Def {
	return Def{
		Name:        "optimizar_rotacion",
		Description: descOptimizarRotacion,
		InputSchema: schema(map[string]any{
			"peleas":    intProp("Peleas por lista (por defecto 400).", 1),
			"objetivos": theoryObjetivos(),
		}),
		Handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			a, err := parseArgs(args)
			if err != nil {
				return Result{}, err
			}
			cls, in, err := theoryFor(ctx, g, ws)
			if err != nil {
				return Result{}, err
			}
			plan, err := cls.OptimizeRotation(in, theory.RotationSearch{Opts: theoryOptions(a)})
			if err != nil {
				return Result{}, err
			}
			return jsonResult(map[string]any{"disponible": true, "resultado": plan})
		},
	}
}

// theoryCandidatesSummary is the short ranking analizar_equipo adds to its answer: the candidates ordered
// by the simulated change, and the ones the model cannot score. ok is false when the sheet or the model is
// missing, so the gear answer goes on without it.
func theoryCandidatesSummary(ctx context.Context, g *gamedata.Service, ws *websearch.Client) (map[string]any, bool) {
	cls, in, err := theoryFor(ctx, g, ws)
	if err != nil {
		return map[string]any{"disponible": false, "motivo": err.Error()}, false
	}
	items, left, ok := g.TheoryCandidates()
	if !ok || len(items) == 0 {
		return map[string]any{"disponible": true, "ranking": []any{}, "nota": "No hay mejoras candidatas que simular."}, true
	}
	in.Opt = theory.Options{Fights: 200}
	rep, err := cls.CompareItems(in, items)
	if err != nil {
		return map[string]any{"disponible": false, "motivo": err.Error()}, false
	}
	out := map[string]any{"disponible": true, "kphBase": rep.KPHBase, "ranking": rep.Items, "supuestos": rep.Supuestos}
	if len(rep.NoComparables) > 0 {
		out["noComparables"] = rep.NoComparables
	}
	if len(left) > 0 {
		out["noUsables"] = left
	}
	return out, true
}

// theoryRotationSummary is the block analizar_rotacion adds for a supported class: the priority list the
// simulation ranks best, with its Heroic Strike threshold and the numbers. nil when the class has no model.
func theoryRotationSummary(ctx context.Context, g *gamedata.Service, ws *websearch.Client) map[string]any {
	cls, in, err := theoryFor(ctx, g, ws)
	if err != nil {
		return map[string]any{"disponible": false, "motivo": err.Error()}
	}
	plan, err := cls.OptimizeRotation(in, theory.RotationSearch{})
	if err != nil {
		return map[string]any{"disponible": false, "motivo": err.Error()}
	}
	if len(plan.Opciones) == 0 {
		return map[string]any{"disponible": false, "motivo": "ninguna prioridad de hechizos se pudo simular con tus hechizos"}
	}
	best := plan.Opciones[0]
	var top []map[string]any
	for _, o := range plan.Opciones {
		top = append(top, map[string]any{
			"prioridad": o.Prioridad, "parametros": o.Parametros,
			"muertesPorHora": o.Resultado.KillsPerHour.Mean, "dps": o.Resultado.DPS.Mean,
		})
	}
	return map[string]any{
		"disponible": true,
		"mejor":      best.Prioridad,
		"parametros": best.Parametros,
		"opciones":   top,
		"supuestos":  plan.Supuestos,
		"nota":       "Cálculo propio de la simulación, no una guía de la comunidad.",
	}
}

// withExtras adds keys to a tool answer that is a JSON object; other answers are wrapped under datos.
func withExtras(v any, extras map[string]any) any {
	if len(extras) == 0 {
		return v
	}
	m := map[string]any{}
	if mv, ok := v.(map[string]any); ok {
		for k, x := range mv {
			m[k] = x
		}
	} else {
		m["datos"] = v
	}
	for k, x := range extras {
		m[k] = x
	}
	return m
}

// theoryReviewSummary compares the recent fights (the last one when ultimo is set, otherwise up to three,
// only the deaths when muertes is set) with the expected damage per second of the model.
func theoryReviewSummary(ctx context.Context, g *gamedata.Service, ws *websearch.Client, n int, ultimo, muertes bool) map[string]any {
	cls, in, err := theoryFor(ctx, g, ws)
	if err != nil {
		return map[string]any{"disponible": false, "motivo": err.Error()}
	}
	want := 3
	if ultimo {
		want = 1
	}
	if n > 0 && n < want {
		want = n
	}
	var reviews []map[string]any
	for _, f := range g.RecentFights(10) {
		if muertes && !f.Muerte {
			continue
		}
		if len(reviews) == want {
			break
		}
		rev := cls.Review(in, f)
		reviews = append(reviews, map[string]any{
			"duracionSeg": f.Segundos, "muerte": f.Muerte, "danoHecho": f.DanoHecho, "revision": rev,
		})
	}
	if len(reviews) == 0 {
		return map[string]any{"disponible": true, "combates": []any{}, "nota": "No hay combates que comparar con el modelo."}
	}
	return map[string]any{
		"disponible": true,
		"combates":   reviews,
		"nota":       "Brecha = daño real frente al esperado por el modelo para tu nivel y un enemigo de nivel ±2; negativa = por debajo.",
	}
}
