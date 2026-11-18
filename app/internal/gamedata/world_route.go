package gamedata

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// ruta_viaje: the best way from the player's position to a destination, walking or taking the
// measured flight paths (taxi) of the character's known flight masters. Walking uses the same
// speed estimate as valor_misiones; flights use the seconds the addon measured on each taxi.
// Walking between maps has no measured route: it costs questMapHopSec and says so.

const (
	worldMaxCands   = 8 // destinations kept after the name search
	worldMaxOptions = 3 // route options kept per destination
	worldHintWorld  = "Abre el mapa de vuelos (taxi) de tus ciudades con el addon para medir los vuelos; escribe /compa sync en el juego"
	worldHintPos    = "No sé dónde estás: abre World of Warcraft en modo ventana o sin bordes"
)

// routeLeg is one step of a route: a walk to a stop or one flight.
type routeLeg struct {
	Kind     string // "caminar" or "vuelo"
	Desc     string // the step in Spanish
	Seconds  float64
	Copper   int64
	Measured bool      // flights are measured; walks are estimated
	Stop     questStop // where the leg ends (for a flight: where the player boards)
	Label    string    // the name of the stop, for the marca
	Estimate bool      // a walk between maps: no measured route
}

// routeOption is one way to a destination.
type routeOption struct {
	Kind     string // "caminar" or "vuelo"
	Seconds  float64
	Copper   int64
	Hops     int
	Measured bool // true when it uses flights only (walk legs are estimated)
	Legs     []routeLeg
	Landing  int64 // the last flight master used (0 when walking only)
}

// routeDest is a destination candidate.
type routeDest struct {
	Stop  questStop
	Label string
	Kind  string // "nodo", "npc", "mision", "coordenadas" or "zona"
	Node  int64  // the flight master, when the destination is one
}

// flightRouter is the known flight graph of the character, with the walking speed in use.
type flightRouter struct {
	nodes map[int64]worldNode // known nodes with a position or without
	adj   map[int64][]worldEdge
	edges map[[2]int64]worldEdge
	speed float64 // yards per second: walking, or mounted
	yps   float64
	any   bool    // there is at least one measured flight between known nodes
	hop   float64 // seconds for a walk across maps: +Inf (no walk) for ruta_viaje, the map hop for valor_misiones
}

// newFlightRouter builds the graph over the nodes the character knows. mounted selects the
// mounted speed estimate.
func newFlightRouter(w worldData, mounted bool) flightRouter {
	fr := flightRouter{nodes: map[int64]worldNode{}, adj: map[int64][]worldEdge{}, edges: map[[2]int64]worldEdge{}}
	fr.speed = questSpeedYPS
	if mounted {
		fr.speed = worldMountYPS
	}
	fr.yps = questYardsPerUnit
	fr.hop = math.Inf(1)
	for id, n := range w.Nodes {
		if w.KnownNodes[id] {
			fr.nodes[id] = n
		}
	}
	for key, e := range w.Edges {
		if _, ok := fr.nodes[key[0]]; !ok {
			continue
		}
		if _, ok := fr.nodes[key[1]]; !ok {
			continue
		}
		fr.edges[key] = e
		fr.adj[key[0]] = append(fr.adj[key[0]], e)
		fr.any = true
	}
	return fr
}

// stopOf returns the position of a flight master.
func (n worldNode) stop() questStop {
	return questStop{MapID: n.MapID, X: n.X, Y: n.Y}
}

// walkSec is the walking time from a to b. Across maps there is no measured walk: the hop of the
// router is used (infinite for ruta_viaje, where walking between maps is not an option) and sameMap
// is false.
func (fr flightRouter) walkSec(a, b questStop) (sec float64, sameMap bool) {
	if a.MapID != b.MapID {
		return fr.hop, false
	}
	units := math.Hypot(a.X-b.X, a.Y-b.Y) * 100
	return units * fr.yps / fr.speed, true
}

