package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codanael/docserve/internal/config"
	"github.com/codanael/docserve/internal/index"
)

// fakeProvider is a test double for Provider.
type fakeProvider struct {
	sha   string
	files map[string]string // relative path -> content
}

func (p *fakeProvider) Resolve(_ context.Context, _ string) (string, error) {
	return p.sha, nil
}

func (p *fakeProvider) Fetch(_ context.Context, _ string, _ []string, destDir string) error {
	for relPath, content := range p.files {
		abs := filepath.Join(destDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
			return err
		}
	}
	return nil
}

// setupStore creates a temporary SQLite store for a test.
func setupStore(t *testing.T) (*index.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := index.OpenStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, dir
}

// sourceCfg builds a minimal SourceConfig for tests.
func sourceCfg(name string) config.SourceConfig {
	return config.SourceConfig{
		Name:     name,
		Provider: "github",
		Repo:     "owner/repo",
	}
}

// refCfg builds a minimal RefConfig for tests.
func refCfg(ref string, paths []string) config.RefConfig {
	return config.RefConfig{
		Ref:   ref,
		Paths: paths,
	}
}

// TestFetchPipeline verifies the full fetch pipeline:
//
//	first fetch   → Updated=true
//	same SHA      → Updated=false (skipped)
//	new SHA       → Updated=true, search finds new content
func TestFetchPipeline(t *testing.T) {
	store, dataDir := setupStore(t)
	fetcher := NewFetcher(store, dataDir)

	prov := &fakeProvider{
		sha: "abc123def456789",
		files: map[string]string{
			"docs/index.md": "# Hello\n\nThis is the first version of the documentation.",
		},
	}
	cfg := sourceCfg("mylib")
	ref := refCfg("main", []string{"docs"})
	libName := config.LibraryName(cfg, ref)
	ctx := context.Background()

	// --- First fetch: should index and return Updated=true ---
	result, err := fetcher.FetchSource(ctx, cfg, ref, libName, prov)
	if err != nil {
		t.Fatalf("first FetchSource: %v", err)
	}
	if !result.Updated {
		t.Error("first fetch: want Updated=true, got false")
	}
	if result.SHA != prov.sha {
		t.Errorf("first fetch: want SHA=%q, got %q", prov.sha, result.SHA)
	}
	if result.ChunkCount == 0 {
		t.Error("first fetch: want ChunkCount>0, got 0")
	}
	if result.Source != libName {
		t.Errorf("first fetch: want Source=%q, got %q", libName, result.Source)
	}

	// --- Second fetch with same SHA: should be a no-op ---
	result2, err := fetcher.FetchSource(ctx, cfg, ref, libName, prov)
	if err != nil {
		t.Fatalf("second FetchSource: %v", err)
	}
	if result2.Updated {
		t.Error("second fetch (same SHA): want Updated=false, got true")
	}
	if result2.SHA != prov.sha {
		t.Errorf("second fetch: want SHA=%q, got %q", prov.sha, result2.SHA)
	}

	// --- Third fetch with new SHA: should re-index with updated content ---
	prov.sha = "newsha9999abcdef"
	prov.files = map[string]string{
		"docs/index.md": "# Hello\n\nThis is the UPDATED documentation with unique_token_xyz.",
	}

	result3, err := fetcher.FetchSource(ctx, cfg, ref, libName, prov)
	if err != nil {
		t.Fatalf("third FetchSource: %v", err)
	}
	if !result3.Updated {
		t.Error("third fetch (new SHA): want Updated=true, got false")
	}
	if result3.SHA != prov.sha {
		t.Errorf("third fetch: want SHA=%q, got %q", prov.sha, result3.SHA)
	}

	// Verify the updated content is findable via search.
	lib, err := store.GetLibrary(libName)
	if err != nil {
		t.Fatalf("GetLibrary: %v", err)
	}
	results, err := store.SearchDocs(lib.ID, "unique_token_xyz", 100000)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("search after third fetch: expected at least one result for 'unique_token_xyz'")
	}
}

// TestFetchPipelineForce verifies that Force=true bypasses the SHA equality
// check and always re-indexes.
func TestFetchPipelineForce(t *testing.T) {
	store, dataDir := setupStore(t)
	fetcher := NewFetcher(store, dataDir)
	fetcher.Force = true

	prov := &fakeProvider{
		sha: "force_sha_12345",
		files: map[string]string{
			"docs/guide.md": "# Guide\n\nForced content.",
		},
	}
	cfg := sourceCfg("forcelib")
	ref := refCfg("main", []string{"docs"})
	libName := config.LibraryName(cfg, ref)
	ctx := context.Background()

	// First fetch.
	result, err := fetcher.FetchSource(ctx, cfg, ref, libName, prov)
	if err != nil {
		t.Fatalf("first FetchSource (force): %v", err)
	}
	if !result.Updated {
		t.Error("first force fetch: want Updated=true, got false")
	}

	// Second fetch with same SHA but Force=true → must still update.
	result2, err := fetcher.FetchSource(ctx, cfg, ref, libName, prov)
	if err != nil {
		t.Fatalf("second FetchSource (force): %v", err)
	}
	if !result2.Updated {
		t.Error("second force fetch (same SHA): want Updated=true, got false")
	}
}
