# MCP Hardening and Spec Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring docserve's MCP server to spec revision 2026-07-28 (dual-era), close the audited security and correctness gaps, and document the result.

**Architecture:** docserve stays a thin wrapper over `mark3labs/mcp-go`: `internal/mcp/server.go` declares tools and server options, `internal/mcp/tools.go` holds handlers, and new `internal/mcp/middleware.go` holds HTTP middleware (Origin check, bearer auth). The index layer (`internal/index/`) gains context propagation, query escaping and truncation reporting. Configuration gains two top-level keys.

**Tech Stack:** Go 1.26, `github.com/mark3labs/mcp-go v1.1.0`, `modernc.org/sqlite`, stdlib `net/http`, `log`, `testing`.

**Spec:** `docs/superpowers/specs/2026-09-22-mcp-hardening-design.md`

## Global Constraints

- Module path is `github.com/codanael/docserve`. The MCP package is imported as `mcpsrv "github.com/codanael/docserve/internal/mcp"`; mcp-go packages as `mcplib "github.com/mark3labs/mcp-go/mcp"` and `"github.com/mark3labs/mcp-go/server"`.
- No new direct dependencies beyond the mcp-go bump. No HTTP framework, no cobra, no slog migration; keep stdlib `log`.
- Every task ends with `go build ./... && go vet ./... && go test ./...` green. Tasks touching `internal/mcp` or `integration_test.go` also run `go test -tags=integration ./...`.
- Commit messages use the conventional prefixes already in history (`feat:`, `fix:`, `docs:`, `chore:`) and end with:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01U8jnsQkU3tuNSH9Cdh3wdd
  ```
- Work on branch `mcp-hardening` created from `main`. Do not touch untracked files at the repo root (`MCP_TEST.md`, `docserve-confluence.yaml`, `Dockerfile.custom-ca`, `ca2.resource.sebp.crt`, `docserve.yaml`) and do not stage the modified `.gitignore`.
- Health endpoints `/healthz` and `/readyz` remain unauthenticated and unaffected by the Origin check.
- mcp-go v1.1.0 source lives at `/home/anael/go/pkg/mod/github.com/mark3labs/mcp-go@v1.1.0/` if an API signature needs checking.

---

### Task 0: Branch

**Files:** none

- [ ] **Step 1: Create the working branch**

```bash
cd /home/anael/Documents/projects/bdl/docserve
git checkout -b mcp-hardening main
git status --short
```
Expected: on branch `mcp-hardening`; only ` M .gitignore` and `??` entries for untracked root files. Leave them alone.

---

### Task 1: Bump mcp-go to v1.1.0 and fix CLAUDE.md drift

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `CLAUDE.md` (Dependencies section, Project Structure providers line)

**Interfaces:**
- Produces: mcp-go v1.1.0 API for all later tasks (`server.WithStateLess`, `server.WithInstructions`, `server.WithRecovery`, `server.WithInputSchemaValidation`, `server.WithStrictInputSchemaDefault`, `server.WithMethodCacheHints`, `server.WithHooks`, `mcplib.WithToolTitle`, `mcplib.WithOpenWorldHintAnnotation`, `mcplib.WithOutputSchema[T]`, `mcplib.Min`, `mcplib.NewToolResultStructured`, `req.RequireString`).

- [ ] **Step 1: Bump the module**

```bash
go get github.com/mark3labs/mcp-go@v1.1.0
go mod tidy
grep -n "mcp-go\|jsonschema\|x/text" go.mod
```
Expected: `github.com/mark3labs/mcp-go v1.1.0` in the direct block; `github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect` and `golang.org/x/text v0.31.0 // indirect` in the indirect block.

- [ ] **Step 2: Build and run all tests**

```bash
go build ./... && go vet ./... && go test ./... && go test -tags=integration ./...
```
Expected: all packages `ok`. (A trial bump in a scratch copy already passed; if something fails, read the error, fix minimally, do not downgrade.)

- [ ] **Step 3: Fix CLAUDE.md**

Replace the `## Dependencies` section with:

```markdown
## Dependencies

Direct modules (everything else is stdlib):
- `github.com/mark3labs/mcp-go` v1.1.0 — MCP protocol (spec 2026-07-28 with legacy fallback) + Streamable HTTP transport
- `modernc.org/sqlite` — SQLite pure Go (no CGO)
- `gopkg.in/yaml.v3` — config parsing
- `github.com/JohannesKaufmann/html-to-markdown/v2` + `github.com/JohannesKaufmann/dom` — Confluence XHTML → Markdown
- `golang.org/x/net` — HTML parsing helpers for the Confluence provider
```

In `## Project Structure`, change the `internal/source/` line to:
```
internal/source/             Provider interface, GitHub + Azure DevOps + Confluence, fetch pipeline
```

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum CLAUDE.md
git commit -m "chore: bump mcp-go to v1.1.0 (MCP 2026-07-28) and fix CLAUDE.md dependency list

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U8jnsQkU3tuNSH9Cdh3wdd"
```

---

### Task 2: Escape FTS5 and LIKE query syntax

**Files:**
- Modify: `internal/index/search.go:8-17` (`buildFTSQuery`)
- Modify: `internal/index/store.go:199-202` (`FindLibraries`)
- Test: `internal/index/search_test.go` (`TestBuildFTSQuery`, new `TestSearchSpecialCharacters`)
- Test: `internal/index/store_test.go` (new `TestFindLibrariesEscapesWildcards`; create the file if it does not exist)

**Interfaces:**
- Produces: `buildFTSQuery(input string) string` now returns `"w1" OR "w2"`; new unexported `escapeLike(s string) string`.

- [ ] **Step 1: Update the FTS unit test expectations and add a special-character test**

In `internal/index/search_test.go`, replace the table in `TestBuildFTSQuery` with:

```go
	tests := []struct {
		input string
		want  string
	}{
		{"database configuration", `"database" OR "configuration"`},
		{"single", `"single"`},
		{"  spaced  words  ", `"spaced" OR "words"`},
		{"", ""},
		{`say "hi"`, `"say" OR """hi"""`},
		{"health AND", `"health" OR "AND"`},
		{"path:foo NEAR(", `"path:foo" OR "NEAR("`},
	}
```

Add after `TestSearchNoResults`:

```go
func TestSearchSpecialCharacters(t *testing.T) {
	s, libID := testLibrary(t)

	chunks := []Chunk{
		{Path: "docs/health.md", Breadcrumb: "Health", Content: "The health endpoint reports status."},
	}
	if err := s.ReplaceChunks(libID, chunks); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}

	for _, q := range []string{`health AND`, `NEAR(`, `"health`, `path:health`, `health OR NOT`, `(health`} {
		if _, err := s.SearchDocs(libID, q, 10000); err != nil {
			t.Errorf("SearchDocs(%q) returned error: %v", q, err)
		}
	}

	results, err := s.SearchDocs(libID, `health AND`, 10000)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for literal search, got %d", len(results))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/index/ -run 'TestBuildFTSQuery|TestSearchSpecialCharacters' -v
