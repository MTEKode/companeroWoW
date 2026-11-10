package websearch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// wftFixture reads a saved page from testdata/wft.
func wftFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wft", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return string(b)
}

// wftSite is a fake wowforevertalents.com: it serves pages, counts requests per path and can
// change the front page's patch date between calls.
type wftSite struct {
	t          *testing.T
	srv        *httptest.Server
	mu         sync.Mutex
	hits       map[string]int
	header     map[string]string // request headers seen on the last request to a path
	patch      string
	pages      map[string]string
	etag       string // when set, pages send it and answer 304 to a matching If-None-Match
	robotsFail bool   // when set, /robots.txt answers 500
}

func newWFTSite(t *testing.T) *wftSite {
	s := &wftSite{t: t, hits: map[string]int{}, header: map[string]string{}, patch: "9 Nov",
		pages: map[string]string{
			"/robots.txt":        "User-agent: *\nAllow: /\nDisallow: /api/\nDisallow: /private/\n",
			"/sitemap-0.xml":     `<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://wowforevertalents.com/priest/talents/mind-blast/</loc></url><url><loc>https://wowforevertalents.com/priest/builds/discipline-19-18-0-6a17f2/</loc></url></urlset>`,
			"/abilities/priest/": wftFixture(t, "abilities-priest.html"),
			"/private/page/":     "secret",
			"/api/x":             "{}",
		}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.hits[r.URL.Path]++
		s.header[r.URL.Path] = r.Header.Get("If-None-Match")
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body><header><a class="pn-chip" href="/patch-notes/"><span>Patch notes</span> <span class="pn-chip-day">%s</span></a></header><main>Front</main></body></html>`, s.patch)
			return
		}
		if r.URL.Path == "/robots.txt" && s.robotsFail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		body, ok := s.pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if s.etag != "" {
			w.Header().Set("ETag", s.etag)
			if r.Header.Get("If-None-Match") == s.etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		if strings.HasSuffix(r.URL.Path, ".xml") {
			w.Header().Set("Content-Type", "application/xml")
		} else {
			w.Header().Set("Content-Type", "text/html")
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(s.srv.Close)
	// The sitemap lists the fake site's own URLs (the index keeps only the configured host).
	s.pages["/sitemap-0.xml"] = fmt.Sprintf(`<?xml version="1.0"?><urlset><url><loc>%[1]s/priest/talents/mind-blast/</loc></url><url><loc>%[1]s/priest/builds/discipline-19-18-0-6a17f2/</loc></url></urlset>`, s.srv.URL)
	return s
}

func (s *wftSite) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

// newWFTClient points a client at the fake site, with the site on the allow-list.
func newWFTClient(t *testing.T, s *wftSite, st bool) *Client {
	t.Helper()
	host := strings.Split(strings.TrimPrefix(s.srv.URL, "http://"), ":")[0]
	var c *Client
	if st {
		c = New([]string{host}, openStore(t))
	} else {
		c = New([]string{host}, nil)
	}
	c.wftRoot = s.srv.URL
	c.minInterval = 0
	return c
}

// ---- robots and the API ------------------------------------------------------------

func TestParseRobotsPrefersOwnGroupAndRefusesPrefixes(t *testing.T) {
	r := parseRobots("User-agent: *\nDisallow: /api/\n\nUser-agent: CompaneroWoW\nAllow: /\nDisallow: /private/\n")
	if r.allowed("/private/x") {
		t.Error("our own group disallows /private/")
	}
	if !r.allowed("/priest/") {
		t.Error("/priest/ should be allowed")
	}
	// Without a group for us, the * group applies.
	g := parseRobots("User-agent: *\nDisallow: /api/\n")
	if g.allowed("/api/x") || !g.allowed("/abilities/priest/") {
		t.Error("* group: /api/ refused, others allowed")
	}
	// The fixture: everything allowed except /api/.
	fix := parseRobots(wftFixture(t, "robots.txt"))
	if fix.allowed("/api/") || !fix.allowed("/") || !fix.allowed("/dungeons/") {
		t.Error("fixture robots.txt: only /api/ is refused")
	}
	var none *robotsRules
	if !none.allowed("/x") {
		t.Error("nil rules allow everything")
	}
}

func TestWFTRefusesAPIAndDisallowedPathsWithoutRequesting(t *testing.T) {
	s := newWFTSite(t)
	c := newWFTClient(t, s, false)
	ctx := context.Background()
	for _, p := range []string{"/api/x", "/private/page/"} {
		if _, err := c.WFTPage(ctx, s.srv.URL+p); err == nil {
			t.Errorf("%s: want a refusal", p)
		}
	}
	if s.count("/api/x") != 0 || s.count("/private/page/") != 0 {
		t.Errorf("refused paths were requested: api=%d private=%d", s.count("/api/x"), s.count("/private/page/"))
	}
}

// ---- patch, cache and validators -------------------------------------------------

func TestWFTPageCacheIsInvalidatedByPatchDate(t *testing.T) {
	s := newWFTSite(t)
	c := newWFTClient(t, s, true)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	c.wftNow = func() time.Time { return now }
	ctx := context.Background()
	url := s.srv.URL + "/abilities/priest/"

	if _, err := c.WFTPage(ctx, url); err != nil {
		t.Fatal(err)
	}
	// Same patch, a day later (front page checked again): the page is still fresh, no request.
	now = now.Add(7 * time.Hour)
	if _, err := c.WFTPage(ctx, url); err != nil {
		t.Fatal(err)
	}
	if n := s.count("/abilities/priest/"); n != 1 {
		t.Fatalf("same patch: page requested %d times, want 1", n)
	}
	// A new patch on the front page invalidates the cached page.
	s.mu.Lock()
	s.patch = "16 Nov"
	s.mu.Unlock()
	now = now.Add(7 * time.Hour)
	if _, err := c.WFTPage(ctx, url); err != nil {
		t.Fatal(err)
	}
	if n := s.count("/abilities/priest/"); n != 2 {
		t.Fatalf("new patch: page requested %d times, want 2", n)
	}
}

func TestWFTPageRevalidatesStaleCopyWithETag(t *testing.T) {
	s := newWFTSite(t)
	s.etag = `"v1"`
	c := newWFTClient(t, s, true)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	c.wftNow = func() time.Time { return now }
	ctx := context.Background()
	url := s.srv.URL + "/abilities/priest/"
	first, err := c.WFTPage(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	// Three days and a bit later the copy is stale: the request carries If-None-Match and a 304
	// keeps the stored body.
	now = now.Add(wftPageTTL + time.Hour)
	again, err := c.WFTPage(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Fatal("a 304 must keep the stored body")
	}
	s.mu.Lock()
	sent := s.header["/abilities/priest/"]
	s.mu.Unlock()
	if sent != `"v1"` {
		t.Errorf("If-None-Match = %q, want the stored ETag", sent)
	}
}

func TestWFTRateLimitSpacesRequestsToTheSite(t *testing.T) {
	s := newWFTSite(t)
	c := newWFTClient(t, s, false)
	c.minInterval = 120 * time.Millisecond
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, _, err := c.get(ctx, s.srv.URL+"/robots.txt"); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < 2*c.minInterval {
		t.Errorf("three requests took %v, want at least %v", d, 2*c.minInterval)
	}
}

// ---- sitemap and the local index ---------------------------------------------------

func TestSitemapParseDescribesPathsAndKeepsTheHost(t *testing.T) {
	body := []byte(`<urlset><url><loc>https://wowforevertalents.com/priest/talents/mind-blast/</loc></url>
	<url><loc>https://evil.example/priest/</loc></url><url><loc>https://wowforevertalents.com/dungeons/ahn-qiraj-temple/</loc></url></urlset>`)
	got, err := parseSitemap(body, "wowforevertalents.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (the other host is dropped): %+v", len(got), got)
	}
	if got[0].Class != "priest" || got[0].Section != "talents" || got[0].Title != "Priest · Talents · Mind Blast" {
		t.Errorf("talent entry = %+v", got[0])
	}
	if got[1].Section != "dungeons" || got[1].Class != "" {
		t.Errorf("dungeon entry = %+v", got[1])
	}
	if _, err := parseSitemap([]byte("<html>"), "wowforevertalents.com"); err == nil {
		t.Error("broken XML must be an error")
	}
}

