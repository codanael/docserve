# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# docserve

Self-hosted MCP documentation server. Fetches docs from Git providers, indexes with SQLite FTS5, serves to LLM agents via MCP Streamable HTTP.

## Build & Run

```bash
make build                          # CGO_ENABLED=0 static binary
make test                           # unit tests
make test-integration               # integration tests (MCP protocol flow)
make lint                           # golangci-lint
go test -run TestFoo ./internal/... # run a single test by name
```

## Project Structure

```
cmd/docserve/main.go        CLI entrypoint (os.Args + flag.NewFlagSet, no framework)
internal/config/             YAML config parsing, validation, proxy/auth config
internal/index/              SQLite FTS5 store, chunkers (markdown + asciidoc + plain), search
internal/source/             Provider interface, GitHub + Azure DevOps + Confluence, fetch pipeline
internal/mcp/                MCP server, 3 tool handlers, Streamable HTTP transport
internal/scheduler/          Cron-based periodic fetch scheduler
internal/confluence/         Confluence HTML→Markdown converter
```

## Dependencies

Only 3 external modules — everything else is stdlib:
- `github.com/mark3labs/mcp-go` — MCP protocol + Streamable HTTP transport
- `modernc.org/sqlite` — SQLite pure Go (no CGO)
- `gopkg.in/yaml.v3` — config parsing

## Key Design Decisions

- **No cobra, no goldmark, no HTTP framework.** CLI is `os.Args` + `flag.NewFlagSet`. Chunking is line-by-line state machines. Health endpoints use `net/http` stdlib.
- **Provider pattern** for Git hosting: each provider implements `Resolve(ref) → sha` and `Fetch(sha, paths, destDir)`. Three providers: `github`, `azure-devops`, `confluence`.
- **Hierarchical config**: providers are grouped by type (github, azure-devops, confluence). Each provider declares repos (git) or pages (confluence). Auth and schedule inherit from provider to repo with per-repo override. Proxy is provider-level only.
- **Config ↔ JSON Schema sync**: `docserve.schema.json` documents the config format. Any change to config structs in `internal/config/config.go` **must** be reflected in the JSON Schema, and vice versa.
- **Per-provider proxy routing**: each provider declares `proxy: true/false` (defaults true). Two `http.Client` instances (proxied + direct) are created at startup.
- **Auth via env vars only**: credentials are never in the YAML config. `token_env`, `username_env`, `password_env` reference environment variable names.
- **FTS5 search**: BM25 ranking with weights (path=1.5, breadcrumb=2.0, content=1.0). Token budget caps results.
- **MCP tool output**: `list-libraries` and `resolve-library` return `structuredContent` + JSON text fallback. `get-library-docs` returns human-readable markdown text (not JSON).
- **Transactional indexation**: chunks are replaced atomically per library in a single SQLite transaction.
- **Config resolution order**: explicit `--config` flag → `$DOCSERVE_CONFIG` env → `./docserve.yaml` → `/etc/docserve/docserve.yaml`.

## Testing

- Unit tests: `make test` (chunkers, config, store, search, tools, scheduler)
- Integration: `make test-integration` (full MCP protocol: init → tools/list → tools/call → verify)
- MCP Inspector: `npx @modelcontextprotocol/inspector --cli http://localhost:8080/mcp --method tools/list`

## Adding a New Provider

1. Create `internal/source/newprovider.go` implementing `source.Provider` (`Resolve` + `Fetch`)
2. Add the case in `NewProvider()` factory in `provider.go`
3. Add new config structs if needed and update `validate()` in `internal/config/config.go`
4. Update `FlattenProviders()` in `internal/config/config.go` to produce `ResolvedSource` entries
5. Update `docserve.schema.json` to include the new provider type and its fields
6. Write tests with `httptest` server serving fake responses

## Release

GoReleaser builds linux/amd64 + linux/arm64 static binaries with cosign signing. Dockerfile produces a `scratch` image (~15 MB). CI runs on tag push (`v*`).

```bash
goreleaser check          # validate config
goreleaser release        # full release
```