```
Expected: FAIL. `TestBuildFTSQuery` mismatches; `TestSearchSpecialCharacters` reports `fts5: syntax error` for at least one query.

- [ ] **Step 3: Implement term quoting**

Replace `buildFTSQuery` in `internal/index/search.go` with:

```go
// buildFTSQuery transforms a plain text query into an FTS5 OR query in which
// every whitespace-separated term is quoted as an FTS5 string, so operators,
// parentheses and column filters typed by the caller are matched literally.
// "database configuration" → `"database" OR "configuration"`
// Empty input returns "".
func buildFTSQuery(input string) string {
	words := strings.Fields(input)
	if len(words) == 0 {
		return ""
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " OR ")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/index/ -v
```
Expected: PASS for the whole package (ranking and budget tests must still pass).

- [ ] **Step 5: Write the failing LIKE-escape test**

Create or append to `internal/index/store_test.go` (package `index`):

```go
func TestFindLibrariesEscapesWildcards(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore error: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	for _, name := range []string{"angular", "spring-boot", "my_lib"} {
		if _, err := s.UpsertLibrary(Library{Name: name, Repo: "r/" + name, Ref: "main", CommitSHA: "x", FetchedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("UpsertLibrary %s: %v", name, err)
		}
	}

	cases := []struct {
		query string
		want  int
	}{
		{"%", 0},        // literal percent matches nothing
		{"_ng", 0},      // underscore is not a single-char wildcard
		{"my_lib", 1},   // literal underscore still matches
		{"ng", 1},       // plain substring works
		{"", 3},         // empty query matches everything
	}
	for _, tc := range cases {
		libs, err := s.FindLibraries(tc.query)
		if err != nil {
			t.Fatalf("FindLibraries(%q) error: %v", tc.query, err)
		}
		if len(libs) != tc.want {
			t.Errorf("FindLibraries(%q) = %d libraries, want %d", tc.query, len(libs), tc.want)
		}
	}
}
```
Ensure the file imports `testing` and `time`.

- [ ] **Step 6: Run it to verify it fails**

```bash
go test ./internal/index/ -run TestFindLibrariesEscapesWildcards -v
```
Expected: FAIL on `"%"` (3 instead of 0) and `"_ng"` (1 instead of 0).

- [ ] **Step 7: Implement LIKE escaping**

In `internal/index/store.go`, add above `FindLibraries`:

```go
// escapeLike escapes the LIKE wildcard characters so that user input is
// matched literally. Must be used with `ESCAPE '\'`.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
```

Change the query and argument in `FindLibraries`:

```go
	const q = `SELECT id, name, repo, ref, commit_sha, fetched_at FROM libraries WHERE name LIKE ? ESCAPE '\' ORDER BY name`
	rows, err := s.db.Query(q, "%"+escapeLike(query)+"%")
```
Add `"strings"` to the imports of `store.go`.

- [ ] **Step 8: Run the package tests**

```bash
go test ./internal/index/ -v && go build ./... && go vet ./...
```
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/index/search.go internal/index/store.go internal/index/search_test.go internal/index/store_test.go
git commit -m "fix: quote FTS5 terms and escape LIKE wildcards in search queries

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U8jnsQkU3tuNSH9Cdh3wdd"
```

---

### Task 3: Propagate context through the store read methods

**Files:**
- Modify: `internal/index/store.go` (`GetLibrary`, `ListLibraries`, `FindLibraries`, `Ready`)
- Modify: `internal/index/search.go` (`SearchDocs`)
- Modify: `internal/mcp/tools.go` (pass `ctx`), `internal/mcp/server.go:82` (`s.store.Ready(r.Context())`)
- Modify: `internal/source/fetcher.go:54` (`f.store.GetLibrary(ctx, libName)`)
- Modify: `cmd/docserve/main.go` (`cmdList`, `cmdSearch`: `context.Background()`)
- Test: `internal/index/search_test.go`, `internal/index/store_test.go`, `internal/mcp/tools_test.go`, `internal/mcp/server_test.go`, and any `internal/source/*_test.go` that call these methods.

**Interfaces:**
- Produces:
  ```go
  func (s *Store) GetLibrary(ctx context.Context, name string) (*Library, error)
  func (s *Store) ListLibraries(ctx context.Context) ([]Library, error)
  func (s *Store) FindLibraries(ctx context.Context, query string) ([]Library, error)
  func (s *Store) SearchDocs(ctx context.Context, libraryID int64, query string, maxTokens int) ([]SearchResult, error)
  func (s *Store) Ready(ctx context.Context) bool
  ```

- [ ] **Step 1: Write the failing cancellation test**

Append to `internal/index/search_test.go`:

```go
func TestSearchDocsCancelledContext(t *testing.T) {
	s, libID := testLibrary(t)
	if err := s.ReplaceChunks(libID, []Chunk{{Path: "a.md", Breadcrumb: "A", Content: "alpha beta"}}); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.SearchDocs(ctx, libID, "alpha", 1000); err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if _, err := s.ListLibraries(ctx); err == nil {
		t.Fatal("expected error from cancelled context on ListLibraries, got nil")
	}
}
```
Add `"context"` to the test file imports.

- [ ] **Step 2: Run it to verify it fails to compile**

```bash
go test ./internal/index/ -run TestSearchDocsCancelledContext
```
Expected: compile error `too many arguments in call to s.SearchDocs`.

- [ ] **Step 3: Change the store signatures**

In `internal/index/store.go` add `"context"` to imports and change:

```go
func (s *Store) GetLibrary(ctx context.Context, name string) (*Library, error) {
	const q = `SELECT id, name, repo, ref, commit_sha, fetched_at FROM libraries WHERE name = ?`
	row := s.db.QueryRowContext(ctx, q, name)
```
```go
func (s *Store) ListLibraries(ctx context.Context) ([]Library, error) {
	const q = `SELECT id, name, repo, ref, commit_sha, fetched_at FROM libraries ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q)
```
```go
func (s *Store) Ready(ctx context.Context) bool {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM libraries`).Scan(&count)
	return err == nil && count > 0
}
```
```go
func (s *Store) FindLibraries(ctx context.Context, query string) ([]Library, error) {
	const q = `SELECT id, name, repo, ref, commit_sha, fetched_at FROM libraries WHERE name LIKE ? ESCAPE '\' ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q, "%"+escapeLike(query)+"%")
```

In `internal/index/search.go` add `"context"` and change:

```go
func (s *Store) SearchDocs(ctx context.Context, libraryID int64, query string, maxTokens int) ([]SearchResult, error) {
	...
	rows, err := s.db.QueryContext(ctx, q, libraryID, ftsQuery)
```

- [ ] **Step 4: Fix every caller**

```bash
go build ./... 2>&1; go vet ./... 2>&1
```
Fix each reported call site:
- `internal/mcp/tools.go`: `h.Store.ListLibraries(ctx)`, `h.Store.FindLibraries(ctx, query)`, `h.Store.GetLibrary(ctx, library)`, `h.Store.SearchDocs(ctx, lib.ID, query, maxTokens)`.
- `internal/mcp/server.go`: `if s.store.Ready(r.Context()) {`.
- `internal/source/fetcher.go:54`: `f.store.GetLibrary(ctx, libName)` (a `ctx` is already in scope in that function).
- `cmd/docserve/main.go` `cmdList`: `store.ListLibraries(context.Background())`; `cmdSearch`: `store.GetLibrary(context.Background(), libraryName)` and `store.SearchDocs(context.Background(), lib.ID, query, *maxTokens)`. `context` is already imported in `main.go`.
- Test files: add `context.Background()` (or the existing `ctx`) as the first argument everywhere `go vet` complains, including `internal/index/store_test.go` from Task 2.

- [ ] **Step 5: Run everything**

```bash
go build ./... && go vet ./... && go test ./... && go test -tags=integration ./...
```
Expected: all `ok`, including the new cancellation test.

- [ ] **Step 6: Commit**

```bash
git add -A internal cmd
git commit -m "fix: propagate context into store read queries

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U8jnsQkU3tuNSH9Cdh3wdd"
```

---

### Task 4: Report truncation from SearchDocs

**Files:**
- Modify: `internal/index/store.go` (add `SearchOutput` type next to `SearchResult`)
- Modify: `internal/index/search.go` (`SearchDocs` return type, row limit detection)
- Modify: `cmd/docserve/main.go` (`cmdSearch`)
- Modify: `internal/mcp/tools.go` (`GetLibraryDocs` uses `.Results`; the truncation notice text is added in Task 5)
- Test: `internal/index/search_test.go`

**Interfaces:**
- Produces:
  ```go
  const maxSearchRows = 50
  type SearchOutput struct {
      Results   []SearchResult
      Truncated bool // more matching chunks existed beyond the row limit or token budget
  }
  func (s *Store) SearchDocs(ctx context.Context, libraryID int64, query string, maxTokens int) (SearchOutput, error)
  ```

- [ ] **Step 1: Write the failing tests**

In `internal/index/search_test.go`, change `TestSearchTokenBudget` so the final assertions read:

```go
	out, err := s.SearchDocs(context.Background(), libID, "budget test keyword", 100)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("expected at least one result")
	}
	if len(out.Results) > 5 {
		t.Errorf("expected <=5 results with maxTokens=100, got %d", len(out.Results))
	}
	if !out.Truncated {
		t.Error("expected Truncated=true when the budget cuts results")
	}
```

Append:

```go
func TestSearchRowLimit(t *testing.T) {
	s, libID := testLibrary(t)

	chunks := make([]Chunk, maxSearchRows+10)
	for i := range chunks {
		chunks[i] = Chunk{
			Path:       fmt.Sprintf("docs/page%03d.md", i),
			Breadcrumb: fmt.Sprintf("Page %d", i),
			Content:    fmt.Sprintf("rowlimit keyword number %03d", i),
		}
	}
	if err := s.ReplaceChunks(libID, chunks); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}

	out, err := s.SearchDocs(context.Background(), libID, "rowlimit", 1_000_000)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(out.Results) != maxSearchRows {
		t.Errorf("expected %d results, got %d", maxSearchRows, len(out.Results))
	}
	if !out.Truncated {
		t.Error("expected Truncated=true when more rows exist than the row limit")
	}
}

func TestSearchNotTruncated(t *testing.T) {
	s, libID := testLibrary(t)
	if err := s.ReplaceChunks(libID, []Chunk{
		{Path: "a.md", Breadcrumb: "A", Content: "small corpus alpha"},
		{Path: "b.md", Breadcrumb: "B", Content: "small corpus beta"},
	}); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}

	out, err := s.SearchDocs(context.Background(), libID, "corpus", 10000)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(out.Results) != 2 || out.Truncated {
		t.Errorf("expected 2 results and Truncated=false, got %d results, Truncated=%v", len(out.Results), out.Truncated)
	}
}
```
Update the other tests in the file (`TestSearchRanking`, `TestSearchNoResults`, `TestSearchSpecialCharacters`, `TestSearchDocsCancelledContext`) to read `out.Results` where they used `results`.

- [ ] **Step 2: Run to verify compile failure**

```bash
go test ./internal/index/ -run 'TestSearch' 
```
Expected: compile errors (`out.Results undefined`, `undefined: maxSearchRows`).

- [ ] **Step 3: Implement**

In `internal/index/store.go`, after `SearchResult`:

```go
// SearchOutput is the result of a documentation search.
type SearchOutput struct {
	Results []SearchResult
	// Truncated is true when more matching chunks existed but were dropped
	// because of the row limit or the token budget.
	Truncated bool
}
```

Replace `SearchDocs` in `internal/index/search.go` with:

```go
// maxSearchRows caps the number of chunks considered for a single search.
const maxSearchRows = 50

// SearchDocs performs an FTS5 search using BM25 ranking and returns results
// within the token budget (approximated as len(content)/4 tokens). The first
// result is always returned even if it exceeds the budget. Truncated is set
// when matching chunks were left out.
func (s *Store) SearchDocs(ctx context.Context, libraryID int64, query string, maxTokens int) (SearchOutput, error) {
	var out SearchOutput

	ftsQuery := buildFTSQuery(query)
	if ftsQuery == "" {
		return out, nil
	}

	const q = `
		SELECT path, breadcrumb, content, bm25(chunks, 0.0, 1.5, 2.0, 1.0) AS score
		FROM chunks
		WHERE library_id = ? AND chunks MATCH ?
		ORDER BY score ASC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, libraryID, ftsQuery, maxSearchRows+1)
	if err != nil {
		return out, fmt.Errorf("fts search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tokensUsed := 0
	for rows.Next() {
		if len(out.Results) == maxSearchRows {
			out.Truncated = true
			break
		}
		var r SearchResult
		if err := rows.Scan(&r.Path, &r.Breadcrumb, &r.Content, &r.Score); err != nil {
			return SearchOutput{}, fmt.Errorf("scan search result: %w", err)
		}
		tokens := len(r.Content) / 4
		if tokensUsed+tokens > maxTokens && len(out.Results) > 0 {
			out.Truncated = true
			break
		}
		tokensUsed += tokens
		out.Results = append(out.Results, r)
	}
	if err := rows.Err(); err != nil {
		return SearchOutput{}, fmt.Errorf("search rows: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 4: Fix callers**

`cmd/docserve/main.go` `cmdSearch`:

```go
	out, err := store.SearchDocs(context.Background(), lib.ID, query, *maxTokens)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error searching: %v\n", err)
		os.Exit(1)
	}

	if len(out.Results) == 0 {
		fmt.Printf("No results for %q in library %q.\n", query, libraryName)
		return
	}

	for i, r := range out.Results {
		fmt.Printf("--- Result %d: %s", i+1, r.Path)
		if r.Breadcrumb != "" {
			fmt.Printf(" (%s)", r.Breadcrumb)
		}
		fmt.Println()
		fmt.Println(r.Content)
		fmt.Println()
	}
	if out.Truncated {
		fmt.Println("(more results omitted; refine the query or raise --max-tokens)")
	}
```

`internal/mcp/tools.go` `GetLibraryDocs`: rename `results` to `out` and use `out.Results` in the length check, the header count and the loop. Do not add the notice text yet (Task 5).

- [ ] **Step 5: Run everything**

```bash
go build ./... && go vet ./... && go test ./... && go test -tags=integration ./...
```
Expected: all `ok`.

- [ ] **Step 6: Commit**

```bash
git add -A internal cmd
git commit -m "feat: report truncation from documentation search

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U8jnsQkU3tuNSH9Cdh3wdd"
```

---

### Task 5: Strict tool arguments, object-shaped structured output, truncation notice

**Files:**
- Modify: `internal/mcp/tools.go` (whole file)
- Test: `internal/mcp/tools_test.go`

**Interfaces:**
- Produces (used by Task 7's `WithOutputSchema`):
  ```go
  type libEntry struct { Name, Repo, Ref, CommitSHA, FetchedAt string }   // json: name, repo, ref, commit_sha, fetched_at
  type libraryList struct { Libraries []libEntry `json:"libraries"` }
  type libMatch struct { Name, Ref string }                                 // json: name, ref
  const defaultMaxTokens = 5000
  ```
- Consumes: `index.SearchOutput` from Task 4.

- [ ] **Step 1: Write the failing tests**

In `internal/mcp/tools_test.go`, update `TestListLibraries` so the structured assertion reads:

```go
	list, ok := result.StructuredContent.(libraryList)
	if !ok {
		t.Fatalf("structuredContent type = %T, want libraryList", result.StructuredContent)
	}
	if len(list.Libraries) != 2 {
		t.Fatalf("expected 2 libraries, got %d", len(list.Libraries))
	}
	if list.Libraries[0].Name != "angular" || list.Libraries[1].Name != "spring-boot" {
		t.Errorf("unexpected order: %+v", list.Libraries)
	}
	if list.Libraries[0].Repo != "github.com/angular/angular" || list.Libraries[0].CommitSHA != "def456" {
		t.Errorf("repo/commit_sha not populated: %+v", list.Libraries[0])
	}
```
(Read the existing test first and keep its text-fallback assertion; it should check the fallback JSON contains `"libraries"`.)

Append:

```go
func TestResolveLibraryMissingQuery(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	for name, args := range map[string]map[string]any{
		"absent": {},
		"empty":  {"query": "   "},
		"number": {"query": 42},
	} {
		result, err := h.ResolveLibrary(context.Background(), makeRequest(args))
		if err != nil {
			t.Fatalf("%s: error: %v", name, err)
		}
		if !result.IsError {
			t.Errorf("%s: expected IsError=true", name)
		}
		if text := result.Content[0].(mcplib.TextContent).Text; !strings.Contains(text, "query") {
			t.Errorf("%s: error text should mention the argument, got %q", name, text)
		}
	}
}

func TestGetLibraryDocsMissingArgs(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	cases := map[string]map[string]any{
		"no library":     {"query": "actuator"},
		"no query":       {"library": "spring-boot"},
		"empty query":    {"library": "spring-boot", "query": ""},
		"zero tokens":    {"library": "spring-boot", "query": "actuator", "max_tokens": 0},
		"negative tokens": {"library": "spring-boot", "query": "actuator", "max_tokens": -5},
	}
	for name, args := range cases {
		result, err := h.GetLibraryDocs(context.Background(), makeRequest(args))
		if err != nil {
			t.Fatalf("%s: error: %v", name, err)
		}
		if !result.IsError {
			t.Errorf("%s: expected IsError=true, got %q", name, result.Content[0].(mcplib.TextContent).Text)
		}
	}
}

func TestGetLibraryDocsTruncationNotice(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	lib, err := store.GetLibrary(context.Background(), "spring-boot")
	if err != nil {
		t.Fatalf("GetLibrary: %v", err)
	}
	chunks := make([]index.Chunk, 60)
	for i := range chunks {
		chunks[i] = index.Chunk{Path: "docs/p.md", Breadcrumb: "P", Content: "trunc keyword " + strings.Repeat("x", 200)}
	}
	if err := store.ReplaceChunks(lib.ID, chunks); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	result, err := h.GetLibraryDocs(context.Background(), makeRequest(map[string]any{"library": "spring-boot", "query": "trunc", "max_tokens": 120}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "max_tokens") {
		t.Errorf("expected truncation notice mentioning max_tokens, got:\n%s", text)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/mcp/ -run 'TestListLibraries|TestResolveLibraryMissingQuery|TestGetLibraryDocsMissingArgs|TestGetLibraryDocsTruncationNotice' -v
```
Expected: compile error (`undefined: libraryList`) or FAIL.

- [ ] **Step 3: Rewrite `internal/mcp/tools.go`**

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/codanael/docserve/internal/index"
)

// defaultMaxTokens is the token budget used by get-library-docs when the
// caller does not provide max_tokens.
const defaultMaxTokens = 5000

// ToolHandlers holds the store and provides MCP tool handler methods.
type ToolHandlers struct {
	Store *index.Store
}

// structuredResult builds a CallToolResult carrying v as structuredContent
// plus the same JSON as a text fallback for clients without structured
// output support.
func structuredResult(v any) *mcplib.CallToolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return mcplib.NewToolResultError("marshal result: " + err.Error())
	}
	return mcplib.NewToolResultStructured(v, string(data))
}

// requireText returns the named string argument, or an error result when it
// is missing, not a string, or blank.
func requireText(req mcplib.CallToolRequest, key string) (string, *mcplib.CallToolResult) {
	v, err := req.RequireString(key)
	if err != nil || strings.TrimSpace(v) == "" {
		return "", mcplib.NewToolResultError(fmt.Sprintf("argument %q is required and must be a non-empty string", key))
	}
	return v, nil
}

// libEntry is one row of the list-libraries output.
type libEntry struct {
	Name      string `json:"name"`
	Repo      string `json:"repo"`
	Ref       string `json:"ref"`
	CommitSHA string `json:"commit_sha"`
	FetchedAt string `json:"fetched_at"`
}

// libraryList is the structured output of list-libraries.
type libraryList struct {
	Libraries []libEntry `json:"libraries"`
}

// ListLibraries returns all indexed libraries.
func (h *ToolHandlers) ListLibraries(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	libs, err := h.Store.ListLibraries(ctx)
	if err != nil {
		return mcplib.NewToolResultError("failed to list libraries: " + err.Error()), nil
	}

	out := libraryList{Libraries: make([]libEntry, 0, len(libs))}
	for _, lib := range libs {
		out.Libraries = append(out.Libraries, libEntry{
			Name:      lib.Name,
			Repo:      lib.Repo,
			Ref:       lib.Ref,
			CommitSHA: lib.CommitSHA,
			FetchedAt: lib.FetchedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return structuredResult(out), nil
}

// libMatch is the structured output of resolve-library.
type libMatch struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
}

// ResolveLibrary finds a library by name query and returns the first match.
func (h *ToolHandlers) ResolveLibrary(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	query, errResult := requireText(req, "query")
	if errResult != nil {
		return errResult, nil
	}

	libs, err := h.Store.FindLibraries(ctx, query)
	if err != nil {
		return mcplib.NewToolResultError("failed to find libraries: " + err.Error()), nil
	}
	if len(libs) == 0 {
		return mcplib.NewToolResultError(fmt.Sprintf("no library found matching %q; call list-libraries to see what is indexed", query)), nil
	}

	return structuredResult(libMatch{Name: libs[0].Name, Ref: libs[0].Ref}), nil
}

// GetLibraryDocs searches a library's documentation and returns human-readable markdown.
func (h *ToolHandlers) GetLibraryDocs(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	library, errResult := requireText(req, "library")
	if errResult != nil {
		return errResult, nil
	}
	query, errResult := requireText(req, "query")
	if errResult != nil {
		return errResult, nil
	}
	maxTokens := req.GetInt("max_tokens", defaultMaxTokens)
	if maxTokens < 1 {
		return mcplib.NewToolResultError("argument \"max_tokens\" must be a positive integer"), nil
	}

	lib, err := h.Store.GetLibrary(ctx, library)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("library %q not found; use resolve-library or list-libraries to get the exact name", library)), nil
	}

	out, err := h.Store.SearchDocs(ctx, lib.ID, query, maxTokens)
	if err != nil {
		return mcplib.NewToolResultError("search failed: " + err.Error()), nil
	}

	if len(out.Results) == 0 {
		return mcplib.NewToolResultText(fmt.Sprintf("No results found for %q in %s (%s).", query, lib.Name, lib.Ref)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s (%s) — %d results\n\n", lib.Name, lib.Ref, len(out.Results))
	for i, r := range out.Results {
		if i > 0 {
			b.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&b, "## %s\n", r.Breadcrumb)
		fmt.Fprintf(&b, "_Source: %s_\n\n", r.Path)
		b.WriteString(r.Content)
		b.WriteByte('\n')
	}
	if out.Truncated {
		b.WriteString("\n---\n\n_More matching chunks were omitted by the token budget or the result limit. Refine the query or raise max_tokens to see them._\n")
	}

	return mcplib.NewToolResultText(b.String()), nil
}
```

- [ ] **Step 4: Run the package tests and fix any other test that referenced the old shapes**

```bash
go test ./internal/mcp/ -v && go test -tags=integration ./... && go vet ./...
```
Expected: PASS. If `TestListLibraries`'s text-fallback assertion looks for a top-level array, change it to `strings.Contains(text, "\"libraries\"")`.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/tools_test.go
git commit -m "fix: enforce required tool arguments and report search truncation to the model

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U8jnsQkU3tuNSH9Cdh3wdd"
```

---

### Task 6: Stateless transport, Origin validation, body limit, server timeouts

**Files:**
- Create: `internal/mcp/middleware.go`
- Create: `internal/mcp/middleware_test.go`
- Modify: `internal/mcp/server.go` (`Options`, `NewServer` signature, `Handler`)
- Modify: `internal/mcp/server_test.go`, `integration_test.go` (new `NewServer` signature)
- Modify: `internal/config/config.go` (`AllowedOrigins`), `internal/config/config_test.go`
- Modify: `cmd/docserve/main.go` (`cmdServe`)

**Interfaces:**
- Produces:
  ```go
  // internal/mcp
  type Options struct { AllowedOrigins []string }
  func NewServer(store *index.Store, version string, opts Options) *Server
  func originCheck(allowed []string, next http.Handler) http.Handler
  const maxBodyBytes = 1 << 20
  // internal/config
  Config.AllowedOrigins []string `yaml:"allowed_origins"`
  ```

- [ ] **Step 1: Write the failing middleware tests**

Create `internal/mcp/middleware_test.go`:

```go
package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestOriginCheck(t *testing.T) {
	h := originCheck([]string{"https://app.example.com"}, okHandler())

	cases := []struct {
		origin string
		want   int
	}{
		{"", http.StatusOK},
		{"http://localhost:3000", http.StatusOK},
		{"http://127.0.0.1", http.StatusOK},
		{"http://[::1]:8080", http.StatusOK},
		{"https://app.example.com", http.StatusOK},
		{"HTTPS://APP.EXAMPLE.COM", http.StatusOK},
		{"https://evil.example", http.StatusForbidden},
		{"http://localhost.evil.example", http.StatusForbidden},
		{"null", http.StatusForbidden},
		{"not a url", http.StatusForbidden},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("Origin %q: got %d, want %d", tc.origin, w.Code, tc.want)
		}
	}
}
```

Append to `internal/mcp/server_test.go`:

```go
func TestMCPServerToolsListWithoutSession(t *testing.T) {
	store := setupTestStore(t)
	handler := NewServer(store, "test", Options{}).Handler()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without Mcp-Session-Id, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Mcp-Session-Id") != "" {
		t.Errorf("stateless server must not issue a session id")
	}
	var resp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Result.Tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(resp.Result.Tools))
	}
}

