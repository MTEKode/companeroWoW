// Command companero is the Compañero WoW desktop companion: it reads the live state of
// World of Warcraft: Forever, answers questions through the AI providers and shows an
// overlay window that a global hotkey toggles.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/assistant"
	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/diag"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/gamemode"
	"github.com/MTEKode/companeroWoW/app/internal/mcpserver"
	"github.com/MTEKode/companeroWoW/app/internal/ondemand"
	"github.com/MTEKode/companeroWoW/app/internal/session"
	"github.com/MTEKode/companeroWoW/app/internal/skill"
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
	hintActivate  = "Pulsa Ctrl+Shift+Espacio dentro de WoW para enviar tus datos (modo sin bordes)"
	hintWoWPath   = "Configura WoWPath en config.json"
	hintSync      = "Escribe /compa sync en el juego para actualizar los datos"
	hintSyncFirst = "Haz /compa sync en el juego para crear los datos"
	hintNoProvide = "No hay proveedores de IA disponibles: instala Antigravity CLI (agy) o Codex CLI, o define una clave de API en .env"
)

func init() {
	// The window needs the main OS thread; keep the main goroutine on it.
	runtime.LockOSThread()
}

// options are the command-line switches.
type options struct {
	ingestSV     bool
	trace        bool
	diag         bool
	diagOut      string
	eval         bool
	evalOut      string
	evalProvider string
	setup        bool
	tray         bool
	testTip      bool
	testAnswer   bool
	exportFB     string
	profileMem   string
}

func main() {
	var o options
	flag.BoolVar(&o.ingestSV, "ingest-sv", false, "guarda en la base de la app lo que hay en CompaneroWoW.lua (SavedVariables) y sale; hacerlo antes de cargar el addon ligero")
	flag.BoolVar(&o.trace, "trace", false, "escribe en companero.log el detalle de cada captura, trama y mensaje del juego (diagnóstico; también trace en config.json)")
	flag.BoolVar(&o.diag, "diag", false, "imprime el estado (WoW, SavedVariables, proveedores de IA) y sale, sin abrir la ventana")
	flag.StringVar(&o.diagOut, "diag-out", "", "con --diag, escribe el JSON en este archivo")
	flag.BoolVar(&o.eval, "eval", false, "responde las preguntas de evaluación con la cadena real, escribe el informe y sale")
	flag.StringVar(&o.evalOut, "eval-out", "", "con --eval, ruta del informe .md (el .json va al lado); por defecto en la carpeta de datos de la app")
	flag.StringVar(&o.evalProvider, "eval-provider", "", "con --eval, usa solo este proveedor: antigravity, codex o api")
	flag.BoolVar(&o.setup, "setup", false, "abre el asistente de configuración en el panel")
	flag.BoolVar(&o.tray, "tray", false, "arranca minimizado en la bandeja, sin mostrar el panel (lo usa el inicio con WoW)")
	flag.BoolVar(&o.testTip, "test-tip", false, "con la ventana, envía un consejo de prueba por el mismo camino que los proactivos (globo de la bandeja o panel, según ajustes)")
	flag.BoolVar(&o.testAnswer, "test-answer", false, "pone una respuesta de ejemplo en el portapapeles, suena el aviso y sale (prueba del modo juego)")
	flag.StringVar(&o.exportFB, "export-feedback", "", "escribe las valoraciones 👍/👎 en este archivo .json y sale")
	flag.StringVar(&o.profileMem, "profile-mem", "", "escribe un perfil de memoria (heap, pprof) en este archivo tras 30 s de arranque, y un resumen en <archivo>.txt")
	flag.Parse()
	os.Exit(run(o))
}