func TestTranslateWordsMapsSpanishToEnglishSlugWords(t *testing.T) {
	got := translateWords("talentos de Sacerdote Sastrería primeros auxilios")
	want := []string{"talents", "priest", "tailoring", "first", "aid"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("translateWords = %v, want %v", got, want)
	}
}

func TestLocalIndexAnswersBeforeDuckDuckGo(t *testing.T) {
	s := newWFTSite(t)
	c := newWFTClient(t, s, true)
	ctx := context.Background()
	hits := c.wftLocal(ctx, "talento Mind Blast sacerdote")
	if len(hits) != 1 || hits[0].URL != s.srv.URL+"/priest/talents/mind-blast/" {
		t.Fatalf("local hits = %+v, want the mind-blast talent page", hits)
	}
	if hits[0].Source != "Índice de WoW Forever" {
		t.Errorf("source = %q", hits[0].Source)
	}
	// The sitemap is downloaded once and then kept for a day.
	c.wftLocal(ctx, "priest talents")
	if n := s.count("/sitemap-0.xml"); n != 1 {
		t.Errorf("sitemap requested %d times, want 1", n)
	}
}

// ---- section-aware reading ------------------------------------------------------------

func TestSelectSectionsKeepsShortTextWhole(t *testing.T) {
	if got := selectSections("# A\nuno", nil, 1000); got != "# A\nuno" {
		t.Errorf("short text changed: %q", got)
	}
}

