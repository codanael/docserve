package mcp

import (
	"fmt"
	"net/http"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/codanael/docserve/internal/index"
)

// Server wraps an MCP server with an HTTP handler.
type Server struct {
	mcpServer  *server.MCPServer
	httpServer *server.StreamableHTTPServer
	store      *index.Store
}

// NewServer creates a new MCP server with tool handlers registered.
func NewServer(store *index.Store, version string) *Server {
	mcpSrv := server.NewMCPServer("docserve", version, server.WithToolCapabilities(false))

	handlers := &ToolHandlers{Store: store}

	readOnly := mcplib.WithReadOnlyHintAnnotation(true)
	notDestructive := mcplib.WithDestructiveHintAnnotation(false)
	idempotent := mcplib.WithIdempotentHintAnnotation(true)

	mcpSrv.AddTool(
		mcplib.NewTool("list-libraries",
			mcplib.WithDescription("List all indexed documentation libraries"),
			readOnly, notDestructive, idempotent,
		),
		handlers.ListLibraries,
	)

	mcpSrv.AddTool(
		mcplib.NewTool("resolve-library",
			mcplib.WithDescription("Resolve a library by name query, returning the first match"),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("The library name query to search for")),
			readOnly, notDestructive, idempotent,
		),
		handlers.ResolveLibrary,
	)

	mcpSrv.AddTool(
		mcplib.NewTool("get-library-docs",
			mcplib.WithDescription("Search a library's documentation and return matching content"),
			mcplib.WithString("library", mcplib.Required(), mcplib.Description("The library name to search")),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("The search query")),
			mcplib.WithNumber("max_tokens", mcplib.Description("Maximum token budget for results (default 5000)")),
			readOnly, notDestructive, idempotent,
		),
		handlers.GetLibraryDocs,
	)

	httpSrv := server.NewStreamableHTTPServer(mcpSrv, server.WithEndpointPath("/mcp"))

	return &Server{
		mcpServer:  mcpSrv,
		httpServer: httpSrv,
		store:      store,
	}
}

// Handler returns an http.Handler that routes MCP and health check requests.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// MCP streamable HTTP endpoint
	mux.Handle("/mcp", s.httpServer)

	// Health check endpoints
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.store.Ready() {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ready")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "not ready")
		}
	})

	return mux
}
