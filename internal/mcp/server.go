package mcp

import (
	"fmt"
	"net/http"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/codanael/docserve/internal/index"
)

// Options configures the HTTP surface of the MCP server.
type Options struct {
	// AllowedOrigins lists browser origins (scheme://host[:port]) accepted in
	// addition to loopback origins. Requests without an Origin header are
	// always accepted.
	AllowedOrigins []string
}

// Server wraps an MCP server with an HTTP handler.
type Server struct {
	mcpServer  *server.MCPServer
	httpServer *server.StreamableHTTPServer
	store      *index.Store
	opts       Options
}

// NewServer creates a new MCP server with tool handlers registered.
func NewServer(store *index.Store, version string, opts Options) *Server {
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

	// docserve keeps no per-session state, so run the transport stateless:
	// no Mcp-Session-Id is issued or required, and legacy `initialize`
	// clients and 2026-07-28 `server/discover` clients share the endpoint.
	httpSrv := server.NewStreamableHTTPServer(mcpSrv, server.WithStateLess(true))

	return &Server{
		mcpServer:  mcpSrv,
		httpServer: httpSrv,
		store:      store,
		opts:       opts,
	}
}

// Handler returns an http.Handler that routes MCP and health check requests.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// MCP streamable HTTP endpoint: Origin check → body limit → transport.
	var mcpHandler http.Handler = http.MaxBytesHandler(s.httpServer, maxBodyBytes)
	mcpHandler = originCheck(s.opts.AllowedOrigins, mcpHandler)
	mux.Handle("/mcp", mcpHandler)

	// Health check endpoints
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "ok")
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.store.Ready(r.Context()) {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "ready")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprint(w, "not ready")
		}
	})

	return securityHeaders(mux)
}

// securityHeaders wraps an http.Handler to add standard security headers.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
