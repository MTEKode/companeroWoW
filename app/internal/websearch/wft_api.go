package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The functions below are what the tools call. Each reads a site page through WFTPage (so the
// robots, rate and patch rules apply), parses it and keeps the result as JSON in the store, under
// the patch it was read with: a new patch parses the page again.

// wftParsed returns the parsed value for key, from the store when it was parsed under the
// current patch and is not older than the page TTL; otherwise it reads pageURL and parses it.
func wftParsed[T any](ctx context.Context, c *Client, key, pageURL string, parse func(page string) (T, error)) (T, error) {
	var zero T
	if !c.wftEnabled() {
		return zero, errors.New("wowforevertalents.com no está en las fuentes web configuradas")
	}
	patch, err := c.wftPatch(ctx)
	if err != nil {
		return zero, err
	}
	if c.st != nil {
		js, p, at, ok, err := c.st.WFTParsedGet(key)
		if err == nil && ok && p == patch && c.now().Sub(at) < wftPageTTL {
			var v T
			if json.Unmarshal([]byte(js), &v) == nil {
				return v, nil
			}
		}
	}
	page, err := c.WFTPage(ctx, pageURL)
	if err != nil {
		return zero, err
	}
	v, err := parse(page)
	if err != nil {
		return zero, err
	}
	if c.st != nil {
		if js, err := json.Marshal(v); err == nil {
			_ = c.st.WFTParsedPut(key, key, string(js), patch)
		}
	}
	return v, nil
}

// WFTClassAbilities returns the trainer spells by school and the proficiencies of a class.
func (c *Client) WFTClassAbilities(ctx context.Context, class string) (WFTAbilities, error) {
	class = strings.ToLower(strings.TrimSpace(class))
	if !wftClasses[class] {
		return WFTAbilities{}, fmt.Errorf("clase desconocida: %q", class)
	}
	return wftParsed(ctx, c, "abilities:"+class, c.wftRoot+"/abilities/"+class+"/", func(page string) (WFTAbilities, error) {
		return parseAbilities(page, class)
	})
}

// WFTBuildRef is a community build listed in the sitemap: its tree and point split, from the path.
type WFTBuildRef struct {
	URL     string `json:"url"`
	Arbol   string `json:"arbol"`   // "Discipline"
	Reparto string `json:"reparto"` // "19/18/0"
}

var buildSlugRe = regexp.MustCompile(`^(.+?)-(\d+)-(\d+)-(\d+)-([0-9a-f]+)$`)

// WFTBuilds lists the community builds of a class from the sitemap (no request for the pages).
func (c *Client) WFTBuilds(ctx context.Context, class string) ([]WFTBuildRef, error) {
	class = strings.ToLower(strings.TrimSpace(class))
	if !wftClasses[class] {
		return nil, fmt.Errorf("clase desconocida: %q", class)
	}
	entries, err := c.WFTIndex(ctx)
	if err != nil {
		return nil, err
	}
	var out []WFTBuildRef
	for _, e := range entries {
		if e.Section != "builds" || e.Class != class {
			continue
		}
		last := e.Slug[strings.LastIndex(e.Slug, "/")+1:]
		m := buildSlugRe.FindStringSubmatch(last)
		if m == nil {
			continue
		}
		out = append(out, WFTBuildRef{
			URL:     e.URL,
			Arbol:   humanSlug(m[1]),
			Reparto: m[2] + "/" + m[3] + "/" + m[4],
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Arbol != out[j].Arbol {
			return out[i].Arbol < out[j].Arbol
		}
		return out[i].Reparto > out[j].Reparto
	})
	return out, nil
}

// WFTIndex returns the sitemap entries of the site, downloading the sitemap when it is due.
func (c *Client) WFTIndex(ctx context.Context) ([]WFTURL, error) {
	if !c.wftEnabled() {
		return nil, errors.New("wowforevertalents.com no está en las fuentes web configuradas")
	}
	if err := c.wftEnsureSitemap(ctx); err != nil {
		if entries := c.wftIndexEntries(); len(entries) > 0 {
			return entries, nil
		}
		return nil, err
	}
	return c.wftIndexEntries(), nil
}

// WFTBuild reads one community build page. rawURL must be on the site.
func (c *Client) WFTBuild(ctx context.Context, rawURL string) (WFTBuild, error) {
	class := ""
	if m := regexp.MustCompile(`/([a-z]+)/builds/`).FindStringSubmatch(rawURL); m != nil {
		class = m[1]
	}
	if !wftClasses[class] {
		return WFTBuild{}, errors.New("la dirección no es una build de clase de wowforevertalents.com")
	}
	return wftParsed(ctx, c, "build:"+rawURL, rawURL, func(page string) (WFTBuild, error) {
		return parseBuild(page, class)
	})
}

// WFTDungeonRows returns the dungeon list (name, group size, level), without duplicates.
func (c *Client) WFTDungeonRows(ctx context.Context) ([]WFTDungeonRow, error) {
	rows, err := wftParsed(ctx, c, "dungeons", c.wftRoot+"/dungeons/", func(page string) ([]WFTDungeonRow, error) {
		rows := parseDungeonList(page)
		if len(rows) == 0 {
			return nil, errors.New("la lista de mazmorras no tiene filas reconocibles")
		}
		return rows, nil
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []WFTDungeonRow
	for _, r := range rows {
		k := foldText(r.Nombre)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out, nil
}

// WFTDungeonSlug returns the site slug of a dungeon named in English ("Ahn'Qiraj Temple" →
// "ahn-qiraj-temple"), when the sitemap has that page.
func (c *Client) WFTDungeonSlug(ctx context.Context, name string) (string, bool, error) {
	entries, err := c.WFTIndex(ctx)
	if err != nil {
		return "", false, err
	}
	want := slugify(name)
	for _, e := range entries {
		if e.Section == "dungeons" && e.Slug == "dungeons/"+want {
			return want, true, nil
		}
	}
	return "", false, nil
}

// WFTDungeonPage reads one dungeon page by its slug.
func (c *Client) WFTDungeonPage(ctx context.Context, slug string) (WFTDungeon, error) {
	if slug == "" || strings.ContainsAny(slug, "/?#") {
		return WFTDungeon{}, errors.New("slug de mazmorra no válido")
	}
	return wftParsed(ctx, c, "dungeon:"+slug, c.wftRoot+"/dungeons/"+slug+"/", func(page string) (WFTDungeon, error) {
		return parseDungeon(page, humanSlug(slug))
	})
}

// WFTProfessionPage reads a profession's page (its recipes and skill cap) by slug ("blacksmithing").
func (c *Client) WFTProfessionPage(ctx context.Context, slug string) (WFTProfession, error) {
	if slug == "" || strings.ContainsAny(slug, "/?#") {
		return WFTProfession{}, errors.New("nombre de profesión no válido")
	}
	return wftParsed(ctx, c, "profession:"+slug, c.wftRoot+"/professions/"+slug+"/", func(page string) (WFTProfession, error) {
		return parseProfession(page, humanSlug(slug))
	})
}

// slugify turns a name into a site slug: lower case, letters and digits, dashes between words.
func slugify(name string) string {
	s := foldText(name)
	var b strings.Builder
	dash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// ClassSlug returns the site's slug for a class named in English or Spanish ("PRIEST", "Sacerdote"
// → "priest"), or "" when it is not one of the nine classes.
func ClassSlug(name string) string {
	for _, w := range translateWords(name) {
		if wftClasses[w] {
			return w
		}
	}
	return ""
}

// TranslateWords returns the English words of a Spanish question, as the site's paths name them.
func TranslateWords(s string) []string {
	return translateWords(s)
}