func run(o options) int {
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

	cfg, created, err := config.LoadInfo(filepath.Join(dir, "config.json"))
	if err != nil {
		log.Printf("config: %v", err)
		return 1
	}
	if o.trace || cfg.Trace {
		diag.SetTrace(true)
		log.Printf("diagnóstico: trazas detalladas activas (--trace o trace en config.json)")
	}

	if o.ingestSV {
		return runIngestSV(cfg, dir)
	}
	if o.diag {
		return runDiag(cfg, dir, o.diagOut)
	}
	if o.eval {
		return runEval(cfg, dir, o.evalOut, o.evalProvider)
	}
	if o.exportFB != "" {
		return runExportFeedback(dir, o.exportFB)
	}
	if o.testAnswer {
		return runTestAnswer(cfg, dir)
	}

	if o.testTip {
		// Like --diag: the test's report goes to the console that started the app, if any.
		attachConsole()
	}
	release, first := acquireSingleInstance()
	if !first {
		// A copy that starts with WoW must not pop up the panel of the running one.
		if !o.tray {
			ui.Activate()
		}
		return 0
	}
	defer release()

	log.Printf("Compañero WoW %s iniciando", version)
	return runApp(cfg, dir, appStart{setup: created || o.setup, tray: o.tray, testTip: o.testTip, profileMem: o.profileMem})
}

// appStart is how the app was started: the first-run wizard opens when setup is set, and
// tray keeps the panel hidden.
type appStart struct {
	setup      bool
	tray       bool
	testTip    bool
	profileMem string // --profile-mem: where the heap profile is written, "" for none
}