// reach returns the seconds to each known flight master from start, by the cheapest combination of
// walking to a master and measured flights; prev gives the master before it (0 for a walk start).
// Masters without a position can be reached only by flight.
func (fr flightRouter) reach(start questStop) (dist map[int64]float64, prev map[int64]int64) {
	dist = map[int64]float64{}
	prev = map[int64]int64{}
	for id, n := range fr.nodes {
		if !n.HasPos {
			continue
		}
		d, _ := fr.walkSec(start, n.stop())
		if math.IsInf(d, 1) {
			continue
		}
		dist[id] = d
		prev[id] = 0
	}
	done := map[int64]bool{}
	for {
		u, best := int64(0), math.Inf(1)
		for id, d := range dist {
			if done[id] {
				continue
			}
			if d < best || (d == best && id < u) {
				u, best = id, d
			}
		}
		if u == 0 {
			break
		}
		done[u] = true
		for _, e := range fr.adj[u] {
			nd := dist[u] + e.Seconds
			if old, ok := dist[e.To]; !ok || nd < old {
				dist[e.To] = nd
				prev[e.To] = u
			}
		}
	}
	return dist, prev
}

// nodeName is the name of a flight master for the text, with its id when unnamed.
func nodeName(n worldNode) string {
	if n.Name != "" {
		return n.Name
	}
	return fmt.Sprintf("nodo %d", n.ID)
}

// options returns the ways from start to dest: walking, and the flight routes that land at the
// flight masters nearest to the destination, best first. At most worldMaxOptions are returned.
func (fr flightRouter) options(start, dest questStop, destLabel string, destNode int64) []routeOption {
	var out []routeOption
	walk, same := fr.walkSec(start, dest)
	walkLeg := routeLeg{
		Kind: "caminar", Seconds: walk, Measured: false, Stop: dest, Label: destLabel,
		Estimate: !same, Desc: "Camina hasta " + destLabel,
	}
	if !math.IsInf(walk, 1) {
		out = append(out, routeOption{Kind: "caminar", Seconds: walk, Legs: []routeLeg{walkLeg}})
	}

	if !fr.any {
		return sortOptions(out)
	}
	dist, prev := fr.reach(start)
	bestPer := map[int64]routeOption{}
	for landing := range dist {
		n := fr.nodes[landing]
		opt := fr.pathOption(start, landing, prev)
		if opt == nil {
			continue
		}
		if destNode != landing {
			if !n.HasPos {
				continue
			}
			w, sameAsDest := fr.walkSec(n.stop(), dest)
			if math.IsInf(w, 1) {
				continue
			}
			opt.Legs = append(opt.Legs, routeLeg{Kind: "caminar", Seconds: w, Stop: dest, Label: destLabel,
				Estimate: !sameAsDest, Desc: "Camina desde " + nodeName(n) + " hasta " + destLabel})
		}
		opt.Landing = landing
		opt = sortOptionsOne(opt)
		if old, ok := bestPer[landing]; !ok || opt.Seconds < old.Seconds {
			bestPer[landing] = *opt
		}
	}
	for _, o := range bestPer {
		out = append(out, o)
	}
	out = sortOptions(out)
	if len(out) > worldMaxOptions {
		out = out[:worldMaxOptions]
	}
	return out
}

// pathOption rebuilds the legs of the cheapest route that lands at landing: the walk to the first
// master, then each taxi in order. It returns nil when the route cannot be rebuilt.
func (fr flightRouter) pathOption(start questStop, landing int64, prev map[int64]int64) *routeOption {
	var path []int64
	cur := landing
	for steps := 0; steps <= len(fr.nodes); steps++ {
		path = append([]int64{cur}, path...)
		p, ok := prev[cur]
		if !ok {
			return nil
		}
		if p == 0 {
			break
		}
		cur = p
	}
	first := fr.nodes[path[0]]
	walk, _ := fr.walkSec(start, first.stop())
	opt := &routeOption{Kind: "vuelo", Measured: true}
	opt.Legs = append(opt.Legs, routeLeg{
		Kind: "caminar", Seconds: walk, Stop: first.stop(), Label: nodeName(first),
		Desc: "Camina hasta " + nodeName(first), Estimate: !sameMap(start, first.stop()),
	})
	for i := 0; i+1 < len(path); i++ {
		a, b := fr.nodes[path[i]], fr.nodes[path[i+1]]
		e, ok := fr.edges[[2]int64{a.ID, b.ID}]
		if !ok {
			return nil
		}
		opt.Legs = append(opt.Legs, routeLeg{
			Kind: "vuelo", Seconds: e.Seconds, Copper: e.Copper, Measured: true,
			Stop: a.stop(), Label: nodeName(a),
			Desc: "Toma vuelo en " + nodeName(a) + " hacia " + nodeName(b),
		})
		opt.Copper += e.Copper
		opt.Hops++
	}
	return opt
}

