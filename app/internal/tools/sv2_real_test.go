package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/store"
	"github.com/MTEKode/companeroWoW/app/internal/websearch"
)

// TestSV2RealTools runs the character tools on a copy of a real schema-2 SavedVariables file, in a
// fresh store (the file is ingested the way the app does it at start). It is skipped unless
// COMPANERO_SV2_FILE names the copy; the site data (wowforevertalents.com) is read from the network
// or its cache. The output is the evidence the tools read the character's sheet.
func TestSV2RealTools(t *testing.T) {
	path := os.Getenv("COMPANERO_SV2_FILE")
	if path == "" {
		t.Skip("COMPANERO_SV2_FILE no está definido: prueba con datos reales desactivada")
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g := gamedata.NewService(nil, bridge.NewSVWatcherFunc(func() (string, error) { return path, nil }, 0), st)
	if err := g.ImportSavedVariablesNow(); err != nil {
		t.Fatalf("leer SavedVariables: %v", err)
	}
	ws := websearch.New(config.Defaults().WebSources, st)
	reg := Build(g, ws)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	for _, call := range []struct {
		name string
		args string
	}{
		{"contexto_jugador", `{}`},
		{"optimizar_talentos", `{}`},
		{"analizar_rotacion", `{}`},
	} {
		res, err := reg.Call(ctx, call.name, json.RawMessage(call.args))
		if err != nil {
			t.Errorf("%s: %v", call.name, err)
			continue
		}
		var parts []string
		for _, c := range res.Content {
			parts = append(parts, c.Text)
		}
		text := strings.Join(parts, "\n")
		t.Logf("== %s (error=%v)\n%s", call.name, res.IsError, text)
		if res.IsError {
			t.Errorf("%s devolvió error: %s", call.name, text)
		}
		if strings.Contains(text, "/compa sync") {
			t.Errorf("%s todavía pide /compa sync con los datos del archivo", call.name)
		}
	}
}
