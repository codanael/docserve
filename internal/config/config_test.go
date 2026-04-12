package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codanael/docserve/internal/config"
)

func TestLoadConfigAllProviders(t *testing.T) {
	content := `
data_dir: /tmp/docserve-data
listen: ":9090"
proxy:
  http: http://proxy.example.com:3128
  https: http://proxy.example.com:3128

providers:
  - type: github
    proxy: true
    schedule: "0 * * * *"
    auth:
      token_env: GITHUB_TOKEN
    repos:
      - name: spring-boot
        slug: spring-projects/spring-boot
        refs:
          - ref: main
            paths: ["docs/"]
          - ref: v3.0
            name: spring-boot-v3
            paths: ["docs/"]

  - type: azure-devops
    base_url: https://dev.azure.com/myorg
    proxy: false
    auth:
      type: basic
      username_env: ADO_USER
      password_env: ADO_PAT
    repos:
      - name: wiki-docs
        project: myproject
        slug: myrepo
        refs:
          - ref: refs/heads/main
            paths: ["wiki/"]

  - type: confluence
    base_url: https://confluence.example.com
    proxy: false
    schedule: "0 */4 * * *"
    auth:
      type: bearer
      token_env: CONFLUENCE_PAT
    pages:
      - name: architecture-decisions
        space: ARCH
        id: "789012"
        depth: 3
      - space: DEV
        id: "123456"
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

	if len(cfg.Providers) != 3 {
		t.Fatalf("len(Providers) = %d, want 3", len(cfg.Providers))
	}

	// GitHub provider
	gh := cfg.Providers[0]
	if gh.Type != "github" {
		t.Errorf("Providers[0].Type = %q", gh.Type)
	}
	if !gh.Proxy {
		t.Errorf("Providers[0].Proxy = false, want true")
	}
	if gh.Schedule != "0 * * * *" {
		t.Errorf("Providers[0].Schedule = %q", gh.Schedule)
	}
	if gh.Auth.TokenEnv != "GITHUB_TOKEN" {
		t.Errorf("Providers[0].Auth.TokenEnv = %q", gh.Auth.TokenEnv)
	}
	if len(gh.Repos) != 1 {
		t.Fatalf("len(Providers[0].Repos) = %d, want 1", len(gh.Repos))
	}
	if gh.Repos[0].Name != "spring-boot" {
		t.Errorf("Repos[0].Name = %q", gh.Repos[0].Name)
	}
	if gh.Repos[0].Slug != "spring-projects/spring-boot" {
		t.Errorf("Repos[0].Slug = %q", gh.Repos[0].Slug)
	}
	if len(gh.Repos[0].Refs) != 2 {
		t.Fatalf("len(Repos[0].Refs) = %d, want 2", len(gh.Repos[0].Refs))
	}
	if gh.Repos[0].Refs[0].Ref != "main" {
		t.Errorf("Refs[0].Ref = %q", gh.Repos[0].Refs[0].Ref)
	}
	if gh.Repos[0].Refs[1].Name != "spring-boot-v3" {
		t.Errorf("Refs[1].Name = %q", gh.Repos[0].Refs[1].Name)
	}

	// Azure DevOps provider
	ado := cfg.Providers[1]
	if ado.Type != "azure-devops" {
		t.Errorf("Providers[1].Type = %q", ado.Type)
	}
	if ado.BaseURL != "https://dev.azure.com/myorg" {
		t.Errorf("Providers[1].BaseURL = %q", ado.BaseURL)
	}
	if ado.Proxy {
		t.Errorf("Providers[1].Proxy = true, want false")
	}
	if ado.Auth.Type != "basic" {
		t.Errorf("Providers[1].Auth.Type = %q", ado.Auth.Type)
	}
	if ado.Auth.UsernameEnv != "ADO_USER" {
		t.Errorf("Providers[1].Auth.UsernameEnv = %q", ado.Auth.UsernameEnv)
	}
	if ado.Auth.PasswordEnv != "ADO_PAT" {
		t.Errorf("Providers[1].Auth.PasswordEnv = %q", ado.Auth.PasswordEnv)
	}
	if len(ado.Repos) != 1 {
		t.Fatalf("len(Providers[1].Repos) = %d, want 1", len(ado.Repos))
	}
	if ado.Repos[0].Project != "myproject" {
		t.Errorf("Repos[0].Project = %q", ado.Repos[0].Project)
	}

	// Confluence provider
	conf := cfg.Providers[2]
	if conf.Type != "confluence" {
		t.Errorf("Providers[2].Type = %q", conf.Type)
	}
	if conf.BaseURL != "https://confluence.example.com" {
		t.Errorf("Providers[2].BaseURL = %q", conf.BaseURL)
	}
	if conf.Proxy {
		t.Errorf("Providers[2].Proxy = true, want false")
	}
	if conf.Schedule != "0 */4 * * *" {
		t.Errorf("Providers[2].Schedule = %q", conf.Schedule)
	}
	if conf.Auth.Type != "bearer" {
		t.Errorf("Providers[2].Auth.Type = %q", conf.Auth.Type)
	}
	if len(conf.Pages) != 2 {
		t.Fatalf("len(Providers[2].Pages) = %d, want 2", len(conf.Pages))
	}
	if conf.Pages[0].Name != "architecture-decisions" {
		t.Errorf("Pages[0].Name = %q", conf.Pages[0].Name)
	}
	if conf.Pages[0].Space != "ARCH" {
		t.Errorf("Pages[0].Space = %q", conf.Pages[0].Space)
	}
	if conf.Pages[0].ID != "789012" {
		t.Errorf("Pages[0].ID = %q", conf.Pages[0].ID)
	}
	if conf.Pages[0].Depth == nil || *conf.Pages[0].Depth != 3 {
		t.Errorf("Pages[0].Depth = %v, want 3", conf.Pages[0].Depth)
	}
	if conf.Pages[1].Space != "DEV" {
		t.Errorf("Pages[1].Space = %q", conf.Pages[1].Space)
	}
	if conf.Pages[1].ID != "123456" {
		t.Errorf("Pages[1].ID = %q", conf.Pages[1].ID)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	content := `
providers:
  - type: github
    repos:
      - name: minimal
        slug: org/repo
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
	if !cfg.Providers[0].Proxy {
		t.Errorf("Providers[0].Proxy = false, want true (default)")
	}
}

func TestLoadConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "no providers",
			content: `listen: ":8080"`,
		},
		{
			name: "empty providers list",
			content: `
providers: []
`,
		},
		{
			name: "no type",
			content: `
providers:
  - repos:
      - name: test
        slug: org/repo
        refs:
          - ref: main
            paths: ["docs/"]
`,
		},
		{
			name: "unknown type",
			content: `
providers:
  - type: bitbucket
    repos:
      - name: test
        slug: org/repo
        refs:
          - ref: main
            paths: ["docs/"]
`,
		},
		{
			name: "github with pages",
			content: `
providers:
  - type: github
    pages:
      - space: ARCH
        id: "123"
    repos:
      - name: test
        slug: org/repo
        refs:
          - ref: main
            paths: ["docs/"]
`,
		},
		{
			name: "confluence with repos",
			content: `
providers:
  - type: confluence
    base_url: https://confluence.example.com
    repos:
      - name: test
        slug: org/repo
        refs:
          - ref: main
            paths: ["docs/"]
    pages:
      - space: ARCH
        id: "123"
`,
		},
		{
			name: "github no repos",
			content: `
providers:
  - type: github
`,
		},
		{
			name: "confluence no pages",
			content: `
providers:
  - type: confluence
    base_url: https://confluence.example.com
`,
		},
		{
			name: "azure-devops missing base_url",
			content: `
providers:
  - type: azure-devops
    repos:
      - name: test
        slug: myrepo
        project: myproject
        refs:
          - ref: main
            paths: ["docs/"]
`,
		},
		{
			name: "confluence missing base_url",
			content: `
providers:
  - type: confluence
    pages:
      - space: ARCH
        id: "123"
`,
		},
		{
			name: "repo missing name",
			content: `
providers:
  - type: github
    repos:
      - slug: org/repo
        refs:
          - ref: main
            paths: ["docs/"]
`,
		},
		{
			name: "repo missing slug",
			content: `
providers:
  - type: github
    repos:
      - name: test
        refs:
          - ref: main
            paths: ["docs/"]
`,
		},
		{
			name: "azure-devops repo missing project",
			content: `
providers:
  - type: azure-devops
    base_url: https://dev.azure.com
    repos:
      - name: test
        slug: myrepo
        refs:
          - ref: main
            paths: ["docs/"]
`,
		},
		{
			name: "repo no refs",
			content: `
providers:
  - type: github
    repos:
      - name: test
        slug: org/repo
`,
		},
		{
			name: "ref missing ref field",
			content: `
providers:
  - type: github
    repos:
      - name: test
        slug: org/repo
        refs:
          - paths: ["docs/"]
`,
		},
		{
			name: "ref missing paths",
			content: `
providers:
  - type: github
    repos:
      - name: test
        slug: org/repo
        refs:
          - ref: main
`,
		},
		{
			name: "page missing space",
			content: `
providers:
  - type: confluence
    base_url: https://confluence.example.com
    pages:
      - id: "123"
`,
		},
		{
			name: "page missing id",
			content: `
providers:
  - type: confluence
    base_url: https://confluence.example.com
    pages:
      - space: ARCH
`,
		},
		{
			name: "duplicate library names",
			content: `
providers:
  - type: github
    repos:
      - name: test
        slug: org/repo
        refs:
          - ref: main
            paths: ["docs/"]
          - ref: main
            paths: ["other/"]
`,
		},
		{
			name: "duplicate library names across providers",
			content: `
providers:
  - type: github
    repos:
      - name: test
        slug: org/repo
        refs:
          - ref: main
            paths: ["docs/"]
  - type: github
    repos:
      - name: test
        slug: org/other
        refs:
          - ref: main
            paths: ["docs/"]
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

func TestFlattenProviders(t *testing.T) {
	t.Run("inherits auth and schedule from provider", func(t *testing.T) {
		providers := []config.ProviderConfig{
			{
				Type:     "github",
				Proxy:    true,
				Schedule: "0 * * * *",
				Auth:     config.AuthConfig{TokenEnv: "GITHUB_TOKEN"},
				Repos: []config.RepoConfig{
					{
						Name: "myrepo",
						Slug: "org/myrepo",
						Refs: []config.RefConfig{{Ref: "main", Paths: []string{"docs/"}}},
					},
				},
			},
		}

		sources := config.FlattenProviders(providers)
		if len(sources) != 1 {
			t.Fatalf("len(sources) = %d, want 1", len(sources))
		}
		s := sources[0]
		if s.Name != "myrepo" {
			t.Errorf("Name = %q, want myrepo", s.Name)
		}
		if s.Provider != "github" {
			t.Errorf("Provider = %q, want github", s.Provider)
		}
		if !s.Proxy {
			t.Errorf("Proxy = false, want true")
		}
		if s.Schedule != "0 * * * *" {
			t.Errorf("Schedule = %q, want '0 * * * *'", s.Schedule)
		}
		if s.Auth.TokenEnv != "GITHUB_TOKEN" {
			t.Errorf("Auth.TokenEnv = %q, want GITHUB_TOKEN", s.Auth.TokenEnv)
		}
	})

	t.Run("repo overrides auth and schedule", func(t *testing.T) {
		providers := []config.ProviderConfig{
			{
				Type:     "github",
				Proxy:    true,
				Schedule: "0 * * * *",
				Auth:     config.AuthConfig{TokenEnv: "GITHUB_TOKEN"},
				Repos: []config.RepoConfig{
					{
						Name:     "private-repo",
						Slug:     "org/private",
						Schedule: "*/30 * * * *",
						Auth:     config.AuthConfig{TokenEnv: "PRIVATE_TOKEN"},
						Refs:     []config.RefConfig{{Ref: "main", Paths: []string{"docs/"}}},
					},
				},
			},
		}

		sources := config.FlattenProviders(providers)
		if len(sources) != 1 {
			t.Fatalf("len(sources) = %d, want 1", len(sources))
		}
		s := sources[0]
		if s.Schedule != "*/30 * * * *" {
			t.Errorf("Schedule = %q, want '*/30 * * * *'", s.Schedule)
		}
		if s.Auth.TokenEnv != "PRIVATE_TOKEN" {
			t.Errorf("Auth.TokenEnv = %q, want PRIVATE_TOKEN", s.Auth.TokenEnv)
		}
	})

	t.Run("confluence produces one source per provider", func(t *testing.T) {
		providers := []config.ProviderConfig{
			{
				Type:     "confluence",
				BaseURL:  "https://confluence.example.com",
				Proxy:    false,
				Schedule: "0 */4 * * *",
				Auth:     config.AuthConfig{Type: "bearer", TokenEnv: "CONFLUENCE_PAT"},
				Pages: []config.PageConfig{
					{Name: "arch", Space: "ARCH", ID: "789012"},
					{Space: "DEV", ID: "123456"},
				},
			},
		}

		sources := config.FlattenProviders(providers)
		if len(sources) != 1 {
			t.Fatalf("len(sources) = %d, want 1", len(sources))
		}
		s := sources[0]
		if s.Provider != "confluence" {
			t.Errorf("Provider = %q, want confluence", s.Provider)
		}
		if s.BaseURL != "https://confluence.example.com" {
			t.Errorf("BaseURL = %q", s.BaseURL)
		}
		if s.Proxy {
			t.Errorf("Proxy = true, want false")
		}
		if len(s.Pages) != 2 {
			t.Errorf("len(Pages) = %d, want 2", len(s.Pages))
		}
	})

	t.Run("multiple repos produce multiple sources", func(t *testing.T) {
		providers := []config.ProviderConfig{
			{
				Type: "github",
				Auth: config.AuthConfig{TokenEnv: "GH_TOKEN"},
				Repos: []config.RepoConfig{
					{
						Name: "repo-a",
						Slug: "org/a",
						Refs: []config.RefConfig{{Ref: "main", Paths: []string{"docs/"}}},
					},
					{
						Name: "repo-b",
						Slug: "org/b",
						Refs: []config.RefConfig{{Ref: "main", Paths: []string{"docs/"}}},
					},
				},
			},
		}

		sources := config.FlattenProviders(providers)
		if len(sources) != 2 {
			t.Fatalf("len(sources) = %d, want 2", len(sources))
		}
		if sources[0].Name != "repo-a" {
			t.Errorf("sources[0].Name = %q", sources[0].Name)
		}
		if sources[1].Name != "repo-b" {
			t.Errorf("sources[1].Name = %q", sources[1].Name)
		}
	})
}

func TestLibraryName(t *testing.T) {
	t.Run("git default name", func(t *testing.T) {
		ref := config.RefConfig{Ref: "main"}
		got := config.LibraryName("github", "myrepo", ref, config.PageConfig{})
		if got != "myrepo/main" {
			t.Errorf("LibraryName() = %q, want %q", got, "myrepo/main")
		}
	})

	t.Run("git custom ref name", func(t *testing.T) {
		ref := config.RefConfig{Name: "custom-lib", Ref: "v3.0"}
		got := config.LibraryName("github", "myrepo", ref, config.PageConfig{})
		if got != "custom-lib" {
			t.Errorf("LibraryName() = %q, want %q", got, "custom-lib")
		}
	})

	t.Run("confluence with page name", func(t *testing.T) {
		page := config.PageConfig{Name: "architecture-decisions", Space: "ARCH", ID: "789012"}
		got := config.LibraryName("confluence", "", config.RefConfig{}, page)
		if got != "architecture-decisions" {
			t.Errorf("LibraryName() = %q, want %q", got, "architecture-decisions")
		}
	})

	t.Run("confluence default name", func(t *testing.T) {
		page := config.PageConfig{Space: "DEV", ID: "123456"}
		got := config.LibraryName("confluence", "", config.RefConfig{}, page)
		if got != "DEV/123456" {
			t.Errorf("LibraryName() = %q, want %q", got, "DEV/123456")
		}
	})
}

func TestConfluenceDepthDefault(t *testing.T) {
	content := `
providers:
  - type: confluence
    base_url: https://confluence.example.com
    pages:
      - space: MYSPACE
        id: "123"
`
	f := writeTempConfig(t, content)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	page := cfg.Providers[0].Pages[0]
	if page.Depth != nil {
		t.Errorf("Depth = %v, want nil (unset)", page.Depth)
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
