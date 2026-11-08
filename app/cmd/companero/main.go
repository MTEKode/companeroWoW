// Command companero is the Compañero WoW desktop companion: it reads the live state of
// World of Warcraft: Forever, answers questions through the AI providers and shows an
// overlay window that a global hotkey toggles.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/assistant"
	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/mcpserver"
	"github.com/MTEKode/companeroWoW/app/internal/store"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
	"github.com/MTEKode/companeroWoW/app/internal/ui"
	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

const (
	version      = "0.1.0"
	appDirName   = "CompaneroWoW"
	logMaxBytes  = 5 << 20
	tipQueueSize = 8
	// svStaleAfter is the SavedVariables age after which the user is asked to sync.
	svStaleAfter = 10 * time.Minute
	shutdownWait = 6 * time.Second
)

// Hints shown in the status bar.
const (
	hintOpenWoW   = "Abre World of Warcraft en modo ventana o sin bordes para leer datos en vivo"
	hintActivate  = "Activa el addon y usa modo ventana sin bordes"
	hintWoWPath   = "Configura WoWPath en config.json"
	hintSync      = "Escribe /compa sync en el juego para actualizar los datos"
	hintNoProvide = "No hay proveedores de IA disponibles: instala Antigravity CLI (agy) o Codex CLI, o define una clave de API en .env"
)

func init() {
	// The window needs the main OS thread; keep the main goroutine on it.
	runtime.LockOSThread()
}

func main() {
	diag := flag.Bool("diag", false, "imprime el estado (WoW, SavedVariables, proveedores de IA) y sale, sin abrir la ventana")
	diagOut := flag.String("diag-out", "", "con --diag, escribe el JSON en este archivo")
	flag.Parse()
	os.Exit(run(*diag, *diagOut))
}

func run(diag bool, diagOut string) int {
	dir, err := config.DefaultDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "no pude crear", dir, ":", err)
		return 1
	}
	if closer, err := setupLog(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		defer closer.Close()
	}

	cfg, err := config.Load(filepath.Join(dir, "config.json"))
	if err != nil {
		log.Printf("config: %v", err)
		return 1
	}

	if diag {
		return runDiag(cfg, dir, diagOut)
	}

	release, first := acquireSingleInstance()
	if !first {
		ui.Activate()
		return 0
	}
	defer release()

	log.Printf("Compañero WoW %s iniciando", version)
	return runApp(cfg, dir)
}

// runApp wires the components and runs the overlay until the user quits.
func runApp(cfg *config.Config, dir string) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	st, err := store.Open(filepath.Join(dir, "companero.db"))
	if err != nil {
		log.Printf("store: %v", err)
		return 1
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Printf("close store: %v", err)
		}
	}()

	reader := bridge.NewReader(cfg.PixelPoll())
	var watcher *bridge.SVWatcher
	svPath, svErr := cfg.SavedVariablesPath()
	if svErr != nil {
		log.Printf("SavedVariables: %v", svErr)
	} else {
		watcher = bridge.NewSVWatcher(svPath, 0)
	}
	g := gamedata.NewService(reader, watcher, st)
	g.Start(ctx)

	ws := websearch.New(cfg.WebSources, st)
	reg := tools.Build(g, ws)

	mcpErrs := &errBox{}
	mcp := mcpserver.New(reg, version)
	mcpDone := make(chan struct{})
	go func() {
		defer close(mcpDone)
		if err := mcp.ListenAndServe(ctx, cfg.MCPPort); err != nil && ctx.Err() == nil {
			log.Printf("mcp: %v", err)
			mcpErrs.set(fmt.Errorf("el servidor de herramientas no pudo iniciar en 127.0.0.1:%d", cfg.MCPPort))
		}
	}()

	chain := ai.BuildChain(cfg, dir, assistant.ToolAdapter(reg))
	asst := assistant.New(chain, g, st, cfg)
	providers := availableProviders(providerReport(cfg, dir))

	tips := make(chan assistant.Tip, tipQueueSize)
	if cfg.Proactive {
		go asst.RunProactive(ctx, reader.Events(), func(t assistant.Tip) {
			select {
			case tips <- t:
			default:
				log.Printf("tip dropped (queue full): %s", t.Title)
			}
		})
	}

	uiErr := ui.Run(ui.Options{
		Hotkey:    cfg.Hotkey,
		StatePath: filepath.Join(dir, "window.json"),
		Avoid:     avoidStrip,
		Backend:   asst,
		Tips:      tips,
		Status: func() ui.Status {
			return buildStatus(g, providers, svErr, mcpErrs.get())
		},
	})

	// Graceful shutdown: stop the MCP server, wait for it briefly, then the store closes.
	cancel()
	select {
	case <-mcpDone:
	case <-time.After(shutdownWait):
		log.Printf("mcp: shutdown timed out")
	}
	if uiErr != nil {
		log.Printf("ui: %v", uiErr)
		return 1
	}
	log.Printf("Compañero WoW cerrado")
	return 0
}

