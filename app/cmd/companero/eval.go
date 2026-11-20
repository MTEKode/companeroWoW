package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/assistant"
	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/eval"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/mcpserver"
	"github.com/MTEKode/companeroWoW/app/internal/skill"
	"github.com/MTEKode/companeroWoW/app/internal/store"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

// evalProviders are the values accepted by --eval-provider.
var evalProviders = []string{"antigravity", "codex", "api"}

// evalStartupWait is the longest time the evaluation waits for the game bridge to report.
const evalStartupWait = 5 * time.Second

// runEval asks the scripted questions through the real answering path: the provider chain,
// the MCP server on the configured port and the game data. It writes a JSON and a Markdown
// report and prints the summary. It never opens the overlay and never sends input to the game.
func runEval(cfg *config.Config, dir, outPath, provider string) int {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "" && !contains(evalProviders, provider) {
		fmt.Fprintf(os.Stderr, "--eval-provider debe ser uno de: %s\n", strings.Join(evalProviders, ", "))
		return 2
	}
	c := *cfg
	if provider != "" {
		c.ProviderOrder = []string{provider}
	}

	release, first := acquireSingleInstance()
	if !first {
		fmt.Fprintln(os.Stderr, "Cierra Compañero WoW antes de ejecutar --eval: el servidor MCP usa el mismo puerto.")
		return 1
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := skill.DeployAll(dir); err != nil {
		log.Printf("eval: skill: %v", err)
	}
	st, err := store.Open(filepath.Join(dir, "companero.db"))
	if err != nil {
		log.Printf("eval: store: %v", err)
		return 1
	}
	defer func() { _ = st.Close() }()

	reader := bridge.NewReader(c.PixelPoll())
	watcher := bridge.NewSVWatcherFunc(func() (string, error) { return svPathOf(&c) }, 0)
	g := gamedata.NewService(reader, watcher, st)
	g.Start(ctx)
	waitForGame(g)

	rec := &eval.Recorder{}
	tools.ScreenshotDir = config.ScreensDir(dir)
	reg := tools.Build(g, websearch.New(c.WebSources, st))
	reg.AddObserver(rec.Observe)
	captures := tools.NewCaptures()
	reg.AddObserver(captures.Observe)

	mcp := mcpserver.New(reg, version)
	mcpErr := make(chan error, 1)
	go func() { mcpErr <- mcp.ListenAndServe(ctx, c.MCPPort) }()
	select {
	case err := <-mcpErr:
		fmt.Fprintf(os.Stderr, "el servidor de herramientas no pudo iniciar: %v\n", err)
		return 1
	case <-time.After(300 * time.Millisecond):
	}

	chain := ai.BuildChain(&c, dir, assistant.ToolAdapter(reg))
	asst := assistant.New(chain, g, st, &c)
	asst.SetCaptures(captures)
	tools.SetProfileSource(asst.Profile)
	rep := eval.Run(ctx, asst, rec.Drain, eval.Questions, provider, skill.Version())

	base := outPath
	if base == "" {
		base = filepath.Join(dir, "eval-"+time.Now().Format("20060102-150405")+".md")
	}
	if ext := strings.ToLower(filepath.Ext(base)); ext == ".md" || ext == ".json" {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	mdPath, jsonPath := base+".md", base+".json"

	data, err := eval.JSON(rep)
	if err != nil {
		log.Printf("eval: %v", err)
		return 1
	}
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		log.Printf("eval: escribir %s: %v", jsonPath, err)
		return 1
	}
	md := eval.Markdown(rep)
	if fb, ferr := st.FeedbackSummaryMarkdown(); ferr != nil {
		log.Printf("eval: feedback: %v", ferr)
	} else if fb != "" {
		md += "\n" + fb
	}
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		log.Printf("eval: escribir %s: %v", mdPath, err)
		return 1
	}

	if attachConsole() {
		fmt.Printf("Evaluación: %d de %d preguntas superan las comprobaciones.\n", rep.Passed, rep.Total)
		fmt.Printf("Informe: %s\nDatos:   %s\n", mdPath, jsonPath)
	}
	return 0
}

// waitForGame gives the bridge and the SavedVariables watcher a moment to read the game.
func waitForGame(g *gamedata.Service) {
	deadline := time.Now().Add(evalStartupWait)
	for time.Now().Before(deadline) {
		if _, ok := g.Live(); ok {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
