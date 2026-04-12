package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the full docserve configuration.
type Config struct {
	DataDir   string           `yaml:"data_dir"`
	Listen    string           `yaml:"listen"`
	Proxy     ProxyConfig      `yaml:"proxy"`
	Providers []ProviderConfig `yaml:"providers"`
}

// ProxyConfig holds HTTP/HTTPS proxy settings.
type ProxyConfig struct {
	HTTP  string `yaml:"http"`
	HTTPS string `yaml:"https"`
}

// ProviderConfig describes a documentation provider and its repositories or pages.
type ProviderConfig struct {
	Type     string       `yaml:"type"`     // "github", "azure-devops", "confluence"
	BaseURL  string       `yaml:"base_url"` // required for azure-devops, confluence
	Proxy    bool         // default true; handled via custom UnmarshalYAML
	Schedule string       `yaml:"schedule"`
	Auth     AuthConfig   `yaml:"auth"`
	Repos    []RepoConfig `yaml:"repos"`  // git providers only
	Pages    []PageConfig `yaml:"pages"`  // confluence only
}

// rawProvider mirrors ProviderConfig but uses *bool for Proxy to detect absence.
type rawProvider struct {
	Type     string       `yaml:"type"`
	BaseURL  string       `yaml:"base_url"`
	Proxy    *bool        `yaml:"proxy"`
	Schedule string       `yaml:"schedule"`
	Auth     AuthConfig   `yaml:"auth"`
	Repos    []RepoConfig `yaml:"repos"`
	Pages    []PageConfig `yaml:"pages"`
}

// UnmarshalYAML implements yaml.Unmarshaler so that Proxy defaults to true.
func (p *ProviderConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw rawProvider
	if err := value.Decode(&raw); err != nil {
		return err
	}

	p.Type = raw.Type
	p.BaseURL = raw.BaseURL
	p.Schedule = raw.Schedule
	p.Auth = raw.Auth
	p.Repos = raw.Repos
	p.Pages = raw.Pages

	if raw.Proxy == nil {
		p.Proxy = true
	} else {
		p.Proxy = *raw.Proxy
	}

	return nil
}

// RepoConfig describes a repository within a git provider.
type RepoConfig struct {
	Name     string      `yaml:"name"`     // required
	Slug     string      `yaml:"slug"`     // owner/repo (github) or repo name (azure-devops)
	Project  string      `yaml:"project"`  // azure-devops only
	Schedule string      `yaml:"schedule"` // override provider schedule
	Auth     AuthConfig  `yaml:"auth"`     // override provider auth
	Refs     []RefConfig `yaml:"refs"`
}

// RefConfig describes a single ref (branch/tag) within a repo.
type RefConfig struct {
	Name  string   `yaml:"name"`  // optional, override library name
	Ref   string   `yaml:"ref"`   // branch/tag/sha
	Paths []string `yaml:"paths"`
}

// PageConfig describes a Confluence page to index.
type PageConfig struct {
	Name  string `yaml:"name"`
	Space string `yaml:"space"`
	ID    string `yaml:"id"`
	Depth *int   `yaml:"depth"` // nil -> -1 (unlimited)
}

// AuthConfig holds authentication settings for a provider or repo.
type AuthConfig struct {
	Type        string `yaml:"type"`         // "basic", "bearer", or "" (github token)
	TokenEnv    string `yaml:"token_env"`
	UsernameEnv string `yaml:"username_env"`
	PasswordEnv string `yaml:"password_env"`
}

// ResolvedSource is a fully resolved source with auth/schedule/proxy inherited
// from the provider. This is what the rest of the codebase consumes.
type ResolvedSource struct {
	// Common
	Name     string
	Provider string // "github", "azure-devops", "confluence"
	BaseURL  string
	Proxy    bool
	Schedule string
	Auth     AuthConfig

	// Git providers
	Slug    string // owner/repo or repo name
	Project string // azure-devops only
	Refs    []RefConfig

	// Confluence
	Pages []PageConfig
}

// isAuthEmpty returns true if the AuthConfig has no fields set.
func isAuthEmpty(a AuthConfig) bool {
	return a.Type == "" && a.TokenEnv == "" && a.UsernameEnv == "" && a.PasswordEnv == ""
}

// FlattenProviders converts hierarchical ProviderConfig entries into a flat
// list of ResolvedSource entries suitable for the rest of the codebase.
// For git providers (github, azure-devops): one ResolvedSource per repo.
// For confluence: one ResolvedSource per provider (with all pages).
func FlattenProviders(providers []ProviderConfig) []ResolvedSource {
	var sources []ResolvedSource

	for _, p := range providers {
		switch p.Type {
		case "confluence":
			name := "confluence"
			if p.BaseURL != "" {
				name = p.BaseURL
			}
			sources = append(sources, ResolvedSource{
				Name:     name,
				Provider: p.Type,
				BaseURL:  p.BaseURL,
				Proxy:    p.Proxy,
				Schedule: p.Schedule,
				Auth:     p.Auth,
				Pages:    p.Pages,
			})
		default: // github, azure-devops
			for _, repo := range p.Repos {
				auth := p.Auth
				if !isAuthEmpty(repo.Auth) {
					auth = repo.Auth
				}
				schedule := p.Schedule
				if repo.Schedule != "" {
					schedule = repo.Schedule
				}
				sources = append(sources, ResolvedSource{
					Name:     repo.Name,
					Provider: p.Type,
					BaseURL:  p.BaseURL,
					Proxy:    p.Proxy,
					Schedule: schedule,
					Auth:     auth,
					Slug:     repo.Slug,
					Project:  repo.Project,
					Refs:     repo.Refs,
				})
			}
		}
	}

	return sources
}

