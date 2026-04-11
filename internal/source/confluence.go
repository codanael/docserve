package source

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/codanael/docserve/internal/config"
	"github.com/codanael/docserve/internal/confluence"
)

// sleepFunc is the function used for retry delays. Override in tests.
var sleepFunc = time.Sleep

// ConfluenceProvider fetches documentation from a Confluence instance.
type ConfluenceProvider struct {
	cfg    config.SourceConfig
	ref    config.RefConfig
	client *http.Client
	apiURL string // base URL trimmed of trailing slash
	depth  int    // -1 = unlimited, from ref.Depth (nil→-1)
}

// confluencePage holds the metadata (and optionally body) for a single page.
type confluencePage struct {
	ID        string
	Title     string
	Version   int
	Ancestors []string // ancestor page IDs
	Body      string   // storage XHTML (only when fetching with body)
}

// NewConfluenceProvider constructs a ConfluenceProvider with authentication applied.
func NewConfluenceProvider(cfg config.SourceConfig, ref config.RefConfig, client *http.Client) *ConfluenceProvider {
	if client == nil {
		client = &http.Client{}
	}
	client = WrapClientAuth(client, cfg.Auth)

	depth := -1
	if ref.Depth != nil {
		depth = *ref.Depth
	}

	return &ConfluenceProvider{
		cfg:    cfg,
		ref:    ref,
		client: client,
		apiURL: strings.TrimRight(cfg.BaseURL, "/"),
		depth:  depth,
	}
}

// Resolve fetches the page tree rooted at ref (a page ID) and returns a
// stable hash derived from each page's ID and version number.
func (p *ConfluenceProvider) Resolve(ctx context.Context, ref string) (string, error) {
	root, err := p.fetchPage(ctx, ref, false)
	if err != nil {
		return "", fmt.Errorf("fetching root page %s: %w", ref, err)
	}

	descendants, err := p.fetchDescendants(ctx, ref, false)
	if err != nil {
		return "", fmt.Errorf("fetching descendants of %s: %w", ref, err)
	}

	pages := p.filterByDepth(root, descendants)

	// Sort by ID for stable ordering.
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].ID < pages[j].ID
	})

	h := sha256.New()
	for _, pg := range pages {
		fmt.Fprintf(h, "%s:%d\n", pg.ID, pg.Version) //nolint:errcheck
	}
	hex := fmt.Sprintf("%x", h.Sum(nil))
	if len(hex) > 40 {
		hex = hex[:40]
	}
	return hex, nil
}

// Fetch re-fetches the page tree with body content and writes each page as
// a markdown file under destDir.
func (p *ConfluenceProvider) Fetch(ctx context.Context, _ string, _ []string, destDir string) error {
	ref := p.ref.ID

	root, err := p.fetchPage(ctx, ref, true)
	if err != nil {
		return fmt.Errorf("fetching root page %s: %w", ref, err)
	}

	descendants, err := p.fetchDescendants(ctx, ref, true)
	if err != nil {
		return fmt.Errorf("fetching descendants of %s: %w", ref, err)
	}

	pages := p.filterByDepth(root, descendants)

	// Build ancestor ID → title map for path construction.
	titleMap := make(map[string]string, len(pages))
	for _, pg := range pages {
		titleMap[pg.ID] = pg.Title
	}

	// Bounded concurrency.
	const maxConcurrency = 3
	sem := make(chan struct{}, maxConcurrency)

	var mu sync.Mutex
	var firstErr error

	var wg sync.WaitGroup
	for _, pg := range pages {
		wg.Add(1)
		go func(pg confluencePage) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			relPath := p.buildFilePath(root, pg, titleMap)
			fullPath := filepath.Join(destDir, filepath.FromSlash(relPath))

			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("creating directory for %s: %w", relPath, err)
				}
				mu.Unlock()
				return
			}

			md, err := confluence.Convert(pg.Body)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("converting page %s: %w", pg.ID, err)
				}
				mu.Unlock()
				return
			}

			if err := os.WriteFile(fullPath, []byte(md), 0644); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("writing %s: %w", relPath, err)
				}
				mu.Unlock()
				return
			}
		}(pg)
	}
	wg.Wait()

	return firstErr
}

// buildFilePath constructs the relative file path for a page based on its
// ancestors relative to the root page.
func (p *ConfluenceProvider) buildFilePath(root confluencePage, pg confluencePage, titleMap map[string]string) string {
	if pg.ID == root.ID {
		return sanitizeTitle(pg.Title) + ".md"
	}

	// Find ancestors that are below root.
	rootDepth := len(root.Ancestors)
	var parts []string
	for i := rootDepth; i < len(pg.Ancestors); i++ {
		title, ok := titleMap[pg.Ancestors[i]]
		if !ok {
			title = pg.Ancestors[i]
		}
		parts = append(parts, sanitizeTitle(title))
	}
	parts = append(parts, sanitizeTitle(pg.Title)+".md")
	return strings.Join(parts, "/")
}

