# Confluence Data Center Provider for docserve

**Date:** 2026-04-11
**Status:** Draft

## Overview

Add a Confluence Data Center provider to docserve, enabling indexation of Confluence page trees for LLM consumption via MCP. Pages are fetched from the Confluence REST API, converted from XHTML storage format to markdown, and fed into the existing pipeline (MarkdownChunker, FTS5 index, MCP tools).

## Configuration

New provider type `"confluence"` in `SourceConfig`. Reuses existing fields where semantically appropriate, adds one new field (`depth`).

```yaml
sources:
  - name: team-runbooks
    provider: confluence
    base_url: https://confluence.example.com
    ref: "123456"              # Page ID of the root page
    depth: 3                   # Max subtree depth (0 = root only, -1 = unlimited)
    schedule: "0 */4 * * *"
    auth:
      type: bearer
      token_env: CONFLUENCE_PAT
```

### Field mapping

| Field | Usage for Confluence |
|---|---|
| `base_url` | Confluence instance URL (already used by Azure DevOps) |
| `ref` | ID of the root page whose descendants are indexed |
| `depth` | **New field** on `SourceConfig`. Max depth of subtree traversal. `0` = root page only, `1` = root + direct children, `-1` = unlimited. Ignored by GitHub/Azure DevOps providers (zero value). |
| `paths` | Not used for Confluence. Validation is relaxed for this provider (paths not required). |
| `repo` | Not set in config. `Library.Repo` receives `{base_url}/pages/{ref}` for display purposes. |

### Config validation changes

- Add `"confluence"` to `validProviders` in `config.go`.
- When `provider == "confluence"`: `base_url` and `ref` are required, `paths` is not required.
- `depth` defaults to `-1` (unlimited) when unset. In Go, the YAML parser sets unset int fields to `0`. Since `0` means "root page only", the config validation must distinguish "unset" from "explicitly 0". Use a `*int` pointer in `SourceConfig`: `nil` → default to `-1`, `0` → root only, `N` → depth N.

## Provider: Resolve

`ConfluenceProvider.Resolve(ctx, ref)` returns a deterministic hash representing the state of the page tree.

### Algorithm

1. Fetch the root page metadata: `GET /rest/api/content/{ref}?expand=version,ancestors`
2. Fetch all descendants via CQL: `GET /rest/api/content/search?cql=ancestor={ref} AND type=page&expand=version,ancestors&limit=200` (paginated)
3. Filter by depth: for each page, compute relative depth as `len(page.ancestors) - len(root.ancestors)`. Exclude pages where relative depth > `depth` (unless `depth == -1`).
4. Sort pages by ID (string sort, stable order).
5. Compute SHA-256 over concatenation of `"{pageID}:{versionNumber}\n"` for each page.
6. Return first 40 hex characters of the hash.

### Cost

1 request for the root page + N/200 paginated requests for descendants (metadata only, no body). For a 500-page tree: ~4 HTTP requests.

### Depth filtering

CQL `ancestor` returns all descendants regardless of depth. Filtering happens client-side using the `ancestors` array. This is simpler and cheaper than recursive level-by-level crawling, since the metadata-only payload is small.

## Provider: Fetch

`ConfluenceProvider.Fetch(ctx, sha, paths, destDir)` downloads pages, converts to markdown, and writes to `destDir`.

### Algorithm

1. Re-fetch page list (same CQL as Resolve, but with `expand=body.storage,version,ancestors`).
2. Filter by depth (same logic as Resolve).
3. For each page, with bounded concurrency (semaphore, 3 goroutines):
   - Compute file path from page title hierarchy. Example: page "API REST" child of "Backend" child of root "Architecture" produces `Architecture/Backend/API REST.md`.
   - Sanitize titles: replace `/`, `\`, `:`, `*`, `?`, `"`, `<`, `>`, `|` with `_`.
   - Pre-process XHTML: wrap in `<div>`, escape CDATA sections, replace `&nbsp;` with `&#160;`.
   - Convert to markdown via `html-to-markdown/v2` with Confluence plugin.
   - Write file to `destDir/{path}.md`.