// runApp wires the components and runs the overlay until the user quits.
func runApp(cfg *config.Config, dir string, start appStart) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	debug.SetGCPercent(gcPercent)
	if start.profileMem != "" {
		startMemProfile(ctx, start.profileMem)
	}

	// The companion's skill is written where each provider reads it, and again before each
	// question if a file goes missing or changes.
	if removed, err := config.CleanupLeftovers(dir); err != nil {
		log.Printf("limpieza: %v", err)
	} else if len(removed) > 0 {
		log.Printf("limpieza: eliminados %s", strings.Join(removed, ", "))
	}
	if err := skill.DeployAll(dir); err != nil {
		log.Printf("skill: %v", err)
	}

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
	// Trims the chat history and the cache at start and then hourly. It is stopped and
	// waited for before the store closes (defers run in reverse order).
	maintDone := make(chan struct{})
	go func() {
		defer close(maintDone)
		st.RunMaintenance(ctx, time.Hour)
	}()
	defer func() {
		cancel()
		<-maintDone
	}()

	// The mode decides when the game is read. On demand (the default) the reader is idle until the
	// panel opens or a question is asked, and the SavedVariables file is checked when the app
	// becomes active; the session then comes from the addon (see internal/ondemand).
	mode := newModeGate(cfg)
	onDemand := cfg.OnDemand()
	reader := bridge.NewReader(cfg.PixelPoll())
	if onDemand {
		reader.SetActive(false)
	}
	// The watcher looks the file up until it exists, so WoW set up or /compa sync run
	// after the app started are picked up without a restart. On demand it is paused: it
	// reads the file only when asked (see gamedata.Service.SetOnDemand).
	watcher := bridge.NewSVWatcherFunc(cfg.SavedVariablesPath, 0)
	g := gamedata.NewService(reader, watcher, st)
	g.SetOnDemand(onDemand)
	// The channel messages the store keeps go to it from a worker, started before anything reads.
	g.StartIngest(ctx)
	reader.Channel().OnMessage(g.QueueChannel)
	cachePath := filepath.Join(dir, ondemand.CacheFile)
	if onDemand {
		// The last known data is loaded, so that an answer after a restart has it (with its age).
		if n, err := ondemand.LoadCache(cachePath, reader.Channel().Data); err != nil {
			log.Printf("caché de datos: %v", err)
		} else if n > 0 {
			log.Printf("caché de datos: %d mensajes del último uso", n)
			g.QueueLiveData(reader.Channel().Data)
		}
	}
	g.Start(ctx)
	// A session left open by an earlier run (a crash) ends at its last data. The tracker
	// then follows every read of the bridge and is saved in the store; it ends after ten
	// minutes without data, and Close ends it when the app exits. In the on-demand mode the
	// tracker follows nothing: the addon keeps the session.
	if n, err := st.CloseOpenSessions(); err != nil {
		log.Printf("sesiones: %v", err)
	} else if n > 0 {
		log.Printf("sesiones: cerradas %d que quedaron abiertas", n)
	}
	sessions := session.NewTracker(time.Now, st)
	reader.Subscribe(func(ls bridge.LiveState) {
		if mode.isContinuous() {
			sessions.Observe(ls)
		}
	})
	g.SetSessions(sessions)
	var sessionsRun sync.Once
	startSessions := func() {
		sessionsRun.Do(func() { go sessions.Run(ctx) })
	}
	if !onDemand {
		startSessions()
	}
	// The leveling pace (ritmo_subida, valor_misiones) keeps its own live samples.
	pace := gamedata.NewPaceSampler(time.Now)
	reader.Subscribe(pace.Observe)
	g.SetPace(pace)
	// Idle state for the memory release (see idle.go): it follows the reader active state set below.
	idle := newIdleTracker(time.Now)

	// The controller turns the panel and the questions into the reader's active state. Its
	// transitions switch the reader, check the SavedVariables file when the app becomes active,
	// and keep the last data on disk when it goes inactive.
	ctl := ondemand.NewController(time.Now, onDemandAfter, func(on bool) {
		continuous := mode.isContinuous()
		reader.SetActive(continuous || on)
		// A hold watches for the addon's share: the strip is polled at the watch rate (see bridge).
		reader.SetWatch(!continuous && on)
		idle.SetActive(continuous || on)
		if continuous {
			return
		}
		if on {
			go refreshSavedVariables(g)
		} else {
			logReadingEnd(reader)
		}
		saveOnDemandState(dir, reader, mode.modeName(), on, time.Time{})
	})
	ctl.SetLogf(log.Printf)
	// A share seen by the reader keeps the hold open: the question the player types after it is read.
	reader.Subscribe(func(ls bridge.LiveState) {
		if ls.ShareRemainingSec > 0 && !mode.isContinuous() {
			ctl.Touch()
		}
	})
	// runtime.json follows the reads each second while the reader is on.
	go runRuntimeTicker(ctx, func() {
		if ctl.Active() {
			writeRuntime(dir, reader, mode.modeName(), true, ctl.HoldUntil())
		}
	})
	// The state file says what runs now: active for the continuous mode, idle at rest otherwise.
	saveOnDemandState(dir, reader, mode.modeName(), !onDemand, time.Time{})

	ws := websearch.New(cfg.WebSources, st)
	// capturar_pantalla also saves each capture where the CLI providers can read it.
	tools.ScreenshotDir = config.ScreensDir(dir)
	reg := tools.Build(g, ws)
	// Each question keeps the tool calls it made, for the answer verifier. The MCP server and the
	// API providers both pass through the registry, so one observer sees them all.
	captures := tools.NewCaptures()
	reg.AddObserver(captures.Observe)

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
	asst.SetCaptures(captures)
	// The player profile reaches the prompts and the ranking tools; the settings screen updates it.
	tools.SetProfileSource(asst.Profile)
	providers := availableProviders(providerReport(cfg, dir))

	tips := make(chan assistant.Tip, tipQueueSize)
	// The tip loop always runs; the gate drops the events while the tips are off (always off on
	// demand), so the setting takes effect at once when the settings screen changes it.
	events := assistant.Gate(ctx, reader.Events(), mode.tipsOn)
	go asst.RunProactive(ctx, events, func(t assistant.Tip) {
		select {
		case tips <- t:
		default:
			log.Printf("tip dropped (queue full): %s", t.Title)
		}
	})

	if start.testTip {
		go sendTestTip(tips, cfg.TipsMode)
	}

	// The start-with-WoW entry is rewritten at each start, so a moved install still starts.
	if cfg.StartWithWoW {
		if err := ui.SetStartWithWoW(true); err != nil {
			log.Printf("inicio con WoW: %v", err)
		}
	}

	// Idle checks: after a minute without the panel the store cache shrinks and the heap goes
	// back to the OS; the SavedVariables tree is dropped after five unused minutes and read
	// again on demand.
	go runIdleRelease(ctx, idle, idleCheckEvery, releaseStoreCache(st.ShrinkMemory),
		func(now time.Time) bool { return watcher.ReleaseIdle(now, svIdleAfter) })

	backend := &onDemandBackend{
		Assistant: asst,
		ctl:       ctl,
		mode:      mode,
		waiter:    &ondemand.Waiter{Feed: ondemand.ReaderFeed{R: reader}},
		g:         g,
		wait:      cfg.OnDemandWait(),
	}
	// The game mode answers the questions typed in the in-game frame (msgType 15). Each answer goes
	// to the clipboard as a CWOW1 block, and the answer sound says it is ready. The question ids are
	// kept per character, so a restart does not answer the same question again.
	answerGate, err := gamemode.NewGate(filepath.Join(dir, "questions.json"))
	if err != nil {
		log.Printf("preguntas: no pude leer questions.json: %v", err)
	}
	announcer := gamemode.NewSystemAnnouncer(cfg.AnswerSound, cfg.AnswerVolume)
	answers := gamemode.NewController(backend, gamemode.ClipboardFunc(ui.CopyText), announcer, answerGate,
		g.ActiveCharacterKey, ui.FriendlyError)
	// The colour-gain notice goes to the clipboard while a question is pending (see runGainNotice).
	go runGainNotice(ctx, reader, answers)
	reader.Channel().OnMessage(func(t uint8, body []byte, _ time.Time) {
		if t != bridge.MsgQuestion {
			return
		}
		q, err := bridge.ParseQuestion(body)
		if err != nil {
			log.Printf("preguntas: %v", err)
			return
		}
		answers.Submit(q)
	})

	uiErr := ui.Run(ui.Options{
		Hotkey:       cfg.Hotkey,
		HotkeyMethod: cfg.HotkeyMethod,
		StatePath:    filepath.Join(dir, "window.json"),
		Avoid:        avoidStrip,
		Backend:      backend,
		Tips:         tips,
		TipsModeChanged: func(m string) {
			mode.setTips(m)
		},
		// The settings screen switches the mode at once: the reader, the watcher and the tracker
		// follow it. Leaving the on-demand mode keeps the reader on while the panel is open.
		ModeChanged: func(m string) {
			continuous := m == config.ModeContinuous
			mode.setContinuous(continuous)
			g.SetOnDemand(!continuous)
			if continuous {
				startSessions()
			}
			reader.SetActive(continuous || ctl.Active())
			reader.SetWatch(!continuous && ctl.Active())
			if !continuous {
				saveOnDemandState(dir, reader, mode.modeName(), ctl.Active(), time.Time{})
			}
		},
		// The game-mode panel key only activates the reader for its hold; the panel stays closed.
		ActivateReading: ctl.Pulse,
		PanelVisibility: func(visible bool) {
			if visible {
				ctl.PanelShown()
			} else {
				ctl.PanelHidden()
			}
		},
		AnswerSettingsChanged: asst.SetAnswerSettings,
		AnswerSoundChanged:    announcer.Set,
		// Probar aviso: plays the answer sound with the settings on the screen (not the saved ones).
		TestAnswerSound: func(mode string, volume int) (string, error) {
			how, err := gamemode.NewSystemAnnouncer(mode, volume).PlayReport(gamemode.CueReady)
			log.Printf("ajustes: probar aviso (%s, volumen %d): %s", mode, volume, how)
			return how, err
		},
		Status: func() ui.Status {
			s := buildStatus(g, providers, watcher, mcpErrs.get())
			// A colour gain the reader cannot decode for 2 s is told to the player in the status bar.
			if gain, ok := reader.ColourGainAlert(time.Now()); ok {
				if s.Notice != "" {
					s.Notice += " · "
				}
				s.Notice += bridge.ColourGainText(gain)
			}
			s.Model = ai.AntigravityStatusLabel(cfg, time.Now())
			s.Idle = !mode.isContinuous() && !ctl.Active()
			return s
		},
		Config:     *cfg,
		ConfigPath: filepath.Join(dir, "config.json"),
		InCombat: func() bool {
			live, ok := g.Live()
			return ok && live.InCombat
		},
		Feedback:    st,
		StartHidden: start.tray,
		Setup:       start.setup,
		DataDir:     dir,
		ExeDir:      executableDir(),
	})

	// The session ends with the app; it is saved before the store closes. On demand, the last
	// game data is kept for the next start.
	sessions.Close()
	if !mode.isContinuous() {
		saveOnDemandState(dir, reader, mode.modeName(), false, time.Time{})
	}

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