func TestSelectSectionsReturnsTheSectionThatMatches(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Intro\n" + strings.Repeat("relleno de introducción. ", 60) + "\n")
	b.WriteString("## Hunter\n" + strings.Repeat("nada que ver. ", 60) + "\n")
	b.WriteString("## Blacksmithing\nSteel Plate Helm 210 y Dark Iron Helm 300.\n")
	got := selectSections(b.String(), []string{"blacksmithing"}, 400)
	if !strings.Contains(got, "Steel Plate Helm 210") {
		t.Fatalf("the matching section is missing:\n%s", got)
	}
	if !strings.Contains(got, "Índice de secciones") || len([]rune(got)) > 400 {
		t.Errorf("outline or limit missing (len %d):\n%s", len([]rune(got)), got)
	}
	if strings.Contains(got, "nada que ver") {
		t.Error("a section without the terms should not be chosen first")
	}
}

func TestFetchQueryUsesTheSectionsAndKeepsTheFooter(t *testing.T) {
	var b strings.Builder
	b.WriteString("<html><body><main><h1>X</h1>")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "<h2>Sección %d</h2><p>%s</p>", i, strings.Repeat("texto de relleno ", 40))
	}
	b.WriteString("<h2>Sastrería</h2><p>Recetas de Sastrería: Bolsa de tela 1.</p></main></body></html>")
	page := b.String()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}))
	defer srv.Close()
	host := strings.Split(strings.TrimPrefix(srv.URL, "http://"), ":")[0]
	c := New([]string{host}, openStore(t))
	c.minInterval = 0
	got, err := c.FetchQuery(context.Background(), srv.URL+"/p", "bolsa de tela")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Bolsa de tela") || !strings.HasSuffix(got, "[Fuente: "+srv.URL+"/p]") {
		t.Errorf("the section with the term or the footer is missing:\n%s", got)
	}
	if n := len([]rune(got)); n > maxPageRunes {
		t.Errorf("length %d exceeds %d runes", n, maxPageRunes)
	}
}

// ---- parsers on the saved pages -----------------------------------------------------

