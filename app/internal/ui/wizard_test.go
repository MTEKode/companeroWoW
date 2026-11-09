package ui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEnv answers the setup checks from in-memory maps. Paths are compared as written, so
// the tests use the same Windows spelling the code builds with filepath.Join on Windows.
type fakeEnv struct {
	reg     map[string]string // "key|name" -> value
	files   map[string][]byte
	dirs    map[string]bool
	env     map[string]string
	cmds    map[string]error // "name arg arg" -> error (nil = exit 0); missing = not found
	ranCmds []string
}

func (f *fakeEnv) RegString(keyPath, name string) (string, bool) {
	v, ok := f.reg[keyPath+"|"+name]
	return v, ok
}

func (f *fakeEnv) ReadFile(path string) ([]byte, error) {
	if b, ok := f.files[filepath.Clean(path)]; ok {
		return b, nil
	}
	return nil, fs.ErrNotExist
}

func (f *fakeEnv) IsDir(path string) bool { return f.dirs[filepath.Clean(path)] }

func (f *fakeEnv) Getenv(key string) string { return f.env[key] }

func (f *fakeEnv) Run(name string, args ...string) error {
	key := strings.Join(append([]string{name}, args...), " ")
	f.ranCmds = append(f.ranCmds, key)
	if err, ok := f.cmds[key]; ok {
		return err
	}
	return errors.New("no encontrado: " + name)
}

func newFakeEnv() *fakeEnv {
	return &fakeEnv{
		reg:   map[string]string{},
		files: map[string][]byte{},
		dirs:  map[string]bool{},
		env:   map[string]string{},
		cmds:  map[string]error{},
	}
}

func (f *fakeEnv) addDir(p string) { f.dirs[filepath.Clean(p)] = true }