### File path construction

Each page's `ancestors` array provides the full path from space root to parent. The relative path from the configured root page is extracted:

```
Root (id=100): "Architecture"
  Child (id=200): "Backend"
    Grandchild (id=300): "API REST"

destDir/Architecture.md
destDir/Architecture/Backend.md
destDir/Architecture/Backend/API REST.md
```

### Rate limiting

Built into the v1 implementation:

- **Bounded concurrency**: 3 goroutines max via buffered channel semaphore.
- **Retry with exponential backoff** on HTTP 503 and 429: 3 attempts max, delays 1s, 2s, 4s.
- **`Retry-After` header** respected when present.

### Why re-fetch in Fetch?

The `Provider` interface signature `Fetch(ctx, sha, paths, destDir)` has no mechanism to pass state from Resolve to Fetch. This is consistent with Git providers which also make separate API calls. The cost is acceptable since Fetch only triggers when the hash has changed.

## XHTML to Markdown Converter

Package `internal/confluence/` with a pure function `Convert(storageXHTML string) (string, error)`.

### Pipeline

1. **Pre-processing**: wrap in `<div>`, escape CDATA sections (regex), replace `&nbsp;` with `&#160;`.
2. **Conversion**: `html-to-markdown/v2` with plugins `base`, `commonmark`, `table` + custom `confluencePlugin`.
3. **Post-processing**: collapse excessive blank lines.

### CDATA pre-processing

Go's `x/net/html` parser treats `<![CDATA[...]]>` as HTML comments, corrupting content. Pre-processing escapes CDATA content before parsing:

```go
var cdataRe = regexp.MustCompile(`<!\[CDATA\[([\s\S]*?)\]\]>`)
input = cdataRe.ReplaceAllStringFunc(input, func(m string) string {
    content := cdataRe.FindStringSubmatch(m)[1]
    content = strings.ReplaceAll(content, "&", "&amp;")
    content = strings.ReplaceAll(content, "<", "&lt;")
    content = strings.ReplaceAll(content, ">", "&gt;")
    return content
})
```

### Confluence plugin conversion table

| Source element | Markdown output |
|---|---|
| `ac:structured-macro[code]` | Fenced code block with language annotation from `ac:parameter[language]` |
| `ac:structured-macro[info]` | `> **Info:** {content}` |
| `ac:structured-macro[note]` | `> **Note:** {content}` |
| `ac:structured-macro[warning]` | `> **Warning:** {content}` |
| `ac:structured-macro[tip]` | `> **Tip:** {content}` |
| `ac:structured-macro[panel]` | `> **{title}**\n> {content}` |
| `ac:structured-macro[expand]` | `**{title}**\n\n{content}` (flattened, `<details>` not useful for LLM) |
| `ac:structured-macro[toc]` | Removed (LLM sees headings directly) |
| `ac:structured-macro[anchor]` | Removed |
| `ac:structured-macro[status]` | `` `[{title}]` `` |
| `ac:structured-macro[excerpt]` | Content rendered directly (wrapper removed) |
| `ac:structured-macro[section/column]` | Content flattened sequentially |
| `ac:structured-macro[*]` (unknown) | Render `rich-text-body` if present, else remove. Log warning. |
| `ac:link` + `ri:page` | `[{link text}]({page title})` |
| `ac:link` + `ri:attachment` | `[{link text}]({filename})` |
| `ac:link` + `ri:url` | `[{link text}]({url})` |
| `ac:image` + `ri:attachment` | `![{filename}]({filename})` |
| `ac:image` + `ri:url` | `![image]({url})` |
| `ac:emoticon` | Removed |
| `ac:task-list` / `ac:task` | `- [x] {body}` or `- [ ] {body}` |
| `ac:layout` / `ac:layout-section` / `ac:layout-cell` | Content flattened sequentially |
| `ac:parameter` | Removed (consumed by parent macro handler) |
| `ac:rich-text-body` | Recurse into children |
| `ac:plain-text-body` | Extract raw text content |