func TestParseAbilitiesFixture(t *testing.T) {
	ab, err := parseAbilities(wftFixture(t, "abilities-priest.html"), "priest")
	if err != nil {
		t.Fatal(err)
	}
	if ab.Fuente != "Forever client build 1.60.1.70862" {
		t.Errorf("fuente = %q", ab.Fuente)
	}
	var holy *WFTSchool
	total := 0 // active spells: the one marked removed in Forever is not counted
	for i := range ab.Escuelas {
		for _, sp := range ab.Escuelas[i].Hechizos {
			if sp.Estado != "retirado" {
				total++
			}
		}
		if ab.Escuelas[i].Nombre == "Holy" {
			holy = &ab.Escuelas[i]
		}
	}
	if holy == nil || len(ab.Escuelas) != 3 {
		t.Fatalf("schools = %d, want Holy among 3", len(ab.Escuelas))
	}
	if total != 49 {
		t.Errorf("spells = %d, want the 49 the page announces", total)
	}
	var lesser *WFTSpell
	for i := range holy.Hechizos {
		if holy.Hechizos[i].Nombre == "Lesser Heal" {
			lesser = &holy.Hechizos[i]
		}
	}
	if lesser == nil || len(lesser.Rangos) < 3 {
		t.Fatalf("Lesser Heal = %+v", lesser)
	}
	if r := lesser.Rangos[2]; r.Rango != "Rank 3" || r.Nivel != 10 || r.Coste != "75 Mana · 40 yd range · 2.5 sec cast" {
		t.Errorf("Lesser Heal rank 3 = %+v", r)
	}
	if lesser.Estado != "cambiado" || lesser.Rangos[2].Classic == "" {
		t.Errorf("status or classic wording missing: %+v", lesser)
	}
	var cloth, leather *WFTProficiency
	for i := range ab.Armaduras {
		switch ab.Armaduras[i].Nombre {
		case "Cloth":
			cloth = &ab.Armaduras[i]
		case "Leather":
			leather = &ab.Armaduras[i]
		}
	}
	if cloth == nil || !cloth.Disponible || cloth.SpellID != 9078 || cloth.Detalle != "From the start" {
		t.Errorf("Cloth = %+v", cloth)
	}
	if leather == nil || leather.Disponible {
		t.Errorf("Leather should be listed as not available: %+v", leather)
	}
	if !strings.Contains(ab.DualWield, "cannot dual wield") {
		t.Errorf("dual wield = %q", ab.DualWield)
	}
	if _, err := parseAbilities("<html><body><p>nada</p></body></html>", "priest"); err == nil {
		t.Error("a page without spells or proficiencies must be an error")
	}
}

func TestParseBuildFixture(t *testing.T) {
	b, err := parseBuild(wftFixture(t, "build-priest-discipline-19-18-0.html"), "priest")
	if err != nil {
		t.Fatal(err)
	}
	if b.Reparto != "21/18/0" || b.Puntos != 39 {
		t.Errorf("split = %q, points = %d", b.Reparto, b.Puntos)
	}
	if len(b.Arboles) != 2 || b.Arboles[0].Arbol != "Discipline" || b.Arboles[0].Puntos != 21 {
		t.Fatalf("trees = %+v", b.Arboles)
	}
	st := b.Arboles[0].Talentos[0]
	if st.Nombre != "Silent Resolve" || st.Rango != 3 || st.Maximo != 3 || st.PuntosReq != 5 || st.Descripcion == "" {
		t.Errorf("first talent = %+v", st)
	}
	if len(b.Otras) != 6 {
		t.Errorf("other builds = %d, want 6", len(b.Otras))
	}
	if !strings.Contains(b.Nota, "comunidad") {
		t.Errorf("the note must say the build is a community pick: %q", b.Nota)
	}
}

