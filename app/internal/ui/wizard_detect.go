package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// wizardEnv is everything the setup checks look at. The Windows build reads the registry,
// the disk and the CLIs; the tests use a fake.
type wizardEnv interface {
	// RegString reads a string value under HKEY_LOCAL_MACHINE.
	RegString(keyPath, name string) (string, bool)
	// ReadFile reads a file.
	ReadFile(path string) ([]byte, error)
	// IsDir reports whether path is an existing folder.
	IsDir(path string) bool
	// Getenv reads an environment variable.
	Getenv(key string) string
	// Run runs a command with a short timeout and reports whether it exited with 0.
	Run(name string, args ...string) error
}

// checkItem is one line of the wizard: a ✅ or ❌ with its detail.
type checkItem struct {
	Title  string
	OK     bool
	Detail string
	// Action names a button the item offers when it is not OK: "install-addon" or "" for none.
	Action string
}

// setupInput is what the checks need from the configuration and the install.
type setupInput struct {
	WoWPath      string // configured wowPath (the _forever_ folder, usually)
	AgyCommand   string
	CodexCommand string
	KeyPresent   bool   // the NVIDIA key resolves (its value is never shown)
	ExeDir       string // folder of the running executable
}

const (
	wowRegKey     = `SOFTWARE\WOW6432Node\Blizzard Entertainment\World of Warcraft`
	addonName     = "CompaneroWoW"
	foreverFolder = "_forever_"
)

// commonWoWRoots are the folders where the WoW client is usually installed.
var commonWoWRoots = []string{
	`C:\Program Files (x86)\World of Warcraft`,
	`C:\Program Files\World of Warcraft`,
	`C:\Games\World of Warcraft`,
	`D:\Games\World of Warcraft`,
	`E:\Games\World of Warcraft`,
	`F:\Games\World of Warcraft`,
	`D:\World of Warcraft`,
	`F:\World of Warcraft`,
}

// isForeverFolder reports whether dir is the _forever_ folder of WoW (the Forever client).
func isForeverFolder(env wizardEnv, dir string) bool {
	return dir != "" && strings.EqualFold(filepath.Base(dir), foreverFolder) && env.IsDir(dir)
}

// foreverCandidates lists the places a _forever_ folder may be, in order of preference:
// the configured path, the Windows registry, the Battle.net configuration and the common
// install folders. Each root is tried both as itself and with _forever_ below it.
func foreverCandidates(env wizardEnv, configured string) []string {
	var roots []string
	if configured != "" {
		roots = append(roots, configured)
	}
	if p, ok := env.RegString(wowRegKey, "InstallPath"); ok && p != "" {
		roots = append(roots, p)
	}
	if appData := env.Getenv("APPDATA"); appData != "" {
		if data, err := env.ReadFile(filepath.Join(appData, "Battle.net", "Battle.net.config")); err == nil {
			roots = append(roots, battleNetWoWPaths(data)...)
		}
	}
	roots = append(roots, commonWoWRoots...)

	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		key := strings.ToLower(filepath.Clean(p))
		if !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	for _, r := range roots {
		if r == "" {
			continue
		}
		// Registry and Battle.net values often end with a separator; the list shows clean paths.
		r = filepath.Clean(r)
		if isForeverFolder(env, r) {
			add(r)
		}
		if f := filepath.Join(r, foreverFolder); isForeverFolder(env, f) {
			add(f)
		}
	}
	return out
}

// findForever returns the first _forever_ folder found, or "" when there is none.
func findForever(env wizardEnv, configured string) string {
	if c := foreverCandidates(env, configured); len(c) > 0 {
		return c[0]
	}
	return ""
}

// battleNetWoWPaths returns the InstallPath values under a WoW entry of Battle.net.config.
// The file's layout is not documented, so it is searched: an InstallPath key counts when
// one of the keys above it mentions WoW or World of Warcraft.
func battleNetWoWPaths(data []byte) []string {
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	var out []string
	var walk func(v any, parent string)
	walk = func(v any, parent string) {
		switch t := v.(type) {
		case map[string]any:
			for k, child := range t {
				if s, ok := child.(string); ok && strings.EqualFold(k, "installpath") && wowKey(parent) && s != "" {
					out = append(out, s)
				}
				walk(child, k)
			}
		case []any:
			for _, child := range t {
				walk(child, parent)
			}
		}
	}
	walk(doc, "")
	return out
}

func wowKey(k string) bool {
	k = strings.ToLower(k)
	return strings.Contains(k, "wow") || strings.Contains(k, "world of warcraft")
}