### Helper functions

- `getMacroParam(node, paramName) string` — walks `ac:parameter` children to extract a value.
- `getMacroBody(node) *html.Node` — finds the `ac:rich-text-body` or `ac:plain-text-body` child.
- `getResourceIdentifier(node, riType) map[string]string` — walks `ri:*` children, returns their attributes.

## Testing

### Unit tests: converter (`internal/confluence/converter_test.go`)

Table-driven tests with one case per Confluence element type:

- Code block with/without language
- Info, Note, Warning, Tip macros
- Panel with title
- Internal link (`ri:page`)
- Image (`ri:attachment`, `ri:url`)
- Task list (complete/incomplete)
- Layout multi-column flattening
- Unknown macro with `rich-text-body` (rendered)
- Unknown macro without body (removed)
- CDATA with special characters (`<`, `>`, `&`) preserved in code blocks
- Realistic full page combining multiple elements

### Golden file tests (`internal/confluence/testdata/`)

XHTML storage format downloaded from public Confluence instances, stored as fixtures. Conversion output compared against `.golden` files.

**Public Confluence sources for fixtures:**

| Instance | Page ID | Content | Macro coverage |
|---|---|---|---|
| confluence.atlassian.com | 1627457251 | Panel Macro documentation | code, table, panel, info, tip, layout, images, links |
| confluence.atlassian.com | 1627457156 | Info/Tip/Note/Warning doc | All 4 admonition types |
| confluence.atlassian.com | 1627457080 | Code Block Macro doc | Code blocks, admonitions, layout |
| cwiki.apache.org | 74688674 | NetBeans Release README | Code, tables, task lists, links (73K) |
| cwiki.apache.org | 308152924 | Flink CDC Release | Code, info, images (42K) |

### Unit tests: provider (`internal/source/confluence_test.go`)

Using `httptest.Server` to simulate Confluence API:

- Resolve: stable/deterministic hash from mocked CQL responses
- Resolve: hash changes when a page version changes
- Resolve: depth filtering excludes deep pages from hash
- Fetch: pages with body.storage produce correct `.md` files with correct paths
- Fetch: title sanitization (special characters in filenames)
- Fetch: retry on 503 with backoff succeeds
- Fetch: 401 returns clear auth error

### Integration test (`internal/source/confluence_integration_test.go`, tag `integration`)

httptest server simulating a mini Confluence with 3-4 pages. Full cycle: Resolve, Fetch, markdown files on disk, chunking via MarkdownChunker, FTS5 indexation, search returns expected results.

## File structure

### New files

```
internal/confluence/converter.go
internal/confluence/converter_test.go
internal/confluence/plugin.go
internal/confluence/testdata/           # XHTML fixtures + .golden files
internal/source/confluence.go
internal/source/confluence_test.go
internal/source/confluence_integration_test.go
```

### Modified files

```
internal/config/config.go              # "confluence" in validProviders, Depth field, relax paths validation
internal/source/provider.go            # case "confluence" in NewProvider()
go.mod / go.sum                        # html-to-markdown/v2
```

### Unchanged

- `internal/index/` — store, chunkers, search: no changes. Existing MarkdownChunker processes the `.md` files.
- `internal/mcp/` — MCP tools: no changes. Confluence results are served identically.
- `cmd/docserve/main.go` — no changes. Provider instantiated by existing factory.
- `internal/scheduler/` — no changes.

## Dependencies

One new direct dependency: `github.com/JohannesKaufmann/html-to-markdown/v2`. Transitive: `golang.org/x/net` (HTML parser), `github.com/JohannesKaufmann/dom` (DOM utilities). Total project dependencies: 3 → 4.
