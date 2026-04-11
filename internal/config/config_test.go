package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codanael/docserve/internal/config"
)

func TestLoadConfig(t *testing.T) {
	content := `
data_dir: /tmp/docserve-data
listen: ":9090"
proxy:
  http: http://proxy.example.com:3128
  https: http://proxy.example.com:3128
sources:
  - name: my-github-docs
    provider: github
    repo: myorg/myrepo
    ref: main
    proxy: true
    paths:
      - docs/
      - README.md
    schedule: "0 * * * *"
    auth:
      type: bearer
      token_env: GITHUB_TOKEN
  - name: my-azure-docs
    provider: azure-devops
    org: myorg
    project: myproject
    repo: myrepo
    ref: refs/heads/main
    base_url: https://dev.azure.com
    proxy: false
    paths:
      - wiki/
    schedule: "30 * * * *"
    auth:
      type: basic
      username_env: ADO_USER
      password_env: ADO_PASS
`
	f := writeTempConfig(t, content)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DataDir != "/tmp/docserve-data" {
		t.Errorf("DataDir = %q, want /tmp/docserve-data", cfg.DataDir)
	}
	if cfg.Listen != ":9090" {
		t.Errorf("Listen = %q, want :9090", cfg.Listen)
	}
	if cfg.Proxy.HTTP != "http://proxy.example.com:3128" {
		t.Errorf("Proxy.HTTP = %q", cfg.Proxy.HTTP)
	}
	if cfg.Proxy.HTTPS != "http://proxy.example.com:3128" {
		t.Errorf("Proxy.HTTPS = %q", cfg.Proxy.HTTPS)
	}

	if len(cfg.Sources) != 2 {
		t.Fatalf("len(Sources) = %d, want 2", len(cfg.Sources))
	}

	gh := cfg.Sources[0]
	if gh.Name != "my-github-docs" {
		t.Errorf("Sources[0].Name = %q", gh.Name)
	}
	if gh.Provider != "github" {
		t.Errorf("Sources[0].Provider = %q", gh.Provider)
	}
	if gh.Repo != "myorg/myrepo" {
		t.Errorf("Sources[0].Repo = %q", gh.Repo)
	}
	if gh.Ref != "main" {
		t.Errorf("Sources[0].Ref = %q", gh.Ref)
	}
	if !gh.Proxy {
		t.Errorf("Sources[0].Proxy = false, want true")
	}
	if len(gh.Paths) != 2 || gh.Paths[0] != "docs/" || gh.Paths[1] != "README.md" {
		t.Errorf("Sources[0].Paths = %v", gh.Paths)
	}
	if gh.Schedule != "0 * * * *" {
		t.Errorf("Sources[0].Schedule = %q", gh.Schedule)
	}
	if gh.Auth.Type != "bearer" {
		t.Errorf("Sources[0].Auth.Type = %q", gh.Auth.Type)
	}
	if gh.Auth.TokenEnv != "GITHUB_TOKEN" {
		t.Errorf("Sources[0].Auth.TokenEnv = %q", gh.Auth.TokenEnv)
	}

	ado := cfg.Sources[1]
	if ado.Name != "my-azure-docs" {
		t.Errorf("Sources[1].Name = %q", ado.Name)
	}
	if ado.Provider != "azure-devops" {
		t.Errorf("Sources[1].Provider = %q", ado.Provider)
	}
	if ado.Org != "myorg" {
		t.Errorf("Sources[1].Org = %q", ado.Org)
	}
	if ado.Project != "myproject" {
		t.Errorf("Sources[1].Project = %q", ado.Project)
	}
	if ado.BaseURL != "https://dev.azure.com" {
		t.Errorf("Sources[1].BaseURL = %q", ado.BaseURL)
	}
	if ado.Proxy {
		t.Errorf("Sources[1].Proxy = true, want false")
	}
	if ado.Auth.Type != "basic" {
		t.Errorf("Sources[1].Auth.Type = %q", ado.Auth.Type)
	}
	if ado.Auth.UsernameEnv != "ADO_USER" {
		t.Errorf("Sources[1].Auth.UsernameEnv = %q", ado.Auth.UsernameEnv)
	}
	if ado.Auth.PasswordEnv != "ADO_PASS" {
		t.Errorf("Sources[1].Auth.PasswordEnv = %q", ado.Auth.PasswordEnv)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	content := `
sources:
  - name: minimal-source
    provider: github
    repo: org/repo
    ref: main
    paths:
      - docs/
`
	f := writeTempConfig(t, content)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want :8080 (default)", cfg.Listen)
	}
	if cfg.DataDir != "data" {
		t.Errorf("DataDir = %q, want data (default)", cfg.DataDir)
	}
	if !cfg.Sources[0].Proxy {
		t.Errorf("Sources[0].Proxy = false, want true (default)")
	}
}

func TestLoadConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "no sources",
			content: `listen: ":8080"`,
		},
		{
			name: "empty sources list",
			content: `
sources: []
`,
		},
		{
			name: "no name",
			content: `
sources:
  - provider: github
    repo: org/repo
    ref: main
    paths:
      - docs/
`,
		},
		{
			name: "no provider",
			content: `
sources:
  - name: test
    repo: org/repo
    ref: main
    paths:
      - docs/
`,
		},
		{
			name: "unknown provider",
			content: `
sources:
  - name: test
    provider: bitbucket
    repo: org/repo
    ref: main
    paths:
      - docs/
`,
		},
		{
			name: "no ref",
			content: `
sources:
  - name: test
    provider: github
    repo: org/repo
    paths:
      - docs/
`,
		},
		{
			name: "no paths",
			content: `
sources:
  - name: test
    provider: github
    repo: org/repo
    ref: main
`,
		},
		{
			name: "empty paths",
			content: `
sources:
  - name: test
    provider: github
    repo: org/repo
    ref: main
    paths: []
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := writeTempConfig(t, tc.content)
			_, err := config.Load(f)
			if err == nil {
				t.Errorf("Load() expected error for case %q, got nil", tc.name)
			}
		})
	}
}

func TestResolveConfigPath(t *testing.T) {
	t.Run("explicit path exists", func(t *testing.T) {
		f := writeTempConfig(t, "listen: ':8080'")
		got, err := config.ResolvePath(f)
		if err != nil {
			t.Fatalf("ResolvePath(%q) error = %v", f, err)
		}
		if got != f {
			t.Errorf("ResolvePath() = %q, want %q", got, f)
		}
	})

	t.Run("explicit path not found", func(t *testing.T) {
		_, err := config.ResolvePath("/nonexistent/path/docserve.yaml")
		if err == nil {
			t.Error("ResolvePath() expected error for nonexistent path, got nil")
		}
	})
}

func TestLoadConfigConfluence(t *testing.T) {
	depth := 3
	content := `
sources:
  - name: my-confluence-docs
    provider: confluence
    base_url: https://confluence.example.com
    ref: SPACEKEY
    depth: 3
    proxy: false
    schedule: "0 */2 * * *"
    auth:
      type: bearer
      token_env: CONFLUENCE_TOKEN
`
	f := writeTempConfig(t, content)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(cfg.Sources))
	}

	src := cfg.Sources[0]
	if src.Name != "my-confluence-docs" {
		t.Errorf("Name = %q", src.Name)
	}
	if src.Provider != "confluence" {
		t.Errorf("Provider = %q", src.Provider)
	}
	if src.BaseURL != "https://confluence.example.com" {
		t.Errorf("BaseURL = %q", src.BaseURL)
	}
	if src.Ref != "SPACEKEY" {
		t.Errorf("Ref = %q", src.Ref)
	}
	if src.Depth == nil {
		t.Fatal("Depth is nil, want pointer to 3")
	}
	if *src.Depth != depth {
		t.Errorf("Depth = %d, want %d", *src.Depth, depth)
	}
	if src.Proxy {
		t.Errorf("Proxy = true, want false")
	}
	// Paths should default to [""] for confluence
	if len(src.Paths) != 1 || src.Paths[0] != "" {
		t.Errorf("Paths = %v, want [\"\"]", src.Paths)
	}
	// Repo should default to base_url + /pages/ + ref
	wantRepo := "https://confluence.example.com/pages/SPACEKEY"
	if src.Repo != wantRepo {
		t.Errorf("Repo = %q, want %q", src.Repo, wantRepo)
	}
	if src.Schedule != "0 */2 * * *" {
		t.Errorf("Schedule = %q", src.Schedule)
	}
	if src.Auth.Type != "bearer" {
		t.Errorf("Auth.Type = %q", src.Auth.Type)
	}
	if src.Auth.TokenEnv != "CONFLUENCE_TOKEN" {
		t.Errorf("Auth.TokenEnv = %q", src.Auth.TokenEnv)
	}
}

func TestLoadConfigConfluenceDepthDefault(t *testing.T) {
	content := `
sources:
  - name: confluence-no-depth
    provider: confluence
    base_url: https://confluence.example.com
    ref: MYSPACE
`
	f := writeTempConfig(t, content)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	src := cfg.Sources[0]
	if src.Depth != nil {
		t.Errorf("Depth = %v, want nil (unset)", src.Depth)
	}
}

func TestLoadConfigConfluenceValidation(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name: "confluence missing base_url",
			content: `
sources:
  - name: test
    provider: confluence
    ref: SPACEKEY
`,
		},
		{
			name: "confluence missing ref",
			content: `
sources:
  - name: test
    provider: confluence
    base_url: https://confluence.example.com
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := writeTempConfig(t, tc.content)
			_, err := config.Load(f)
			if err == nil {
				t.Errorf("Load() expected error for case %q, got nil", tc.name)
			}
		})
	}
}

// writeTempConfig writes content to a temp file and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "docserve.yaml")
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatalf("writeTempConfig: %v", err)
	}
	return f
}