// testTipDelay is how long --test-tip waits for the panel and the tray icon to be up.
const testTipDelay = 3 * time.Second

// sendTestTip delivers one tip through the channel the proactive tips use, so that --test-tip
// shows what a real tip does with the current tips mode. It reports on the console and the log.
func sendTestTip(tips chan<- assistant.Tip, mode string) {
	time.Sleep(testTipDelay)
	t := assistant.Tip{
		Title: "Consejo de prueba",
		Text:  "Así se ve un consejo proactivo. Si ves este aviso, la notificación de la bandeja funciona.",
		At:    time.Now(),
	}
	select {
	case tips <- t:
		msg := fmt.Sprintf("test-tip: enviado con consejos en modo %q", mode)
		log.Print(msg)
		fmt.Fprintln(os.Stdout, msg)
	default:
		log.Printf("test-tip: cola llena, no enviado")
	}
}

// executableDir is the folder of the running executable ("" when it is unknown).
func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// avoidStrip returns the protected band of the WoW client: the top StripHeightV3 rows over the
// whole client width, where the pixel channel is drawn (protocol v3 spans the width). The
// bridge reads it from the screen, so the overlay must never cover it.
func avoidStrip() (ui.Rect, bool) {
	_, rect, err := bridge.FindWoWWindow()
	if err != nil {
		return ui.Rect{}, false
	}
	return ui.Rect{X: rect.Min.X, Y: rect.Min.Y, W: rect.Dx(), H: bridge.StripHeightV3}, true
}