// sameMap is true when both stops are on one map.
func sameMap(a, b questStop) bool { return a.MapID == b.MapID }

// sortOptionsOne sets the totals of one option from its legs.
func sortOptionsOne(o *routeOption) *routeOption {
	out := sortOptions([]routeOption{*o})
	return &out[0]
}

// sortOptions orders the options by total seconds, walking first on a tie, and recomputes each
// option's totals from its legs.
func sortOptions(opts []routeOption) []routeOption {
	for i := range opts {
		var sec float64
		var cop int64
		for _, l := range opts[i].Legs {
			sec += l.Seconds
			cop += l.Copper
		}
		opts[i].Seconds = sec
		opts[i].Copper = cop
		opts[i].Measured = opts[i].Kind == "vuelo"
	}
	sort.SliceStable(opts, func(i, j int) bool {
		if opts[i].Seconds != opts[j].Seconds {
			return opts[i].Seconds < opts[j].Seconds
		}
		return opts[i].Kind == "caminar" && opts[j].Kind != "caminar"
	})
	return opts
}

// coordRe finds the numbers of a coordinate text such as "37 45.2 53.1" or "45.2, 53.1".
var coordRe = regexp.MustCompile(`\d+(?:\.\d+)?`)

// coordsText reads a coordinate destination: "x y" on the current map, or "mapID x y". The numbers
// are on the 0..100 scale of the app. ok is false when the text is not coordinates.
func coordsOf(text string, pos position) (questStop, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.Trim(trimmed, "0123456789.,;:/ ") != "" {
		return questStop{}, false
	}
	nums := coordRe.FindAllString(trimmed, -1)
	var mapID int
	var x, y float64
	switch len(nums) {
	case 2:
		if !pos.OK {
			return questStop{}, false
		}
		mapID = pos.MapID
		fmt.Sscanf(nums[0], "%g", &x)
		fmt.Sscanf(nums[1], "%g", &y)
	case 3:
		fmt.Sscanf(nums[0], "%d", &mapID)
		fmt.Sscanf(nums[1], "%g", &x)
		fmt.Sscanf(nums[2], "%g", &y)
	default:
		return questStop{}, false
	}
	if mapID <= 0 || x < 0 || y < 0 || x > 100 || y > 100 {
		return questStop{}, false
	}
	return questStop{MapID: mapID, X: x / 100, Y: y / 100}, true
}

