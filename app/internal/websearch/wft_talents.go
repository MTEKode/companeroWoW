package websearch

// The talent tree of a class. wowforevertalents.com feeds its talent tooltips from one JSON
// file per class (/tips/<class>.json): every talent with its tree, row, points needed in the tree,
// prerequisite text, maximum rank and the description of its first and last rank. The site
// does not publish the column of a talent, so the tree is kept by row only.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// WFTTalent is one talent of a class tree, as the site's tooltip feed describes it.
type WFTTalent struct {
	Slug      string `json:"slug"`
	Nombre    string `json:"nombre"`
	Arbol     string `json:"arbol"`
	Fila      int    `json:"fila"`
	Maximo    int    `json:"maximo"`
	Necesita  int    `json:"puntosArbolRequeridos"` // points spent in the tree before this talent
	Requisito string `json:"requisito,omitempty"`   // "3 points in Improved Rend", as the page writes it
	Estado    string `json:"estado,omitempty"`      // "nuevo en Forever", "cambiado", "igual que Classic"
	// Rangos are the rank descriptions the feed gives: rank 1 and the maximum rank, or one only.
	Rangos []WFTTalentRank `json:"rangos"`
}

// WFTTalentRank is the client text of one rank the feed gives.
type WFTTalentRank struct {
	Rango int    `json:"rango"`
	Texto string `json:"texto"`
}

// WFTClassTalents is the talent tree of a class and the build the feed was read with.
type WFTClassTalents struct {
	Clase    string      `json:"clase"`
	Build    string      `json:"build,omitempty"` // client build, as the feed names it
	Talentos []WFTTalent `json:"talentos"`
	Fuente   string      `json:"fuente"`
}

var reqPointsRe = regexp.MustCompile(`^(\d+) points? in (.+)$`)

// ParseWFTTalents reads the tooltip feed of a class (/tips/<class>.json). Talents are sorted by tree,
// row and name, so the output is the same for the same feed.
func ParseWFTTalents(raw []byte, class string) (WFTClassTalents, error) {
	var feed struct {
		B string                     `json:"b"`
		T map[string]json.RawMessage `json:"t"`
	}
	if err := json.Unmarshal(raw, &feed); err != nil {
		return WFTClassTalents{}, fmt.Errorf("leer el JSON de talentos de %s: %w", class, err)
	}
	out := WFTClassTalents{Clase: class, Build: feed.B, Fuente: "wowforevertalents.com/tips/" + class + ".json"}
	for slug, rawT := range feed.T {
		var t struct {
			N  string `json:"n"`
			M  int    `json:"m"`
			Tr string `json:"tr"`
			R  int    `json:"row"`
			Nd int    `json:"need"`
			K  []struct {
				R int    `json:"r"`
				D string `json:"d"`
			} `json:"k"`
			Pre string `json:"pre"`
			Sl  string `json:"sl"`
		}
		if err := json.Unmarshal(rawT, &t); err != nil || t.N == "" {
			continue
		}
		tal := WFTTalent{
			Slug: slug, Nombre: collapse(t.N), Arbol: collapse(t.Tr), Fila: t.R, Maximo: t.M,
			Necesita: t.Nd, Requisito: collapse(t.Pre), Estado: collapse(t.Sl),
		}
		for _, k := range t.K {
			tal.Rangos = append(tal.Rangos, WFTTalentRank{Rango: k.R, Texto: collapse(k.D)})
		}
		sort.Slice(tal.Rangos, func(i, j int) bool { return tal.Rangos[i].Rango < tal.Rangos[j].Rango })
		out.Talentos = append(out.Talentos, tal)
	}
	if len(out.Talentos) == 0 {
		return out, fmt.Errorf("el JSON de talentos de %s no tiene talentos", class)
	}
	sort.Slice(out.Talentos, func(i, j int) bool {
		a, b := out.Talentos[i], out.Talentos[j]
		if a.Arbol != b.Arbol {
			return a.Arbol < b.Arbol
		}
		if a.Fila != b.Fila {
			return a.Fila < b.Fila
		}
		return a.Slug < b.Slug
	})
	return out, nil
}

// PrerequisiteOf splits a prerequisite text ("3 points in Improved Rend") into the points and the
// talent's name. ok is false when the talent has no prerequisite.
func PrerequisiteOf(req string) (points int, name string, ok bool) {
	m := reqPointsRe.FindStringSubmatch(strings.TrimSpace(req))
	if m == nil {
		return 0, "", false
	}
	points, _ = strconv.Atoi(m[1])
	return points, strings.TrimSpace(m[2]), true
}

// WFTClassTalents returns the talent tree of a class from the site's tooltip feed.
func (c *Client) WFTClassTalents(ctx context.Context, class string) (WFTClassTalents, error) {
	class = strings.ToLower(strings.TrimSpace(class))
	if !wftClasses[class] {
		return WFTClassTalents{}, fmt.Errorf("clase desconocida: %q", class)
	}
	if !c.wftEnabled() {
		return WFTClassTalents{}, errors.New("wowforevertalents.com no está en las fuentes web configuradas")
	}
	return wftParsed(ctx, c, "talents:"+class, c.wftRoot+"/tips/"+class+".json", func(page string) (WFTClassTalents, error) {
		return ParseWFTTalents([]byte(page), class)
	})
}

// ParseWFTAbilities reads a saved class page (/abilities/<class>/) for the tools' tests and the
// theory engine, which need the same parse the live client uses.
func ParseWFTAbilities(page, class string) (WFTAbilities, error) {
	return parseAbilities(page, class)
}