func TestMCPServerRejectsForeignOrigin(t *testing.T) {
	store := setupTestStore(t)
	handler := NewServer(store, "test", Options{}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}

	// Health endpoints are not subject to the Origin check.
	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("healthz with foreign origin: expected 200, got %d", w.Code)
	}
}

func TestMCPServerBodyLimit(t *testing.T) {
	store := setupTestStore(t)
	handler := NewServer(store, "test", Options{}).Handler()

	huge := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("x", 2<<20) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(huge))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Errorf("expected oversized body to be rejected, got 200")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/mcp/ -run 'TestOriginCheck|TestMCPServerToolsListWithoutSession|TestMCPServerRejectsForeignOrigin|TestMCPServerBodyLimit'
```
Expected: compile errors (`undefined: originCheck`, `undefined: Options`).

- [ ] **Step 3: Create `internal/mcp/middleware.go`**

```go
package mcp

import (
	"net/http"
	"net/url"
	"strings"
)

// maxBodyBytes caps the size of a single MCP request body.
const maxBodyBytes = 1 << 20

// originCheck rejects browser requests whose Origin header is neither a
// loopback origin nor in the allowed list, as required by the MCP Streamable
// HTTP transport specification. Requests without an Origin header (CLI and
// agent clients) pass through.
func originCheck(allowed []string, next http.Handler) http.Handler {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		allowedSet[normalizeOrigin(o)] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || originAllowed(origin, allowedSet) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "forbidden: origin not allowed", http.StatusForbidden)
	})
}

