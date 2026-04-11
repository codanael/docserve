package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/codanael/docserve/internal/config"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// assertFileExists checks that a file exists at path relative to dir.
func assertFileExists(t *testing.T, dir, path string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if _, err := os.Stat(full); err != nil {
		t.Errorf("expected file %q to exist, but got: %v", full, err)
	}
}

// assertFileNotExists checks that no file exists at path relative to dir.
func assertFileNotExists(t *testing.T, dir, path string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if _, err := os.Stat(full); err == nil {
		t.Errorf("expected file %q to NOT exist, but it does", full)
	}
}

// createTestTarball builds an in-memory gzip-compressed tarball.
// files maps archive-relative paths (without a top-level prefix) to content.
// A synthetic top-level directory "owner-repo-abc123" is prepended to each path.
func createTestTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	const topDir = "owner-repo-abc123/"

	// Write the top-level directory entry.
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     topDir,
		Mode:     0755,
	}); err != nil {
		t.Fatalf("writing top-dir header: %v", err)
	}

	for name, content := range files {
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     topDir + name,
			Size:     int64(len(content)),
			Mode:     0644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing header for %q: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("writing content for %q: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("closing gzip writer: %v", err)
	}

	return buf.Bytes()
}

// ---------------------------------------------------------------------------
// TestGitHubResolve
// ---------------------------------------------------------------------------

func TestGitHubResolve(t *testing.T) {
	const wantSHA = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	const wantTag = "v1.2.3"
	const wantToken = "mytoken"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header.
		auth := r.Header.Get("Authorization")
		if auth != "token "+wantToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/repos/owner/repo/commits/main":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"sha": wantSHA})

		case "/repos/owner/repo/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"tag_name": wantTag})

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Set token in env.
	t.Setenv("GITHUB_TOKEN", wantToken)

	cfg := config.SourceConfig{
		Provider: "github",
		Repo:     "owner/repo",
		Auth: config.AuthConfig{
			TokenEnv: "GITHUB_TOKEN",
		},
	}

	p := NewGitHubProvider(cfg, srv.Client())
	p.apiURL = srv.URL

	t.Run("branch ref returns sha", func(t *testing.T) {
		got, err := p.Resolve(context.Background(), "main")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got != wantSHA {
			t.Errorf("got SHA %q, want %q", got, wantSHA)
		}
	})

	t.Run("latest returns tag", func(t *testing.T) {
		got, err := p.Resolve(context.Background(), "latest")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got != wantTag {
			t.Errorf("got tag %q, want %q", got, wantTag)
		}
	})
}

// ---------------------------------------------------------------------------
// TestGitHubFetch
// ---------------------------------------------------------------------------

func TestGitHubFetch(t *testing.T) {
	const sha = "abc123"
	const wantToken = "fetchtoken"

	allFiles := map[string]string{
		"docs/index.md":    "# index",
		"docs/guide.md":    "# guide",
		"other/readme.txt": "ignore me",
	}

	tarball := createTestTarball(t, allFiles)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header.
		auth := r.Header.Get("Authorization")
		if auth != "token "+wantToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if r.URL.Path != "/repos/owner/myrepo/tarball/"+sha {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/x-gzip")
		_, _ = w.Write(tarball)
	}))
	defer srv.Close()

	t.Setenv("FETCH_TOKEN", wantToken)

	cfg := config.SourceConfig{
		Provider: "github",
		Repo:     "owner/myrepo",
		Auth: config.AuthConfig{
			TokenEnv: "FETCH_TOKEN",
		},
	}

	p := NewGitHubProvider(cfg, srv.Client())
	p.apiURL = srv.URL

	destDir := t.TempDir()

	if err := p.Fetch(context.Background(), sha, []string{"docs"}, destDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Files under "docs" should be present.
	assertFileExists(t, destDir, "docs/index.md")
	assertFileExists(t, destDir, "docs/guide.md")

	// Files outside "docs" should NOT be present.
	assertFileNotExists(t, destDir, "other/readme.txt")
}

// ---------------------------------------------------------------------------
// TestMatchesAnyPrefix
// ---------------------------------------------------------------------------

func TestMatchesAnyPrefix(t *testing.T) {
	cases := []struct {
		path     string
		prefixes []string
		want     bool
	}{
		{"docs/index.md", []string{"docs"}, true},
		{"docs/sub/page.md", []string{"docs"}, true},
		{"other/file.md", []string{"docs"}, false},
		{"docs", []string{"docs"}, true},
		{"docs/index.md", []string{}, true}, // empty = match all
		{"docs/index.md", []string{"docs/"}, true},
		{"docs-extra/file.md", []string{"docs"}, false},
	}

	for _, tc := range cases {
		got := matchesAnyPrefix(tc.path, tc.prefixes)
		if got != tc.want {
			t.Errorf("matchesAnyPrefix(%q, %v) = %v, want %v", tc.path, tc.prefixes, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// TestSplitRepo
// ---------------------------------------------------------------------------

func TestSplitRepo(t *testing.T) {
	owner, name, err := splitRepo("owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "owner" || name != "repo" {
		t.Errorf("got owner=%q name=%q", owner, name)
	}

	if _, _, err := splitRepo("invalid"); err == nil {
		t.Error("expected error for invalid repo string")
	}
}
