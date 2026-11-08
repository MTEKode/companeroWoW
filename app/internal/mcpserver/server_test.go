package mcpserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/gamedata"
	"github.com/MTEKode/companeroWoW/app/internal/mcpserver"
	"github.com/MTEKode/companeroWoW/app/internal/store"
	"github.com/MTEKode/companeroWoW/app/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const fixture = "../../testdata/CompaneroWoW.lua"

func fixtureRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "companero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w := bridge.NewSVWatcher(fixture, time.Hour)
	if err := w.Poll(); err != nil {
		t.Fatalf("poll fixture: %v", err)
	}
	if _, _, err := st.ImportKnowledge(w.Current()); err != nil {
		t.Fatalf("import knowledge: %v", err)
	}
	return tools.Build(gamedata.NewService(nil, w, st), nil)
}

func connect(t *testing.T, url string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: authedClient()}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestToolsOverStreamableHTTP(t *testing.T) {
	srv := mcpserver.New(fixtureRegistry(t), "test")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	cs := connect(t, ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	list, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 22 {
		t.Fatalf("ListTools returned %d tools, want 22", len(list.Tools))
	}
	for _, tool := range list.Tools {
		if tool.Description == "" || tool.InputSchema == nil {
			t.Errorf("tool %q lacks description or schema", tool.Name)
		}
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "obtener_ubicacion", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("obtener_ubicacion returned IsError: %+v", res.Content)
	}
	text := contentText(t, res)
	if !strings.Contains(text, "Orgrimmar") {
		t.Errorf("location text lacks zone: %s", text)
	}
}

func TestErrorResultsAndImagesAreMapped(t *testing.T) {
	orig := tools.CaptureFunc
	t.Cleanup(func() { tools.CaptureFunc = orig })
	png := []byte("\x89PNG\r\n\x1a\nfake")
	tools.CaptureFunc = func() ([]byte, error) { return png, nil }

	srv := mcpserver.New(fixtureRegistry(t), "test")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	cs := connect(t, ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "obtener_mision", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("missing quest_id should come back as an MCP error result")
	}

	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "capturar_pantalla", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool capturar_pantalla: %v", err)
	}
	if res.IsError || len(res.Content) == 0 {
		t.Fatalf("capturar_pantalla result = %+v", res)
	}
	img, ok := res.Content[0].(*mcp.ImageContent)
	if !ok {
		t.Fatalf("first content is %T, want *mcp.ImageContent", res.Content[0])
	}
	if img.MIMEType != "image/png" || string(img.Data) != string(png) {
		t.Errorf("image = %s %d bytes, want image/png with the PNG bytes", img.MIMEType, len(img.Data))
	}
}

func TestListenAndServeBusyPortIsDescriptive(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	srv := mcpserver.New(tools.NewRegistry(), "test")
	err = srv.ListenAndServe(context.Background(), port)
	if err == nil || !strings.Contains(err.Error(), "mcpPort") {
		t.Fatalf("err = %v, want a descriptive busy-port error naming mcpPort", err)
	}
}

func TestListenAndServeShutsDownOnCancel(t *testing.T) {
	port := freePort(t)
	srv := mcpserver.New(fixtureRegistry(t), "test")
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx, port) }()

	url := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
	waitListening(t, port)
	cs := connect(t, url)
	if _, err := cs.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("ListTools over /mcp: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ListenAndServe returned %v after cancel, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ListenAndServe did not return after cancel")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitListening(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond); err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("port %d never opened", port)
}

func contentText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		raw, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &m)
		sb.WriteString(m.Text)
	}
	return sb.String()
}

// authedClient sends the process token, as the CLI providers do.
func authedClient() *http.Client {
	return &http.Client{Transport: bearerTransport{base: http.DefaultTransport}}
}

type bearerTransport struct{ base http.RoundTripper }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.Header.Set("Authorization", "Bearer "+mcpserver.Token())
	return b.base.RoundTrip(r2)
}

func TestRequestsWithoutTokenAreUnauthorized(t *testing.T) {
	srv := mcpserver.New(fixtureRegistry(t), "test")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	for _, auth := range []string{"", "Bearer wrong", "Basic " + mcpserver.Token(), mcpserver.Token()} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Authorization %q: status %d, want 401", auth, resp.StatusCode)
		}
		body = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	}
}
