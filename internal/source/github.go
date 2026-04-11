package source

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/codanael/docserve/internal/config"
)

const defaultGitHubAPIURL = "https://api.github.com"

// GitHubProvider fetches documentation from a GitHub repository.
type GitHubProvider struct {
	cfg    config.SourceConfig
	client *http.Client
	apiURL string
}

// NewGitHubProvider constructs a GitHubProvider with authentication applied.
func NewGitHubProvider(cfg config.SourceConfig, client *http.Client) *GitHubProvider {
	if client == nil {
		client = &http.Client{}
	}
	client = WrapClientAuth(client, cfg.Auth)
	return &GitHubProvider{
		cfg:    cfg,
		client: client,
		apiURL: defaultGitHubAPIURL,
	}
}

// Resolve converts a symbolic ref to a concrete identifier.
//
//   - "latest" → the tag_name of the latest GitHub release
//   - anything else → the full commit SHA for that ref
func (p *GitHubProvider) Resolve(ctx context.Context, ref string) (string, error) {
	owner, repo, err := splitRepo(p.cfg.Repo)
	if err != nil {
		return "", err
	}

	var url string
	if ref == "latest" {
		url = fmt.Sprintf("%s/repos/%s/%s/releases/latest", p.apiURL, owner, repo)
	} else {
		url = fmt.Sprintf("%s/repos/%s/%s/commits/%s", p.apiURL, owner, repo, ref)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github resolve request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(body))
	}

	if ref == "latest" {
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return "", fmt.Errorf("decoding release response: %w", err)
		}
		if release.TagName == "" {
			return "", fmt.Errorf("release response missing tag_name")
		}
		return release.TagName, nil
	}

	var commit struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&commit); err != nil {
		return "", fmt.Errorf("decoding commit response: %w", err)
	}
	if commit.SHA == "" {
		return "", fmt.Errorf("commit response missing sha")
	}
	return commit.SHA, nil
}

// Fetch downloads the tarball for sha and extracts only files whose paths
// match one of the given path prefixes into destDir.
func (p *GitHubProvider) Fetch(ctx context.Context, sha string, paths []string, destDir string) error {
	owner, repo, err := splitRepo(p.cfg.Repo)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/repos/%s/%s/tarball/%s", p.apiURL, owner, repo, sha)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("github fetch request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(body))
	}

	return extractTarGz(resp.Body, destDir, paths)
}

// extractTarGz reads a gzip-compressed tar from r, strips the top-level
// directory component, and writes files matching paths into destDir.
func extractTarGz(r io.Reader, destDir string, paths []string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("opening gzip stream: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		// Strip the top-level directory (e.g. "owner-repo-sha/").
		strippedName := stripTopDir(hdr.Name)
		if strippedName == "" {
			continue // skip the top-level directory entry itself
		}

		// Skip files that don't match any requested path prefix.
		if !matchesAnyPrefix(strippedName, paths) {
			continue
		}

		target := filepath.Join(destDir, filepath.FromSlash(strippedName))

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("creating directory %s: %w", target, err)
			}

		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("creating parent directory for %s: %w", target, err)
			}

			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0755|0644)
			if err != nil {
				return fmt.Errorf("creating file %s: %w", target, err)
			}

			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("writing file %s: %w", target, err)
			}
			_ = f.Close()

		default:
			// Ignore symlinks, hard links, etc.
		}
	}

	return nil
}

// stripTopDir removes the first path component from name.
// e.g. "owner-repo-abc123/docs/index.md" → "docs/index.md"
func stripTopDir(name string) string {
	// Normalise separators.
	name = filepath.ToSlash(name)
	idx := strings.Index(name, "/")
	if idx < 0 {
		return ""
	}
	return name[idx+1:]
}

// matchesAnyPrefix reports whether path starts with any of the given prefixes.
// An empty prefixes slice means "match everything".
func matchesAnyPrefix(path string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		// Normalise the prefix so both "docs" and "docs/" work.
		prefix := strings.TrimRight(filepath.ToSlash(p), "/")
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