// sanitizeTitle replaces characters that are invalid in file names.
func sanitizeTitle(title string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(title)
}

// filterByDepth returns the root plus descendants whose relative depth
// is within the configured limit.
func (p *ConfluenceProvider) filterByDepth(root confluencePage, descendants []confluencePage) []confluencePage {
	rootAncestorCount := len(root.Ancestors)
	result := []confluencePage{root}

	for _, pg := range descendants {
		relDepth := len(pg.Ancestors) - rootAncestorCount
		if p.depth == -1 || relDepth <= p.depth {
			result = append(result, pg)
		}
	}

	return result
}

// fetchPage fetches a single page by ID.
func (p *ConfluenceProvider) fetchPage(ctx context.Context, pageID string, withBody bool) (confluencePage, error) {
	expand := "version,ancestors"
	if withBody {
		expand = "version,ancestors,body.storage"
	}
	rawURL := fmt.Sprintf("%s/rest/api/content/%s?expand=%s", p.apiURL, pageID, expand)

	body, err := p.doGet(ctx, rawURL)
	if err != nil {
		return confluencePage{}, err
	}
	defer body.Close() //nolint:errcheck

	var raw struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Version struct {
			Number int `json:"number"`
		} `json:"version"`
		Ancestors []struct {
			ID string `json:"id"`
		} `json:"ancestors"`
		Body struct {
			Storage struct {
				Value string `json:"value"`
			} `json:"storage"`
		} `json:"body"`
	}
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return confluencePage{}, fmt.Errorf("decoding page %s: %w", pageID, err)
	}

	pg := confluencePage{
		ID:      raw.ID,
		Title:   raw.Title,
		Version: raw.Version.Number,
		Body:    raw.Body.Storage.Value,
	}
	for _, a := range raw.Ancestors {
		pg.Ancestors = append(pg.Ancestors, a.ID)
	}
	return pg, nil
}

// fetchDescendants fetches all descendant pages via CQL search, paginating
// until all results are retrieved.
func (p *ConfluenceProvider) fetchDescendants(ctx context.Context, rootID string, withBody bool) ([]confluencePage, error) {
	expand := "version,ancestors"
	if withBody {
		expand = "version,ancestors,body.storage"
	}

	const limit = 200
	var all []confluencePage

	for start := 0; ; start += limit {
		cql := fmt.Sprintf("ancestor=%s AND type=page", rootID)
		rawURL := fmt.Sprintf("%s/rest/api/content/search?cql=%s&expand=%s&start=%d&limit=%d",
			p.apiURL, url.QueryEscape(cql), expand, start, limit)

		body, err := p.doGet(ctx, rawURL)
		if err != nil {
			return nil, err
		}

		var result struct {
			Results []struct {
				ID      string `json:"id"`
				Title   string `json:"title"`
				Version struct {
					Number int `json:"number"`
				} `json:"version"`
				Ancestors []struct {
					ID string `json:"id"`
				} `json:"ancestors"`
				Body struct {
					Storage struct {
						Value string `json:"value"`
					} `json:"storage"`
				} `json:"body"`
			} `json:"results"`
			Start int `json:"start"`
			Limit int `json:"limit"`
			Size  int `json:"size"`
		}
		if err := json.NewDecoder(body).Decode(&result); err != nil {
			_ = body.Close()
			return nil, fmt.Errorf("decoding search results: %w", err)
		}
		_ = body.Close()

		for _, r := range result.Results {
			pg := confluencePage{
				ID:      r.ID,
				Title:   r.Title,
				Version: r.Version.Number,
				Body:    r.Body.Storage.Value,
			}
			for _, a := range r.Ancestors {
				pg.Ancestors = append(pg.Ancestors, a.ID)
			}
			all = append(all, pg)
		}

		if result.Size < limit {
			break
		}
	}

	return all, nil
}

// doGet performs a GET request with retry on 429/503 (up to 3 attempts).
func (p *ConfluenceProvider) doGet(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	delays := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("building request: %w", err)
		}

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("confluence request: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			return resp.Body, nil
		}

		// Retry on 429 or 503.
		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) && attempt < len(delays) {
			_ = resp.Body.Close()

			delay := delays[attempt]
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if parsed, err := time.ParseDuration(ra + "s"); err == nil {
					delay = parsed
				}
			}
			sleepFunc(delay)
			continue
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("confluence API returned %d: %s", resp.StatusCode, string(bodyBytes))
	}
}
