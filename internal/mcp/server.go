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
			mcplib.WithDescription("List all indexed documentation libraries. Returns each library's exact name and git ref (branch/tag). Use the exact name from this list when calling get-library-docs."),
			readOnly, notDestructive, idempotent,
		),
		handlers.ListLibraries,
	)

	mcpSrv.AddTool(
		mcplib.NewTool("resolve-library",
			mcplib.WithDescription("Resolve a library by fuzzy name query, returning the best match with its exact name and ref. Use this to find the correct library name before calling get-library-docs."),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("A partial or fuzzy library name to search for (e.g. 'spring', 'angular')")),
			readOnly, notDestructive, idempotent,
		),
		handlers.ResolveLibrary,
	)

	mcpSrv.AddTool(
		mcplib.NewTool("get-library-docs",
			mcplib.WithDescription("Search a library's indexed documentation by keyword query. The 'library' parameter must be the exact library name as returned by list-libraries or resolve-library. If unsure of the exact name, call resolve-library first."),
			mcplib.WithString("library", mcplib.Required(), mcplib.Description("Exact library name as returned by list-libraries or resolve-library (e.g. 'spring-boot'). Must match exactly.")),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("Keywords to search for in the documentation (e.g. 'health endpoint', 'routing'). Plain text, no special syntax needed.")),
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
		_, _ = fmt.Fprint(w, "ok")
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.store.Ready() {
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
