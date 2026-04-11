# docserve - Self-hosted MCP Documentation Server

## Overview

A single Go binary that fetches documentation from GitHub repositories, indexes it with full-text search, and serves it to LLM coding agents via the MCP (Model Context Protocol) Streamable HTTP transport.

Designed for air-gapped or proxy-restricted environments where coding agents cannot access the internet directly but need up-to-date framework and library documentation.

### Goals

- Serve official documentation (Spring Boot, Angular, internal SDKs...) to LLM agents via MCP
- Run as a portable Go binary on any Linux server or OpenShift/k8s cluster
- Fetch docs from GitHub repos through an HTTP proxy
- Full-text search (FTS5) with semantic search as a future extension
- Comply with the MCP Streamable HTTP transport specification (2025-03-26)

### Non-goals (v1)

- Authentication on the MCP endpoint (internal network assumed)
- Web UI for administration
- Semantic/vector search
- Multi-tenancy
- Prometheus metrics
- `include::` resolution for AsciiDoc

## CLI Interface

Single binary with subcommands:

```
docserve serve                      # Start the MCP server
docserve fetch                      # Fetch/update docs from all sources
docserve fetch --source spring-boot # Fetch a single source
docserve fetch --force              # Ignore cache, re-fetch everything
docserve list                       # List indexed libraries and versions
docserve search <library> <query>   # Interactive search (debug/test)
```

CLI implemented with `os.Args` dispatch and `flag.NewFlagSet` per subcommand. No framework dependency.

### Config resolution order

1. `--config path/to/docserve.yaml`
2. `$DOCSERVE_CONFIG`
3. `./docserve.yaml`
4. `/etc/docserve/docserve.yaml`

## Configuration

```yaml
data_dir: /var/lib/docserve

listen: ":8080"

proxy:
  http: http://proxy.internal:3128
  https: http://proxy.internal:3128
  no_proxy: "*.internal,.local"

sources:
  - name: spring-boot
    type: github
    repo: spring-projects/spring-boot
    ref: v3.4.x
    paths:
      - "spring-boot-project/spring-boot-docs/src/docs/asciidoc"
    schedule: "0 3 * * 0"

  - name: angular
    type: github
    repo: angular/angular
    ref: main
    paths:
      - "adev/src/content"

  - name: internal-sdk
    type: github
    repo: myorg/internal-sdk
    ref: main
    paths:
      - "docs/"
```

## Architecture

```
docserve fetch
      |
      v
+-------------+     +--------------+     +--------------+     +-------------+
| Source       |---->| Content      |---->| Chunker      |---->| SQLite      |
| Resolver    |     | Fetcher      |     |              |     | Indexer     |
+-------------+     +--------------+     +--------------+     +-------------+
  - resolve ref       - tarball download   - split on          - FTS5 insert
  - compare sha       - via HTTP proxy       headings          - metadata
  - skip if same      - cache in raw/      - breadcrumbs       - transactional

docserve serve
      |
      v
+------------------------------+
| MCP Streamable HTTP Server   |
|  (mcp-go / mark3labs)        |
|                              |
|  POST /mcp  - JSON-RPC      |
|  GET  /mcp  - SSE stream    |
|  DELETE /mcp - end session   |
|  GET /healthz - health check |
|  GET /readyz  - ready check  |
+-------------+----------------+
              |
              v
+-------------+----------------+
| SQLite FTS5                  |
| (modernc.org/sqlite)         |
+------------------------------+
```

## Pipeline: Fetch and Indexation

### 1. Source Resolver

Resolves the concrete commit to fetch from the config:

- `ref: v3.4.x` - resolved via GitHub API `GET /repos/:owner/:repo/git/ref/...`
- `ref: latest` - resolved via `GET /repos/:owner/:repo/releases/latest`
- `ref: main` - resolved to its current `commit_sha` via `GET /repos/:owner/:repo/commits/:ref` (HEAD only)

Produces a manifest: `{source, resolved_ref, commit_sha, paths[]}`.

Compares `commit_sha` against the value stored in the database. If identical, skips the fetch entirely (idempotent).

### 2. Content Fetcher

Primary strategy: **tarball download**.

`GET /repos/:owner/:repo/tarball/:ref` returns the full repo as a `.tar.gz`. We extract only files matching the configured `paths[]` globs. Single HTTP call, efficient.

Fallback for very large repos: **API tree traversal**. `GET /repos/:owner/:repo/git/trees/:sha?recursive=1` then fetch individual files. Rate-limit aware.

HTTP proxy injected via `http.Transport` on the Go `http.Client`. All traffic goes through the configured proxy.

Raw files cached in `data_dir/raw/{source}/{commit_sha}/` to allow re-indexation without re-fetching.