// resolveDests turns the destination text into candidates: coordinates, flight masters, vendors and
// trainers (by name), NPCs and quests from the knowledge store, and the flight masters of a zone.
// Exact name matches come first; the list is capped at worldMaxCands.
func (g *Service) resolveDests(w worldData, text string, pos position) []routeDest {
	var out []routeDest
	seen := map[string]bool{}
	add := func(d routeDest) {
		key := fmt.Sprintf("%d|%.3f|%.3f|%d", d.Stop.MapID, d.Stop.X, d.Stop.Y, d.Node)
		if seen[key] || len(out) >= worldMaxCands || d.Stop.MapID <= 0 {
			return
		}
		seen[key] = true
		out = append(out, d)
	}
	if s, ok := coordsOf(text, pos); ok {
		add(routeDest{Stop: s, Label: coordsText(s.X, s.Y) + " en el mapa " + fmt.Sprint(s.MapID), Kind: "coordenadas"})
		return out
	}
	needle := fold(strings.TrimSpace(text))
	if needle == "" {
		return nil
	}
	exact := func(name string) bool { return fold(name) == needle }
	contains := func(name string) bool { return name != "" && strings.Contains(fold(name), needle) }

	// Flight masters: an exact name first, then a partial one.
	nodes := sortedNodes(w.Nodes)
	for _, pass := range []func(string) bool{exact, contains} {
		for _, n := range nodes {
			if n.HasPos && pass(n.Name) {
				add(routeDest{Stop: n.stop(), Label: n.Name, Kind: "nodo", Node: n.ID})
			}
		}
	}
	for _, id := range sortedVendorIDs(w.Vendors) {
		v := w.Vendors[id]
		if v.HasPos && contains(v.Name) {
			add(routeDest{Stop: questStop{MapID: v.MapID, X: v.X, Y: v.Y}, Label: v.Name, Kind: "npc"})
		}
	}
	for _, id := range sortedTrainerIDs(w.Trainers) {
		t := w.Trainers[id]
		if t.HasPos && contains(t.Name) {
			add(routeDest{Stop: questStop{MapID: t.MapID, X: t.X, Y: t.Y}, Label: t.Name, Kind: "npc"})
		}
	}
	if g.store != nil && len([]rune(needle)) >= 3 {
		if npcs, err := g.store.SearchNpcs(strings.TrimSpace(text), 10); err == nil {
			for _, n := range npcs {
				if n.MapID > 0 && (n.X != 0 || n.Y != 0) {
					x, y := n.X, n.Y
					if x > 1 || y > 1 {
						x, y = x/100, y/100
					}
					add(routeDest{Stop: questStop{MapID: n.MapID, X: x, Y: y}, Label: n.Name, Kind: "npc"})
				}
			}
		}
		if qs, err := g.store.SearchQuests(strings.TrimSpace(text), 5); err == nil {
			for _, q := range qs {
				if q.GiverMapID > 0 && (q.GiverX != 0 || q.GiverY != 0) {
					x, y := q.GiverX, q.GiverY
					if x > 1 || y > 1 {
						x, y = x/100, y/100
					}
					add(routeDest{Stop: questStop{MapID: q.GiverMapID, X: x, Y: y},
						Label: "«" + q.Title + "» (aceptar en " + q.GiverName + ")", Kind: "mision"})
				}
			}
		}
	}
	// A zone: the flight masters on a map whose name matches.
	if len(out) == 0 {
		maps := map[int]bool{}
		for _, n := range nodes {
			maps[n.MapID] = true
		}
		for _, mapID := range sortedInts(maps) {
			if name, _ := g.zoneName(mapID, nil); name != "" && contains(name) {
				for _, n := range nodes {
					if n.MapID == mapID && n.HasPos {
						add(routeDest{Stop: n.stop(), Label: n.Name + " (" + name + ")", Kind: "zona", Node: n.ID})
					}
				}
			}
		}
	}
	return out
}

// travelHop returns the time of the best route between two stops on different maps: the cheapest
// measured flight route when there is one, else the map hop estimate. It is the travel of valor_misiones.
type travelHop func(a, b questStop) (sec float64, measured bool)

// travelHopper builds the travel function of valor_misiones from the character's flight data. It
// is nil when there is no flight data, so the quest estimate keeps its map hop.
func (g *Service) travelHopper() travelHop {
	w, ok := g.loadWorld()
	if !ok {
		return nil
	}
	fr := newFlightRouter(w, false)
	if !fr.any {
		return nil
	}
	return func(a, b questStop) (float64, bool) {
		if a.MapID == b.MapID {
			return travelSeconds(a, b), false
		}
		// Across maps the measured flights are the only route: the cheapest one, when there is one.
		for _, o := range fr.options(a, b, "destino", 0) {
			if o.Kind == "vuelo" {
				return o.Seconds, true
			}
		}
		return questMapHopSec, false
	}
}

