package websearch

// Parsers for the pages of wowforevertalents.com. They are tolerant on purpose: the site is
// generated, so they look for elements by their role (a heading, a labelled row, a
// data-tip attribute) and not by brittle CSS class chains. Each parser returns what it can
// read and leaves out what it cannot; nothing is guessed.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// ---- abilities (/abilities/<class>/) -----------------------------------------------

// WFTRank is one rank of a trainer spell.
type WFTRank struct {
	Rango   string `json:"rango"`
	Nivel   int    `json:"nivel"`
	Coste   string `json:"coste,omitempty"` // mana or resource, range, cast time, cooldown as the page writes them
	Texto   string `json:"texto"`
	Classic string `json:"clasico,omitempty"` // the Classic Era wording, when it changed
}

// WFTSpell is one trainer spell of a class, with all its ranks.
type WFTSpell struct {
	Nombre string    `json:"nombre"`
	Estado string    `json:"estado"` // "nuevo en Forever", "cambiado", "igual que Classic" or "retirado"
	Raza   []string  `json:"razas,omitempty"`
	Pasiva bool      `json:"pasiva,omitempty"`
	Rangos []WFTRank `json:"rangos"`
}

// WFTSchool is one spell school (Holy, Shadow Magic...) of a class.
type WFTSchool struct {
	Nombre   string     `json:"escuela"`
	Hechizos []WFTSpell `json:"hechizos"`
}

// WFTProficiency is one armour or weapon row of the class page. SpellID is the spell that
// grants it, when the page links it.
type WFTProficiency struct {
	Nombre     string `json:"nombre"`
	Detalle    string `json:"detalle,omitempty"`
	Disponible bool   `json:"disponible"`
	SpellID    int    `json:"spellId,omitempty"`
}

// WFTAbilities is a class page: its spells by school and its proficiencies.
type WFTAbilities struct {
	Clase     string           `json:"clase"`
	Escuelas  []WFTSchool      `json:"escuelas"`
	Armaduras []WFTProficiency `json:"armaduras"`
	Armas     []WFTProficiency `json:"armas"`
	DualWield string           `json:"dualWield,omitempty"`
	Fuente    string           `json:"fuente,omitempty"`
}

// wftTip is the JSON in a spell cell's data-tip attribute.
type wftTip struct {
	N string   `json:"n"`
	S string   `json:"s"`
	P bool     `json:"p"`
	T []string `json:"t"`
	K []struct {
		R string `json:"r"`
		L int    `json:"l"`
		M string `json:"m"`
		D string `json:"d"`
		C string `json:"c"`
	} `json:"k"`
}

var (
	spellIDRe     = regexp.MustCompile(`spell=(\d+)`)
	buildSourceRe = regexp.MustCompile(`(?i)(Forever client build [0-9.]+)`)
)

