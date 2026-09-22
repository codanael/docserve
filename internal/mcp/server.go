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

	// AuthToken, when non-empty, is required as a bearer token on /mcp.
	AuthToken string
}

// Server wraps an MCP server with an HTTP handler.
type Server struct {
	mcpServer  *server.MCPServer
	httpServer *server.StreamableHTTPServer
	store      *index.Store
	opts       Options
}

// instructions is sent to clients at initialize / server/discover time. It
// describes how to combine the tools and deliberately does not repeat the
// per-tool descriptions.
const instructions = `docserve exposes full-text search over documentation libraries that were fetched and indexed locally.

Recommended workflow:
1. Call resolve-library with a fragment of the library name to obtain its exact name (or list-libraries to browse everything that is indexed).
2. Call get-library-docs with that exact name and a short keyword query. Results are markdown chunks with their source path.
3. If the result ends with a truncation notice, narrow the query or raise max_tokens.`

// toolsListCacheTTLMs tells 2026-07-28 clients how long they may cache tools/list.
const toolsListCacheTTLMs int64 = 60 * 60 * 1000

// NewServer creates a new MCP server with tool handlers registered.
func NewServer(store *index.Store, version string, opts Options) *Server {
	mcpSrv := server.NewMCPServer("docserve", version,
		server.WithToolCapabilities(false),
		server.WithInstructions(instructions),
		server.WithRecovery(),
		server.WithInputSchemaValidation(),
		server.WithStrictInputSchemaDefault(),
		// The tool list is identical for every caller and changes only on
		// deploy, so let 2026-07-28 clients cache it.
		server.WithMethodCacheHints(mcplib.MethodToolsList, toolsListCacheTTLMs, mcplib.CacheScopePublic),
	)

	handlers := &ToolHandlers{Store: store}

	readOnly := mcplib.WithReadOnlyHintAnnotation(true)
	notDestructive := mcplib.WithDestructiveHintAnnotation(false)
	idempotent := mcplib.WithIdempotentHintAnnotation(true)
	closedWorld := mcplib.WithOpenWorldHintAnnotation(false)

	mcpSrv.AddTool(
		mcplib.NewTool("list-libraries",
			mcplib.WithToolTitle("List indexed libraries"),
			mcplib.WithDescription("List every documentation library in the local index with its name, repository, ref, commit and fetch time. Use the returned name as the `library` argument of get-library-docs."),
			mcplib.WithOutputSchema[libraryList](),
			readOnly, notDestructive, idempotent, closedWorld,
		),
		handlers.ListLibraries,
	)

	mcpSrv.AddTool(
		mcplib.NewTool("resolve-library",
			mcplib.WithToolTitle("Resolve library name"),
			mcplib.WithDescription("Find the exact name of an indexed library from a partial, case-insensitive name fragment (for example \"spring\" → \"spring-boot/v3.2.0\"). Returns the first alphabetical match."),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("Partial library name to match, such as \"spring\" or \"angular\"")),
			mcplib.WithOutputSchema[libMatch](),
			readOnly, notDestructive, idempotent, closedWorld,
		),
		handlers.ResolveLibrary,
	)

	mcpSrv.AddTool(
		mcplib.NewTool("get-library-docs",
			mcplib.WithToolTitle("Search library documentation"),
			mcplib.WithDescription("Full-text search (BM25) inside one library and return the best matching documentation chunks as markdown, each with its source path. Prefer a few specific keywords over long sentences. Ends with a truncation notice when more matches were omitted."),
			mcplib.WithString("library", mcplib.Required(), mcplib.Description("Exact library name as returned by resolve-library or list-libraries")),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("Search keywords, for example \"actuator health endpoint\"")),
			mcplib.WithNumber("max_tokens", mcplib.Min(1), mcplib.Description("Approximate token budget for the returned content (default 5000). Raise it when the output reports truncation.")),
			readOnly, notDestructive, idempotent, closedWorld,
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

	// MCP streamable HTTP endpoint: Origin check → auth → body limit → transport.
	var mcpHandler http.Handler = http.MaxBytesHandler(s.httpServer, maxBodyBytes)
	mcpHandler = bearerAuth(s.opts.AuthToken, mcpHandler)
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