func originAllowed(origin string, allowed map[string]struct{}) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	if _, ok := allowed[normalizeOrigin(origin)]; ok {
		return true
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// normalizeOrigin lower-cases scheme and host and drops any path so that
// configured and received origins compare equal.
func normalizeOrigin(origin string) string {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.ToLower(strings.TrimRight(origin, "/"))
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}
```

- [ ] **Step 4: Update `internal/mcp/server.go`**

Add the `Options` type and change `NewServer` and `Handler`:

```go
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
	... (tool registration unchanged in this task) ...

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

	... (health handlers unchanged, but `s.store.Ready(r.Context())`) ...
```
Remove the `server.WithEndpointPath("/mcp")` call (it is a no-op when the transport is mounted as an `http.Handler`).

- [ ] **Step 5: Add `allowed_origins` to config**

In `internal/config/config.go`:

```go
type Config struct {
	DataDir        string         `yaml:"data_dir"`
	Listen         string         `yaml:"listen"`
	AllowedOrigins []string       `yaml:"allowed_origins"`
	Proxy          ProxyConfig    `yaml:"proxy"`
	Sources        []SourceConfig `yaml:"sources"`
}
```
Add `"net/url"` to imports and, at the top of `validate`, before the sources check:

```go
	for _, o := range cfg.AllowedOrigins {
		u, err := url.Parse(o)
		if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" {
			return fmt.Errorf("allowed_origins: %q must be scheme://host[:port] with no path", o)
		}
	}
