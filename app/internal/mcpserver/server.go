// Package mcpserver exposes the tool registry over MCP (streamable HTTP) so that
// Antigravity CLI (agy) and Codex CLI can call the companion's tools.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "companero-wow"
	endpointPath  = "/mcp"
	shutdownGrace = 5 * time.Second
)

// Server serves the registry's tools over MCP.
type Server struct {
	reg     *tools.Registry
	mcp     *mcp.Server
	handler http.Handler
}

// New builds an MCP server with every tool of reg. version is reported to clients.
func New(reg *tools.Registry, version string) *Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: version}, &mcp.ServerOptions{
		Instructions: "Herramientas de WoW Forever: estado del juego, misiones, NPCs, captura de pantalla y búsqueda en fuentes de la versión Forever. Todo el texto está en español.",
	})
	s := &Server{reg: reg, mcp: srv}
	for _, d := range reg.Defs() {
		name := d.Name
		srv.AddTool(&mcp.Tool{
			Name:        name,
			Description: d.Description,
			InputSchema: d.InputSchema,
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args json.RawMessage
			if req != nil && req.Params != nil {
				args = req.Params.Arguments
			}
			return s.call(ctx, name, args)
		})
	}
	s.handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	return s
}

// Handler is the streamable HTTP MCP endpoint.
func (s *Server) Handler() http.Handler { return s.handler }

// ListenAndServe serves the MCP endpoint at /mcp on 127.0.0.1:port until ctx is done,
// then shuts down gracefully. A busy port yields a descriptive error.
func (s *Server) ListenAndServe(ctx context.Context, port int) error {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// The usual cause is a busy port; the OS error text differs per platform, so
		// the message names the likely cause and the setting to change.
		return fmt.Errorf("no pude usar el puerto %d del servidor MCP (¿lo usa otra aplicación?). Cambia mcpPort en la configuración: %w", port, err)
	}

	mux := http.NewServeMux()
	mux.Handle(endpointPath, s.handler)
	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("servidor MCP: %w", err)
	case <-ctx.Done():
		// Let in-flight requests finish for a short grace period; open MCP streams
		// (clients that stay connected) are then cut so that shutdown never hangs.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			_ = httpSrv.Close()
		}
		<-serveErr
		return nil
	}
}

// call runs a registry tool and maps its result to MCP content.
func (s *Server) call(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
	res, err := s.reg.Call(ctx, name, args)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil
	}
	out := &mcp.CallToolResult{
		IsError: res.IsError,
		Content: make([]mcp.Content, 0, len(res.Content)),
	}
	for _, c := range res.Content {
		if c.Type == "image" {
			out.Content = append(out.Content, &mcp.ImageContent{Data: c.Data, MIMEType: c.MimeType})
			continue
		}
		out.Content = append(out.Content, &mcp.TextContent{Text: c.Text})
	}
	return out, nil
}