// avoidStrip returns the WoW pixel strip at the top-left of the client area. The bridge
// reads it from the screen, so the overlay must never cover it. The bridge's strip
// (StripWidth x StripHeight) is larger than the minimum the overlay needs, so it is used.
func avoidStrip() (ui.Rect, bool) {
	_, rect, err := bridge.FindWoWWindow()
	if err != nil {
		return ui.Rect{}, false
	}
	return ui.Rect{X: rect.Min.X, Y: rect.Min.Y, W: bridge.StripWidth, H: bridge.StripHeight}, true
}

// buildStatus gathers what the status bar shows. It is called by the UI every two seconds.
func buildStatus(g *gamedata.Service, providers []string, svErr error, mcpErr error) ui.Status {
	s := ui.Status{
		Bridge:          "no_wow",
		SavedVarsAgeSec: -1,
		Hints:           []string{},
		Providers:       providers,
	}
	if live, ok := g.Live(); ok {
		s.Bridge = "connected"
		s.Level = int(live.Level)
	} else if _, _, err := bridge.FindWoWWindow(); err == nil {
		s.Bridge = "no_strip"
	}
	if loc, err := g.Location(); err == nil {
		if m, ok := loc.(map[string]any); ok {
			if zone, _ := m["zone"].(string); zone != "" {
				s.Zone = zone
			}
		}
	}

	ageKnown := false
	if st, err := g.Status(); err == nil {
		if m, ok := st.(map[string]any); ok {
			if age, ok := m["savedVariablesAgeSec"].(int64); ok {
				s.SavedVarsAgeSec = age
				ageKnown = true
			}
		}
	}

	switch s.Bridge {
	case "no_wow":
		s.Hints = append(s.Hints, hintOpenWoW)
	case "no_strip":
		s.Hints = append(s.Hints, hintActivate)
	}
	if svErr != nil {
		s.Hints = append(s.Hints, hintWoWPath)
	} else if !ageKnown || time.Duration(s.SavedVarsAgeSec)*time.Second > svStaleAfter {
		s.Hints = append(s.Hints, hintSync)
	}
	if len(providers) == 0 {
		s.Hints = append(s.Hints, hintNoProvide)
	}
	if mcpErr != nil {
		s.Notice = mcpErr.Error()
	}
	return s
}

// providerInfo is the availability of one provider, for the diagnosis and the status bar.
type providerInfo struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// providerReport checks every provider of cfg.ProviderOrder without calling it.
func providerReport(cfg *config.Config, workDir string) []providerInfo {
	timeout := time.Duration(cfg.AITimeoutSec) * time.Second
	if cfg.AITimeoutSec <= 0 {
		timeout = 120 * time.Second
	}
	var out []providerInfo
	for _, name := range cfg.ProviderOrder {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "antigravity":
			out = append(out, checkProvider(ai.NewAntigravity(cfg, workDir)))
		case "codex":
			out = append(out, checkProvider(ai.NewCodex(cfg, workDir)))
		case "api":
			for _, p := range cfg.APIProviders {
				out = append(out, checkProvider(ai.NewOpenAICompat(p, nil, timeout)))
			}
		}
	}
	return out
}

func checkProvider(p ai.Provider) providerInfo {
	info := providerInfo{Name: p.Name(), Available: true}
	if err := p.Available(); err != nil {
		info.Available = false
		info.Reason = err.Error()
	}
	return info
}

