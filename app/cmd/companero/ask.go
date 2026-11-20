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

// svFileOverride is the SavedVariables file named by --sv ("" for the one of the configuration).
var svFileOverride string

// svPathOf is the SavedVariables file the app reads: the one --sv names, or the configured one.
func svPathOf(cfg *config.Config) (string, error) {
	if svFileOverride != "" {
		return svFileOverride, nil
	}
	return cfg.SavedVariablesPath()
}

// askTimeout bounds one --ask run: the chain may try several providers.
const askTimeout = 6 * time.Minute

// runAsk answers one question through the real answering path (the provider chain, the MCP server and
// the game data read from the store and SavedVariables) and prints the answer and the tools it called.
// Nothing is stored in the chat history and the window does not open. It does not need the app to be
// closed when the data directory or the MCP port differ from the running app's.
func runAsk(cfg *config.Config, dir, question, svFile string) int {
	q := strings.TrimSpace(question)
	if q == "" {
		fmt.Fprintln(os.Stderr, "--ask: la pregunta está vacía")
		return 2
	}
	c := *cfg
	if svFile != "" {
		svFileOverride = svFile
	}
	if err := skill.DeployAll(dir); err != nil {
		log.Printf("ask: skill: %v", err)
	}
	st, err := store.Open(filepath.Join(dir, "companero.db"))
	if err != nil {
		log.Printf("ask: store: %v", err)
		return 1
	}
	defer func() { _ = st.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), askTimeout)
	defer cancel()

	reader := bridge.NewReader(c.PixelPoll())
	watcher := bridge.NewSVWatcherFunc(func() (string, error) { return svPathOf(&c) }, 0)
	g := gamedata.NewService(reader, watcher, st)
	g.Start(ctx)
	// The file is read and stored before the question: the answer must see the data as it is now.
	if err := g.ImportSavedVariablesNow(); err != nil {
		log.Printf("ask: saved variables: %v", err)
	}

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
		fmt.Fprintf(os.Stderr, "el servidor de herramientas no pudo iniciar en el puerto %d: %v\n", c.MCPPort, err)
		return 1
	case <-time.After(300 * time.Millisecond):
	}

	chain := ai.BuildChain(&c, dir, assistant.ToolAdapter(reg))
	asst := assistant.New(chain, g, st, &c)
	asst.SetCaptures(captures)
	tools.SetProfileSource(asst.Profile)

	resp, err := asst.Answer(ctx, q, false)
	calls := rec.Drain()
	if err != nil {
		fmt.Fprintf(os.Stderr, "--ask: no hubo respuesta: %v\n", err)
		return 1
	}
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.Name)
	}
	fmt.Printf("Pregunta: %s\nProveedor: %s\nHerramientas: %s\n\n%s\n", q, resp.Provider, strings.Join(names, ", "), resp.Text)
	return 0
}