// parseAbilities reads a class page. Spells come from the data-tip of each spell cell (its
// name, status and ranks); the school is the last page title above the cell.
func parseAbilities(page, class string) (WFTAbilities, error) {
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return WFTAbilities{}, fmt.Errorf("leer la página de %s: %w", class, err)
	}
	out := WFTAbilities{Clase: class}
	if m := buildSourceRe.FindStringSubmatch(textOf(doc)); m != nil {
		out.Fuente = m[1]
	}
	school := -1
	seen := map[string]bool{}
	schoolAt := func(name string) int {
		for i, s := range out.Escuelas {
			if s.Nombre == name {
				return i
			}
		}
		out.Escuelas = append(out.Escuelas, WFTSchool{Nombre: name})
		return len(out.Escuelas) - 1
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch {
			case n.Data == "h2" && hasClass(n, "page-title"):
				school = schoolAt(textOf(n))
				return
			case n.Data == "li" && hasClass(n, "cell"):
				if sp, ok := spellFromCell(n); ok {
					if school < 0 {
						school = schoolAt("General")
					}
					key := out.Escuelas[school].Nombre + "|" + sp.Nombre
					if !seen[key] {
						seen[key] = true
						out.Escuelas[school].Hechizos = append(out.Escuelas[school].Hechizos, sp)
					}
				}
				return
			case n.Data == "section" && hasClass(n, "prof-part"):
				parseProficiencySection(n, &out)
				return
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	if len(out.Escuelas) == 0 && len(out.Armaduras) == 0 && len(out.Armas) == 0 {
		return out, fmt.Errorf("la página de %s no tiene hechizos ni competencias reconocibles", class)
	}
	return out, nil
}

// spellFromCell decodes one spell cell.
func spellFromCell(li *html.Node) (WFTSpell, bool) {
	tipJSON := ""
	var find func(n *html.Node)
	find = func(n *html.Node) {
		if tipJSON != "" {
			return
		}
		if n.Type == html.ElementNode {
			if v := attr(n, "data-tip"); v != "" {
				tipJSON = v
				return
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			find(ch)
		}
	}
	find(li)
	if tipJSON == "" {
		return WFTSpell{}, false
	}
	var tip wftTip
	if err := json.Unmarshal([]byte(tipJSON), &tip); err != nil || strings.TrimSpace(tip.N) == "" {
		return WFTSpell{}, false
	}
	sp := WFTSpell{
		Nombre: collapse(tip.N),
		Estado: statusES(tip.S),
		Raza:   tip.T,
		Pasiva: tip.P,
	}
	for i, k := range tip.K {
		rank := collapse(k.R)
		if rank == "" {
			rank = fmt.Sprintf("Rango %d", i+1)
		}
		r := WFTRank{Rango: rank, Nivel: k.L, Coste: collapse(k.M), Texto: collapse(k.D)}
		if c := collapse(k.C); c != "" && c != r.Texto {
			r.Classic = c
		}
		sp.Rangos = append(sp.Rangos, r)
	}
	return sp, true
}

// statusES names the status the page gives a spell.
func statusES(s string) string {
	switch s {
	case "new":
		return "nuevo en Forever"
	case "changed":
		return "cambiado"
	case "unchanged":
		return "igual que Classic"
	case "removed":
		return "retirado"
	}
	return s
}

// parseProficiencySection reads one "prof-part" block (armour, weapons or dual wield).
func parseProficiencySection(sec *html.Node, out *WFTAbilities) {
	heading := ""
	for ch := sec.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && ch.Data == "h3" {
			heading = textOf(ch)
			break
		}
	}
	lower := strings.ToLower(heading)
	if strings.Contains(lower, "dual") {
		text := strings.TrimSpace(strings.TrimPrefix(textOf(sec), heading))
		out.DualWield = truncateRunes(text, 400)
		return
	}
	var items []WFTProficiency
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "li" && hasClass(n, "prof-item") {
			p := WFTProficiency{Disponible: !hasClass(n, "is-off")}
			p.Nombre, p.Detalle = profItemText(n)
			if a := findElement(n, "a"); a != nil {
				if m := spellIDRe.FindStringSubmatch(attr(a, "href")); m != nil {
					p.SpellID, _ = strconv.Atoi(m[1])
				}
			}
			if p.Nombre != "" {
				items = append(items, p)
			}
			return
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(sec)
	if strings.Contains(lower, "weapon") || strings.Contains(lower, "arma") {
		out.Armas = append(out.Armas, items...)
	} else {
		out.Armaduras = append(out.Armaduras, items...)
	}
}

// profItemText returns the name and the "from" text of a proficiency row: the labelled spans
// are read first, then the row text as a fallback.
func profItemText(li *html.Node) (name, from string) {
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "span" {
			switch {
			case hasClass(n, "prof-name") && name == "":
				name = textOf(n)
				return
			case hasClass(n, "prof-lvl") && from == "":
				from = textOf(n)
				return
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(li)
	if name == "" {
		name = textOf(li)
	}
	return name, from
}

// ---- builds (/<class>/builds/<spec>-<a>-<b>-<c>-<hash>/) ---------------------------

// WFTTalentPick is one talent of a build, as the page lists it (by tier, not as picked).
type WFTTalentPick struct {
	Nombre      string `json:"nombre"`
	Rango       int    `json:"rango"`
	Maximo      int    `json:"maximo"`
	PuntosReq   int    `json:"puntosRequeridos,omitempty"`
	Descripcion string `json:"descripcion,omitempty"`
}

// WFTBuildSpec is one spec of a build (Discipline 21 points...).
type WFTBuildSpec struct {
	Arbol    string          `json:"arbol"`
	Puntos   int             `json:"puntos"`
	Talentos []WFTTalentPick `json:"talentos"`
}

// WFTBuild is a community build saved in the calculator. It is a suggestion from players,
// not the game's: the caller must say so.
type WFTBuild struct {
	Clase   string         `json:"clase"`
	Resumen string         `json:"resumen,omitempty"`
	Puntos  int            `json:"puntosTotales,omitempty"`
	Reparto string         `json:"reparto,omitempty"` // "21/18/0"
	Arboles []WFTBuildSpec `json:"arboles"`
	Otras   []string       `json:"otrasBuilds,omitempty"`
	Nota    string         `json:"nota"`
}

var (
	buildIntroRe = regexp.MustCompile(`(\d+) points, ([\d/]+) across`)
	specHeadRe   = regexp.MustCompile(`^(.+?): (\d+) points$`)
	talentHeadRe = regexp.MustCompile(`^(.+?) (\d+)/(\d+)(?: (\d+) pts)?(?: [◆★])?$`)
	otherBuildRe = regexp.MustCompile(`^(.+?) (\d+/\d+/\d+) (\d+) talents`)
)

// parseBuild reads a build page by its headings: "## <tree>: N points", then "### <talent>
// r/m [N pts]" with the description under each talent.
func parseBuild(page, class string) (WFTBuild, error) {
	lines := renderLines(page)
	b := WFTBuild{Clase: class, Nota: "Build de la comunidad (jugadores que la guardaron en la calculadora de WoW Forever), no una recomendación oficial."}
	var cur *WFTBuildSpec
	inOther := false
	var lastTalent *WFTTalentPick
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "## "):
			head := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			lastTalent = nil
			inOther = false
			if m := specHeadRe.FindStringSubmatch(head); m != nil {
				pts, _ := strconv.Atoi(m[2])
				b.Arboles = append(b.Arboles, WFTBuildSpec{Arbol: m[1], Puntos: pts})
				cur = &b.Arboles[len(b.Arboles)-1]
				continue
			}
			if strings.Contains(strings.ToLower(head), "other") || strings.Contains(strings.ToLower(head), "otras") {
				inOther = true
			}
			cur = nil
		case strings.HasPrefix(line, "### ") && cur != nil:
			head := strings.TrimSpace(strings.TrimPrefix(line, "### "))
			m := talentHeadRe.FindStringSubmatch(head)
			if m == nil {
				lastTalent = nil
				continue
			}
			t := WFTTalentPick{Nombre: strings.TrimSpace(m[1])}
			t.Rango, _ = strconv.Atoi(m[2])
			t.Maximo, _ = strconv.Atoi(m[3])
			if m[4] != "" {
				t.PuntosReq, _ = strconv.Atoi(m[4])
			}
			cur.Talentos = append(cur.Talentos, t)
			lastTalent = &cur.Talentos[len(cur.Talentos)-1]
		case strings.HasPrefix(line, "###"):
			lastTalent = nil
		case inOther && strings.HasPrefix(line, "- "):
			if m := otherBuildRe.FindStringSubmatch(strings.TrimPrefix(line, "- ")); m != nil {
				b.Otras = append(b.Otras, m[1]+" "+m[2]+" ("+m[3]+" talentos)")
			}
		case lastTalent != nil && lastTalent.Descripcion == "" && line != "":
			lastTalent.Descripcion = truncateRunes(line, 300)
			lastTalent = nil
		case b.Resumen == "" && buildIntroRe.MatchString(line):
			b.Resumen = truncateRunes(line, 400)
			if m := buildIntroRe.FindStringSubmatch(line); m != nil {
				b.Puntos, _ = strconv.Atoi(m[1])
				b.Reparto = m[2]
			}
		}
	}
	if len(b.Arboles) == 0 {
		return b, fmt.Errorf("la página no tiene árboles de talentos reconocibles")
	}
	return b, nil
}

// ---- dungeons (/dungeons/ and /dungeons/<slug>/) ------------------------------------

// WFTDungeonRow is one row of the dungeon list: an instance, raid or battleground.
type WFTDungeonRow struct {
	Nombre     string `json:"nombre"`
	Jugadores  int    `json:"jugadores,omitempty"` // group size; for battlegrounds, players a side
	NivelMin   int    `json:"nivelMinimo,omitempty"`
	NivelMax   int    `json:"nivelMaximo,omitempty"` // a level band or the Group Finder level
	Estado     string `json:"estado,omitempty"`      // "confirmada", "anunciada", "no confirmada" or ""
	Novedad    string `json:"novedad,omitempty"`     // "nuevo en Forever", "cambiado" or ""
	Aproximado bool   `json:"aproximado,omitempty"`  // the page marks the level with "?"
}

// WFTDungeon is one dungeon page: its group, bosses in order and where it is.
type WFTDungeon struct {
	Nombre    string   `json:"nombre"`
	Jugadores string   `json:"jugadores,omitempty"` // "40 players", "5 players"
	Bloqueo   string   `json:"bloqueo,omitempty"`   // "weekly lockout"
	Jefes     []string `json:"jefes"`
	Zonas     []string `json:"zonas,omitempty"`
	Fuente    string   `json:"fuente,omitempty"`
}

var (
	dungeonNumsRe = regexp.MustCompile(`((?:\s+\d+\??)+)$`)
	bgRe          = regexp.MustCompile(`^(.+?)\s+(\d+)v(\d+)\s+(\d+)\s*[–-]\s*(\d+)$`)
	playersRe     = regexp.MustCompile(`(\d+) players`)
)

// parseDungeonList reads the "Every instance" rows: name, marks, group size and level.
func parseDungeonList(page string) []WFTDungeonRow {
	var rows []WFTDungeonRow
	for _, line := range renderLines(page) {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		s := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		row := WFTDungeonRow{}
		switch {
		case strings.Contains(s, "★"):
			row.Novedad = "nuevo en Forever"
		case strings.Contains(s, "◆"):
			row.Novedad = "cambiado"
		}
		s = strings.NewReplacer("★", "", "◆", "", "✕", "").Replace(s)
		s = strings.Join(strings.Fields(s), " ")
		if m := bgRe.FindStringSubmatch(s); m != nil {
			row.Nombre = m[1]
			row.Jugadores, _ = strconv.Atoi(m[2])
			row.NivelMin, _ = strconv.Atoi(m[4])
			row.NivelMax, _ = strconv.Atoi(m[5])
			rows = append(rows, row)
			continue
		}
		m := dungeonNumsRe.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		head := strings.TrimSpace(strings.TrimSuffix(s, m[1]))
		nums := strings.Fields(m[1])
		for _, st := range []string{"Not confirmed", "Confirmed", "Announced"} {
			if strings.HasSuffix(head, " "+st) {
				row.Estado = map[string]string{"Confirmed": "confirmada", "Announced": "anunciada", "Not confirmed": "no confirmada"}[st]
				head = strings.TrimSpace(strings.TrimSuffix(head, " "+st))
				break
			}
		}
		row.Nombre = head
		if row.Nombre == "" {
			continue
		}
		vals := make([]int, 0, len(nums))
		for _, n := range nums {
			if strings.HasSuffix(n, "?") {
				row.Aproximado = true
			}
			v, _ := strconv.Atoi(strings.TrimSuffix(n, "?"))
			vals = append(vals, v)
		}
		switch len(vals) {
		case 1:
			row.Jugadores = vals[0]
		default:
			row.Jugadores = vals[0]
			row.NivelMax = vals[len(vals)-1]
		}
		rows = append(rows, row)
	}
	return rows
}

// parseDungeon reads one dungeon page by its headings: "## Entering", "## Bosses · N", "## Areas inside".
func parseDungeon(page, name string) (WFTDungeon, error) {
	d := WFTDungeon{Nombre: name}
	section := ""
	for _, line := range renderLines(page) {
		if strings.HasPrefix(line, "## ") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "## ")))
			continue
		}
		if i := strings.Index(line, "Sources:"); i >= 0 {
			d.Fuente = truncateRunes(strings.TrimSpace(line[i:]), 200)
		}
		switch {
		case strings.HasPrefix(section, "entering"):
			if strings.HasPrefix(line, "- ") {
				s := strings.TrimPrefix(line, "- ")
				if m := playersRe.FindStringSubmatch(s); m != nil && d.Jugadores == "" {
					d.Jugadores = m[0]
				}
				if i := strings.Index(s, "·"); i >= 0 && d.Bloqueo == "" {
					d.Bloqueo = strings.TrimSpace(s[i+len("·"):])
				}
			}
		case strings.HasPrefix(section, "bosses"):
			if strings.HasPrefix(line, "- ") {
				jefe := strings.TrimSpace(strings.TrimPrefix(line, "- "))
				jefe = strings.TrimSpace(strings.TrimSuffix(jefe, "Search Wowhead ↗"))
				if jefe != "" {
					d.Jefes = append(d.Jefes, jefe)
				}
			}
		case strings.HasPrefix(section, "areas"):
			if line != "" && !strings.HasPrefix(line, "-") {
				d.Zonas = append(d.Zonas, line)
				section = "" // only the first line under the heading names the zones
			}
		}
	}
	if len(d.Jefes) == 0 && d.Jugadores == "" {
		return d, fmt.Errorf("la página de %s no tiene jefes ni grupo reconocibles", name)
	}
	return d, nil
}

