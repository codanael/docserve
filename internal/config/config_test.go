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
    proxy: true
    refs:
      - ref: main
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
    base_url: https://dev.azure.com
    proxy: false
    refs:
      - ref: refs/heads/main
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
	if !gh.Proxy {
		t.Errorf("Sources[0].Proxy = false, want true")
	}
	if len(gh.Refs) != 1 {
		t.Fatalf("len(Sources[0].Refs) = %d, want 1", len(gh.Refs))
	}
	if gh.Refs[0].Ref != "main" {
		t.Errorf("Sources[0].Refs[0].Ref = %q, want main", gh.Refs[0].Ref)
	}
	if len(gh.Refs[0].Paths) != 2 || gh.Refs[0].Paths[0] != "docs/" || gh.Refs[0].Paths[1] != "README.md" {
		t.Errorf("Sources[0].Refs[0].Paths = %v", gh.Refs[0].Paths)
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
	if len(ado.Refs) != 1 {
		t.Fatalf("len(Sources[1].Refs) = %d, want 1", len(ado.Refs))
	}
	if ado.Refs[0].Ref != "refs/heads/main" {
		t.Errorf("Sources[1].Refs[0].Ref = %q", ado.Refs[0].Ref)
	}
	if len(ado.Refs[0].Paths) != 1 || ado.Refs[0].Paths[0] != "wiki/" {
		t.Errorf("Sources[1].Refs[0].Paths = %v", ado.Refs[0].Paths)
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
    refs:
      - ref: main
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
    refs:
      - ref: main
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
    refs:
      - ref: main
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
    refs:
      - ref: main
        paths:
          - docs/
`,
		},
		{
			name: "no refs",
			content: `
sources:
  - name: test
    provider: github
    repo: org/repo
`,
		},
		{
			name: "ref missing ref field",
			content: `
sources:
  - name: test
    provider: github
    repo: org/repo
    refs:
      - paths:
          - docs/
`,
		},
		{
			name: "ref missing paths",
			content: `
sources:
  - name: test
    provider: github
    repo: org/repo
    refs:
      - ref: main
`,
		},
		{
			name: "confluence ref missing space",
			content: `
sources:
  - name: test
    provider: confluence
    base_url: https://confluence.example.com
    refs:
      - id: "123"
`,
		},
		{
			name: "confluence ref missing id",
			content: `
sources:
  - name: test
    provider: confluence
    base_url: https://confluence.example.com
    refs:
      - space: MYSPACE
`,
		},
		{
			name: "duplicate library names",
			content: `
sources:
  - name: test
    provider: github
    repo: org/repo
    refs:
      - ref: main
        paths:
          - docs/
      - ref: main
        paths:
          - other/
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
    proxy: false
    refs:
      - space: MYSPACE
        id: "123456"
        depth: 3
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
	if src.Proxy {
		t.Errorf("Proxy = true, want false")
	}
	if len(src.Refs) != 1 {
		t.Fatalf("len(Refs) = %d, want 1", len(src.Refs))
	}
	if src.Refs[0].Space != "MYSPACE" {
		t.Errorf("Refs[0].Space = %q, want MYSPACE", src.Refs[0].Space)
	}
	if src.Refs[0].ID != "123456" {
		t.Errorf("Refs[0].ID = %q, want 123456", src.Refs[0].ID)
	}
	if src.Refs[0].Depth == nil {
		t.Fatal("Refs[0].Depth is nil, want pointer to 3")
	}
	if *src.Refs[0].Depth != depth {
		t.Errorf("Refs[0].Depth = %d, want %d", *src.Refs[0].Depth, depth)
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
    refs:
      - space: MYSPACE
        id: "123"
`
	f := writeTempConfig(t, content)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	src := cfg.Sources[0]
	if src.Refs[0].Depth != nil {
		t.Errorf("Refs[0].Depth = %v, want nil (unset)", src.Refs[0].Depth)
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
    refs:
      - space: MYSPACE
        id: "123"
`,
		},
		{
			name: "confluence missing refs",
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

func TestLibraryName(t *testing.T) {
	t.Run("git default name", func(t *testing.T) {
		src := config.SourceConfig{Name: "myrepo", Provider: "github"}
		ref := config.RefConfig{Ref: "main"}
		got := config.LibraryName(src, ref)
		if got != "myrepo/main" {
			t.Errorf("LibraryName() = %q, want %q", got, "myrepo/main")
		}
	})

	t.Run("confluence default name", func(t *testing.T) {
		src := config.SourceConfig{Name: "mydocs", Provider: "confluence"}
		ref := config.RefConfig{Space: "MYSPACE", ID: "123"}
		got := config.LibraryName(src, ref)
		if got != "mydocs/MYSPACE/123" {
			t.Errorf("LibraryName() = %q, want %q", got, "mydocs/MYSPACE/123")
		}
	})

	t.Run("custom name override", func(t *testing.T) {
		src := config.SourceConfig{Name: "myrepo", Provider: "github"}
		ref := config.RefConfig{Name: "custom-lib", Ref: "main"}
		got := config.LibraryName(src, ref)
		if got != "custom-lib" {
			t.Errorf("LibraryName() = %q, want %q", got, "custom-lib")
		}
	})
}

func TestLoadConfigAllowedOrigins(t *testing.T) {
	content := `
allowed_origins:
  - https://app.example.com
  - http://intranet:3000
sources:
  - name: s
    provider: github
    repo: org/repo
    refs:
      - ref: main
        paths: [docs/]
`
	cfg, err := config.Load(writeTempConfig(t, content))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[0] != "https://app.example.com" {
		t.Errorf("AllowedOrigins = %v", cfg.AllowedOrigins)
	}

	bad := `
allowed_origins: ["app.example.com"]
sources:
  - name: s
    provider: github
    repo: org/repo
    refs:
      - ref: main
        paths: [docs/]
`
	if _, err := config.Load(writeTempConfig(t, bad)); err == nil {
		t.Error("expected error for origin without scheme")
	}
}

func TestLoadConfigAuthToken(t *testing.T) {
	content := `
auth_token_env: DOCSERVE_TEST_TOKEN
sources:
  - name: s
    provider: github
    repo: org/repo
    refs:
      - ref: main
        paths: [docs/]
`
	f := writeTempConfig(t, content)

	t.Setenv("DOCSERVE_TEST_TOKEN", "")
	if _, err := config.Load(f); err == nil {
		t.Error("expected error when auth_token_env variable is empty")
	}

	t.Setenv("DOCSERVE_TEST_TOKEN", "abc")
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AuthToken != "abc" {
		t.Errorf("AuthToken = %q, want abc", cfg.AuthToken)
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