// checkForever reports whether the _forever_ folder was found.
func checkForever(env wizardEnv, configured string) (checkItem, string) {
	item := checkItem{Title: "Carpeta de WoW Forever"}
	dir := findForever(env, configured)
	if dir == "" {
		item.Detail = "No la encuentro. Usa «Buscar carpeta…» y elige la carpeta _forever_ dentro de World of Warcraft."
		return item, ""
	}
	item.OK = true
	item.Detail = dir
	return item, dir
}

// checkAddon reports whether the addon folder is in place; when it is not and the app
// carries a copy, the item offers to install it.
func checkAddon(env wizardEnv, forever, exeDir string) checkItem {
	item := checkItem{Title: "Addon en la carpeta AddOns"}
	if forever == "" {
		item.Detail = "Primero hay que encontrar la carpeta de WoW."
		return item
	}
	dst := addonDir(forever)
	if env.IsDir(dst) {
		item.OK = true
		item.Detail = dst
		return item
	}
	item.Detail = "Falta " + dst + "."
	if addonSource(env, exeDir) != "" {
		item.Action = "install-addon"
		item.Detail += " Puedo instalarlo desde la app."
	} else {
		item.Detail += " Copia la carpeta CompaneroWoW de la app en Interface\\AddOns."
	}
	return item
}

func addonDir(forever string) string {
	return filepath.Join(forever, "Interface", "AddOns", addonName)
}

// addonSource returns the folder with the addon that ships next to the executable, or ""
// when there is none. It looks for addon\CompaneroWoW, then for the development layout
// (dist\ next to the repository's CompaneroWoW folder).
func addonSource(env wizardEnv, exeDir string) string {
	if exeDir == "" {
		return ""
	}
	for _, c := range []string{
		filepath.Join(exeDir, "addon", addonName),
		filepath.Join(exeDir, "..", addonName),
	} {
		if env.IsDir(c) && fileExists(env, filepath.Join(c, addonName+".toc")) {
			return filepath.Clean(c)
		}
	}
	return ""
}

func fileExists(env wizardEnv, path string) bool {
	_, err := env.ReadFile(path)
	return err == nil
}

// checkAgy runs `agy models`, the read-only command that fails when the CLI is not logged in.
func checkAgy(env wizardEnv, command string) checkItem {
	item := checkItem{Title: "Antigravity (agy): sesión iniciada"}
	if command == "" {
		command = "agy"
	}
	if err := env.Run(command, "models"); err != nil {
		item.Detail = "No responde a «agy models». Inicia sesión con el CLI de Antigravity."
		return item
	}
	item.OK = true
	item.Detail = "«agy models» responde."
	return item
}

// checkCodex runs `codex login status`, read-only.
func checkCodex(env wizardEnv, command string) checkItem {
	item := checkItem{Title: "Codex: sesión iniciada"}
	if command == "" {
		command = "codex"
	}
	if err := env.Run(command, "login", "status"); err != nil {
		item.Detail = "«codex login status» no confirma la sesión. Ejecuta «codex login» en una terminal."
		return item
	}
	item.OK = true
	item.Detail = "«codex login status» confirma la sesión."
	return item
}

// checkKey reports whether the NVIDIA key is configured. The value is never read out.
func checkKey(present bool) checkItem {
	item := checkItem{Title: "Clave de NVIDIA (opcional)"}
	if present {
		item.OK = true
		item.Detail = "Configurada en .env o en el entorno."
		return item
	}
	item.Detail = "No configurada. Solo hace falta si no usas Antigravity ni Codex."
	return item
}

// runSetupChecks runs every check. The returned forever folder ("" when not found) is what
// the addon check and the install use.
func runSetupChecks(env wizardEnv, in setupInput) (items []checkItem, forever string) {
	fItem, forever := checkForever(env, in.WoWPath)
	items = append(items, fItem)
	items = append(items, checkAddon(env, forever, in.ExeDir))
	items = append(items, checkAgy(env, in.AgyCommand))
	items = append(items, checkCodex(env, in.CodexCommand))
	items = append(items, checkKey(in.KeyPresent))
	return items, forever
}

// installAddon copies the addon folder src to dst, which must not exist yet. src must
// hold the .toc file. Nothing is overwritten.
func installAddon(src, dst string) error {
	if _, err := os.Stat(filepath.Join(src, addonName+".toc")); err != nil {
		return fmt.Errorf("no encuentro %s.toc en %s", addonName, src)
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("ya existe %s; no lo sobrescribo", dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("comprobar %s: %w", dst, err)
	}
	return copyTree(src, dst)
}

// copyTree copies the regular files and folders under src into dst.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}