// ---- professions (/professions/<profesion>/<seccion>/) --------------------------------

// WFTRecipe is one recipe line of a profession page with the skill it needs.
type WFTRecipe struct {
	Nombre string `json:"nombre"`
	Nivel  int    `json:"nivel"`
	Estado string `json:"estado,omitempty"`
}

// WFTProfession is a profession page: the skill cap shown and the recipes listed.
type WFTProfession struct {
	Profesion string      `json:"profesion"`
	Maximo    int         `json:"maximo,omitempty"`
	Recetas   []WFTRecipe `json:"recetas"`
}

var (
	skillCapRe = regexp.MustCompile(`^([A-Za-zÀ-ÿ' -]+?) (\d+)/(\d+)\b`)
	recipeRe   = regexp.MustCompile(`^(.+?)(?:\s+\d+)?\s+(\d+)$`)
)

// parseProfession reads recipe lines ("- Name[mark] skill") and the skill cap line.
func parseProfession(page, profession string) (WFTProfession, error) {
	p := WFTProfession{Profesion: profession}
	for _, line := range renderLines(page) {
		if p.Maximo == 0 {
			if m := skillCapRe.FindStringSubmatch(line); m != nil {
				p.Maximo, _ = strconv.Atoi(m[3])
				continue
			}
		}
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		s := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		estado := ""
		switch {
		case strings.Contains(s, "★"):
			estado = "nuevo en Forever"
		case strings.Contains(s, "◆"):
			estado = "cambiado"
		}
		s = strings.Join(strings.Fields(strings.NewReplacer("★", "", "◆", "", "✕", "").Replace(s)), " ")
		m := recipeRe.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		lvl, _ := strconv.Atoi(m[2])
		p.Recetas = append(p.Recetas, WFTRecipe{Nombre: m[1], Nivel: lvl, Estado: estado})
	}
	if len(p.Recetas) == 0 {
		return p, fmt.Errorf("la página de %s no tiene recetas reconocibles", profession)
	}
	return p, nil
}

// ---- shared helpers ------------------------------------------------------------

// renderLines returns the readable lines of a page, as htmlToText writes them.
func renderLines(page string) []string {
	var out []string
	for _, l := range strings.Split(htmlToText(page), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	return out
}
