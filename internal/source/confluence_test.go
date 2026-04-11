package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codanael/docserve/internal/config"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// confluencePageJSON builds a JSON object matching the Confluence REST API
// response for a single page.
func confluencePageJSON(id, title string, version int, ancestorIDs []string, body string) map[string]any {
	ancestors := make([]map[string]any, len(ancestorIDs))
	for i, aid := range ancestorIDs {
		ancestors[i] = map[string]any{"id": aid}
	}
	pg := map[string]any{
		"id":        id,
		"title":     title,
		"version":   map[string]any{"number": version},
		"ancestors": ancestors,
	}
	if body != "" {
		pg["body"] = map[string]any{
			"storage": map[string]any{
				"value": body,
			},
		}
	}
	return pg
}

// newConfluenceTestServer creates an httptest server that serves fake
// Confluence REST API responses. pages maps page ID to its JSON object.
// searchResults is the flat list of descendant pages returned for CQL search.
func newConfluenceTestServer(t *testing.T, pages map[string]map[string]any, searchResults []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Single page endpoint: /rest/api/content/{id}
		if strings.HasPrefix(r.URL.Path, "/rest/api/content/") && !strings.Contains(r.URL.Path, "/search") {
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rest/api/content/"), "/")
			id := parts[0]
			pg, ok := pages[id]
			if !ok {
				http.Error(w, fmt.Sprintf("page %s not found", id), http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(pg) //nolint:errcheck
			return
		}

		// Search endpoint: /rest/api/content/search
		if r.URL.Path == "/rest/api/content/search" {
			envelope := map[string]any{
				"results": searchResults,
				"start":   0,
				"limit":   200,
				"size":    len(searchResults),
			}
			json.NewEncoder(w).Encode(envelope) //nolint:errcheck
			return
		}

		http.NotFound(w, r)
	}))
}

func makeConfluenceProvider(srv *httptest.Server, depth *int) *ConfluenceProvider {
	cfg := config.SourceConfig{
		Provider: "confluence",
		BaseURL:  srv.URL,
	}
	ref := config.RefConfig{
		Space: "TEST",
		ID:    "100",
	}
	if depth != nil {
		ref.Depth = depth
	}
	p := NewConfluenceProvider(cfg, ref, srv.Client())
	p.apiURL = srv.URL
	return p
}

func intPtr(v int) *int {
	return &v
}

// ---------------------------------------------------------------------------
// TestConfluenceResolveStableHash
// ---------------------------------------------------------------------------

func TestConfluenceResolveStableHash(t *testing.T) {
	root := confluencePageJSON("100", "Root", 5, nil, "")
	child := confluencePageJSON("200", "Child", 3, []string{"100"}, "")

	pages := map[string]map[string]any{
		"100": root,
		"200": child,
	}
	searchResults := []map[string]any{child}

	srv := newConfluenceTestServer(t, pages, searchResults)
	defer srv.Close()

	p := makeConfluenceProvider(srv, nil)

	hash1, err := p.Resolve(context.Background(), "100")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hash1) != 40 {
		t.Errorf("expected 40-char hash, got %d chars: %q", len(hash1), hash1)
	}

	hash2, err := p.Resolve(context.Background(), "100")
	if err != nil {
		t.Fatalf("Resolve (2nd call): %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("hash not stable: %q != %q", hash1, hash2)
	}
}

// ---------------------------------------------------------------------------
// TestConfluenceResolveHashChangesOnVersionBump
// ---------------------------------------------------------------------------

func TestConfluenceResolveHashChangesOnVersionBump(t *testing.T) {
	root := confluencePageJSON("100", "Root", 5, nil, "")
	childV3 := confluencePageJSON("200", "Child", 3, []string{"100"}, "")
	childV4 := confluencePageJSON("200", "Child", 4, []string{"100"}, "")

	pages1 := map[string]map[string]any{"100": root, "200": childV3}
	search1 := []map[string]any{childV3}
	srv1 := newConfluenceTestServer(t, pages1, search1)
	defer srv1.Close()

	p1 := makeConfluenceProvider(srv1, nil)
	hash1, err := p1.Resolve(context.Background(), "100")
	if err != nil {
		t.Fatalf("Resolve v3: %v", err)
	}

	pages2 := map[string]map[string]any{"100": root, "200": childV4}
	search2 := []map[string]any{childV4}
	srv2 := newConfluenceTestServer(t, pages2, search2)
	defer srv2.Close()

	p2 := makeConfluenceProvider(srv2, nil)
	hash2, err := p2.Resolve(context.Background(), "100")
	if err != nil {
		t.Fatalf("Resolve v4: %v", err)
	}

	if hash1 == hash2 {
		t.Errorf("expected hash to change on version bump, got same: %q", hash1)
	}
}

// ---------------------------------------------------------------------------
// TestConfluenceResolveDepthFilter
// ---------------------------------------------------------------------------