### 3. Chunker

Splits documentation files into searchable sections. Two implementations behind a common interface, dispatched by file extension.

```go
type Chunker interface {
    Chunk(filename string, content []byte) ([]Chunk, error)
}
```

**Markdown chunker** (`.md`, `.mdx`):
- Line-by-line state machine
- Splits on headings (`^#{1,6}\s`)
- Tracks fenced code blocks (`` ``` ``) to avoid splitting inside them
- Preserves heading hierarchy as breadcrumbs ("Getting Started > Installation")
- Chunks exceeding ~4000 tokens are split further on paragraph boundaries
- Parses YAML front matter as file metadata

**AsciiDoc chunker** (`.adoc`, `.asciidoc`):
- Same state machine approach
- Splits on headings (`^={1,6}\s`)
- Tracks code blocks (`----`)
- Handles admonitions (`NOTE:`, `TIP:`, etc.) as inline content
- Does NOT resolve `include::` directives; each file is indexed independently

**Plain text** (`.txt`, `.rst`): treated as a single chunk per file.

### 4. SQLite Schema

```sql
CREATE TABLE libraries (
    id          INTEGER PRIMARY KEY,
    name        TEXT UNIQUE NOT NULL,
    repo        TEXT NOT NULL,
    ref         TEXT NOT NULL,
    commit_sha  TEXT NOT NULL,
    fetched_at  DATETIME NOT NULL
);

CREATE VIRTUAL TABLE chunks USING fts5(
    library_id UNINDEXED,
    path,
    breadcrumb,
    content,
    tokenize='porter unicode61'
);

CREATE TABLE chunk_meta (
    rowid       INTEGER PRIMARY KEY,
    library_id  INTEGER NOT NULL REFERENCES libraries(id),
    path        TEXT NOT NULL,
    breadcrumb  TEXT NOT NULL,
    byte_size   INTEGER NOT NULL
);
```

Indexation is transactional: for a given library, old chunks are deleted and new ones inserted within a single transaction.

### 5. Scheduling

When running `docserve serve`, an internal scheduler triggers `fetch` based on the `schedule` cron expressions in config. Implemented with `time.Ticker` and cron expression parsing.

`docserve fetch` can also be run standalone (CLI, CronJob, systemd timer).

## MCP Interface

### Tools

Three MCP tools exposed:

#### `list-libraries`

No parameters. Returns all indexed libraries with version and fetch timestamp.

```json
[
  {"name": "spring-boot", "ref": "v3.4.1", "fetched_at": "2026-04-10T03:00:00Z"},
  {"name": "angular", "ref": "main@abc1234", "fetched_at": "2026-04-10T03:00:00Z"}
]
```

#### `resolve-library`

Fuzzy-matches a library name from agent input.

```json
// Input
{"query": "spring"}

// Output
{"name": "spring-boot", "ref": "v3.4.1"}
```

Implementation: `LIKE` on `libraries.name` with proximity ranking.

#### `get-library-docs`

Main search tool. Queries FTS5 within a resolved library.

```json
// Input
{
  "library": "spring-boot",
  "query": "actuator health endpoint configuration",
  "max_tokens": 5000
}

// Output
{
  "library": "spring-boot",
  "version": "v3.4.1",
  "results": [
    {
      "path": "spring-boot-actuator/health.md",
      "breadcrumb": "Actuator > Health Endpoint > Configuration",
      "content": "## Configuration\n\nThe health endpoint can be configured...",
      "score": 12.5
    }
  ]
}
```

### Search Engine

Query processing:
1. Tokenize agent query (split on spaces/punctuation, lowercase)
2. Build FTS5 MATCH query with OR between terms
3. Rank with `bm25()`: breadcrumb weight 2x, path weight 1.5x, content weight 1x
4. Accumulate results until `max_tokens` budget exhausted (estimated as `len(text) / 4`)
5. Return top results scoped to the requested `library_id`

### Future: Semantic Search Extension Point

Not implemented in v1, but the design accommodates it:

```yaml
# Future config
search:
  semantic:
    enabled: true
    endpoint: http://embedding-service:8081/embed
```

When enabled:
- Additional `embeddings` table (`chunk_rowid INTEGER, vector BLOB`)
- Indexation pipeline calls the embedding model
- Search combines BM25 + cosine similarity via Reciprocal Rank Fusion

No code, no feature flag, no dead code for this in v1.

### Transport: Streamable HTTP

Implemented via `mcp-go` (mark3labs). Compliant with MCP spec 2025-03-26.

- **POST `/mcp`**: receives JSON-RPC requests/notifications. Returns `application/json` or `text/event-stream`.
- **GET `/mcp`**: opens SSE stream for server-initiated messages.
- **DELETE `/mcp`**: session termination.
- **Session management**: `Mcp-Session-Id` header, in-memory session store.
- **Health endpoints**: `GET /healthz` (liveness), `GET /readyz` (DB accessible + at least one library indexed).

Conformance validated with MCP Inspector and integration tests.

## Project Structure

```
docserve/
├── cmd/
│   └── docserve/
│       └── main.go              # CLI entrypoint (os.Args + flag.NewFlagSet)
│
├── internal/
│   ├── config/
│   │   └── config.go            # YAML parsing, validation, resolution
│   │
│   ├── source/
│   │   ├── resolver.go          # Ref resolution → commit_sha
│   │   ├── fetcher.go           # Tarball download, extraction
│   │   └── github.go            # GitHub API client (proxy-aware)
│   │
│   ├── index/
│   │   ├── chunker.go           # Chunker interface + dispatch
│   │   ├── markdown.go          # Markdown chunker (state machine)
│   │   ├── asciidoc.go          # AsciiDoc chunker (state machine)
│   │   ├── store.go             # SQLite schema, migrations, CRUD
│   │   └── search.go            # FTS5 queries, ranking, truncation
│   │
│   ├── mcp/
│   │   ├── server.go            # mcp-go setup, tool registration
│   │   ├── tools.go             # Tool implementations (3 tools)
│   │   └── transport.go         # Streamable HTTP config, health endpoints
│   │
│   └── scheduler/
│       └── scheduler.go         # Internal cron for periodic fetch
│
├── testdata/                    # Markdown/AsciiDoc fixtures for tests
│
├── docserve.example.yaml
├── .goreleaser.yaml
├── Dockerfile
├── flake.nix
├── go.mod
├── go.sum
└── Makefile
```

## Dependencies

Three external Go modules:

| Dependency | Role | Justification |
|-----------|------|---------------|
| `github.com/mark3labs/mcp-go` | MCP server + Streamable HTTP transport | Implementing the full MCP transport spec from scratch would take weeks and produce conformance bugs. This is the core protocol layer. |
| `modernc.org/sqlite` | SQLite driver, pure Go | Only way to get SQLite without CGO. Required for portable static binaries. |
| `gopkg.in/yaml.v3` | YAML config parsing | YAML is a complex format. No reasonable path to hand-rolling a parser. |

Notably absent:
- **No cobra** — 4 subcommands handled with `os.Args` + `flag.NewFlagSet` (~60 lines)
- **No goldmark** — chunking done with a line-by-line state machine (~120 lines per format)
- **No HTTP framework** — `net/http` stdlib for health endpoints

## Build and Release

### Local development

```makefile
VERSION := $(shell git describe --tags --always)

build:
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$(VERSION)" \
		-o bin/docserve ./cmd/docserve

test:
	go test ./...

test-integration:
	go test -tags=integration ./...
```

### Release: GoReleaser

`.goreleaser.yaml` handles:
- Multi-platform builds: `linux/amd64`, `linux/arm64`
- `CGO_ENABLED=0` for static binaries
- SHA256 checksums
- Binary signing (cosign or GPG) for source attestation
- `.tar.gz` archives
- OCI image build and push (optional)

### Dockerfile

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /docserve ./cmd/docserve

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /docserve /docserve
ENTRYPOINT ["/docserve"]
```

Image `scratch`, ~15-20 MB. CA certs included for HTTPS to GitHub via proxy.

### flake.nix (dev environment)

Provides: `go`, `gopls`, `golangci-lint`, `goreleaser`, `sqlite` (debug), `npx` (MCP Inspector), `git`.

## Deployment

### Linux server

```bash
# Install
tar xzf docserve_linux_amd64.tar.gz -C /usr/local/bin/

# Configure
cp docserve.example.yaml /etc/docserve/docserve.yaml
# edit config...

# Initial fetch
docserve fetch --config /etc/docserve/docserve.yaml

# Run as systemd service
systemctl enable --now docserve
```

### OpenShift / Kubernetes

Same binary, same image. Deployment for `serve`, CronJob for `fetch`, shared PVC for SQLite.

No k8s-specific code in the binary.

## Testing Strategy

| Level | What | How |
|-------|------|-----|
| Unit | Chunkers, config parsing, FTS query builder | `go test`, fixtures in `testdata/` |
| Integration | Pipeline: fetch → index → search | Build tag `integration`, SQLite `:memory:` |
| MCP conformance | Full Streamable HTTP protocol | Go HTTP client exercising: init → tools/list → tools/call → session close |
| E2E | MCP Inspector | Manual in dev, verifies real protocol compliance |
