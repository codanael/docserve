# docserve

Self-hosted MCP documentation server. Fetches docs from Git providers, indexes with SQLite FTS5, serves to LLM agents via MCP Streamable HTTP.

## Build & Run

```bash
make build                          # CGO_ENABLED=0 static binary
make test                           # unit tests
go test -tags=integration ./...     # integration tests (MCP protocol flow)
make lint                           # golangci-lint
```

## Project Structure

```
cmd/docserve/main.go        CLI entrypoint (os.Args + flag.NewFlagSet, no framework)
internal/config/             YAML config parsing, validation, proxy/auth config
internal/index/              SQLite FTS5 store, chunkers (markdown + asciidoc), search
internal/source/             Provider interface, GitHub + Azure DevOps + Confluence, fetch pipeline
internal/mcp/                MCP server, 3 tool handlers, Streamable HTTP transport
internal/scheduler/          Cron-based periodic fetch scheduler
```

## Dependencies

Direct modules (everything else is stdlib):
- `github.com/mark3labs/mcp-go` v1.1.0 — MCP protocol (spec 2026-07-28 with legacy fallback) + Streamable HTTP transport
- `modernc.org/sqlite` — SQLite pure Go (no CGO)
- `gopkg.in/yaml.v3` — config parsing
- `github.com/JohannesKaufmann/html-to-markdown/v2` + `github.com/JohannesKaufmann/dom` — Confluence XHTML → Markdown
- `golang.org/x/net` — HTML parsing helpers for the Confluence provider

## Key Design Decisions

- **No cobra, no goldmark, no HTTP framework.** CLI is `os.Args` + `flag.NewFlagSet`. Chunking is line-by-line state machines. Health endpoints use `net/http` stdlib.
- **Provider pattern** for Git hosting: each provider implements `Resolve(ref) → sha` and `Fetch(sha, paths, destDir)`. Add a new provider by implementing that interface.
- **Per-source proxy routing**: each source declares `proxy: true/false`. Two `http.Client` instances (proxied + direct) are created at startup.
- **Auth via env vars only**: credentials are never in the YAML config. `token_env`, `username_env`, `password_env` reference environment variable names.
- **FTS5 search**: BM25 ranking with weights (path=1.5, breadcrumb=2.0, content=1.0). Token budget caps results. Semantic search is a future extension.
- **MCP transport is stateless** (`server.WithStateLess(true)`): no `Mcp-Session-Id`; mcp-go v1.1.0 serves 2026-07-28 (`server/discover`) and legacy `initialize` clients on the same `/mcp` endpoint.
- **HTTP hardening lives in `internal/mcp/middleware.go`**: Origin check (loopback + `allowed_origins`), optional bearer auth (`auth_token_env`), 1 MiB body cap. Health endpoints bypass all of it.
- **Tools are strict**: `WithInputSchemaValidation` + `WithStrictInputSchemaDefault`; handlers use `RequireString`. All tools declare `readOnly/idempotent=true`, `destructive/openWorld=false`, a `title`, and (for the two JSON tools) an `outputSchema`.
- **MCP tool output**: `list-libraries` returns `{"libraries": [...]}` and `resolve-library` returns `{"name","ref"}` as `structuredContent` + JSON text fallback. `get-library-docs` returns markdown text and appends a truncation notice when `SearchOutput.Truncated` is set.
- **Search queries are escaped**: FTS5 terms are quoted (`buildFTSQuery`), LIKE wildcards escaped (`escapeLike`). Store read methods take a `context.Context`.
- **Transactional indexation**: chunks are replaced atomically per library in a single SQLite transaction.

## Testing

- Unit tests: `go test ./...` (chunkers, config, store, search, tools, scheduler)
- Integration: `go test -tags=integration ./...` (full MCP protocol: init → tools/list → tools/call → verify)
- MCP Inspector: `npx @modelcontextprotocol/inspector --cli http://localhost:8080/mcp --method tools/list`
- Conformance: `npx -y @modelcontextprotocol/conformance server --url http://127.0.0.1:8080/mcp`

## Adding a New Provider

1. Create `internal/source/newprovider.go` implementing `source.Provider`
2. Add the case in `NewProvider()` factory in `provider.go`
3. Add the provider name to `validProviders` in `internal/config/config.go`
4. Write tests with `httptest` server serving fake archives

## Release

GoReleaser builds linux/amd64 + linux/arm64 static binaries with cosign signing. Dockerfile produces a `scratch` image (~15 MB).

```bash
goreleaser check          # validate config
goreleaser release        # full release
```
