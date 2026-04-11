package source

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/codanael/docserve/internal/config"
)

// createTestZip creates an in-memory zip archive containing the given files.
func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("creating zip entry %q: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("writing zip entry %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing zip writer: %v", err)
	}
	return buf.Bytes()
}

func TestAzureDevOpsResolve(t *testing.T) {
	const wantCommitID = "abc123def456abc123def456abc123def456abc1"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify basic auth header: base64("user:pat") = "dXNlcjpwYXQ="
		got := r.Header.Get("Authorization")
		want := "Basic dXNlcjpwYXQ="
		if got != want {
			t.Errorf("Authorization header = %q, want %q", got, want)
		}

		// Verify URL contains expected path segments.
		if r.URL.Path != "/myorg/myproject/_apis/git/repositories/myrepo/commits" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("searchCriteria.itemVersion.version") != "main" {
			t.Errorf("unexpected version query param: %s", r.URL.Query().Get("searchCriteria.itemVersion.version"))
		}

		resp := map[string]interface{}{
			"value": []map[string]string{
				{"commitId": wantCommitID},
			},
			"count": 1,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Set env vars for basic auth.
	t.Setenv("AZDO_USERNAME", "user")
	t.Setenv("AZDO_PASSWORD", "pat")

	cfg := config.SourceConfig{
		Provider: "azure-devops",
		Org:      "myorg",
		Project:  "myproject",
		Repo:     "myrepo",
		Auth: config.AuthConfig{
			Type:        "basic",
			UsernameEnv: "AZDO_USERNAME",
			PasswordEnv: "AZDO_PASSWORD",
		},
	}

	p := NewAzureDevOpsProvider(cfg, &http.Client{})
	p.apiURL = srv.URL

	sha, err := p.Resolve(t.Context(), "main")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if sha != wantCommitID {
		t.Errorf("Resolve() = %q, want %q", sha, wantCommitID)
	}
}

func TestAzureDevOpsFetch(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"docs/index.md":      "# Index",
		"docs/guide.md":      "# Guide",
		"src/main.go":        "package main",
		"README.md":          "# README",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify URL path and query.
		if r.URL.Path != "/myorg/myproject/_apis/git/repositories/myrepo/items" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("versionDescriptor.version") != "deadbeef" {
			t.Errorf("unexpected sha query param: %s", r.URL.Query().Get("versionDescriptor.version"))
		}

		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipData)
	}))
	defer srv.Close()

	cfg := config.SourceConfig{
		Provider: "azure-devops",
		Org:      "myorg",
		Project:  "myproject",
		Repo:     "myrepo",
	}

	p := NewAzureDevOpsProvider(cfg, &http.Client{})
	p.apiURL = srv.URL

	destDir := t.TempDir()

	err := p.Fetch(t.Context(), "deadbeef", []string{"docs"}, destDir)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	// Only "docs" files should be extracted.
	assertFileExists(t, destDir, "docs/index.md")
	assertFileExists(t, destDir, "docs/guide.md")
	assertFileNotExists(t, destDir, "src/main.go")
	assertFileNotExists(t, destDir, "README.md")

	// Verify content.
	content, err := os.ReadFile(destDir + "/docs/index.md")
	if err != nil {
		t.Fatalf("reading docs/index.md: %v", err)
	}
	if string(content) != "# Index" {
		t.Errorf("docs/index.md content = %q, want %q", string(content), "# Index")
	}
}

func TestAzureDevOpsFetchAllPaths(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"docs/index.md": "# Index",
		"src/main.go":   "package main",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipData)
	}))
	defer srv.Close()

	cfg := config.SourceConfig{
		Provider: "azure-devops",
		Org:      "myorg",
		Project:  "myproject",
		Repo:     "myrepo",
	}

	p := NewAzureDevOpsProvider(cfg, &http.Client{})
	p.apiURL = srv.URL

	destDir := t.TempDir()

	// Empty paths means "extract everything".
	err := p.Fetch(t.Context(), "deadbeef", []string{}, destDir)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	assertFileExists(t, destDir, "docs/index.md")
	assertFileExists(t, destDir, "src/main.go")
}