func TestForeverFromRegistryFirst(t *testing.T) {
	f := newFakeEnv()
	root := filepath.Join(`D:\Juegos`, "World of Warcraft")
	forever := filepath.Join(root, "_forever_")
	f.addDir(forever)
	f.reg[wowRegKey+"|InstallPath"] = root + `\`
	f.addDir(filepath.Join(`F:\Games\World of Warcraft`, "_forever_"))

	item, got := checkForever(f, "")
	if !item.OK || got != forever {
		t.Fatalf("forever = %q (ok %v), want %q", got, item.OK, forever)
	}
}

func TestForeverConfiguredPathWins(t *testing.T) {
	f := newFakeEnv()
	configured := filepath.Join(`G:\WoW`, "_forever_")
	f.addDir(configured)
	f.addDir(filepath.Join(`F:\Games\World of Warcraft`, "_forever_"))
	if got := findForever(f, configured); got != configured {
		t.Errorf("findForever = %q, want the configured %q", got, configured)
	}
}

func TestForeverCommonPathsAndFGames(t *testing.T) {
	f := newFakeEnv()
	want := filepath.Join(`F:\Games\World of Warcraft`, "_forever_")
	f.addDir(want)
	if got := findForever(f, ""); got != want {
		t.Errorf("findForever = %q, want %q", got, want)
	}
}

func TestForeverNotFound(t *testing.T) {
	f := newFakeEnv()
	item, got := checkForever(f, "")
	if item.OK || got != "" {
		t.Errorf("nothing installed: ok=%v forever=%q", item.OK, got)
	}
	if !strings.Contains(item.Detail, "Buscar carpeta") {
		t.Errorf("detail should suggest the browse button: %q", item.Detail)
	}
}

func TestBattleNetInstallPath(t *testing.T) {
	cfg := `{
		"Games": {
			"wow_retail": {"InstallPath": "C:\\Wrong\\Retail"},
			"wow_forever": {"InstallPath": "F:\\Games\\World of Warcraft"}
		},
		"Other": {"InstallPath": "C:\\Nope"}
	}`
	got := battleNetWoWPaths([]byte(cfg))
	if len(got) != 2 {
		t.Fatalf("paths = %v, want the two WoW entries", got)
	}
	for _, p := range got {
		if strings.Contains(p, "Nope") {
			t.Errorf("non-WoW path picked up: %v", got)
		}
	}
	if battleNetWoWPaths([]byte("not json")) != nil {
		t.Error("invalid JSON should give no paths")
	}
}

func TestBattleNetConfigUsedForForever(t *testing.T) {
	f := newFakeEnv()
	f.env["APPDATA"] = `C:\Users\x\AppData\Roaming`
	f.files[filepath.Clean(`C:\Users\x\AppData\Roaming\Battle.net\Battle.net.config`)] =
		[]byte(`{"WoW": {"InstallPath": "H:\\Juegos\\World of Warcraft"}}`)
	want := filepath.Join(`H:\Juegos\World of Warcraft`, "_forever_")
	f.addDir(want)
	if got := findForever(f, ""); got != want {
		t.Errorf("findForever = %q, want %q", got, want)
	}
}

func TestAddonCheckOffersInstallOnlyWithSource(t *testing.T) {
	f := newFakeEnv()
	forever := filepath.Join(`F:\Games\World of Warcraft`, "_forever_")
	exe := filepath.Join(`E:\Apps`, "dist")
	f.addDir(forever)

	item := checkAddon(f, forever, exe)
	if item.OK || item.Action != "" {
		t.Errorf("no source: ok=%v action=%q, want a manual instruction", item.OK, item.Action)
	}

	src := filepath.Join(exe, "addon", addonName)
	f.addDir(src)
	f.files[filepath.Clean(filepath.Join(src, addonName+".toc"))] = []byte("## Title")
	item = checkAddon(f, forever, exe)
	if item.OK || item.Action != "install-addon" {
		t.Errorf("source present: ok=%v action=%q, want the install button", item.OK, item.Action)
	}

	f.addDir(addonDir(forever))
	if item := checkAddon(f, forever, exe); !item.OK {
		t.Errorf("addon present: ok=%v, want OK", item.OK)
	}
}

func TestAddonSourceDevLayout(t *testing.T) {
	f := newFakeEnv()
	exe := filepath.Join(`E:\Projects\companeroWoW`, "dist")
	src := filepath.Join(`E:\Projects\companeroWoW`, addonName)
	f.addDir(src)
	f.files[filepath.Clean(filepath.Join(src, addonName+".toc"))] = []byte("x")
	if got := addonSource(f, exe); got != filepath.Clean(src) {
		t.Errorf("addonSource = %q, want %q", got, src)
	}
	if got := addonSource(f, ""); got != "" {
		t.Errorf("empty exe dir gives %q", got)
	}
}

func TestCommandChecks(t *testing.T) {
	f := newFakeEnv()
	f.cmds["agy models"] = nil
	f.cmds["codex login status"] = errors.New("exit 1")
	if item := checkAgy(f, ""); !item.OK {
		t.Errorf("agy logged in: %+v", item)
	}
	if item := checkCodex(f, "codex"); item.OK {
		t.Errorf("codex not logged in reported OK: %+v", item)
	}
	if got := strings.Join(f.ranCmds, " | "); got != "agy models | codex login status" {
		t.Errorf("commands run = %q; the checks must be read-only status commands", got)
	}
}

func TestKeyCheckNeverShowsValue(t *testing.T) {
	item := checkKey(true)
	if !item.OK || strings.Contains(item.Detail, "nvapi") {
		t.Errorf("key check = %+v", item)
	}
	if checkKey(false).OK {
		t.Error("missing key reported OK")
	}
}

func TestRunSetupChecksAllItems(t *testing.T) {
	f := newFakeEnv()
	items, forever := runSetupChecks(f, setupInput{})
	if forever != "" {
		t.Errorf("forever = %q, want none", forever)
	}
	if len(items) != 5 {
		t.Fatalf("%d items, want 5", len(items))
	}
	for _, it := range items {
		if it.OK {
			t.Errorf("empty machine: %q reported OK", it.Title)
		}
	}
}

func TestInstallAddonCopiesAndRefusesOverwrite(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, addonName+".toc"), []byte("## Title: X"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "Core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Core", "main.lua"), []byte("print(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "Interface", "AddOns", addonName)
	if err := installAddon(src, dst); err != nil {
		t.Fatalf("installAddon: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "Core", "main.lua")); err != nil || string(b) != "print(1)" {
		t.Errorf("copied file = %q, %v", b, err)
	}
	if err := installAddon(src, dst); err == nil {
		t.Error("second install overwrote an existing addon")
	}
}

func TestInstallAddonNeedsTOC(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "x")
	if err := installAddon(src, dst); err == nil {
		t.Error("a folder without the .toc file was installed")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("destination created for a bad source")
	}
}