```

Add to `internal/config/config_test.go`:

```go
func TestLoadConfigAllowedOrigins(t *testing.T) {
	content := `
allowed_origins:
  - https://app.example.com
  - http://intranet:3000
sources:
  - name: s
    provider: github
    repo: org/repo
    refs:
      - ref: main
        paths: [docs/]
`
	cfg, err := config.Load(writeTempConfig(t, content))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[0] != "https://app.example.com" {
		t.Errorf("AllowedOrigins = %v", cfg.AllowedOrigins)
	}

	bad := `
allowed_origins: ["app.example.com"]
sources:
  - name: s
    provider: github
    repo: org/repo
    refs:
      - ref: main
        paths: [docs/]
`
	if _, err := config.Load(writeTempConfig(t, bad)); err == nil {
		t.Error("expected error for origin without scheme")
	}
}
```

- [ ] **Step 6: Update `cmd/docserve/main.go` `cmdServe`**

```go
	srv := mcpsrv.NewServer(store, version, mcpsrv.Options{
		AllowedOrigins: cfg.AllowedOrigins,
	})

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		// WriteTimeout stays 0: SSE responses may be long-lived.
	}
```

- [ ] **Step 7: Fix remaining callers of `NewServer`**

```bash
go build ./... ; go vet ./... ; go vet -tags=integration ./...
```
Update every `NewServer(store, "test")` in `internal/mcp/server_test.go` and `integration_test.go` to `NewServer(store, "test", Options{})` / `mcpsrv.NewServer(store, "test", mcpsrv.Options{})`.

In `integration_test.go`, the `initialize` call now returns an empty session id; `jsonRPCWithSession` and `sendNotification` already skip the header when it is empty. Verify `sendNotification` does so; if it sets the header unconditionally, guard it with `if sessionID != ""`.

- [ ] **Step 8: Run everything**

```bash
go build ./... && go vet ./... && go test ./... && go test -tags=integration ./...
```
Expected: all `ok`.

- [ ] **Step 9: Commit**

```bash
git add internal/mcp internal/config cmd/docserve/main.go integration_test.go
git commit -m "feat: stateless MCP transport with Origin validation, body limit and server timeouts

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U8jnsQkU3tuNSH9Cdh3wdd"
```

---

### Task 7: Tool metadata, output schemas, instructions and server options

**Files:**
- Modify: `internal/mcp/server.go` (`NewServer` body)
- Test: `internal/mcp/server_test.go`

**Interfaces:**
- Consumes: `libraryList`, `libMatch` from Task 5.

- [ ] **Step 1: Write the failing tests**

Append to `internal/mcp/server_test.go`:

```go
// postMCP sends one JSON-RPC request to the handler and returns the decoded envelope.
func postMCP(t *testing.T, handler http.Handler, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, w.Body.String())
	}
	return env
}

func TestMCPServerInstructions(t *testing.T) {
	handler := NewServer(setupTestStore(t), "test", Options{}).Handler()
	env := postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	result := env["result"].(map[string]any)
	instr, _ := result["instructions"].(string)
	if !strings.Contains(instr, "resolve-library") || !strings.Contains(instr, "get-library-docs") {
		t.Errorf("instructions should describe the workflow, got %q", instr)
	}
}