func TestConfluenceResolveDepthFilter(t *testing.T) {
	root := confluencePageJSON("100", "Root", 1, nil, "")
	child := confluencePageJSON("200", "Child", 1, []string{"100"}, "")
	grandchild := confluencePageJSON("300", "Grandchild", 1, []string{"100", "200"}, "")

	pages := map[string]map[string]any{
		"100": root,
		"200": child,
		"300": grandchild,
	}
	searchResults := []map[string]any{child, grandchild}

	srv := newConfluenceTestServer(t, pages, searchResults)
	defer srv.Close()

	// depth=1: exclude grandchild
	pDepth1 := makeConfluenceProvider(srv, intPtr(1))
	hashDepth1, err := pDepth1.Resolve(context.Background(), "100")
	if err != nil {
		t.Fatalf("Resolve depth=1: %v", err)
	}

	// depth=-1 (unlimited): include grandchild
	pUnlimited := makeConfluenceProvider(srv, nil)
	hashUnlimited, err := pUnlimited.Resolve(context.Background(), "100")
	if err != nil {
		t.Fatalf("Resolve unlimited: %v", err)
	}

	if hashDepth1 == hashUnlimited {
		t.Errorf("expected different hashes for depth=1 vs unlimited, got same: %q", hashDepth1)
	}
}

// ---------------------------------------------------------------------------
// TestConfluenceFetch
// ---------------------------------------------------------------------------

func TestConfluenceFetch(t *testing.T) {
	rootBody := `<p>Architecture overview</p>`
	backendBody := `<p>Backend details</p><ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[func main() {}]]></ac:plain-text-body></ac:structured-macro>`
	apiBody := `<p>REST API docs</p>`

	root := confluencePageJSON("100", "Architecture", 1, nil, rootBody)
	child := confluencePageJSON("200", "Backend", 1, []string{"100"}, backendBody)
	grandchild := confluencePageJSON("300", "API REST", 1, []string{"100", "200"}, apiBody)

	pages := map[string]map[string]any{
		"100": root,
		"200": child,
		"300": grandchild,
	}
	searchResults := []map[string]any{child, grandchild}

	srv := newConfluenceTestServer(t, pages, searchResults)
	defer srv.Close()

	p := makeConfluenceProvider(srv, nil)
	destDir := t.TempDir()

	if err := p.Fetch(context.Background(), "ignored", nil, destDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Verify files exist at correct paths.
	assertFileExists(t, destDir, "Architecture.md")
	assertFileExists(t, destDir, "Architecture/Backend.md")
	assertFileExists(t, destDir, "Architecture/Backend/API REST.md")

	// Verify content is markdown, not XHTML.
	backendContent, err := os.ReadFile(filepath.Join(destDir, "Architecture", "Backend.md"))
	if err != nil {
		t.Fatalf("reading Backend.md: %v", err)
	}
	if !strings.Contains(string(backendContent), "```") {
		t.Errorf("expected fenced code block in Backend.md, got:\n%s", backendContent)
	}
	if strings.Contains(string(backendContent), "ac:structured-macro") {
		t.Errorf("Backend.md still contains XHTML macro tags")
	}
}

// ---------------------------------------------------------------------------
// TestConfluenceFetchSanitizesTitles
// ---------------------------------------------------------------------------

func TestConfluenceFetchSanitizesTitles(t *testing.T) {
	rootBody := `<p>Content</p>`
	root := confluencePageJSON("100", "Docs: A/B Test", 1, nil, rootBody)

	pages := map[string]map[string]any{"100": root}
	var searchResults []map[string]any

	srv := newConfluenceTestServer(t, pages, searchResults)
	defer srv.Close()

	cfg := config.SourceConfig{
		Provider: "confluence",
		BaseURL:  srv.URL,
	}
	ref := config.RefConfig{
		Space: "TEST",
		ID:    "100",
	}
	p := NewConfluenceProvider(cfg, ref, srv.Client())
	p.apiURL = srv.URL

	destDir := t.TempDir()
	if err := p.Fetch(context.Background(), "ignored", nil, destDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	assertFileExists(t, destDir, "Docs_ A_B Test.md")
}

// ---------------------------------------------------------------------------
// TestConfluenceFetchRetryOn503
// ---------------------------------------------------------------------------

func TestConfluenceFetchRetryOn503(t *testing.T) {
	// Override sleepFunc to be instant during tests.
	origSleep := sleepFunc
	sleepFunc = func(_ time.Duration) {}
	defer func() { sleepFunc = origSleep }()

	var attempt int64

	rootBody := `<p>Content</p>`
	root := confluencePageJSON("100", "Root", 1, nil, rootBody)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		n := atomic.AddInt64(&attempt, 1)

		// First two attempts for any request return 503.
		// We track per-request by checking if we've served enough 503s total.
		// Since Resolve+Fetch make multiple requests, we only return 503 for
		// the first endpoint hit (single page fetch for root).
		if strings.HasPrefix(r.URL.Path, "/rest/api/content/100") && !strings.Contains(r.URL.Path, "search") && n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"message":"service unavailable"}`)) //nolint:errcheck
			return
		}

		// Single page endpoint
		if strings.HasPrefix(r.URL.Path, "/rest/api/content/") && !strings.Contains(r.URL.Path, "/search") {
			json.NewEncoder(w).Encode(root) //nolint:errcheck
			return
		}

		// Search endpoint: no descendants
		if r.URL.Path == "/rest/api/content/search" {
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"results": []any{},
				"start":   0,
				"limit":   200,
				"size":    0,
			})
			return
		}

		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := config.SourceConfig{
		Provider: "confluence",
		BaseURL:  srv.URL,
	}
	ref := config.RefConfig{
		Space: "TEST",
		ID:    "100",
	}
	p := NewConfluenceProvider(cfg, ref, srv.Client())
	p.apiURL = srv.URL

	destDir := t.TempDir()
	if err := p.Fetch(context.Background(), "ignored", nil, destDir); err != nil {
		t.Fatalf("Fetch should succeed after retries: %v", err)
	}

	assertFileExists(t, destDir, "Root.md")
}
