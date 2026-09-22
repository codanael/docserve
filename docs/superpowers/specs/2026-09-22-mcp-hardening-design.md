# MCP Hardening and Spec Alignment — Design

**Date:** 2026-09-22
**Status:** approved for implementation

## Context

An audit of `internal/mcp/` against the current MCP specification (revision
2026-07-28, released 2026-07-28) found that docserve:

- pins `github.com/mark3labs/mcp-go v0.47.1`, which negotiates at most
  revision 2025-11-25 and ignores the `MCP-Protocol-Version` header;
- performs no Origin validation, has no authentication, no body size limit
  and no `http.Server` timeouts, and binds all interfaces by default;
- uses a "session" model in which any well-formed UUID is accepted without
  `initialize`, DELETE is a no-op, and sessions are never reaped;
- omits `openWorldHint` (so mcp-go defaults it to `true`), `title`,
  `outputSchema` and server `instructions`;
- does not enforce required tool arguments (a missing `query` on
  `resolve-library` returns the alphabetically first library);
- passes raw FTS5 syntax and raw LIKE wildcards to SQLite, leaking SQLite
  error text to the model;
- ignores `ctx` in handlers and uses non-Context SQL calls;
- silently drops results beyond the 50-row limit or token budget;
- has no tool-call logging and no panic recovery.

A trial bump to mcp-go v1.1.0 (released 2026-09-15) compiles and passes the
existing unit and integration tests unchanged.

## Goals

1. Speak MCP 2026-07-28 to modern clients and 2025-xx to legacy clients on
   the same `/mcp` endpoint (dual-era, provided by mcp-go v1.1.0).
2. Close every spec deviation listed above.
3. Make the HTTP surface safe to expose: Origin validation, optional bearer
   token, body limit, timeouts.
4. Make tool behaviour predictable for the model: strict arguments, typed
   output, truncation signalled, no raw SQL errors.
5. Keep the dependency footprint and the no-framework style of the project.

## Non-goals

- OAuth 2.1 / Protected Resource Metadata. A static bearer token is enough
  for a self-hosted server behind a reverse proxy. PRM can be added later
  with mcp-go's `WithProtectedResourceMetadata`.
- Migrating to the official `modelcontextprotocol/go-sdk`.
- Resources, prompts, tasks, elicitation, or semantic search.
- TLS termination inside docserve (use a reverse proxy).

## Decisions

### Library
Bump to `github.com/mark3labs/mcp-go v1.1.0`. It adds
`github.com/santhosh-tekuri/jsonschema/v6` and `golang.org/x/text` as
indirect dependencies.

### Transport
- `server.WithStateLess(true)`. docserve keeps no per-session state, so no
  `Mcp-Session-Id` is issued, none is required, and nothing can leak.
  Legacy clients still complete `initialize`; modern clients use
  `server/discover`.
- Origin validation lives in docserve middleware, not in mcp-go (which only
  offers CORS headers and loopback Host protection). A request carrying an
  `Origin` header is rejected with 403 unless the origin is a loopback origin
  (`http(s)://localhost`, `127.0.0.1`, `[::1]`, any port) or is listed in
  the new `allowed_origins` config key. Requests without `Origin` (CLI and
  agent clients) pass.
- Request bodies on `/mcp` are capped at 1 MiB with `http.MaxBytesHandler`.
- `http.Server` gets `ReadHeaderTimeout: 10s`, `ReadTimeout: 30s`,
  `IdleTimeout: 120s`, `MaxHeaderBytes: 64 KiB`. `WriteTimeout` stays 0 so
  long-lived SSE responses are not cut.

### Authentication
New top-level config key `auth_token_env`. When set, `config.Load` reads the
named environment variable and fails if it is empty. When the token is
non-empty, `/mcp` requires `Authorization: Bearer <token>` (constant-time
compare) and answers 401 with `WWW-Authenticate: Bearer realm="docserve"`
otherwise. `/healthz` and `/readyz` stay open. When no token is configured,
`serve` logs a warning that the MCP endpoint is unauthenticated.

### Server metadata and tools
- `WithInstructions` describing the intended workflow:
  `resolve-library` → `get-library-docs`, or `list-libraries` to browse.
- `WithRecovery()`, `WithInputSchemaValidation()`,
  `WithStrictInputSchemaDefault()`.
- `WithMethodCacheHints(mcp.MethodToolsList, 3_600_000, mcp.CacheScopePublic)`
  because the tool list is identical for every caller.
- Every tool: `WithToolTitle`, `readOnlyHint=true`, `destructiveHint=false`,
  `idempotentHint=true`, `openWorldHint=false`.
- `list-libraries` returns `{"libraries":[{name, repo, ref, commit_sha,
  fetched_at}]}` as `structuredContent` with a matching `outputSchema`.
  Wrapping in an object is required because `outputSchema` must be
  `type: object`.
- `resolve-library` returns `{name, ref}` with `outputSchema`.
- `get-library-docs` keeps its markdown text output. `max_tokens` gains
  `minimum: 1`. When results were cut by the row limit or the budget, the
  text ends with a line telling the model to refine the query or raise
  `max_tokens`.
- Handlers use `RequireString`; a missing or non-string argument becomes an
  `isError: true` result with a clear message.

### Index layer
- `buildFTSQuery` quotes every term as an FTS5 string (`"` doubled inside)
  so operators, parentheses and column filters are searched literally.
- `FindLibraries` escapes `%`, `_` and `\` and uses `ESCAPE '\'`.
- `ListLibraries`, `FindLibraries`, `GetLibrary`, `SearchDocs` and `Ready`
  take a `context.Context` and use the `*Context` SQL methods.
- `SearchDocs` returns `SearchOutput{Results, Truncated}`. It queries
  `LIMIT maxSearchRows+1` (51) to detect a row-limit cut.

### Logging
mcp-go hooks log each tool call (`tool`, `isError`) and each JSON-RPC error
(`method`, error) through the stdlib `log` package, matching the rest of the
binary.

### Documentation
README, `docs/testing-guide.md`, `docserve.example.yaml`, `CLAUDE.md` and
`CHANGELOG.md` are updated. CLAUDE.md's dependency and provider lists are
corrected (six direct modules, three providers).

## Verification

- `go build ./... && go vet ./... && go test ./... && go test -tags=integration ./...`
- `golangci-lint run ./...` when available.
- Official conformance suite when `npx` is available:
  `npx -y @modelcontextprotocol/conformance server --url http://127.0.0.1:8080/mcp`
- Manual curl checks: foreign `Origin` → 403; missing bearer → 401 when
  configured; `tools/list` without any session header → 200.