func TestMCPServerToolMetadata(t *testing.T) {
	handler := NewServer(setupTestStore(t), "test", Options{}).Handler()
	env := postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	tools := env["result"].(map[string]any)["tools"].([]any)

	byName := map[string]map[string]any{}
	for _, tl := range tools {
		tm := tl.(map[string]any)
		byName[tm["name"].(string)] = tm
	}

	for _, name := range []string{"list-libraries", "resolve-library", "get-library-docs"} {
		tm, ok := byName[name]
		if !ok {
			t.Fatalf("tool %s missing", name)
		}
		if title, _ := tm["title"].(string); title == "" {
			t.Errorf("%s: missing title", name)
		}
		ann, _ := tm["annotations"].(map[string]any)
		if ann["openWorldHint"] != false {
			t.Errorf("%s: openWorldHint = %v, want false", name, ann["openWorldHint"])
		}
		if ann["readOnlyHint"] != true || ann["destructiveHint"] != false || ann["idempotentHint"] != true {
			t.Errorf("%s: unexpected annotations %v", name, ann)
		}
		in, _ := tm["inputSchema"].(map[string]any)
		if in["additionalProperties"] != false {
			t.Errorf("%s: inputSchema.additionalProperties = %v, want false", name, in["additionalProperties"])
		}
	}

	for _, name := range []string{"list-libraries", "resolve-library"} {
		out, _ := byName[name]["outputSchema"].(map[string]any)
		if out["type"] != "object" {
			t.Errorf("%s: outputSchema.type = %v, want object", name, out["type"])
		}
		props, _ := out["properties"].(map[string]any)
		if len(props) == 0 {
			t.Errorf("%s: outputSchema has no properties", name)
		}
	}
	if _, has := byName["get-library-docs"]["outputSchema"]; has {
		t.Errorf("get-library-docs returns markdown text and must not declare an outputSchema")
	}

	props := byName["get-library-docs"]["inputSchema"].(map[string]any)["properties"].(map[string]any)
	mt := props["max_tokens"].(map[string]any)
	if mt["minimum"] != float64(1) {
		t.Errorf("max_tokens.minimum = %v, want 1", mt["minimum"])
	}
}

func TestMCPServerInputValidation(t *testing.T) {
	handler := NewServer(setupTestStore(t), "test", Options{}).Handler()

	// Missing required argument → tool execution error (isError), not a JSON-RPC error.
	env := postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"resolve-library","arguments":{}}}`)
	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result envelope, got %v", env)
	}
	if result["isError"] != true {
		t.Errorf("expected isError=true, got %v", result)
	}

	// Unknown argument → rejected because additionalProperties is false.
	env = postMCP(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list-libraries","arguments":{"bogus":1}}}`)
	if result, _ := env["result"].(map[string]any); result["isError"] != true {
		t.Errorf("expected isError=true for unknown argument, got %v", env)
	}

	// Valid call still works.
	env = postMCP(t, handler, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"resolve-library","arguments":{"query":"spring"}}}`)
	if result, _ := env["result"].(map[string]any); result["isError"] == true {
		t.Errorf("valid call failed: %v", env)
	} else if sc, _ := result["structuredContent"].(map[string]any); sc["name"] != "spring-boot" {
		t.Errorf("structuredContent.name = %v", sc["name"])
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/mcp/ -run 'TestMCPServerInstructions|TestMCPServerToolMetadata|TestMCPServerInputValidation' -v
```
Expected: FAIL (no instructions, openWorldHint true, no outputSchema, additionalProperties absent).

- [ ] **Step 3: Rewrite the tool registration in `NewServer`**

Replace the body from `mcpSrv := ...` through the last `AddTool` with:

```go
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
```

Add above `NewServer`:

```go
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
```

- [ ] **Step 4: Run the package and integration tests**

```bash
go build ./... && go vet ./... && go test ./internal/mcp/ -v && go test -tags=integration ./...
```
Expected: PASS. If `mcplib.Min(1)` does not compile as a `PropertyOption` with an untyped constant, use `mcplib.Min(1.0)`; the test compares against `float64(1)` either way. If `WithOutputSchema` emits an error on stderr about the struct, check `mcp/struct_schema.go` in the module cache and adjust the struct tags; the test requires `type: object` with a non-empty `properties`.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go
git commit -m "feat: tool titles, output schemas, instructions and strict input validation

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U8jnsQkU3tuNSH9Cdh3wdd"
```

---

### Task 8: Optional bearer-token authentication

**Files:**
- Modify: `internal/mcp/middleware.go` (`bearerAuth`), `internal/mcp/middleware_test.go`
- Modify: `internal/mcp/server.go` (`Options.AuthToken`, wiring in `Handler`), `internal/mcp/server_test.go`
- Modify: `internal/config/config.go` (`AuthTokenEnv`, `AuthToken`), `internal/config/config_test.go`
- Modify: `cmd/docserve/main.go` (`cmdServe`: pass token, warn when empty)

**Interfaces:**
- Produces:
  ```go
  // internal/mcp
  Options.AuthToken string
  func bearerAuth(token string, next http.Handler) http.Handler
  // internal/config
  Config.AuthTokenEnv string `yaml:"auth_token_env"`
  Config.AuthToken    string `yaml:"-"`   // resolved from the environment by Load
  ```

- [ ] **Step 1: Write the failing tests**

Append to `internal/mcp/middleware_test.go`:

```go
func TestBearerAuth(t *testing.T) {
	h := bearerAuth("s3cret", okHandler())

	cases := []struct {
		header string
		want   int
	}{
		{"", http.StatusUnauthorized},
		{"Bearer wrong", http.StatusUnauthorized},
		{"Basic czNjcmV0", http.StatusUnauthorized},
		{"Bearer s3cret", http.StatusOK},
		{"bearer s3cret", http.StatusOK},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("Authorization %q: got %d, want %d", tc.header, w.Code, tc.want)
		}
		if tc.want == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("Authorization %q: missing WWW-Authenticate header", tc.header)
		}
	}
}