func TestParseDungeonListAndPageFixtures(t *testing.T) {
	rows := parseDungeonList(wftFixture(t, "dungeons.html"))
	byName := map[string]WFTDungeonRow{}
	for _, r := range rows {
		byName[r.Nombre] = r
	}
	if r := byName["Ragefire Chasm"]; r.Jugadores != 5 || r.NivelMax != 13 || r.Novedad != "cambiado" {
		t.Errorf("Ragefire Chasm = %+v", r)
	}
	if r := byName["Alterac Valley"]; r.Jugadores != 40 || r.NivelMin != 51 || r.NivelMax != 60 {
		t.Errorf("Alterac Valley = %+v", r)
	}
	if r := byName["Onyxia's Lair"]; r.Estado != "confirmada" {
		t.Errorf("Onyxia's Lair = %+v", r)
	}
	if r := byName["Ahn'Qiraj Temple"]; !r.Aproximado || r.Jugadores != 40 {
		t.Errorf("Ahn'Qiraj Temple = %+v", r)
	}
	if len(rows) < 40 {
		t.Errorf("rows = %d, want the list of about 49", len(rows))
	}

	d, err := parseDungeon(wftFixture(t, "dungeon-ahn-qiraj-temple.html"), "Ahn'Qiraj Temple")
	if err != nil {
		t.Fatal(err)
	}
	if d.Jugadores != "40 players" || d.Bloqueo != "weekly lockout" || len(d.Jefes) != 9 {
		t.Errorf("dungeon = %+v", d)
	}
	if d.Jefes[0] != "The Prophet Skeram" || d.Jefes[8] != "C'thun" {
		t.Errorf("boss order = %v", d.Jefes)
	}
	if len(d.Zonas) == 0 || !strings.Contains(d.Fuente, "Forever client build") {
		t.Errorf("zones %v, source %q", d.Zonas, d.Fuente)
	}
}

func TestParseProfessionFixture(t *testing.T) {
	p, err := parseProfession(wftFixture(t, "profession-blacksmithing.html"), "Blacksmithing")
	if err != nil {
		t.Fatal(err)
	}
	if p.Maximo != 300 || len(p.Recetas) < 100 {
		t.Fatalf("max %d, recipes %d", p.Maximo, len(p.Recetas))
	}
	var helm *WFTRecipe
	for i := range p.Recetas {
		if p.Recetas[i].Nombre == "Steel Plate Helm" {
			helm = &p.Recetas[i]
		}
	}
	if helm == nil || helm.Nivel != 210 {
		t.Errorf("Steel Plate Helm = %+v", helm)
	}
}

func TestParseTalentFixtureHasNoBrokenMarkup(t *testing.T) {
	txt := htmlToText(wftFixture(t, "talent-priest-blackout.html"))
	if strings.TrimSpace(txt) == "" || strings.Contains(txt, "<") {
		t.Errorf("talent text = %q", txt)
	}
}

func TestExtractPatchFromFrontPageFixture(t *testing.T) {
	if got := extractPatch(wftFixture(t, "front-page.html")); got != "9 Nov" {
		t.Errorf("patch = %q, want 9 Nov", got)
	}
	if got := extractPatch("<html><body>Nada</body></html>"); got != "" {
		t.Errorf("no chip, patch = %q", got)
	}
}

func TestSlugifyAndSitePathHelpers(t *testing.T) {
	if got := slugify("Ahn'Qiraj Temple"); got != "ahn-qiraj-temple" {
		t.Errorf("slugify = %q", got)
	}
	if got := slugify("Alcaz Prison"); got != "alcaz-prison" {
		t.Errorf("slugify = %q", got)
	}
}

func TestFailedRobotsDownloadRefusesThenRetriesAfterAnHour(t *testing.T) {
	s := newWFTSite(t)
	s.robotsFail = true
	c := newWFTClient(t, s, true)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	c.wftNow = func() time.Time { return now }
	ctx := context.Background()
	url := s.srv.URL + "/abilities/priest/"

	if _, err := c.WFTPage(ctx, url); err == nil {
		t.Fatal("no robots.txt and no copy: the site must be refused")
	}
	// Within the hour: refused without asking again, and never treated as "allow all".
	now = now.Add(30 * time.Minute)
	if _, err := c.WFTPage(ctx, url); err == nil {
		t.Fatal("within the retry wait the site must still be refused")
	}
	if n := s.count("/robots.txt"); n != 1 {
		t.Fatalf("robots requested %d times during the wait, want 1", n)
	}
	// After the hour the download works again.
	s.robotsFail = false
	now = now.Add(time.Hour)
	if _, err := c.WFTPage(ctx, url); err != nil {
		t.Fatalf("after the retry wait the site should work: %v", err)
	}
}
