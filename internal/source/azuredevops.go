package source

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/codanael/docserve/internal/config"
)

const defaultAzureDevOpsAPIURL = "https://dev.azure.com"

// AzureDevOpsProvider fetches documentation from an Azure DevOps Git repository.
type AzureDevOpsProvider struct {
	cfg    config.SourceConfig
	client *http.Client
	apiURL string
}

// NewAzureDevOpsProvider constructs an AzureDevOpsProvider with authentication applied.
func NewAzureDevOpsProvider(cfg config.SourceConfig, client *http.Client) *AzureDevOpsProvider {
	if client == nil {
		client = &http.Client{}
	}
	client = WrapClientAuth(client, cfg.Auth)
	return &AzureDevOpsProvider{
		cfg:    cfg,
		client: client,
		apiURL: defaultAzureDevOpsAPIURL,
	}
}

// Resolve converts a branch or tag name into the full commit SHA of its tip.
//
// API: GET /{org}/{project}/_apis/git/repositories/{repo}/commits
//
//	?searchCriteria.itemVersion.version={ref}&$top=1&api-version=7.0
func (p *AzureDevOpsProvider) Resolve(ctx context.Context, ref string) (string, error) {
	url := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/commits?searchCriteria.itemVersion.version=%s&$top=1&api-version=7.0",
		p.apiURL, p.cfg.Org, p.cfg.Project, p.cfg.Repo, ref,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("azure devops resolve request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("azure devops API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Value []struct {
			CommitID string `json:"commitId"`
		} `json:"value"`
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding commits response: %w", err)
	}
	if len(result.Value) == 0 {
		return "", fmt.Errorf("no commits found for ref %q", ref)
	}
	if result.Value[0].CommitID == "" {
		return "", fmt.Errorf("commit response missing commitId")
	}
	return result.Value[0].CommitID, nil
}

// Fetch downloads the zip archive for sha and extracts only files whose paths
// match one of the given path prefixes into destDir.
//
// API: GET /{org}/{project}/_apis/git/repositories/{repo}/items
//
//	?path=/&$format=zip&versionDescriptor.version={sha}&versionDescriptor.versionType=commit&api-version=7.0
func (p *AzureDevOpsProvider) Fetch(ctx context.Context, sha string, paths []string, destDir string) error {
	url := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/items?path=/&$format=zip&versionDescriptor.version=%s&versionDescriptor.versionType=commit&api-version=7.0",
		p.apiURL, p.cfg.Org, p.cfg.Project, p.cfg.Repo, sha,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("azure devops fetch request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("azure devops API returned %d: %s", resp.StatusCode, string(body))
	}

	// Read the entire response into memory — zip.NewReader requires random access.
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading zip response: %w", err)
	}

	return extractZip(data, destDir, paths)
}

// extractZip reads a zip archive from data, and extracts files matching any of
// the given path prefixes into destDir.
func extractZip(data []byte, destDir string, paths []string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("opening zip: %w", err)
	}

	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)

		// Skip directory entries.
		if f.FileInfo().IsDir() {
			continue
		}

		if !matchesAnyPrefix(name, paths) {
			continue
		}

		target := filepath.Join(destDir, filepath.FromSlash(name))

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("creating parent directory for %s: %w", target, err)
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("opening zip entry %s: %w", name, err)
		}

		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			_ = rc.Close()
			return fmt.Errorf("creating file %s: %w", target, err)
		}

		if _, err := io.Copy(out, rc); err != nil {
			_ = rc.Close()
			_ = out.Close()
			return fmt.Errorf("writing file %s: %w", target, err)
		}

		_ = rc.Close()
		_ = out.Close()
	}

	return nil
}
