package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the full docserve configuration.
type Config struct {
	DataDir        string         `yaml:"data_dir"`
	Listen         string         `yaml:"listen"`
	AllowedOrigins []string       `yaml:"allowed_origins"`
	AuthTokenEnv   string         `yaml:"auth_token_env"`
	AuthToken      string         `yaml:"-"`
	Proxy          ProxyConfig    `yaml:"proxy"`
	Sources        []SourceConfig `yaml:"sources"`
}

// ProxyConfig holds HTTP/HTTPS proxy settings.
type ProxyConfig struct {
	HTTP  string `yaml:"http"`
	HTTPS string `yaml:"https"`
}

// RefConfig describes a single ref (branch/tag/space) within a source.
type RefConfig struct {
	Name  string   `yaml:"name"`
	Ref   string   `yaml:"ref"`
	Paths []string `yaml:"paths"`
	Space string   `yaml:"space"`
	ID    string   `yaml:"id"`
	Depth *int     `yaml:"depth"`
}

// SourceConfig describes a documentation source.
type SourceConfig struct {
	Name     string      `yaml:"name"`
	Provider string      `yaml:"provider"`
	Repo     string      `yaml:"repo"`
	Org      string      `yaml:"org"`      // Azure DevOps
	Project  string      `yaml:"project"`  // Azure DevOps
	BaseURL  string      `yaml:"base_url"`
	Refs     []RefConfig `yaml:"refs"`
	Proxy    bool        // defaults to true; handled via custom UnmarshalYAML
	Schedule string      `yaml:"schedule"`
	Auth     AuthConfig  `yaml:"auth"`
}

// rawSource mirrors SourceConfig but uses *bool for Proxy to detect absence.
type rawSource struct {
	Name     string      `yaml:"name"`
	Provider string      `yaml:"provider"`
	Repo     string      `yaml:"repo"`
	Org      string      `yaml:"org"`
	Project  string      `yaml:"project"`
	BaseURL  string      `yaml:"base_url"`
	Refs     []RefConfig `yaml:"refs"`
	Proxy    *bool       `yaml:"proxy"`
	Schedule string      `yaml:"schedule"`
	Auth     AuthConfig  `yaml:"auth"`
}

// UnmarshalYAML implements yaml.Unmarshaler so that Proxy defaults to true.
func (s *SourceConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw rawSource
	if err := value.Decode(&raw); err != nil {
		return err
	}

	s.Name = raw.Name
	s.Provider = raw.Provider
	s.Repo = raw.Repo
	s.Org = raw.Org
	s.Project = raw.Project
	s.BaseURL = raw.BaseURL
	s.Refs = raw.Refs
	s.Schedule = raw.Schedule
	s.Auth = raw.Auth

	if raw.Proxy == nil {
		s.Proxy = true
	} else {
		s.Proxy = *raw.Proxy
	}

	return nil
}

// AuthConfig holds authentication settings for a source.
type AuthConfig struct {
	Type        string `yaml:"type"`        // "basic", "bearer", or "" (github token)
	TokenEnv    string `yaml:"token_env"`
	UsernameEnv string `yaml:"username_env"`
	PasswordEnv string `yaml:"password_env"`
}

// LibraryName returns the library name for a given source and ref.
// If the ref has a custom name, it is returned; otherwise a default is generated.
func LibraryName(src SourceConfig, ref RefConfig) string {
	if ref.Name != "" {
		return ref.Name
	}
	return defaultLibraryName(src, ref)
}

func defaultLibraryName(src SourceConfig, ref RefConfig) string {
	switch src.Provider {
	case "confluence":
		return src.Name + "/" + ref.Space + "/" + ref.ID
	default:
		return src.Name + "/" + ref.Ref
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

	if cfg.AuthTokenEnv != "" {
		cfg.AuthToken = os.Getenv(cfg.AuthTokenEnv)
		if cfg.AuthToken == "" {
			return nil, fmt.Errorf("auth_token_env: environment variable %q is not set or empty", cfg.AuthTokenEnv)
		}
	}

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validate checks that the config is semantically correct.
func validate(cfg *Config) error {
	for i, o := range cfg.AllowedOrigins {
		o = strings.TrimSuffix(o, "/")
		u, err := url.Parse(o)
		if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" {
			return fmt.Errorf("allowed_origins: %q must be scheme://host[:port] with no path", o)
		}
		cfg.AllowedOrigins[i] = o
	}

	if len(cfg.Sources) == 0 {
		return fmt.Errorf("config must have at least one source")
	}

	seen := make(map[string]bool)

	for i, src := range cfg.Sources {
		if src.Name == "" {
			return fmt.Errorf("source[%d]: name is required", i)
		}
		if src.Provider == "" {
			return fmt.Errorf("source %q: provider is required", src.Name)
		}

		switch src.Provider {
		case "github", "azure-devops":
			if len(src.Refs) == 0 {
				return fmt.Errorf("source %q: refs must have at least one entry", src.Name)
			}
			for j, ref := range src.Refs {
				if ref.Ref == "" {
					return fmt.Errorf("source %q: refs[%d]: ref is required", src.Name, j)
				}
				if len(ref.Paths) == 0 {
					return fmt.Errorf("source %q: refs[%d]: at least one path is required", src.Name, j)
				}
				libName := LibraryName(src, ref)
				if seen[libName] {
					return fmt.Errorf("duplicate library name %q", libName)
				}
				seen[libName] = true
			}
		case "confluence":
			if src.BaseURL == "" {
				return fmt.Errorf("source %q: base_url is required for confluence provider", src.Name)
			}
			if len(src.Refs) == 0 {
				return fmt.Errorf("source %q: refs must have at least one entry", src.Name)
			}
			for j, ref := range src.Refs {
				if ref.Space == "" {
					return fmt.Errorf("source %q: refs[%d]: space is required for confluence provider", src.Name, j)
				}
				if ref.ID == "" {
					return fmt.Errorf("source %q: refs[%d]: id is required for confluence provider", src.Name, j)
				}
				libName := LibraryName(src, ref)
				if seen[libName] {
					return fmt.Errorf("duplicate library name %q", libName)
				}
				seen[libName] = true
			}
		default:
			return fmt.Errorf("source %q: unknown provider %q (must be github, azure-devops, or confluence)", src.Name, src.Provider)
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