// RouteTo answers ruta_viaje. The mounted speed is used while the live bridge says the player is mounted.
func (g *Service) RouteTo(destino string) (any, error) {
	live, liveOK := g.Live()
	mounted := liveOK && live.Mounted
	pos := g.playerPosition()
	if !pos.OK {
		return unavailable(worldHintPos), nil
	}
	w, ok := g.loadWorld()
	if !ok {
		return unavailable(worldHintWorld), nil
	}
	dests := g.resolveDests(w, destino, pos)
	if len(dests) == 0 {
		return map[string]any{"available": true, "source": sourceSV, "encontrado": false,
			"nota": fmt.Sprintf("No encuentro «%s» en los datos del mapa, los vuelos ni los NPC que he visto. Di el nombre tal como sale en el juego o las coordenadas (mapa x y).", destino)}, nil
	}
	fr := newFlightRouter(w, mounted)
	start := questStop{MapID: pos.MapID, X: pos.X, Y: pos.Y}
	type ranked struct {
		d    routeDest
		opts []routeOption
	}
	var all []ranked
	for _, d := range dests {
		if opts := fr.options(start, d.Stop, d.Label, d.Node); len(opts) > 0 {
			all = append(all, ranked{d: d, opts: opts})
		}
	}
	if len(all) == 0 {
		return map[string]any{"available": true, "source": sourceSV, "encontrado": false,
			"nota": "No tengo ruta medida hasta «" + destino + "»: a pie no hay camino entre mapas y no hay vuelos medidos que lleguen. " + worldHintWorld + "."}, nil
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].opts[0].Seconds < all[j].opts[0].Seconds })
	best := all[0]
	mejor := best.opts[0]
	alt := []map[string]any{}
	for _, o := range best.opts[1:] {
		alt = append(alt, optionView(o))
	}
	for _, r := range all[1:] {
		if len(alt) >= 3 {
			break
		}
		alt = append(alt, withDest(optionView(r.opts[0]), r.d))
	}
	out := map[string]any{
		"encontrado":   true,
		"destino":      best.d.Label,
		"tipoDestino":  best.d.Kind,
		"mejor":        optionView(mejor),
		"alternativas": alt,
		"pasos":        legTexts(mejor.Legs),
		"medido":       mejor.Measured,
		"velocidad":    speedText(mounted),
		"supuestos": []string{
			fmt.Sprintf("Caminar a %.0f yardas por segundo; montado %.0f (estimado).", questSpeedYPS, worldMountYPS),
			"Entre mapas a pie no hay ruta medida: se cuenta un cambio de mapa como " + fmt.Sprintf("%.0f", questMapHopSec) + " s (estimado).",
			"Los vuelos son los segundos medidos en el taxi (inicio hasta aterrizaje); cada vuelo se toma en su nodo.",
		},
		"vuelosConocidos": len(fr.edges),
		"nodosConocidos":  len(fr.nodes),
	}
	if len(mejor.Legs) > 0 {
		out["marca"] = marcaCommand(spawn{MapID: mejor.Legs[0].Stop.MapID, X: mejor.Legs[0].Stop.X, Y: mejor.Legs[0].Stop.Y}, mejor.Legs[0].Label)
	}
	if !fr.any {
		out["nota"] = "No tengo vuelos medidos entre tus nodos conocidos: solo calculo a pie. " + worldHintWorld + "."
	}
	return available(sourceSV, w.At, out), nil
}

// optionView is one route for the answer.
func optionView(o routeOption) map[string]any {
	m := map[string]any{
		"tipo":       o.Kind,
		"tiempo":     durText(o.Seconds),
		"segundos":   round1(o.Seconds),
		"cobre":      o.Copper,
		"costeTexto": moneyText(o.Copper),
		"vuelos":     o.Hops,
		"tiempoFuente": func() string {
			if o.Kind == "vuelo" {
				return "medido (vuelos) + estimado (a pie)"
			}
			return "estimado (a pie)"
		}(),
	}
	if o.Kind == "vuelo" {
		m["vuelosRuta"] = legTexts(o.Legs)
	}
	return m
}

func withDest(m map[string]any, d routeDest) map[string]any {
	m["destino"] = d.Label
	return m
}

// legTexts lists the steps of a route in Spanish, each with its time.
func legTexts(legs []routeLeg) []string {
	out := make([]string, 0, len(legs))
	for _, l := range legs {
		text := l.Desc
		if l.Seconds > 0 {
			text += " (" + durText(l.Seconds) + ")"
		}
		if l.Estimate {
			text += " [estimado]"
		}
		out = append(out, text)
	}
	return out
}

func speedText(mounted bool) string {
	if mounted {
		return "montado (estimado)"
	}
	return "a pie (estimado)"
}

// durText is a short Spanish duration: "45 s", "3 min 20 s".
func durText(sec float64) string {
	s := int(math.Round(sec))
	if s < 60 {
		return fmt.Sprintf("%d s", s)
	}
	return fmt.Sprintf("%d min %d s", s/60, s%60)
}

func sortedNodes(m map[int64]worldNode) []worldNode {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]worldNode, 0, len(ids))
	for _, id := range ids {
		out = append(out, m[id])
	}
	return out
}

func sortedVendorIDs(m map[int64]worldVendor) []int64 {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func sortedTrainerIDs(m map[int64]worldTrainer) []int64 {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func sortedInts(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
