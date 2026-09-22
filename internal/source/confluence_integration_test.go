//go:build integration

package source_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codanael/docserve/internal/config"
	"github.com/codanael/docserve/internal/index"
	"github.com/codanael/docserve/internal/source"
)

// confluencePage builds a JSON-serialisable map matching the Confluence REST
// API shape for a single page.
func confluencePage(id, title string, version int, ancestors []string, body string) map[string]any {
	anc := make([]map[string]any, len(ancestors))
	for i, a := range ancestors {
		anc[i] = map[string]any{"id": a}
	}
	pg := map[string]any{
		"id":        id,
		"title":     title,
		"version":   map[string]any{"number": version},
		"ancestors": anc,
	}
	if body != "" {
		pg["body"] = map[string]any{
			"storage": map[string]any{"value": body},
		}
	}
	return pg
}

// newConfluenceServer returns an httptest.Server that fakes enough of the
// Confluence REST API for the provider to work. pages maps page ID to its
// JSON representation; descendants is the flat list returned for CQL search.
func newConfluenceServer(t *testing.T, pages map[string]map[string]any, descendants []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Single page: /rest/api/content/{id}?expand=...
		if strings.HasPrefix(r.URL.Path, "/rest/api/content/") && !strings.Contains(r.URL.Path, "/search") {
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rest/api/content/"), "/")
			id := parts[0]
			pg, ok := pages[id]
			if !ok {
				http.Error(w, fmt.Sprintf("page %s not found", id), http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(pg)
			return
		}

		// Search: /rest/api/content/search?cql=...
		if r.URL.Path == "/rest/api/content/search" {
			envelope := map[string]any{
				"results": descendants,
				"start":   0,
				"limit":   200,
				"size":    len(descendants),
			}
			json.NewEncoder(w).Encode(envelope)
			return
		}

		http.NotFound(w, r)
	}))
}

func TestConfluenceIntegration(t *testing.T) {
	// ---------------------------------------------------------------
	// 1. Build fake Confluence pages
	// ---------------------------------------------------------------
	rootBody := `<h1>Documentation</h1><p>Welcome to our docs.</p>`

	gettingStartedBody := `<p>Follow these steps to get started.</p>` +
		`<ac:structured-macro ac:name="info"><ac:rich-text-body><p>Requires Node 18+</p></ac:rich-text-body></ac:structured-macro>` +
		`<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">bash</ac:parameter>` +
		`<ac:plain-text-body><![CDATA[npm install && npm start]]></ac:plain-text-body></ac:structured-macro>`

	apiRefBody := `<h2>API Reference</h2>` +
		`<table><tbody>` +
		`<tr><th>Method</th><th>Path</th></tr>` +
		`<tr><td>GET</td><td>/api/users</td></tr>` +
		`</tbody></table>`

	root := confluencePage("100", "Documentation", 5, nil, rootBody)
	child1 := confluencePage("200", "Getting Started", 3, []string{"100"}, gettingStartedBody)
	child2 := confluencePage("300", "API Reference", 7, []string{"100"}, apiRefBody)

	pages := map[string]map[string]any{
		"100": root,
		"200": child1,
		"300": child2,
	}
	descendants := []map[string]any{child1, child2}

	srv := newConfluenceServer(t, pages, descendants)
	defer srv.Close()

	// ---------------------------------------------------------------
	// 2. Open in-memory store and create fetcher
	// ---------------------------------------------------------------
	store, err := index.OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	dataDir := t.TempDir()
	fetcher := source.NewFetcher(store, dataDir)

	cfg := config.ResolvedSource{
		Name:     "test-confluence",
		Provider: "confluence",
		BaseURL:  srv.URL,
	}
	page := config.PageConfig{
		Space: "TEST",
		ID:    "100",
	}
	libName := config.LibraryName("confluence", "", config.RefConfig{}, page)

	prov := source.NewConfluenceProvider(cfg, page, srv.Client())

	ctx := context.Background()

	// ---------------------------------------------------------------
	// 3. First fetch: should update
	// ---------------------------------------------------------------
	result, err := fetcher.FetchSource(ctx, cfg, config.RefConfig{}, page, libName, prov)
	if err != nil {
		t.Fatalf("FetchSource (1st): %v", err)
	}
	if !result.Updated {
		t.Error("expected Updated=true on first fetch")
	}
	if result.ChunkCount == 0 {
		t.Error("expected ChunkCount > 0")
	}
	t.Logf("first fetch: Updated=%v ChunkCount=%d SHA=%s", result.Updated, result.ChunkCount, result.SHA)

	// ---------------------------------------------------------------
	// 4. Verify library stored
	// ---------------------------------------------------------------
	lib, err := store.GetLibrary(ctx, libName)
	if err != nil {
		t.Fatalf("GetLibrary: %v", err)
	}
	if lib.CommitSHA == "" {
		t.Error("expected CommitSHA to be non-empty")
	}
	t.Logf("library: ID=%d CommitSHA=%s", lib.ID, lib.CommitSHA)

	// ---------------------------------------------------------------
	// 5. Search for "npm install"
	// ---------------------------------------------------------------
	npmOut, err := store.SearchDocs(ctx, lib.ID, "npm install", 4000)
	if err != nil {
		t.Fatalf("SearchDocs(npm install): %v", err)
	}
	if len(npmOut.Results) == 0 {
		t.Error("expected search results for 'npm install', got none")
	} else {
		t.Logf("search 'npm install': %d results, first path=%s", len(npmOut.Results), npmOut.Results[0].Path)
	}

	// ---------------------------------------------------------------
	// 6. Search for "api users"
	// ---------------------------------------------------------------
	apiOut, err := store.SearchDocs(ctx, lib.ID, "api users", 4000)
	if err != nil {
		t.Fatalf("SearchDocs(api users): %v", err)
	}
	if len(apiOut.Results) == 0 {
		t.Error("expected search results for 'api users', got none")
	} else {
		t.Logf("search 'api users': %d results, first path=%s", len(apiOut.Results), apiOut.Results[0].Path)
	}

	// ---------------------------------------------------------------
	// 7. Second fetch: should be a cache hit (Updated=false)
	// ---------------------------------------------------------------
	result2, err := fetcher.FetchSource(ctx, cfg, config.RefConfig{}, page, libName, prov)
	if err != nil {
		t.Fatalf("FetchSource (2nd): %v", err)
	}
	if result2.Updated {
		t.Error("expected Updated=false on second fetch (cache hit)")
	}
	t.Logf("second fetch: Updated=%v", result2.Updated)
}