// buildStatus gathers what the status bar shows. It is called by the UI every two seconds.
func buildStatus(g *gamedata.Service, providers []string, sv *bridge.SVWatcher, mcpErr error) ui.Status {
	svErr := sv.PathError()
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

	if text, _, _ := g.ChannelFreshness(time.Now()); text != "" {
		s.Channel = text
	}
	s.ShareRemainingSec = g.ShareRemaining()
	if age, ok := g.KeyAge(time.Now()); ok {
		s.KeyKnown = true
		s.KeyAgeSec = int64(age / time.Second)
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
	switch {
	case errors.Is(svErr, config.ErrNoSavedVariables):
		s.Hints = append(s.Hints, hintSyncFirst)
	case svErr != nil:
		s.Hints = append(s.Hints, hintWoWPath)
	default:
		if !ageKnown || time.Duration(s.SavedVarsAgeSec)*time.Second > svStaleAfter {
			s.Hints = append(s.Hints, hintSync)
		}
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
	// Mode is how the game is read (config.ModeOnDemand or config.ModeContinuous); TipsEffective
	// is the tips mode in use (off on demand).
	Mode          string `json:"mode"`
	TipsEffective string `json:"tipsEffective"`
	// RunningApp is the state the running app wrote at its last transition (runtime.json): its
	// active state and the captures it had made. Absent when no app has written one.
	RunningApp      *ondemand.RuntimeState `json:"runningApp,omitempty"`
	RunningAppError string                 `json:"runningAppError,omitempty"`
	SkillVersion    string                 `json:"skillVersion"`
	SkillInstalled  bool                   `json:"skillInstalled"`
	Channel         *channelDiag           `json:"channel,omitempty"`
	UIMode          string                 `json:"uiMode"`
	AnswerSound     string                 `json:"answerSound"`
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
		UIMode:            cfg.UIMode,
		AnswerSound:       cfg.AnswerSound,
		Mode:              cfg.Mode,
		TipsEffective:     cfg.EffectiveTipsMode(),
		SavedVariablesAge: -1,
		SkillVersion:      skill.Version(),
		SkillInstalled:    skill.Installed(dir),
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
	if st, ok, err := ondemand.ReadRuntime(filepath.Join(dir, ondemand.RuntimeFile)); err != nil {
		rep.RunningAppError = err.Error()
	} else if ok {
		rep.RunningApp = &st
	}
	if rep.WoWDetected {
		rep.Channel = diagChannel(diagChannelWait)
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