// LibraryName returns the library name for a given source context.
// For git: repo.Name/ref.Ref unless ref.Name is set.
// For confluence: page.Name if set, else page.Space/page.ID.
func LibraryName(provider string, repoName string, ref RefConfig, page PageConfig) string {
	switch provider {
	case "confluence":
		if page.Name != "" {
			return page.Name
		}
		return page.Space + "/" + page.ID
	default:
		if ref.Name != "" {
			return ref.Name
		}
		return repoName + "/" + ref.Ref
	}
}

// Load reads the YAML config at path, applies defaults, and validates it.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}

	// Apply defaults.
	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validate checks that the config is semantically correct.
func validate(cfg *Config) error {
	if len(cfg.Providers) == 0 {
		return fmt.Errorf("config must have at least one provider")
	}

	seen := make(map[string]bool)

	for i, p := range cfg.Providers {
		if p.Type == "" {
			return fmt.Errorf("providers[%d]: type is required", i)
		}

		switch p.Type {
		case "github", "azure-devops":
			if len(p.Pages) > 0 {
				return fmt.Errorf("provider %q (providers[%d]): pages not allowed for %s provider", p.Type, i, p.Type)
			}
			if len(p.Repos) == 0 {
				return fmt.Errorf("provider %q (providers[%d]): repos must have at least one entry", p.Type, i)
			}
			if p.Type == "azure-devops" && p.BaseURL == "" {
				return fmt.Errorf("provider %q (providers[%d]): base_url is required for azure-devops provider", p.Type, i)
			}
			for j, repo := range p.Repos {
				if repo.Name == "" {
					return fmt.Errorf("provider %q (providers[%d]): repos[%d]: name is required", p.Type, i, j)
				}
				if repo.Slug == "" {
					return fmt.Errorf("provider %q (providers[%d]): repos[%d] %q: slug is required", p.Type, i, j, repo.Name)
				}
				if p.Type == "azure-devops" && repo.Project == "" {
					return fmt.Errorf("provider %q (providers[%d]): repos[%d] %q: project is required for azure-devops", p.Type, i, j, repo.Name)
				}
				if len(repo.Refs) == 0 {
					return fmt.Errorf("provider %q (providers[%d]): repos[%d] %q: at least one ref is required", p.Type, i, j, repo.Name)
				}
				for k, ref := range repo.Refs {
					if ref.Ref == "" {
						return fmt.Errorf("provider %q (providers[%d]): repos[%d] %q: refs[%d]: ref is required", p.Type, i, j, repo.Name, k)
					}
					if len(ref.Paths) == 0 {
						return fmt.Errorf("provider %q (providers[%d]): repos[%d] %q: refs[%d]: at least one path is required", p.Type, i, j, repo.Name, k)
					}
					libName := LibraryName(p.Type, repo.Name, ref, PageConfig{})
					if seen[libName] {
						return fmt.Errorf("duplicate library name %q", libName)
					}
					seen[libName] = true
				}
			}

		case "confluence":
			if len(p.Repos) > 0 {
				return fmt.Errorf("provider %q (providers[%d]): repos not allowed for confluence provider", p.Type, i)
			}
			if p.BaseURL == "" {
				return fmt.Errorf("provider %q (providers[%d]): base_url is required for confluence provider", p.Type, i)
			}
			if len(p.Pages) == 0 {
				return fmt.Errorf("provider %q (providers[%d]): pages must have at least one entry", p.Type, i)
			}
			for j, page := range p.Pages {
				if page.Space == "" {
					return fmt.Errorf("provider %q (providers[%d]): pages[%d]: space is required", p.Type, i, j)
				}
				if page.ID == "" {
					return fmt.Errorf("provider %q (providers[%d]): pages[%d]: id is required", p.Type, i, j)
				}
				libName := LibraryName(p.Type, "", RefConfig{}, page)
				if seen[libName] {
					return fmt.Errorf("duplicate library name %q", libName)
				}
				seen[libName] = true
			}

		default:
			return fmt.Errorf("providers[%d]: unknown type %q (must be github, azure-devops, or confluence)", i, p.Type)
		}
	}

	return nil
}

// ResolvePath finds the config file to use. Priority:
//  1. explicit (non-empty string passed by caller)
//  2. $DOCSERVE_CONFIG environment variable
//  3. ./docserve.yaml
//  4. /etc/docserve/docserve.yaml
func ResolvePath(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("config file %q not found: %w", explicit, err)
		}
		return explicit, nil
	}

	if env := os.Getenv("DOCSERVE_CONFIG"); env != "" {
		if _, err := os.Stat(env); err != nil {
			return "", fmt.Errorf("config file from $DOCSERVE_CONFIG %q not found: %w", env, err)
		}
		return env, nil
	}

	candidates := []string{
		"./docserve.yaml",
		"/etc/docserve/docserve.yaml",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	return "", fmt.Errorf("no config file found; tried $DOCSERVE_CONFIG, ./docserve.yaml, /etc/docserve/docserve.yaml")
}
