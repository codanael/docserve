package source

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/codanael/docserve/internal/config"
	"github.com/codanael/docserve/internal/index"
)

// FetchResult summarises the outcome of a FetchSource call.
type FetchResult struct {
	Source     string
	SHA        string
	Updated    bool
	ChunkCount int
}

// Fetcher orchestrates the pipeline: resolve → cache-check → download → index.
type Fetcher struct {
	store   *index.Store
	dataDir string
	Force   bool
}

// NewFetcher creates a Fetcher that uses store for persistence and dataDir for
// the raw file cache.
func NewFetcher(store *index.Store, dataDir string) *Fetcher {
	return &Fetcher{store: store, dataDir: dataDir}
}

// FetchSource executes the full fetch pipeline for a single source+ref (or source+page for confluence).
func (f *Fetcher) FetchSource(ctx context.Context, cfg config.ResolvedSource, ref config.RefConfig, page config.PageConfig, libName string, prov Provider) (*FetchResult, error) {
	// Determine the reference identifier based on provider type.
	resolveRef := ref.Ref
	if cfg.Provider == "confluence" {
		resolveRef = page.ID
	}

	// Step 1: resolve ref → SHA.
	sha, err := prov.Resolve(ctx, resolveRef)
	if err != nil {
		return nil, fmt.Errorf("resolve ref %q: %w", resolveRef, err)
	}
	log.Printf("[%s] resolved ref %q → %s", libName, resolveRef, sha)

	// Step 2: check if we already have this SHA indexed (unless Force).
	if !f.Force {
		lib, err := f.store.GetLibrary(ctx, libName)
		if err == nil && lib.CommitSHA == sha {
			log.Printf("[%s] already up to date at %s, skipping", libName, sha)
			return &FetchResult{Source: libName, SHA: sha, Updated: false}, nil
		}
	}

	// Step 3: check raw cache.
	rawDir := filepath.Join(f.dataDir, "raw", libName, sha)
	if _, err := os.Stat(rawDir); os.IsNotExist(err) {
		// Step 4: download into rawDir.
		log.Printf("[%s] fetching %s into %s", libName, sha, rawDir)
		if err := os.MkdirAll(rawDir, 0755); err != nil {
			return nil, fmt.Errorf("create raw dir: %w", err)
		}
		paths := ref.Paths
		if len(paths) == 0 {
			paths = []string{""}
		}
		if err := prov.Fetch(ctx, sha, paths, rawDir); err != nil {
			_ = os.RemoveAll(rawDir)
			return nil, fmt.Errorf("fetch source %q: %w", libName, err)
		}
	} else {
		log.Printf("[%s] using cached raw files at %s", libName, rawDir)
	}

	// Step 5: walk each path prefix dir and chunk files.
	paths := ref.Paths
	if len(paths) == 0 {
		paths = []string{""}
	}
	var chunks []index.Chunk
	for _, pathPrefix := range paths {
		dir := filepath.Join(rawDir, filepath.FromSlash(pathPrefix))
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// Log but skip unreadable entries.
				log.Printf("[%s] warning: walking %s: %v", libName, path, err)
				return nil
			}
			if d.IsDir() {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("[%s] warning: reading %s: %v", libName, path, err)
				return nil
			}

			// Derive a relative path for the chunk's Path field.
			rel, err := filepath.Rel(rawDir, path)
			if err != nil {
				rel = path
			}
			relSlash := filepath.ToSlash(rel)

			chunker := index.NewChunker(path)
			fileChunks, err := chunker.Chunk(relSlash, data)
			if err != nil {
				log.Printf("[%s] warning: chunking %s: %v", libName, path, err)
				return nil
			}
			chunks = append(chunks, fileChunks...)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %q: %w", dir, err)
		}
	}
	log.Printf("[%s] collected %d chunks", libName, len(chunks))

	// Build the short ref label (up to 12 chars of sha).
	shortSHA := sha
	if len(shortSHA) > 12 {
		shortSHA = shortSHA[:12]
	}
	refLabel := ref.Ref
	if cfg.Provider == "confluence" {
		refLabel = page.Space + "/" + page.ID
	}
	refStr := refLabel + "@" + shortSHA

	// Determine repo for the library record.
	repo := cfg.Slug
	if cfg.Provider == "confluence" {
		repo = cfg.BaseURL + "/spaces/" + page.Space + "/pages/" + page.ID
	}

	// Step 6: upsert library then replace chunks.
	lib := index.Library{
		Name:      libName,
		Repo:      repo,
		Ref:       refStr,
		CommitSHA: sha,
		FetchedAt: time.Now().UTC(),
	}
	libID, err := f.store.UpsertLibrary(lib)
	if err != nil {
		return nil, fmt.Errorf("upsert library: %w", err)
	}
	if err := f.store.ReplaceChunks(libID, chunks); err != nil {
		return nil, fmt.Errorf("replace chunks: %w", err)
	}
	log.Printf("[%s] indexed %d chunks (libID=%d)", libName, len(chunks), libID)

	// Step 7: return result.
	return &FetchResult{
		Source:     libName,
		SHA:        sha,
		Updated:    true,
		ChunkCount: len(chunks),
	}, nil
}
