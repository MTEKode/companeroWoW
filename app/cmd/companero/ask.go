package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
		return askFail(2, "--ask: la pregunta está vacía")
	}
	c := *cfg
	if svFile != "" {
		svFileOverride = svFile
	}
	if err := skill.DeployAll(dir); err != nil {
		log.Printf("ask: skill: %v", err)
	}
	// The run works on a copy of the database (see askStore), so it runs while the app has its database open.
	st, closeStore, err := askStore(dir)
	if err != nil {
		return askFail(1, "--ask: no pude abrir la base de datos: %v", err)
	}
	defer closeStore()
	// The tools port: the configured one is the running app's when the app runs, so this run serves on a free
	// one and the CLI providers are given that address (see ai.mcpURL).
	port, err := askMCPPort(c.MCPPort)
	if err != nil {
		return askFail(1, "--ask: no hay un puerto libre para el servidor de herramientas: %v", err)
	}
	if port != c.MCPPort {
		log.Printf("ask: el puerto MCP %d está en uso; esta ejecución usa el %d", c.MCPPort, port)
	}
	c.MCPPort = port

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
		return askFail(1, "--ask: el servidor de herramientas no pudo iniciar en el puerto %d: %v", c.MCPPort, err)
	case <-time.After(300 * time.Millisecond):
	}

	chain := ai.BuildChain(&c, dir, assistant.ToolAdapter(reg))
	asst := assistant.New(chain, g, st, &c)
	asst.SetCaptures(captures)
	tools.SetProfileSource(asst.Profile)

	resp, err := asst.Answer(ctx, q, false)
	calls := rec.Drain()
	if err != nil {
		return askFail(1, "--ask: no hubo respuesta: %v", err)
	}
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.Name)
	}
	fmt.Printf("Pregunta: %s\nProveedor: %s\nHerramientas: %s\n\n%s\n", q, resp.Provider, strings.Join(names, ", "), resp.Text)
	return 0
}

// askFail reports why --ask stopped, on the standard output and on the error output: a caller that captures
// only one of them still sees the reason, and the exit code says the run failed.
func askFail(code int, format string, a ...any) int {
	msg := fmt.Sprintf(format, a...)
	fmt.Println(msg)
	fmt.Fprintln(os.Stderr, msg)
	return code
}

// askStore opens the store of one --ask run on a copy of the data directory's database (store.Snapshot), so the
// run never writes into the database the app keeps open, and it works while the app runs. The import of the
// SavedVariables file and the migrations touch only the copy. The returned func closes the store and removes
// the copy. Without a database yet the run starts on an empty one.
func askStore(dir string) (*store.Store, func(), error) {
	tmp, err := os.MkdirTemp("", "companero-ask-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	dst := filepath.Join(tmp, "companero.db")
	src := filepath.Join(dir, "companero.db")
	if _, err := os.Stat(src); err == nil {
		if err := store.Snapshot(src, dst); err != nil {
			cleanup()
			return nil, nil, err
		}
	}
	st, err := store.Open(dst)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return st, func() {
		_ = st.Close()
		cleanup()
	}, nil
}

// askMCPPort is the port one --ask run serves its tools on: the configured one when it is free, otherwise a free
// one chosen by the system. The running app holds the configured port, and the run must not reach its server.
func askMCPPort(configured int) (int, error) {
	if ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(configured))); err == nil {
		_ = ln.Close()
		return configured, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