func availableProviders(list []providerInfo) []string {
	names := []string{}
	for _, p := range list {
		if p.Available {
			names = append(names, p.Name)
		}
	}
	return names
}

// diagReport is the JSON written by --diag.
type diagReport struct {
	Version             string         `json:"version"`
	ConfigPath          string         `json:"configPath"`
	DataDir             string         `json:"dataDir"`
	LogPath             string         `json:"logPath"`
	WoWPath             string         `json:"wowPath"`
	WoWDetected         bool           `json:"wowDetected"`
	WoWDetectError      string         `json:"wowDetectError,omitempty"`
	SavedVariablesPath  string         `json:"savedVariablesPath,omitempty"`
	SavedVariablesFound bool           `json:"savedVariablesFound"`
	SavedVariablesAge   int64          `json:"savedVariablesAgeSec"`
	SavedVariablesError string         `json:"savedVariablesError,omitempty"`
	ProviderOrder       []string       `json:"providerOrder"`
	Providers           []providerInfo `json:"providers"`
	AvailableProviders  []string       `json:"availableProviders"`
	MCPPort             int            `json:"mcpPort"`
	Hotkey              string         `json:"hotkey"`
	Proactive           bool           `json:"proactive"`
}

// runDiag prints the diagnosis as JSON. The output goes to the parent console when the
// process was started from one, to diagOut when given, and to diag.json otherwise.
func runDiag(cfg *config.Config, dir, diagOut string) int {
	rep := diagReport{
		Version:           version,
		ConfigPath:        filepath.Join(dir, "config.json"),
		DataDir:           dir,
		LogPath:           filepath.Join(dir, "companero.log"),
		WoWPath:           cfg.WoWPath,
		ProviderOrder:     cfg.ProviderOrder,
		MCPPort:           cfg.MCPPort,
		Hotkey:            cfg.Hotkey,
		Proactive:         cfg.Proactive,
		SavedVariablesAge: -1,
	}
	if _, _, err := bridge.FindWoWWindow(); err == nil {
		rep.WoWDetected = true
	} else {
		rep.WoWDetectError = err.Error()
	}
	if p, err := cfg.SavedVariablesPath(); err != nil {
		rep.SavedVariablesError = err.Error()
	} else {
		rep.SavedVariablesPath = p
		if fi, err := os.Stat(p); err == nil {
			rep.SavedVariablesFound = true
			rep.SavedVariablesAge = int64(time.Since(fi.ModTime()).Seconds())
		} else {
			rep.SavedVariablesError = err.Error()
		}
	}
	rep.Providers = providerReport(cfg, dir)
	rep.AvailableProviders = availableProviders(rep.Providers)
	if rep.AvailableProviders == nil {
		rep.AvailableProviders = []string{}
	}
	if rep.Providers == nil {
		rep.Providers = []providerInfo{}
	}

	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		log.Printf("diag: %v", err)
		return 1
	}
	data = append(data, '\n')

	attached := attachConsole()
	if attached {
		if _, err := os.Stdout.Write(data); err != nil {
			log.Printf("diag: write stdout: %v", err)
		}
	}
	target := diagOut
	if target == "" && !attached {
		target = filepath.Join(dir, "diag.json")
	}
	if target != "" {
		if err := os.WriteFile(target, data, 0o644); err != nil {
			log.Printf("diag: write %s: %v", target, err)
			return 1
		}
	}
	return 0
}

// setupLog sends the log to companero.log in dir. The file is rotated when it reaches
// 5 MB; one previous file is kept as companero.log.1.
func setupLog(dir string) (io.Closer, error) {
	path := filepath.Join(dir, "companero.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() >= logMaxBytes {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, fmt.Errorf("rotate log: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log %s: %w", path, err)
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags)
	return f, nil
}

// errBox holds the last error of a background component, safe for concurrent use.
type errBox struct {
	mu  sync.Mutex
	err error
}

func (b *errBox) set(err error) {
	b.mu.Lock()
	b.err = err
	b.mu.Unlock()
}

func (b *errBox) get() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}