func TestBearerAuthDisabledWhenEmpty(t *testing.T) {
	h := bearerAuth("", okHandler())
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected passthrough with empty token, got %d", w.Code)
	}
}
```

Append to `internal/mcp/server_test.go`:

```go
func TestMCPServerAuthProtectsOnlyMCP(t *testing.T) {
	handler := NewServer(setupTestStore(t), "test", Options{AuthToken: "tok"}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("/mcp without token: got %d, want 401", w.Code)
	}

	req.Header.Set("Authorization", "Bearer tok")
	req.Body = io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("/mcp with token: got %d, want 200: %s", w.Code, w.Body.String())
	}

	for _, path := range []string{"/healthz", "/readyz"} {
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code == http.StatusUnauthorized {
			t.Errorf("%s must not require auth", path)
		}
	}
}
```
Add `"io"` to that file's imports.

Append to `internal/config/config_test.go`:

```go
func TestLoadConfigAuthToken(t *testing.T) {
	content := `
auth_token_env: DOCSERVE_TEST_TOKEN
sources:
  - name: s
    provider: github
    repo: org/repo
    refs:
      - ref: main
        paths: [docs/]
`
	f := writeTempConfig(t, content)

	t.Setenv("DOCSERVE_TEST_TOKEN", "")
	if _, err := config.Load(f); err == nil {
		t.Error("expected error when auth_token_env variable is empty")
	}

	t.Setenv("DOCSERVE_TEST_TOKEN", "abc")
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AuthToken != "abc" {
		t.Errorf("AuthToken = %q, want abc", cfg.AuthToken)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/mcp/ ./internal/config/ 2>&1 | head
```
Expected: compile errors (`undefined: bearerAuth`, unknown field `AuthToken`).

- [ ] **Step 3: Implement the middleware**

Append to `internal/mcp/middleware.go` (add `"crypto/subtle"` to imports):

```go
// bearerAuth requires `Authorization: Bearer <token>` on every request when
// token is non-empty. With an empty token it is a no-op.
func bearerAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "bearer "
		got := r.Header.Get("Authorization")
		if len(got) > len(prefix) && strings.EqualFold(got[:len(prefix)], prefix) &&
			subtle.ConstantTimeCompare([]byte(got[len(prefix):]), want) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="docserve"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}
```

- [ ] **Step 4: Wire it in `server.go`**

Add to `Options`:
```go
	// AuthToken, when non-empty, is required as a bearer token on /mcp.
	AuthToken string
```
In `Handler`, build the chain as:
```go
	var mcpHandler http.Handler = http.MaxBytesHandler(s.httpServer, maxBodyBytes)
	mcpHandler = bearerAuth(s.opts.AuthToken, mcpHandler)
	mcpHandler = originCheck(s.opts.AllowedOrigins, mcpHandler)
	mux.Handle("/mcp", mcpHandler)
```

- [ ] **Step 5: Config and main**

`internal/config/config.go`:
```go
type Config struct {
	DataDir        string         `yaml:"data_dir"`
	Listen         string         `yaml:"listen"`
	AllowedOrigins []string       `yaml:"allowed_origins"`
	AuthTokenEnv   string         `yaml:"auth_token_env"`
	AuthToken      string         `yaml:"-"`
	Proxy          ProxyConfig    `yaml:"proxy"`
	Sources        []SourceConfig `yaml:"sources"`
}
```
In `Load`, after applying defaults and before `validate`:
```go
	if cfg.AuthTokenEnv != "" {
		cfg.AuthToken = os.Getenv(cfg.AuthTokenEnv)
		if cfg.AuthToken == "" {
			return nil, fmt.Errorf("auth_token_env: environment variable %q is not set or empty", cfg.AuthTokenEnv)
		}
	}
```

`cmd/docserve/main.go` `cmdServe`:
```go
	if cfg.AuthToken == "" {
		log.Printf("warning: no auth_token_env configured; the /mcp endpoint accepts unauthenticated requests")
	}
	srv := mcpsrv.NewServer(store, version, mcpsrv.Options{
		AllowedOrigins: cfg.AllowedOrigins,
		AuthToken:      cfg.AuthToken,
	})
```

- [ ] **Step 6: Run everything**

```bash
go build ./... && go vet ./... && go test ./... && go test -tags=integration ./...
```
Expected: all `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/mcp internal/config cmd/docserve/main.go
git commit -m "feat: optional bearer-token authentication for the MCP endpoint

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U8jnsQkU3tuNSH9Cdh3wdd"
```

---

### Task 9: Tool-call and error logging via mcp-go hooks

**Files:**
- Modify: `internal/mcp/server.go` (`NewServer`: hooks)
- Test: `internal/mcp/server_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/mcp/server_test.go` (add `"bytes"`, `"log"`, `"os"` to imports):

```go
func TestMCPServerLogsToolCalls(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	handler := NewServer(setupTestStore(t), "test", Options{}).Handler()
	postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list-libraries","arguments":{}}}`)
	postMCP(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"resolve-library","arguments":{"query":"nope"}}}`)

	logs := buf.String()
	if !strings.Contains(logs, "tool=list-libraries") || !strings.Contains(logs, "error=false") {
		t.Errorf("expected success log line, got:\n%s", logs)
	}
	if !strings.Contains(logs, "tool=resolve-library") || !strings.Contains(logs, "error=true") {
		t.Errorf("expected error log line, got:\n%s", logs)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/mcp/ -run TestMCPServerLogsToolCalls -v
```
Expected: FAIL (no log lines).

- [ ] **Step 3: Implement**

In `internal/mcp/server.go`, add `"context"` and `"log"` to imports, add the option `server.WithHooks(newHooks())` to `server.NewMCPServer(...)`, and add:

```go
// newHooks logs every tool call outcome and every JSON-RPC level error with
// the stdlib logger used by the rest of the binary.
func newHooks() *server.Hooks {
	hooks := &server.Hooks{}
	hooks.AddAfterCallTool(func(_ context.Context, _ any, req *mcplib.CallToolRequest, result any) {
		isErr := false
		switch r := result.(type) {
		case *mcplib.CallToolResult:
			isErr = r != nil && r.IsError
		case mcplib.CallToolResult:
			isErr = r.IsError
		}
		name := ""
		if req != nil {
			name = req.Params.Name
		}
		log.Printf("mcp tools/call tool=%s error=%t", name, isErr)
	})
	hooks.AddOnError(func(_ context.Context, id any, method mcplib.MCPMethod, _ any, err error) {
		log.Printf("mcp %s id=%v error: %v", method, id, err)
	})
	return hooks
}
```
If `AddAfterCallTool`'s callback signature differs in v1.1.0, open `/home/anael/go/pkg/mod/github.com/mark3labs/mcp-go@v1.1.0/server/hooks.go` (`OnAfterCallToolFunc`) and match it exactly.

- [ ] **Step 4: Run everything**

```bash
go build ./... && go vet ./... && go test ./... && go test -tags=integration ./...
```
Expected: all `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go
git commit -m "feat: log MCP tool calls and protocol errors

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U8jnsQkU3tuNSH9Cdh3wdd"
```

---

### Task 10: Documentation and changelog

**Files:**
- Modify: `README.md` (Configuration + MCP Client Configuration sections)
- Modify: `docserve.example.yaml`
- Modify: `docs/testing-guide.md`
- Modify: `CLAUDE.md` (Key Design Decisions, Testing)
- Modify: `CHANGELOG.md`

- [ ] **Step 1: `docserve.example.yaml`**

After the `listen:` block add:

```yaml
# Optional: name of an environment variable holding a bearer token.
# When set, every request to /mcp must carry "Authorization: Bearer <token>".
# Loading fails if the variable is unset or empty. /healthz and /readyz stay open.
# auth_token_env: DOCSERVE_TOKEN

# Optional: browser origins allowed to call /mcp in addition to loopback
# origins (http://localhost, http://127.0.0.1, http://[::1], any port).
# Requests without an Origin header (CLI and agent clients) are always accepted.
# allowed_origins:
#   - https://my-ai-app.example.com
```

- [ ] **Step 2: README.md**

In `## Configuration`, after the sentence about credentials in env vars, add:

```markdown
Two optional top-level keys control the MCP endpoint:

- `auth_token_env` -- name of an environment variable holding a bearer token. When set, `/mcp` requires `Authorization: Bearer <token>`. Without it the endpoint is open; only do that on a trusted network.
- `allowed_origins` -- browser origins allowed to call `/mcp` besides loopback origins. Requests without an `Origin` header are always accepted; foreign origins get `403`.
```

Replace the `### MCP Client Configuration` section with:

```markdown
### MCP Client Configuration

docserve speaks MCP specification revision 2026-07-28 and also serves clients on the 2025-11-25, 2025-06-18 and 2025-03-26 revisions through the same `/mcp` endpoint. The transport is stateless: no `Mcp-Session-Id` is issued or required.

Point your MCP client at the server's `/mcp` endpoint:

```json
{
  "mcpServers": {
    "docserve": {
      "type": "streamable-http",
      "url": "http://localhost:8080/mcp",
      "headers": {
        "Authorization": "Bearer ${DOCSERVE_TOKEN}"
      }
    }
  }
}
```

Drop the `headers` block when `auth_token_env` is not configured.

Tools:

| Tool | Purpose | Output |
|---|---|---|
| `list-libraries` | Browse every indexed library | structured `{"libraries": [...]}` + JSON text |
| `resolve-library` | Exact name from a partial name | structured `{"name", "ref"}` + JSON text |
| `get-library-docs` | BM25 search inside one library | markdown chunks, with a truncation notice when results were cut |

All tools are read-only and idempotent. Request bodies are capped at 1 MiB.
```

- [ ] **Step 3: `docs/testing-guide.md`**

- Replace the sentence `After initialization, every request must include the \`Mcp-Session-Id\` header.` with:
  ```
  The transport is stateless: no `Mcp-Session-Id` is issued and none is required. The `initialize` step below is what legacy (pre-2026-07-28) clients send; modern clients call `server/discover` instead. Both work without prior setup. If `auth_token_env` is configured, add `-H "Authorization: Bearer $DOCSERVE_TOKEN"` to every `/mcp` request.
  ```
- In step 2 (`Initialize a session`), rename the heading to `### 2. Initialize (legacy handshake, optional)` and keep the curl but delete the `SESSION=$(...)` capture and the `echo "Session: $SESSION"` line; keep it as a plain `curl -s ... | jq .`.
- Remove every `-H "Mcp-Session-Id: $SESSION" \` line from steps 3–7.
- Replace `### 8. Close the session` and its DELETE curl with:
  ````markdown
  ### 8. Discover (2026-07-28 clients)

  ```bash
  curl -s -X POST http://localhost:8080/mcp \
    -H "Content-Type: application/json" \
    -H "Accept: application/json, text/event-stream" \
    -H "MCP-Protocol-Version: 2026-07-28" \
    -H "Mcp-Method: server/discover" \
    -d '{
      "jsonrpc": "2.0",
      "id": 9,
      "method": "server/discover",
      "params": {
        "_meta": {
          "io.modelcontextprotocol/protocolVersion": "2026-07-28",
          "io.modelcontextprotocol/clientCapabilities": {}
        }
      }
    }' | jq .
  ```
  ````
- After the `## MCP Inspector` section append:
  ````markdown
  ## Conformance Suite

  The official conformance tests validate the transport and tool behaviour against the specification:

  ```bash
  npx -y @modelcontextprotocol/conformance server --url http://127.0.0.1:8080/mcp
  npx -y @modelcontextprotocol/conformance server --url http://127.0.0.1:8080/mcp --spec-version 2025-11-25
  ```
  ````

- [ ] **Step 4: CLAUDE.md**

In `## Key Design Decisions` replace the `MCP tool output` bullet with these bullets:

```markdown
- **MCP transport is stateless** (`server.WithStateLess(true)`): no `Mcp-Session-Id`; mcp-go v1.1.0 serves 2026-07-28 (`server/discover`) and legacy `initialize` clients on the same `/mcp` endpoint.
- **HTTP hardening lives in `internal/mcp/middleware.go`**: Origin check (loopback + `allowed_origins`), optional bearer auth (`auth_token_env`), 1 MiB body cap. Health endpoints bypass all of it.
- **Tools are strict**: `WithInputSchemaValidation` + `WithStrictInputSchemaDefault`; handlers use `RequireString`. All tools declare `readOnly/idempotent=true`, `destructive/openWorld=false`, a `title`, and (for the two JSON tools) an `outputSchema`.
- **MCP tool output**: `list-libraries` returns `{"libraries": [...]}` and `resolve-library` returns `{"name","ref"}` as `structuredContent` + JSON text fallback. `get-library-docs` returns markdown text and appends a truncation notice when `SearchOutput.Truncated` is set.
- **Search queries are escaped**: FTS5 terms are quoted (`buildFTSQuery`), LIKE wildcards escaped (`escapeLike`). Store read methods take a `context.Context`.
```

In `## Testing` add a bullet:
```markdown
- Conformance: `npx -y @modelcontextprotocol/conformance server --url http://127.0.0.1:8080/mcp`
```

- [ ] **Step 5: CHANGELOG.md**

Insert after the intro paragraph, before `## [1.0.0]`:

```markdown
## [Unreleased]

### Added
- Optional bearer-token authentication for `/mcp` (`auth_token_env`)
- Origin validation with `allowed_origins`; foreign browser origins get 403
- Tool `title`, `outputSchema` for `list-libraries` and `resolve-library`, server `instructions`
- Truncation notice at the end of `get-library-docs` output when results were cut
- Tool-call and protocol-error logging

### Changed
- mcp-go bumped to v1.1.0: MCP specification 2026-07-28 with legacy fallback; transport is now stateless (no `Mcp-Session-Id`)
- `list-libraries` structured output is now an object `{"libraries": [...]}` and includes `repo` and `commit_sha`
- Tool arguments are validated against their schema; unknown or missing arguments return `isError` results
- `openWorldHint` is now `false` on all tools
- HTTP server timeouts and a 1 MiB request body limit

### Fixed
- FTS5 operators and quotes in queries no longer cause SQLite syntax errors
- `%` and `_` in `resolve-library` queries are matched literally
- Store read queries honour request cancellation
```

- [ ] **Step 6: Verify docs build nothing wrong and commit**

```bash
go build ./... && go test ./...
git add README.md docserve.example.yaml docs/testing-guide.md CLAUDE.md CHANGELOG.md
git commit -m "docs: describe MCP 2026-07-28 support, auth, origins and conformance testing

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U8jnsQkU3tuNSH9Cdh3wdd"
```

---

### Task 11: End-to-end verification against a running binary

**Files:** none modified. Scratch files go in the session scratchpad directory, never in the repo.

- [ ] **Step 1: Static checks**

```bash
go build ./... && go vet ./... && go test ./... && go test -tags=integration ./...
command -v golangci-lint && golangci-lint run ./... || echo "golangci-lint not installed; skipped"
```
Expected: all green; report lint status verbatim.

- [ ] **Step 2: Build and start the server on a scratch database**

Write `$SCRATCH/verify.yaml`:

```yaml
data_dir: $SCRATCH/data
listen: "127.0.0.1:18080"
auth_token_env: DOCSERVE_VERIFY_TOKEN
allowed_origins:
  - https://app.example.com
sources:
  - name: dummy
    provider: github
    repo: org/repo
    refs:
      - ref: main
        paths: [docs/]
```
(Expand `$SCRATCH` to the absolute scratchpad path.) Then:

```bash
make build
DOCSERVE_VERIFY_TOKEN=verify123 bin/docserve serve --config $SCRATCH/verify.yaml > $SCRATCH/serve.log 2>&1 &
sleep 1; curl -s http://127.0.0.1:18080/healthz; echo
```
Expected: `ok`. Seed a library so tool calls have data: run `sqlite3` if available, or skip seeding and accept that `list-libraries` returns an empty list (the checks below do not depend on data).

- [ ] **Step 3: Protocol and security probes**

```bash
U=http://127.0.0.1:18080/mcp
H='-H Content-Type:application/json -H Accept:application/json,text/event-stream'
# 401 without token
curl -s -o /dev/null -w "%{http_code}\n" $H -X POST $U -d '{"jsonrpc":"2.0","id":1,"method":"ping"}'
# 403 with foreign origin
curl -s -o /dev/null -w "%{http_code}\n" $H -H "Authorization: Bearer verify123" -H "Origin: https://evil.example" -X POST $U -d '{"jsonrpc":"2.0","id":1,"method":"ping"}'
# 200 with allowed origin
curl -s -o /dev/null -w "%{http_code}\n" $H -H "Authorization: Bearer verify123" -H "Origin: https://app.example.com" -X POST $U -d '{"jsonrpc":"2.0","id":1,"method":"ping"}'
# legacy initialize: no Mcp-Session-Id header in response, instructions present
curl -si $H -H "Authorization: Bearer verify123" -X POST $U -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}' | grep -i "mcp-session-id\|instructions" 
# tools/list without any session
curl -s $H -H "Authorization: Bearer verify123" -X POST $U -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' | head -c 600; echo
# 2026-07-28 discover
curl -s $H -H "Authorization: Bearer verify123" -H "MCP-Protocol-Version: 2026-07-28" -H "Mcp-Method: server/discover" -X POST $U -d '{"jsonrpc":"2.0","id":3,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' | head -c 600; echo
# oversized body rejected
head -c 2000000 /dev/zero | tr '\0' 'x' > $SCRATCH/big.json
curl -s -o /dev/null -w "%{http_code}\n" $H -H "Authorization: Bearer verify123" -X POST $U --data-binary @$SCRATCH/big.json
```
Expected: `401`, `403`, `200`, no `Mcp-Session-Id` line but an `instructions` match, a tools list with three tools showing `openWorldHint:false` and `outputSchema`, a discover result with `supportedVersions` containing `2026-07-28`, and a non-200 for the oversized body. Also check `$SCRATCH/serve.log` contains the unauthenticated warning only when no token is set (here it must NOT appear) and contains `mcp tools/call` lines if any tool was called.

- [ ] **Step 4: Conformance suite (best effort)**

```bash
command -v npx && DOCSERVE_VERIFY_TOKEN=verify123 npx -y @modelcontextprotocol/conformance server --url http://127.0.0.1:18080/mcp --header "Authorization: Bearer verify123" 2>&1 | tail -40 || echo "npx unavailable; conformance skipped"
```
If the tool has no header flag, restart the server with the `auth_token_env` line removed from `verify.yaml` and rerun without the header. Record pass/fail counts and the names of any failing scenarios verbatim. Do not modify code in this task; report failures for follow-up.

- [ ] **Step 5: Stop the server and report**

```bash
kill %1 2>/dev/null; wait 2>/dev/null; tail -5 $SCRATCH/serve.log
git status --short; git log --oneline main..HEAD
```
Expected: clean tree apart from the pre-existing untracked root files and `.gitignore`; ten commits on `mcp-hardening`. Report every probe result and the conformance summary verbatim.
