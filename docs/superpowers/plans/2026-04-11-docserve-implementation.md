# docserve Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a self-hosted MCP documentation server that fetches docs from Git providers, indexes them with FTS5, and serves them to LLM agents via Streamable HTTP.

**Architecture:** Single Go binary with CLI subcommands. Provider-based source fetching (GitHub, Azure DevOps). Markdown/AsciiDoc chunking via line-by-line state machines. SQLite FTS5 for search. mcp-go for MCP Streamable HTTP transport.

**Tech Stack:** Go 1.22+, `mcp-go` (mark3labs), `modernc.org/sqlite`, `yaml.v3`, GoReleaser.

**Spec:** `docs/superpowers/specs/2026-04-11-docserve-design.md`

---

## File Map

| File | Responsibility |
|------|---------------|
| `cmd/docserve/main.go` | CLI entrypoint, subcommand dispatch, flag parsing |
| `internal/config/config.go` | YAML config struct, parsing, validation, file resolution |
| `internal/index/store.go` | SQLite connection, schema migration, library CRUD |
| `internal/index/search.go` | FTS5 query building, BM25 ranking, token budget |
| `internal/index/chunker.go` | Chunker interface, dispatch by file extension |
| `internal/index/markdown.go` | Markdown chunker state machine |
| `internal/index/asciidoc.go` | AsciiDoc chunker state machine |
| `internal/source/provider.go` | Provider interface, factory, auth helpers |
| `internal/source/github.go` | GitHub/GHE provider: resolve + fetch tarball |
| `internal/source/azuredevops.go` | Azure DevOps provider: resolve + fetch zip |
| `internal/source/fetcher.go` | Orchestration: resolve -> skip-if-same -> fetch -> chunk -> index |
| `internal/mcp/server.go` | MCP server setup, tool registration, Streamable HTTP |
| `internal/mcp/tools.go` | Tool handler implementations (3 tools) |
| `internal/scheduler/scheduler.go` | Internal cron scheduler for periodic fetch |
| `testdata/markdown/simple.md` | Markdown fixture with headings and code blocks |
| `testdata/markdown/frontmatter.md` | Markdown fixture with YAML front matter |
| `testdata/markdown/large.md` | Markdown fixture exceeding chunk size limit |
| `testdata/asciidoc/simple.adoc` | AsciiDoc fixture with headings and code blocks |
| `testdata/asciidoc/admonitions.adoc` | AsciiDoc fixture with admonitions |
| `docserve.example.yaml` | Example configuration file |
| `Makefile` | Build, test, lint targets |
| `.goreleaser.yaml` | Multi-platform release config |
| `Dockerfile` | Multi-stage scratch image |

---

### Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`, `cmd/docserve/main.go`, `Makefile`
- Modify: `flake.nix`

- [ ] **Step 1: Initialize Go module**

```bash
cd /home/agent/projects/mcp-docs
go mod init github.com/codanael/docserve
```

- [ ] **Step 2: Create minimal main.go**

Create `cmd/docserve/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		fmt.Println("serve: not implemented")
	case "fetch":
		fmt.Println("fetch: not implemented")
	case "list":
		fmt.Println("list: not implemented")
	case "search":
		fmt.Println("search: not implemented")
	case "version":
		fmt.Println("docserve", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: docserve <command> [flags]

Commands:
  serve     Start the MCP documentation server
  fetch     Fetch/update documentation from sources
  list      List indexed libraries
  search    Search documentation (debug/test)
  version   Print version`)
}
```

- [ ] **Step 3: Create Makefile**

Create `Makefile`:

```makefile
VERSION := $(shell git describe --tags --always 2>/dev/null || echo "dev")

.PHONY: build test test-integration lint clean

build:
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$(VERSION)" \
		-o bin/docserve ./cmd/docserve

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
```

- [ ] **Step 4: Update flake.nix**

Replace the existing flake.nix with a docserve-specific dev shell. Keep the nix structure but replace the buildInputs and shellHook:

```nix
{
  description = "docserve — self-hosted MCP documentation server";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    claude-code.url = "github:sadjow/claude-code-nix";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
    claude-code,
  }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ claude-code.overlays.default ];
          config.allowUnfreePredicate = pkg: builtins.elem (nixpkgs.lib.getName pkg) [
            "claude-code"
          ];
        };
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = [
            pkgs.claude-code

            # Go
            pkgs.go_1_24
            pkgs.gopls
            pkgs.gotools
            pkgs.golangci-lint
            pkgs.goreleaser

            # SQLite (debug)
            pkgs.sqlite

            # Node.js (MCP Inspector via npx)
            pkgs.nodejs_22

            # Tools
            pkgs.git
            pkgs.jq
            pkgs.curl
          ];

          shellHook = ''
            echo "docserve dev environment loaded"
            echo "Go:   $(go version)"
            echo "Node: $(node --version)"
          '';
        };
      }
    );
}
```

- [ ] **Step 5: Verify build**

```bash
make build
bin/docserve version
```

Expected: `docserve dev`

- [ ] **Step 6: Commit**

```bash
git add cmd/docserve/main.go go.mod Makefile flake.nix
git commit -m "feat: project scaffolding with CLI skeleton and dev shell"
```

---

### Task 2: Configuration

**Files:**
- Create: `internal/config/config.go`, `internal/config/config_test.go`, `docserve.example.yaml`

- [ ] **Step 1: Write the test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	yaml := `
data_dir: /tmp/docserve-test
listen: ":9090"

proxy:
  http: http://proxy.local:3128
  https: http://proxy.local:3128

sources:
  - name: mylib
    provider: github
    repo: org/mylib
    ref: main
    proxy: true
    paths:
      - "docs/"
    schedule: "0 3 * * 0"
    auth:
      token_env: MY_TOKEN
  - name: internal
    provider: azure-devops
    org: myorg
    project: myproj
    repo: myrepo
    base_url: https://dev.azure.com
    ref: main
    proxy: false
    paths:
      - "docs/"
    auth:
      type: basic
      username_env: AZ_USER
      password_env: AZ_PAT
`
	path := filepath.Join(t.TempDir(), "docserve.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.DataDir != "/tmp/docserve-test" {
		t.Errorf("DataDir = %q, want /tmp/docserve-test", cfg.DataDir)
	}
	if cfg.Listen != ":9090" {
		t.Errorf("Listen = %q, want :9090", cfg.Listen)
	}
	if cfg.Proxy.HTTP != "http://proxy.local:3128" {
		t.Errorf("Proxy.HTTP = %q", cfg.Proxy.HTTP)
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("len(Sources) = %d, want 2", len(cfg.Sources))
	}

	gh := cfg.Sources[0]
	if gh.Name != "mylib" || gh.Provider != "github" || gh.Repo != "org/mylib" {
		t.Errorf("github source: %+v", gh)
	}
	if !gh.Proxy {
		t.Error("github source: Proxy should be true")
	}
	if gh.Auth.TokenEnv != "MY_TOKEN" {
		t.Errorf("github auth token_env = %q", gh.Auth.TokenEnv)
	}

	az := cfg.Sources[1]
	if az.Provider != "azure-devops" || az.Org != "myorg" || az.Project != "myproj" {
		t.Errorf("azure source: %+v", az)
	}
	if az.Proxy {
		t.Error("azure source: Proxy should be false")
	}
	if az.Auth.Type != "basic" || az.Auth.UsernameEnv != "AZ_USER" || az.Auth.PasswordEnv != "AZ_PAT" {
		t.Errorf("azure auth: %+v", az.Auth)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	yaml := `
sources:
  - name: test
    provider: github
    repo: org/test
    ref: main
    paths:
      - "docs/"
`
	path := filepath.Join(t.TempDir(), "docserve.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Listen != ":8080" {
		t.Errorf("default Listen = %q, want :8080", cfg.Listen)
	}
	if cfg.DataDir != "data" {
		t.Errorf("default DataDir = %q, want data", cfg.DataDir)
	}
	if !cfg.Sources[0].Proxy {
		t.Error("default Proxy should be true")
	}
}

func TestLoadConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"no sources", `sources: []`},
		{"no name", `sources: [{provider: github, repo: x/y, ref: main, paths: ["docs/"]}]`},
		{"no provider", `sources: [{name: x, repo: x/y, ref: main, paths: ["docs/"]}]`},
		{"no ref", `sources: [{name: x, provider: github, repo: x/y, paths: ["docs/"]}]`},
		{"no paths", `sources: [{name: x, provider: github, repo: x/y, ref: main}]`},
		{"empty paths", `sources: [{name: x, provider: github, repo: x/y, ref: main, paths: []}]`},
		{"unknown provider", `sources: [{name: x, provider: bitbucket, repo: x/y, ref: main, paths: ["docs/"]}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "docserve.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestResolveConfigPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docserve.yaml")
	if err := os.WriteFile(path, []byte("sources: [{name: x, provider: github, repo: x/y, ref: main, paths: [\"d/\"]}]"), 0644); err != nil {
		t.Fatal(err)
	}

	// Explicit path
	got, err := ResolvePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("ResolvePath(%q) = %q", path, got)
	}

	// Non-existent
	_, err = ResolvePath("/nonexistent/path.yaml")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/config/...
```

Expected: compilation error — `config` package doesn't exist yet.

- [ ] **Step 3: Implement config.go**

Create `internal/config/config.go`:

```go
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DataDir string       `yaml:"data_dir"`
	Listen  string       `yaml:"listen"`
	Proxy   ProxyConfig  `yaml:"proxy"`
	Sources []SourceConfig `yaml:"sources"`
}

type ProxyConfig struct {
	HTTP  string `yaml:"http"`
	HTTPS string `yaml:"https"`
}

type SourceConfig struct {
	Name     string   `yaml:"name"`
	Provider string   `yaml:"provider"`
	Repo     string   `yaml:"repo"`
	Org      string   `yaml:"org"`
	Project  string   `yaml:"project"`
	BaseURL  string   `yaml:"base_url"`
	Ref      string   `yaml:"ref"`
	Proxy    bool     `yaml:"proxy"`
	Paths    []string `yaml:"paths"`
	Schedule string   `yaml:"schedule"`
	Auth     AuthConfig `yaml:"auth"`
}

type AuthConfig struct {
	Type        string `yaml:"type"`
	TokenEnv    string `yaml:"token_env"`
	UsernameEnv string `yaml:"username_env"`
	PasswordEnv string `yaml:"password_env"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.DataDir == "" {
		c.DataDir = "data"
	}
	for i := range c.Sources {
		// Proxy defaults to true when not explicitly set.
		// yaml.v3 unmarshals missing bool as false, so we need
		// a different approach: use a pointer or a custom unmarshaler.
		// For simplicity, we check if the YAML had the field set
		// by re-parsing. Instead, we use a pragmatic approach:
		// proxy defaults to true, and users set proxy: false explicitly.
		// Since yaml.v3 unmarshals missing bool as false, we handle this
		// by making the default in the struct tag not possible.
		// We'll use a *bool approach instead — see SourceConfig change.
	}
}

func (c *Config) validate() error {
	if len(c.Sources) == 0 {
		return fmt.Errorf("at least one source is required")
	}
	validProviders := map[string]bool{"github": true, "azure-devops": true}
	for i, s := range c.Sources {
		if s.Name == "" {
			return fmt.Errorf("source[%d]: name is required", i)
		}
		if s.Provider == "" {
			return fmt.Errorf("source[%d] %q: provider is required", i, s.Name)
		}
		if !validProviders[s.Provider] {
			return fmt.Errorf("source[%d] %q: unknown provider %q (supported: github, azure-devops)", i, s.Name, s.Provider)
		}
		if s.Ref == "" {
			return fmt.Errorf("source[%d] %q: ref is required", i, s.Name)
		}
		if len(s.Paths) == 0 {
			return fmt.Errorf("source[%d] %q: at least one path is required", i, s.Name)
		}
	}
	return nil
}

// ResolvePath finds the config file path from explicit path, env var, or defaults.
func ResolvePath(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("config file not found: %w", err)
		}
		return explicit, nil
	}

	if envPath := os.Getenv("DOCSERVE_CONFIG"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
	}

	candidates := []string{
		"docserve.yaml",
		"/etc/docserve/docserve.yaml",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	return "", fmt.Errorf("no config file found (tried: --config flag, $DOCSERVE_CONFIG, ./docserve.yaml, /etc/docserve/docserve.yaml)")
}
```

**Important fix needed**: the `Proxy` bool default issue. Since YAML unmarshals missing booleans as `false`, but we want the default to be `true`, we need to handle this. The cleanest approach: use a custom `UnmarshalYAML` on `SourceConfig`.

Update the `SourceConfig` definition and add custom unmarshaling:

```go
// Replace the Proxy field in SourceConfig with:
type SourceConfig struct {
	Name     string   `yaml:"name"`
	Provider string   `yaml:"provider"`
	Repo     string   `yaml:"repo"`
	Org      string   `yaml:"org"`
	Project  string   `yaml:"project"`
	BaseURL  string   `yaml:"base_url"`
	Ref      string   `yaml:"ref"`
	Proxy    bool     // not tagged — handled by UnmarshalYAML
	Paths    []string `yaml:"paths"`
	Schedule string   `yaml:"schedule"`
	Auth     AuthConfig `yaml:"auth"`
}

// rawSource is used for unmarshaling to detect missing fields.
type rawSource struct {
	Name     string   `yaml:"name"`
	Provider string   `yaml:"provider"`
	Repo     string   `yaml:"repo"`
	Org      string   `yaml:"org"`
	Project  string   `yaml:"project"`
	BaseURL  string   `yaml:"base_url"`
	Ref      string   `yaml:"ref"`
	Proxy    *bool    `yaml:"proxy"`
	Paths    []string `yaml:"paths"`
	Schedule string   `yaml:"schedule"`
	Auth     AuthConfig `yaml:"auth"`
}

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
	s.Ref = raw.Ref
	s.Paths = raw.Paths
	s.Schedule = raw.Schedule
	s.Auth = raw.Auth
	if raw.Proxy != nil {
		s.Proxy = *raw.Proxy
	} else {
		s.Proxy = true // default
	}
	return nil
}
```

Remove the `applyDefaults` loop for proxy (keep `Listen` and `DataDir` defaults):

```go
func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.DataDir == "" {
		c.DataDir = "data"
	}
}
```

- [ ] **Step 4: Add yaml.v3 dependency**

```bash
go get gopkg.in/yaml.v3
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/config/... -v
```

Expected: all tests PASS.

- [ ] **Step 6: Create example config**

Create `docserve.example.yaml`:

```yaml
# docserve configuration
# See: docs/superpowers/specs/2026-04-11-docserve-design.md

data_dir: /var/lib/docserve
listen: ":8080"

# Global HTTP proxy (used when a source has proxy: true)
proxy:
  http: http://proxy.internal:3128
  https: http://proxy.internal:3128

sources:
  # GitHub (public or GitHub Enterprise) — via proxy
  - name: spring-boot
    provider: github
    repo: spring-projects/spring-boot
    # base_url: https://github.example.com/api/v3  # uncomment for GHE
    ref: v3.4.x
    proxy: true
    paths:
      - "spring-boot-project/spring-boot-docs/src/docs/asciidoc"
    schedule: "0 3 * * 0"
    auth:
      token_env: GITHUB_TOKEN

  # Azure DevOps — direct access, no proxy
  # - name: internal-api
  #   provider: azure-devops
  #   org: myorg
  #   project: myproject
  #   repo: internal-api
  #   base_url: https://dev.azure.com
  #   ref: main
  #   proxy: false
  #   paths:
  #     - "docs/"
  #   auth:
  #     type: basic
  #     username_env: AZURE_USER
  #     password_env: AZURE_PAT
```

- [ ] **Step 7: Commit**

```bash
git add internal/config/ docserve.example.yaml go.mod go.sum
git commit -m "feat: config parsing with YAML, validation, and proxy defaults"
```

---

### Task 3: SQLite Store

**Files:**
- Create: `internal/index/store.go`, `internal/index/store_test.go`

- [ ] **Step 1: Write the test**

Create `internal/index/store_test.go`:

```go
package index

import (
	"testing"
	"time"
)

func TestStoreMigration(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
}

func TestStoreLibraryCRUD(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().Truncate(time.Second)
	lib := Library{
		Name:      "mylib",
		Repo:      "org/mylib",
		Ref:       "v1.0.0",
		CommitSHA: "abc123",
		FetchedAt: now,
	}

	id, err := s.UpsertLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}

	got, err := s.GetLibrary("mylib")
	if err != nil {
		t.Fatal(err)
	}
	if got.CommitSHA != "abc123" {
		t.Errorf("CommitSHA = %q, want abc123", got.CommitSHA)
	}

	// Update
	lib.CommitSHA = "def456"
	lib.Ref = "v2.0.0"
	id2, err := s.UpsertLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if id2 != id {
		t.Errorf("UpsertLibrary should return same id, got %d vs %d", id2, id)
	}
	got, err = s.GetLibrary("mylib")
	if err != nil {
		t.Fatal(err)
	}
	if got.CommitSHA != "def456" || got.Ref != "v2.0.0" {
		t.Errorf("after update: %+v", got)
	}

	// List
	libs, err := s.ListLibraries()
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 1 {
		t.Errorf("len(ListLibraries) = %d, want 1", len(libs))
	}
}

func TestStoreChunks(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	lib := Library{
		Name: "testlib", Repo: "org/test", Ref: "main",
		CommitSHA: "aaa", FetchedAt: time.Now(),
	}
	libID, err := s.UpsertLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}

	chunks := []Chunk{
		{Path: "docs/intro.md", Breadcrumb: "Introduction", Content: "Welcome to testlib documentation."},
		{Path: "docs/config.md", Breadcrumb: "Configuration > Database", Content: "Configure the database connection string."},
		{Path: "docs/config.md", Breadcrumb: "Configuration > Cache", Content: "Configure the Redis cache endpoint."},
	}

	if err := s.ReplaceChunks(libID, chunks); err != nil {
		t.Fatal(err)
	}

	// Verify chunks exist
	results, err := s.Search(libID, "database", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("expected search results for 'database'")
	}
	if results[0].Breadcrumb != "Configuration > Database" {
		t.Errorf("top result breadcrumb = %q", results[0].Breadcrumb)
	}

	// Replace chunks (transactional)
	newChunks := []Chunk{
		{Path: "docs/intro.md", Breadcrumb: "Introduction", Content: "Updated intro."},
	}
	if err := s.ReplaceChunks(libID, newChunks); err != nil {
		t.Fatal(err)
	}

	// Old chunks should be gone
	results, err = s.Search(libID, "database", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Error("expected no results after replacing chunks")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/index/... -v
```

Expected: compilation error — package doesn't exist.

- [ ] **Step 3: Implement store.go**

Create `internal/index/store.go`:

```go
package index

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Library struct {
	ID        int64
	Name      string
	Repo      string
	Ref       string
	CommitSHA string
	FetchedAt time.Time
}

type Chunk struct {
	Path       string
	Breadcrumb string
	Content    string
}

type SearchResult struct {
	Path       string
	Breadcrumb string
	Content    string
	Score      float64
}

type Store struct {
	db *sql.DB
}

func OpenStore(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS libraries (
			id          INTEGER PRIMARY KEY,
			name        TEXT UNIQUE NOT NULL,
			repo        TEXT NOT NULL,
			ref         TEXT NOT NULL,
			commit_sha  TEXT NOT NULL,
			fetched_at  DATETIME NOT NULL
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS chunks USING fts5(
			library_id UNINDEXED,
			path,
			breadcrumb,
			content,
			tokenize='porter unicode61'
		)`,
		`CREATE TABLE IF NOT EXISTS chunk_meta (
			rowid       INTEGER PRIMARY KEY,
			library_id  INTEGER NOT NULL REFERENCES libraries(id),
			path        TEXT NOT NULL,
			breadcrumb  TEXT NOT NULL,
			byte_size   INTEGER NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:40], err)
		}
	}
	return nil
}

func (s *Store) UpsertLibrary(lib Library) (int64, error) {
	res, err := s.db.Exec(`
		INSERT INTO libraries (name, repo, ref, commit_sha, fetched_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			repo = excluded.repo,
			ref = excluded.ref,
			commit_sha = excluded.commit_sha,
			fetched_at = excluded.fetched_at`,
		lib.Name, lib.Repo, lib.Ref, lib.CommitSHA, lib.FetchedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert library: %w", err)
	}

	// ON CONFLICT DO UPDATE sets LastInsertId to the existing row.
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		// Fallback: query by name.
		row := s.db.QueryRow("SELECT id FROM libraries WHERE name = ?", lib.Name)
		if err := row.Scan(&id); err != nil {
			return 0, fmt.Errorf("get library id: %w", err)
		}
	}
	return id, nil
}

func (s *Store) GetLibrary(name string) (*Library, error) {
	var lib Library
	err := s.db.QueryRow(
		"SELECT id, name, repo, ref, commit_sha, fetched_at FROM libraries WHERE name = ?",
		name,
	).Scan(&lib.ID, &lib.Name, &lib.Repo, &lib.Ref, &lib.CommitSHA, &lib.FetchedAt)
	if err != nil {
		return nil, fmt.Errorf("get library %q: %w", name, err)
	}
	return &lib, nil
}

func (s *Store) ListLibraries() ([]Library, error) {
	rows, err := s.db.Query("SELECT id, name, repo, ref, commit_sha, fetched_at FROM libraries ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var libs []Library
	for rows.Next() {
		var lib Library
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.Repo, &lib.Ref, &lib.CommitSHA, &lib.FetchedAt); err != nil {
			return nil, err
		}
		libs = append(libs, lib)
	}
	return libs, rows.Err()
}

func (s *Store) ReplaceChunks(libraryID int64, chunks []Chunk) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete old chunks for this library.
	if _, err := tx.Exec("DELETE FROM chunk_meta WHERE library_id = ?", libraryID); err != nil {
		return fmt.Errorf("delete chunk_meta: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM chunks WHERE library_id = ?", libraryID); err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}

	// Insert new chunks.
	ftsStmt, err := tx.Prepare("INSERT INTO chunks (library_id, path, breadcrumb, content) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer ftsStmt.Close()

	metaStmt, err := tx.Prepare("INSERT INTO chunk_meta (rowid, library_id, path, breadcrumb, byte_size) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer metaStmt.Close()

	for _, c := range chunks {
		res, err := ftsStmt.Exec(libraryID, c.Path, c.Breadcrumb, c.Content)
		if err != nil {
			return fmt.Errorf("insert chunk: %w", err)
		}
		rowid, _ := res.LastInsertId()
		if _, err := metaStmt.Exec(rowid, libraryID, c.Path, c.Breadcrumb, len(c.Content)); err != nil {
			return fmt.Errorf("insert chunk_meta: %w", err)
		}
	}

	return tx.Commit()
}

// Search is a placeholder — full implementation in search.go (Task 9).
func (s *Store) Search(libraryID int64, query string, maxTokens int) ([]SearchResult, error) {
	rows, err := s.db.Query(`
		SELECT path, breadcrumb, content, bm25(chunks, 0.0, 1.5, 2.0, 1.0) AS score
		FROM chunks
		WHERE library_id = ? AND chunks MATCH ?
		ORDER BY score
		LIMIT 20`,
		libraryID, query,
	)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	tokenBudget := maxTokens
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Path, &r.Breadcrumb, &r.Content, &r.Score); err != nil {
			return nil, err
		}
		tokens := len(r.Content) / 4
		if tokenBudget-tokens < 0 && len(results) > 0 {
			break
		}
		tokenBudget -= tokens
		results = append(results, r)
	}
	return results, rows.Err()
}

// Ready returns true if the DB is accessible and at least one library is indexed.
func (s *Store) Ready() bool {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM libraries").Scan(&count)
	return err == nil && count > 0
}
```

- [ ] **Step 4: Add sqlite dependency**

```bash
go get modernc.org/sqlite
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/index/... -v
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/index/store.go internal/index/store_test.go go.mod go.sum
git commit -m "feat: SQLite store with FTS5, library CRUD, and chunk management"
```

---

### Task 4: Markdown Chunker

**Files:**
- Create: `internal/index/chunker.go`, `internal/index/markdown.go`, `internal/index/markdown_test.go`
- Create: `testdata/markdown/simple.md`, `testdata/markdown/frontmatter.md`, `testdata/markdown/large.md`

- [ ] **Step 1: Create test fixtures**

Create `testdata/markdown/simple.md`:

```markdown
# Getting Started

Welcome to the library.

## Installation

Run the following command:

```bash
npm install mylib
```

## Configuration

### Database

Set the connection string:

```yaml
db: postgres://localhost/mydb
```

### Cache

Configure Redis:

```
REDIS_URL=redis://localhost:6379
```
```

Create `testdata/markdown/frontmatter.md`:

```markdown
---
title: API Reference
category: reference
---

# API Reference

The main API endpoint.

## Authentication

Use Bearer tokens.
```

Create `testdata/markdown/large.md` — a file with a single heading but very long content (>4000 tokens, i.e. >16000 chars). Generate with a paragraph repeated many times:

```markdown
# Large Section

This is a paragraph about configuring the system. It contains important details about how the configuration works and what options are available. This paragraph will be repeated many times to simulate a very large documentation section that exceeds the chunk size limit.

This is a paragraph about configuring the system. It contains important details about how the configuration works and what options are available. This paragraph will be repeated many times to simulate a very large documentation section that exceeds the chunk size limit.
```

(Repeat the paragraph ~100 times to exceed 16000 characters.)

- [ ] **Step 2: Write the test**

Create `internal/index/markdown_test.go`:

```go
package index

import (
	"os"
	"testing"
)

func TestMarkdownChunkerSimple(t *testing.T) {
	data, err := os.ReadFile("../../testdata/markdown/simple.md")
	if err != nil {
		t.Fatal(err)
	}

	c := &MarkdownChunker{}
	chunks, err := c.Chunk("docs/simple.md", data)
	if err != nil {
		t.Fatal(err)
	}

	// Expect chunks for: Getting Started, Installation, Configuration > Database, Configuration > Cache
	if len(chunks) < 4 {
		t.Fatalf("got %d chunks, want at least 4", len(chunks))
	}

	// Check breadcrumbs
	breadcrumbs := map[string]bool{}
	for _, ch := range chunks {
		breadcrumbs[ch.Breadcrumb] = true
	}

	expected := []string{
		"Getting Started",
		"Getting Started > Installation",
		"Getting Started > Configuration > Database",
		"Getting Started > Configuration > Cache",
	}
	for _, e := range expected {
		if !breadcrumbs[e] {
			t.Errorf("missing breadcrumb %q, got %v", e, breadcrumbs)
		}
	}

	// Check that code blocks are preserved intact
	for _, ch := range chunks {
		if ch.Breadcrumb == "Getting Started > Installation" {
			if !contains(ch.Content, "npm install mylib") {
				t.Error("Installation chunk should contain the code block")
			}
		}
	}
}

func TestMarkdownChunkerFrontmatter(t *testing.T) {
	data, err := os.ReadFile("../../testdata/markdown/frontmatter.md")
	if err != nil {
		t.Fatal(err)
	}

	c := &MarkdownChunker{}
	chunks, err := c.Chunk("docs/api.md", data)
	if err != nil {
		t.Fatal(err)
	}

	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want at least 2", len(chunks))
	}

	// Front matter should be stripped, not appear as content
	for _, ch := range chunks {
		if contains(ch.Content, "category: reference") {
			t.Error("front matter should be stripped from content")
		}
	}
}

func TestMarkdownChunkerLargeSection(t *testing.T) {
	data, err := os.ReadFile("../../testdata/markdown/large.md")
	if err != nil {
		t.Fatal(err)
	}

	c := &MarkdownChunker{}
	chunks, err := c.Chunk("docs/large.md", data)
	if err != nil {
		t.Fatal(err)
	}

	// A section >16000 chars (~4000 tokens) should be split into multiple chunks
	if len(chunks) < 2 {
		t.Fatalf("large section should be split, got %d chunks", len(chunks))
	}

	// All chunks should share the same breadcrumb prefix
	for _, ch := range chunks {
		if ch.Breadcrumb != "Large Section" {
			t.Errorf("unexpected breadcrumb %q", ch.Breadcrumb)
		}
	}
}

func TestMarkdownChunkerEmpty(t *testing.T) {
	c := &MarkdownChunker{}
	chunks, err := c.Chunk("empty.md", []byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Errorf("empty input should produce 0 chunks, got %d", len(chunks))
	}
}

func TestMarkdownChunkerNoHeadings(t *testing.T) {
	c := &MarkdownChunker{}
	chunks, err := c.Chunk("plain.md", []byte("Just some text.\n\nAnother paragraph."))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("no-heading file should produce 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Breadcrumb != "" {
		t.Errorf("breadcrumb should be empty, got %q", chunks[0].Breadcrumb)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/index/... -run TestMarkdown -v
```

Expected: compilation error — `MarkdownChunker` not defined.

- [ ] **Step 4: Implement chunker interface**

Create `internal/index/chunker.go`:

```go
package index

import (
	"fmt"
	"path/filepath"
)

const maxChunkBytes = 16000 // ~4000 tokens

type Chunker interface {
	Chunk(filename string, content []byte) ([]Chunk, error)
}

func NewChunker(filename string) Chunker {
	switch filepath.Ext(filename) {
	case ".adoc", ".asciidoc":
		return &AsciidocChunker{}
	case ".md", ".mdx":
		return &MarkdownChunker{}
	default:
		return &PlainChunker{}
	}
}

// PlainChunker treats the entire file as one chunk.
type PlainChunker struct{}

func (p *PlainChunker) Chunk(filename string, content []byte) ([]Chunk, error) {
	text := string(content)
	if len(text) == 0 {
		return nil, nil
	}
	chunks := splitBySize(text, "", maxChunkBytes)
	result := make([]Chunk, len(chunks))
	for i, c := range chunks {
		result[i] = Chunk{Path: filename, Breadcrumb: "", Content: c}
	}
	return result, nil
}

// splitBySize splits text on paragraph boundaries ("\n\n") when it exceeds maxBytes.
func splitBySize(text string, breadcrumb string, maxBytes int) []string {
	if len(text) <= maxBytes {
		return []string{text}
	}

	var parts []string
	remaining := text
	for len(remaining) > maxBytes {
		// Find the last paragraph break before maxBytes.
		cutoff := maxBytes
		idx := lastIndex(remaining[:cutoff], "\n\n")
		if idx > 0 {
			cutoff = idx
		}
		parts = append(parts, remaining[:cutoff])
		remaining = remaining[cutoff:]
		// Skip leading newlines.
		for len(remaining) > 0 && remaining[0] == '\n' {
			remaining = remaining[1:]
		}
	}
	if len(remaining) > 0 {
		parts = append(parts, remaining)
	}
	return parts
}

func lastIndex(s, substr string) int {
	for i := len(s) - len(substr); i >= 0; i-- {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// AsciidocChunker is a placeholder — implemented in Task 5.
type AsciidocChunker struct{}

func (a *AsciidocChunker) Chunk(filename string, content []byte) ([]Chunk, error) {
	return nil, fmt.Errorf("asciidoc chunker not yet implemented")
}
```

- [ ] **Step 5: Implement markdown.go**

Create `internal/index/markdown.go`:

```go
package index

import (
	"strings"
)

type MarkdownChunker struct{}

func (m *MarkdownChunker) Chunk(filename string, content []byte) ([]Chunk, error) {
	lines := strings.Split(string(content), "\n")
	lines = stripFrontmatter(lines)

	if len(lines) == 0 || (len(lines) == 1 && strings.TrimSpace(lines[0]) == "") {
		return nil, nil
	}

	type section struct {
		breadcrumb string
		level      int
		lines      []string
	}

	var sections []section
	// headingStack tracks the current heading hierarchy.
	// headingStack[i] is the heading text at level i+1.
	headingStack := make([]string, 6)
	inCodeBlock := false
	var currentLines []string
	currentBreadcrumb := ""
	currentLevel := 0

	flush := func() {
		text := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if text != "" {
			sections = append(sections, section{
				breadcrumb: currentBreadcrumb,
				level:      currentLevel,
				lines:      currentLines,
			})
		}
		currentLines = nil
	}

	for _, line := range lines {
		// Track fenced code blocks.
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			currentLines = append(currentLines, line)
			continue
		}
		if inCodeBlock {
			currentLines = append(currentLines, line)
			continue
		}

		// Detect heading.
		if level, title := parseMarkdownHeading(line); level > 0 {
			flush()

			// Update heading stack.
			headingStack[level-1] = title
			// Clear deeper levels.
			for i := level; i < 6; i++ {
				headingStack[i] = ""
			}

			// Build breadcrumb from stack.
			var parts []string
			for i := 0; i < level; i++ {
				if headingStack[i] != "" {
					parts = append(parts, headingStack[i])
				}
			}
			currentBreadcrumb = strings.Join(parts, " > ")
			currentLevel = level
			continue
		}

		currentLines = append(currentLines, line)
	}
	flush()

	// Convert sections to chunks, splitting large ones.
	var chunks []Chunk
	for _, sec := range sections {
		text := strings.TrimSpace(strings.Join(sec.lines, "\n"))
		if text == "" {
			continue
		}
		parts := splitBySize(text, sec.breadcrumb, maxChunkBytes)
		for _, part := range parts {
			chunks = append(chunks, Chunk{
				Path:       filename,
				Breadcrumb: sec.breadcrumb,
				Content:    part,
			})
		}
	}

	return chunks, nil
}

// parseMarkdownHeading returns (level, title) if the line is a markdown heading.
// Returns (0, "") if not a heading.
func parseMarkdownHeading(line string) (int, string) {
	if len(line) == 0 || line[0] != '#' {
		return 0, ""
	}
	level := 0
	for level < len(line) && level < 6 && line[level] == '#' {
		level++
	}
	if level >= len(line) || line[level] != ' ' {
		return 0, "" // "###word" is not a heading
	}
	title := strings.TrimSpace(line[level+1:])
	return level, title
}

// stripFrontmatter removes YAML front matter (--- delimited) from the start of lines.
func stripFrontmatter(lines []string) []string {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return lines
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return lines[i+1:]
		}
	}
	// Unclosed front matter — return as-is.
	return lines
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/index/... -run TestMarkdown -v
```

Expected: all Markdown tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/index/chunker.go internal/index/markdown.go internal/index/markdown_test.go testdata/
git commit -m "feat: markdown chunker with heading-based splitting and front matter"
```

---

### Task 5: AsciiDoc Chunker

**Files:**
- Create: `internal/index/asciidoc.go`, `internal/index/asciidoc_test.go`
- Create: `testdata/asciidoc/simple.adoc`, `testdata/asciidoc/admonitions.adoc`

- [ ] **Step 1: Create test fixtures**

Create `testdata/asciidoc/simple.adoc`:

```asciidoc
= Getting Started

Welcome to the library.

== Installation

Run the following command:

[source,bash]
----
mvn install mylib
----

== Configuration

=== Database

Set the connection string:

[source,yaml]
----
spring.datasource.url: jdbc:postgresql://localhost/mydb
----

=== Cache

Configure Redis endpoint.
```

Create `testdata/asciidoc/admonitions.adoc`:

```asciidoc
= Security Guide

== Authentication

Use OAuth2 for authentication.

NOTE: Always use HTTPS in production.

TIP: You can configure multiple providers.

== Authorization

Role-based access control.

WARNING: Admin role grants full access.

IMPORTANT: Review permissions regularly.
```

- [ ] **Step 2: Write the test**

Create `internal/index/asciidoc_test.go`:

```go
package index

import (
	"os"
	"strings"
	"testing"
)

func TestAsciidocChunkerSimple(t *testing.T) {
	data, err := os.ReadFile("../../testdata/asciidoc/simple.adoc")
	if err != nil {
		t.Fatal(err)
	}

	c := &AsciidocChunker{}
	chunks, err := c.Chunk("docs/simple.adoc", data)
	if err != nil {
		t.Fatal(err)
	}

	if len(chunks) < 4 {
		t.Fatalf("got %d chunks, want at least 4", len(chunks))
	}

	breadcrumbs := map[string]bool{}
	for _, ch := range chunks {
		breadcrumbs[ch.Breadcrumb] = true
	}

	expected := []string{
		"Getting Started",
		"Getting Started > Installation",
		"Getting Started > Configuration > Database",
		"Getting Started > Configuration > Cache",
	}
	for _, e := range expected {
		if !breadcrumbs[e] {
			t.Errorf("missing breadcrumb %q, got %v", e, breadcrumbs)
		}
	}

	// Check code blocks are preserved
	for _, ch := range chunks {
		if ch.Breadcrumb == "Getting Started > Installation" {
			if !strings.Contains(ch.Content, "mvn install mylib") {
				t.Error("Installation chunk should contain the code block")
			}
		}
	}
}

func TestAsciidocChunkerAdmonitions(t *testing.T) {
	data, err := os.ReadFile("../../testdata/asciidoc/admonitions.adoc")
	if err != nil {
		t.Fatal(err)
	}

	c := &AsciidocChunker{}
	chunks, err := c.Chunk("docs/security.adoc", data)
	if err != nil {
		t.Fatal(err)
	}

	// Admonitions should be part of their section, not split out
	for _, ch := range chunks {
		if ch.Breadcrumb == "Security Guide > Authentication" {
			if !strings.Contains(ch.Content, "NOTE:") || !strings.Contains(ch.Content, "TIP:") {
				t.Error("Authentication chunk should contain admonitions")
			}
		}
		if ch.Breadcrumb == "Security Guide > Authorization" {
			if !strings.Contains(ch.Content, "WARNING:") {
				t.Error("Authorization chunk should contain WARNING admonition")
			}
		}
	}
}

func TestAsciidocChunkerEmpty(t *testing.T) {
	c := &AsciidocChunker{}
	chunks, err := c.Chunk("empty.adoc", []byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Errorf("empty input should produce 0 chunks, got %d", len(chunks))
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/index/... -run TestAsciidoc -v
```

Expected: FAIL — `AsciidocChunker.Chunk` returns "not yet implemented".

- [ ] **Step 4: Implement asciidoc.go**

Replace the placeholder in `internal/index/chunker.go` and create `internal/index/asciidoc.go`:

```go
package index

import (
	"strings"
)

// Replace the placeholder AsciidocChunker in chunker.go by removing it there
// and defining the real implementation here.

func (a *AsciidocChunker) Chunk(filename string, content []byte) ([]Chunk, error) {
	lines := strings.Split(string(content), "\n")

	if len(lines) == 0 || (len(lines) == 1 && strings.TrimSpace(lines[0]) == "") {
		return nil, nil
	}

	type section struct {
		breadcrumb string
		level      int
		lines      []string
	}

	var sections []section
	headingStack := make([]string, 6)
	inCodeBlock := false
	var currentLines []string
	currentBreadcrumb := ""
	currentLevel := 0

	flush := func() {
		text := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if text != "" {
			sections = append(sections, section{
				breadcrumb: currentBreadcrumb,
				level:      currentLevel,
				lines:      currentLines,
			})
		}
		currentLines = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track code blocks (---- delimited).
		if strings.HasPrefix(trimmed, "----") {
			inCodeBlock = !inCodeBlock
			currentLines = append(currentLines, line)
			continue
		}
		if inCodeBlock {
			currentLines = append(currentLines, line)
			continue
		}

		// Detect heading.
		if level, title := parseAsciidocHeading(line); level > 0 {
			flush()

			headingStack[level-1] = title
			for i := level; i < 6; i++ {
				headingStack[i] = ""
			}

			var parts []string
			for i := 0; i < level; i++ {
				if headingStack[i] != "" {
					parts = append(parts, headingStack[i])
				}
			}
			currentBreadcrumb = strings.Join(parts, " > ")
			currentLevel = level
			continue
		}

		// Skip source block annotations like [source,java] — they stay as lines.
		currentLines = append(currentLines, line)
	}
	flush()

	var chunks []Chunk
	for _, sec := range sections {
		text := strings.TrimSpace(strings.Join(sec.lines, "\n"))
		if text == "" {
			continue
		}
		parts := splitBySize(text, sec.breadcrumb, maxChunkBytes)
		for _, part := range parts {
			chunks = append(chunks, Chunk{
				Path:       filename,
				Breadcrumb: sec.breadcrumb,
				Content:    part,
			})
		}
	}

	return chunks, nil
}

// parseAsciidocHeading returns (level, title) if the line is an AsciiDoc heading.
// AsciiDoc uses = for headings: "= Title" (level 1), "== Section" (level 2), etc.
func parseAsciidocHeading(line string) (int, string) {
	if len(line) == 0 || line[0] != '=' {
		return 0, ""
	}
	level := 0
	for level < len(line) && level < 6 && line[level] == '=' {
		level++
	}
	if level >= len(line) || line[level] != ' ' {
		return 0, "" // "===word" is not a heading
	}
	title := strings.TrimSpace(line[level+1:])
	return level, title
}
```

Now remove the placeholder `AsciidocChunker` struct and method from `chunker.go`. The struct definition stays in `chunker.go`, but remove the placeholder `Chunk` method:

In `internal/index/chunker.go`, remove these lines:
```go
// AsciidocChunker is a placeholder — implemented in Task 5.
type AsciidocChunker struct{}

func (a *AsciidocChunker) Chunk(filename string, content []byte) ([]Chunk, error) {
	return nil, fmt.Errorf("asciidoc chunker not yet implemented")
}
```

And replace with just the struct definition (the method is now in `asciidoc.go`):
```go
type AsciidocChunker struct{}
```

Also remove the `"fmt"` import from `chunker.go` if it's no longer used.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/index/... -run TestAsciidoc -v
```

Expected: all AsciiDoc tests PASS.

- [ ] **Step 6: Run all index tests to check for regressions**

```bash
go test ./internal/index/... -v
```

Expected: all tests PASS (markdown + asciidoc + store).

- [ ] **Step 7: Commit**

```bash
git add internal/index/asciidoc.go internal/index/asciidoc_test.go internal/index/chunker.go testdata/asciidoc/
git commit -m "feat: AsciiDoc chunker with heading splitting and code block handling"
```

---

### Task 6: Provider Interface + GitHub Provider

**Files:**
- Create: `internal/source/provider.go`, `internal/source/github.go`, `internal/source/github_test.go`

- [ ] **Step 1: Write the test**

Create `internal/source/github_test.go`:

```go
package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/codanael/docserve/internal/config"
)

func TestGitHubResolve(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/org/repo/commits/main", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token test-token" {
			t.Errorf("missing auth header: %q", r.Header.Get("Authorization"))
		}
		json.NewEncoder(w).Encode(map[string]string{"sha": "abc123def456"})
	})
	mux.HandleFunc("GET /repos/org/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name":         "v3.4.1",
			"target_commitish": "main",
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("TEST_GH_TOKEN", "test-token")

	cfg := config.SourceConfig{
		Name:     "test",
		Provider: "github",
		Repo:     "org/repo",
		BaseURL:  srv.URL,
		Auth:     config.AuthConfig{TokenEnv: "TEST_GH_TOKEN"},
	}

	p := NewGitHubProvider(cfg, srv.Client())

	// Resolve branch
	sha, err := p.Resolve(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if sha != "abc123def456" {
		t.Errorf("Resolve(main) = %q, want abc123def456", sha)
	}

	// Resolve latest
	tag, err := p.Resolve(context.Background(), "latest")
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v3.4.1" {
		t.Errorf("Resolve(latest) = %q, want v3.4.1", tag)
	}
}

func TestGitHubFetch(t *testing.T) {
	// Create a fake tarball with some files.
	tarball := createTestTarball(t, map[string]string{
		"org-repo-abc123/docs/intro.md":     "# Intro\nHello",
		"org-repo-abc123/docs/config.md":    "# Config\nSettings",
		"org-repo-abc123/src/main.go":       "package main",
		"org-repo-abc123/docs/sub/deep.md":  "# Deep\nNested doc",
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/org/repo/tarball/abc123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(tarball)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := config.SourceConfig{
		Name:     "test",
		Provider: "github",
		Repo:     "org/repo",
		BaseURL:  srv.URL,
		Paths:    []string{"docs/"},
		Auth:     config.AuthConfig{TokenEnv: "TEST_GH_TOKEN"},
	}
	t.Setenv("TEST_GH_TOKEN", "test-token")

	p := NewGitHubProvider(cfg, srv.Client())
	destDir := t.TempDir()

	err := p.Fetch(context.Background(), "abc123", cfg.Paths, destDir)
	if err != nil {
		t.Fatal(err)
	}

	// Check that only docs/ files were extracted.
	assertFileExists(t, filepath.Join(destDir, "docs/intro.md"))
	assertFileExists(t, filepath.Join(destDir, "docs/config.md"))
	assertFileExists(t, filepath.Join(destDir, "docs/sub/deep.md"))
	assertFileNotExists(t, filepath.Join(destDir, "src/main.go"))
}

func createTestTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, content); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gw.Close()
	return buf.Bytes()
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file to exist: %s", path)
	}
}

func assertFileNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("expected file to NOT exist: %s", path)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/source/... -v
```

Expected: compilation error.

- [ ] **Step 3: Implement provider.go**

Create `internal/source/provider.go`:

```go
package source

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"

	"github.com/codanael/docserve/internal/config"
)

type Provider interface {
	Resolve(ctx context.Context, ref string) (string, error)
	Fetch(ctx context.Context, sha string, paths []string, destDir string) error
}

func NewProvider(cfg config.SourceConfig, client *http.Client) (Provider, error) {
	switch cfg.Provider {
	case "github":
		return NewGitHubProvider(cfg, client), nil
	case "azure-devops":
		return NewAzureDevOpsProvider(cfg, client), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
}

// authTransport wraps an http.RoundTripper to inject auth headers.
type authTransport struct {
	base http.RoundTripper
	auth config.AuthConfig
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	switch t.auth.Type {
	case "basic":
		user := os.Getenv(t.auth.UsernameEnv)
		pass := os.Getenv(t.auth.PasswordEnv)
		if user != "" && pass != "" {
			cred := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
			req.Header.Set("Authorization", "Basic "+cred)
		}
	case "bearer":
		if token := os.Getenv(t.auth.TokenEnv); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	default:
		// GitHub-style token auth (type is empty, token_env is set).
		if token := os.Getenv(t.auth.TokenEnv); token != "" {
			req.Header.Set("Authorization", "token "+token)
		}
	}
	return t.base.RoundTrip(req)
}

// WrapClientAuth wraps an http.Client with authentication from the config.
func WrapClientAuth(client *http.Client, auth config.AuthConfig) *http.Client {
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &http.Client{
		Transport: &authTransport{base: transport, auth: auth},
		Timeout:   client.Timeout,
	}
}
```

- [ ] **Step 4: Implement github.go**

Create `internal/source/github.go`:

```go
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

const defaultGitHubAPI = "https://api.github.com"

type GitHubProvider struct {
	cfg    config.SourceConfig
	client *http.Client
	apiURL string
}

func NewGitHubProvider(cfg config.SourceConfig, client *http.Client) *GitHubProvider {
	apiURL := cfg.BaseURL
	if apiURL == "" {
		apiURL = defaultGitHubAPI
	}
	return &GitHubProvider{
		cfg:    cfg,
		client: WrapClientAuth(client, cfg.Auth),
		apiURL: strings.TrimRight(apiURL, "/"),
	}
}

func (g *GitHubProvider) Resolve(ctx context.Context, ref string) (string, error) {
	owner, repo, err := splitRepo(g.cfg.Repo)
	if err != nil {
		return "", err
	}

	if ref == "latest" {
		return g.resolveLatestRelease(ctx, owner, repo)
	}
	return g.resolveCommitSHA(ctx, owner, repo, ref)
}

func (g *GitHubProvider) resolveCommitSHA(ctx context.Context, owner, repo, ref string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s", g.apiURL, owner, repo, ref)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve ref %q: %w", ref, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve ref %q: HTTP %d", ref, resp.StatusCode)
	}

	var result struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.SHA, nil
}

func (g *GitHubProvider) resolveLatestRelease(ctx context.Context, owner, repo string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", g.apiURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve latest release: HTTP %d", resp.StatusCode)
	}

	var result struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.TagName, nil
}

func (g *GitHubProvider) Fetch(ctx context.Context, sha string, paths []string, destDir string) error {
	owner, repo, err := splitRepo(g.cfg.Repo)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/repos/%s/%s/tarball/%s", g.apiURL, owner, repo, sha)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("download tarball: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download tarball: HTTP %d", resp.StatusCode)
	}

	return extractTarGz(resp.Body, destDir, paths)
}

// extractTarGz extracts a .tar.gz archive, keeping only files under the given path prefixes.
// GitHub tarballs have a top-level directory like "owner-repo-sha/" which is stripped.
func extractTarGz(r io.Reader, destDir string, paths []string) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		// Strip the top-level directory (e.g., "owner-repo-sha/").
		name := hdr.Name
		idx := strings.IndexByte(name, '/')
		if idx < 0 {
			continue
		}
		relPath := name[idx+1:]

		if !matchesAnyPrefix(relPath, paths) {
			continue
		}

		destPath := filepath.Join(destDir, relPath)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		f, err := os.Create(destPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}

	return nil
}

func matchesAnyPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func splitRepo(repo string) (owner, name string, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo format %q (expected owner/repo)", repo)
	}
	return parts[0], parts[1], nil
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/source/... -v
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/source/
git commit -m "feat: provider interface and GitHub provider with tarball fetching"
```

---

### Task 7: Azure DevOps Provider

**Files:**
- Create: `internal/source/azuredevops.go`, `internal/source/azuredevops_test.go`

- [ ] **Step 1: Write the test**

Create `internal/source/azuredevops_test.go`:

```go
package source

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/codanael/docserve/internal/config"
)

func TestAzureDevOpsResolve(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /myorg/myproj/_apis/git/repositories/myrepo/commits", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Basic dXNlcjpwYXQ=" { // base64("user:pat")
			t.Errorf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		version := r.URL.Query().Get("searchCriteria.itemVersion.version")
		if version != "main" {
			t.Errorf("version = %q, want main", version)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"value": []map[string]string{{"commitId": "azdo-sha-789"}},
			"count": 1,
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("AZ_USER", "user")
	t.Setenv("AZ_PAT", "pat")

	cfg := config.SourceConfig{
		Name:     "test",
		Provider: "azure-devops",
		Org:      "myorg",
		Project:  "myproj",
		Repo:     "myrepo",
		BaseURL:  srv.URL,
		Auth: config.AuthConfig{
			Type:        "basic",
			UsernameEnv: "AZ_USER",
			PasswordEnv: "AZ_PAT",
		},
	}

	p := NewAzureDevOpsProvider(cfg, srv.Client())

	sha, err := p.Resolve(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if sha != "azdo-sha-789" {
		t.Errorf("Resolve(main) = %q", sha)
	}
}

func TestAzureDevOpsFetch(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"docs/intro.md":    "# Intro\nAzDO doc",
		"docs/config.md":   "# Config\nSettings",
		"src/main.go":      "package main",
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /myorg/myproj/_apis/git/repositories/myrepo/items", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Write(zipData)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("AZ_USER", "user")
	t.Setenv("AZ_PAT", "pat")

	cfg := config.SourceConfig{
		Name:     "test",
		Provider: "azure-devops",
		Org:      "myorg",
		Project:  "myproj",
		Repo:     "myrepo",
		BaseURL:  srv.URL,
		Paths:    []string{"docs/"},
		Auth: config.AuthConfig{
			Type:        "basic",
			UsernameEnv: "AZ_USER",
			PasswordEnv: "AZ_PAT",
		},
	}

	p := NewAzureDevOpsProvider(cfg, srv.Client())
	destDir := t.TempDir()

	err := p.Fetch(context.Background(), "abc123", cfg.Paths, destDir)
	if err != nil {
		t.Fatal(err)
	}

	assertFileExists(t, filepath.Join(destDir, "docs/intro.md"))
	assertFileExists(t, filepath.Join(destDir, "docs/config.md"))
	assertFileNotExists(t, filepath.Join(destDir, "src/main.go"))
}

func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	zw.Close()
	return buf.Bytes()
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/source/... -run TestAzureDevOps -v
```

Expected: compilation error — `NewAzureDevOpsProvider` not defined.

- [ ] **Step 3: Implement azuredevops.go**

Create `internal/source/azuredevops.go`:

```go
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
	"strings"

	"github.com/codanael/docserve/internal/config"
)

const defaultAzureDevOpsURL = "https://dev.azure.com"

type AzureDevOpsProvider struct {
	cfg    config.SourceConfig
	client *http.Client
	apiURL string
}

func NewAzureDevOpsProvider(cfg config.SourceConfig, client *http.Client) *AzureDevOpsProvider {
	apiURL := cfg.BaseURL
	if apiURL == "" {
		apiURL = defaultAzureDevOpsURL
	}
	return &AzureDevOpsProvider{
		cfg:    cfg,
		client: WrapClientAuth(client, cfg.Auth),
		apiURL: strings.TrimRight(apiURL, "/"),
	}
}

func (a *AzureDevOpsProvider) Resolve(ctx context.Context, ref string) (string, error) {
	url := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/commits?searchCriteria.itemVersion.version=%s&$top=1&api-version=7.0",
		a.apiURL, a.cfg.Org, a.cfg.Project, a.cfg.Repo, ref)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve ref %q: %w", ref, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve ref %q: HTTP %d", ref, resp.StatusCode)
	}

	var result struct {
		Value []struct {
			CommitID string `json:"commitId"`
		} `json:"value"`
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.Count == 0 || len(result.Value) == 0 {
		return "", fmt.Errorf("no commits found for ref %q", ref)
	}
	return result.Value[0].CommitID, nil
}

func (a *AzureDevOpsProvider) Fetch(ctx context.Context, sha string, paths []string, destDir string) error {
	url := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/items?path=/&$format=zip&versionDescriptor.version=%s&versionDescriptor.versionType=commit&api-version=7.0",
		a.apiURL, a.cfg.Org, a.cfg.Project, a.cfg.Repo, sha)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("download zip: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download zip: HTTP %d", resp.StatusCode)
	}

	// Read the entire zip into memory (archives need random access).
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read zip body: %w", err)
	}

	return extractZip(body, destDir, paths)
}

func extractZip(data []byte, destDir string, paths []string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}

		if !matchesAnyPrefix(f.Name, paths) {
			continue
		}

		destPath := filepath.Join(destDir, f.Name)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		out, err := os.Create(destPath)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}

	return nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/source/... -v
```

Expected: all tests PASS (GitHub + Azure DevOps).

- [ ] **Step 5: Commit**

```bash
git add internal/source/azuredevops.go internal/source/azuredevops_test.go
git commit -m "feat: Azure DevOps provider with zip archive fetching"
```

---

### Task 8: Fetcher Orchestration

**Files:**
- Create: `internal/source/fetcher.go`, `internal/source/fetcher_test.go`

- [ ] **Step 1: Write the test**

Create `internal/source/fetcher_test.go`:

```go
package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codanael/docserve/internal/config"
	"github.com/codanael/docserve/internal/index"
)

// fakeProvider implements Provider for testing the fetch pipeline.
type fakeProvider struct {
	sha   string
	files map[string]string
}

func (f *fakeProvider) Resolve(ctx context.Context, ref string) (string, error) {
	return f.sha, nil
}

func (f *fakeProvider) Fetch(ctx context.Context, sha string, paths []string, destDir string) error {
	for name, content := range f.files {
		p := filepath.Join(destDir, name)
		os.MkdirAll(filepath.Dir(p), 0755)
		os.WriteFile(p, []byte(content), 0644)
	}
	return nil
}

func TestFetchPipeline(t *testing.T) {
	store, err := index.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	prov := &fakeProvider{
		sha: "sha-111",
		files: map[string]string{
			"docs/intro.md":  "# Intro\n\nWelcome to the lib.",
			"docs/config.md": "# Config\n\n## Database\n\nConnection settings.",
		},
	}

	cfg := config.SourceConfig{
		Name:     "testlib",
		Provider: "github",
		Repo:     "org/testlib",
		Ref:      "main",
		Paths:    []string{"docs/"},
	}

	dataDir := t.TempDir()
	f := NewFetcher(store, dataDir)

	result, err := f.FetchSource(context.Background(), cfg, prov)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated {
		t.Error("expected Updated=true on first fetch")
	}
	if result.ChunkCount == 0 {
		t.Error("expected chunks to be indexed")
	}

	// Second fetch with same SHA — should skip.
	result, err = f.FetchSource(context.Background(), cfg, prov)
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated {
		t.Error("expected Updated=false on duplicate SHA")
	}

	// Third fetch with new SHA — should update.
	prov.sha = "sha-222"
	prov.files["docs/intro.md"] = "# Intro\n\nUpdated welcome."
	result, err = f.FetchSource(context.Background(), cfg, prov)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated {
		t.Error("expected Updated=true on new SHA")
	}

	// Search should return updated content.
	lib, err := store.GetLibrary("testlib")
	if err != nil {
		t.Fatal(err)
	}
	results, err := store.Search(lib.ID, "Updated welcome", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("expected to find updated content")
	}
}

func TestFetchPipelineForce(t *testing.T) {
	store, err := index.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	prov := &fakeProvider{
		sha:   "sha-111",
		files: map[string]string{"docs/a.md": "# A\nContent"},
	}

	cfg := config.SourceConfig{
		Name: "testlib", Provider: "github", Repo: "org/testlib",
		Ref: "main", Paths: []string{"docs/"},
	}

	dataDir := t.TempDir()
	f := NewFetcher(store, dataDir)

	// First fetch.
	f.FetchSource(context.Background(), cfg, prov)

	// Force fetch — same SHA but should still update.
	f.Force = true
	result, err := f.FetchSource(context.Background(), cfg, prov)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated {
		t.Error("expected Updated=true with Force=true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/source/... -run TestFetchPipeline -v
```

Expected: compilation error — `NewFetcher`, `FetchSource` not defined.

- [ ] **Step 3: Implement fetcher.go**

Create `internal/source/fetcher.go`:

```go
package source

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/codanael/docserve/internal/config"
	"github.com/codanael/docserve/internal/index"
)

type FetchResult struct {
	Source     string
	SHA       string
	Updated   bool
	ChunkCount int
}

type Fetcher struct {
	store   *index.Store
	dataDir string
	Force   bool
}

func NewFetcher(store *index.Store, dataDir string) *Fetcher {
	return &Fetcher{store: store, dataDir: dataDir}
}

func (f *Fetcher) FetchSource(ctx context.Context, cfg config.SourceConfig, prov Provider) (*FetchResult, error) {
	result := &FetchResult{Source: cfg.Name}

	// 1. Resolve ref to commit SHA.
	sha, err := prov.Resolve(ctx, cfg.Ref)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", cfg.Name, err)
	}
	result.SHA = sha

	// 2. Check if already indexed (skip if same SHA and not forced).
	if !f.Force {
		lib, err := f.store.GetLibrary(cfg.Name)
		if err == nil && lib.CommitSHA == sha {
			log.Printf("[%s] already up-to-date at %s, skipping", cfg.Name, sha[:12])
			return result, nil
		}
	}

	// 3. Check raw cache — if we already have the files, don't re-download.
	rawDir := filepath.Join(f.dataDir, "raw", cfg.Name, sha)
	if _, err := os.Stat(rawDir); os.IsNotExist(err) {
		log.Printf("[%s] fetching %s...", cfg.Name, sha[:min(12, len(sha))])
		if err := os.MkdirAll(rawDir, 0755); err != nil {
			return nil, err
		}
		if err := prov.Fetch(ctx, sha, cfg.Paths, rawDir); err != nil {
			os.RemoveAll(rawDir)
			return nil, fmt.Errorf("fetch %s: %w", cfg.Name, err)
		}
	} else {
		log.Printf("[%s] using cached files for %s", cfg.Name, sha[:min(12, len(sha))])
	}

	// 4. Chunk all files.
	var chunks []index.Chunk
	for _, pathPrefix := range cfg.Paths {
		dir := filepath.Join(rawDir, pathPrefix)
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			relPath, _ := filepath.Rel(rawDir, path)
			chunker := index.NewChunker(path)
			fileChunks, err := chunker.Chunk(relPath, data)
			if err != nil {
				log.Printf("[%s] warning: chunking %s: %v", cfg.Name, relPath, err)
				return nil // skip file, don't fail the whole fetch
			}
			chunks = append(chunks, fileChunks...)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", pathPrefix, err)
		}
	}

	// 5. Index.
	lib := index.Library{
		Name:      cfg.Name,
		Repo:      cfg.Repo,
		Ref:       cfg.Ref + "@" + sha[:min(12, len(sha))],
		CommitSHA: sha,
		FetchedAt: time.Now(),
	}
	libID, err := f.store.UpsertLibrary(lib)
	if err != nil {
		return nil, fmt.Errorf("upsert library: %w", err)
	}
	if err := f.store.ReplaceChunks(libID, chunks); err != nil {
		return nil, fmt.Errorf("replace chunks: %w", err)
	}

	result.Updated = true
	result.ChunkCount = len(chunks)
	log.Printf("[%s] indexed %d chunks at %s", cfg.Name, len(chunks), sha[:min(12, len(sha))])
	return result, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/source/... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/source/fetcher.go internal/source/fetcher_test.go
git commit -m "feat: fetch pipeline orchestrating resolve, download, chunk, and index"
```

---

### Task 9: FTS5 Search Engine

**Files:**
- Create: `internal/index/search.go`, `internal/index/search_test.go`

- [ ] **Step 1: Write the test**

Create `internal/index/search_test.go`:

```go
package index

import (
	"testing"
	"time"
)

func TestSearchRanking(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	libID, _ := s.UpsertLibrary(Library{
		Name: "testlib", Repo: "org/test", Ref: "main",
		CommitSHA: "aaa", FetchedAt: time.Now(),
	})

	chunks := []Chunk{
		{Path: "docs/other.md", Breadcrumb: "Other Topic", Content: "This section is about unrelated features."},
		{Path: "docs/db.md", Breadcrumb: "Database Configuration", Content: "Configure the database connection string and pool size."},
		{Path: "docs/db.md", Breadcrumb: "Database Configuration > Advanced", Content: "Advanced database tuning: connection pool, timeouts, and retry configuration."},
	}
	if err := s.ReplaceChunks(libID, chunks); err != nil {
		t.Fatal(err)
	}

	results, err := s.Search(libID, "database configuration", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// The "Database Configuration" chunk should rank higher due to breadcrumb weight.
	if results[0].Breadcrumb != "Database Configuration" && results[0].Breadcrumb != "Database Configuration > Advanced" {
		t.Errorf("top result breadcrumb = %q", results[0].Breadcrumb)
	}
}

func TestSearchTokenBudget(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	libID, _ := s.UpsertLibrary(Library{
		Name: "testlib", Repo: "org/test", Ref: "main",
		CommitSHA: "aaa", FetchedAt: time.Now(),
	})

	// Create many chunks that match.
	var chunks []Chunk
	for i := 0; i < 20; i++ {
		chunks = append(chunks, Chunk{
			Path:       "docs/config.md",
			Breadcrumb: "Config",
			Content:    "The database configuration section provides details about connecting to the primary database server and configuring connection pools. " + string(rune('A'+i)),
		})
	}
	if err := s.ReplaceChunks(libID, chunks); err != nil {
		t.Fatal(err)
	}

	// With a very small token budget, we should get fewer results.
	results, err := s.Search(libID, "database", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > 5 {
		t.Errorf("expected <=5 results with 100 token budget, got %d", len(results))
	}
}

func TestSearchNoResults(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	libID, _ := s.UpsertLibrary(Library{
		Name: "testlib", Repo: "org/test", Ref: "main",
		CommitSHA: "aaa", FetchedAt: time.Now(),
	})

	chunks := []Chunk{
		{Path: "docs/a.md", Breadcrumb: "A", Content: "Hello world."},
	}
	s.ReplaceChunks(libID, chunks)

	results, err := s.Search(libID, "xyznonexistent", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestBuildFTSQuery(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"database configuration", "database OR configuration"},
		{"spring-boot actuator", "spring-boot OR actuator"},
		{"single", "single"},
		{"  spaced  words  ", "spaced OR words"},
	}
	for _, tt := range tests {
		got := buildFTSQuery(tt.input)
		if got != tt.expected {
			t.Errorf("buildFTSQuery(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/index/... -run "TestSearch|TestBuildFTS" -v
```

Expected: compilation error — `buildFTSQuery` not defined.

- [ ] **Step 3: Implement search.go**

Create `internal/index/search.go`:

```go
package index

import (
	"fmt"
	"strings"
)

// Search queries the FTS5 index for a library, returning results within the token budget.
// Replaces the placeholder Search method in store.go.
func (s *Store) SearchDocs(libraryID int64, query string, maxTokens int) ([]SearchResult, error) {
	ftsQuery := buildFTSQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}

	// BM25 weights: library_id (0 — unindexed), path (1.5), breadcrumb (2.0), content (1.0).
	// bm25() returns negative values where more negative = better match.
	rows, err := s.db.Query(`
		SELECT path, breadcrumb, content, bm25(chunks, 0.0, 1.5, 2.0, 1.0) AS score
		FROM chunks
		WHERE library_id = ? AND chunks MATCH ?
		ORDER BY score
		LIMIT 50`,
		libraryID, ftsQuery,
	)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	tokenBudget := maxTokens
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Path, &r.Breadcrumb, &r.Content, &r.Score); err != nil {
			return nil, err
		}
		tokens := len(r.Content) / 4
		if tokenBudget-tokens < 0 && len(results) > 0 {
			break
		}
		tokenBudget -= tokens
		results = append(results, r)
	}
	return results, rows.Err()
}

// buildFTSQuery converts a natural language query into an FTS5 MATCH expression.
// "database configuration" → "database OR configuration"
func buildFTSQuery(input string) string {
	words := strings.Fields(input)
	if len(words) == 0 {
		return ""
	}
	return strings.Join(words, " OR ")
}
```

Now update the tests to use `SearchDocs` instead of `Search`. Also update `store.go` to remove the placeholder `Search` method and update the store test to use `SearchDocs`.

In `internal/index/store.go`, remove the `Search` method (the placeholder we added in Task 3). In `internal/index/store_test.go`, replace all calls to `s.Search(` with `s.SearchDocs(`.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/index/... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/index/search.go internal/index/search_test.go internal/index/store.go internal/index/store_test.go
git commit -m "feat: FTS5 search with BM25 ranking and token budget"
```

---

### Task 10: MCP Tools

**Files:**
- Create: `internal/mcp/tools.go`, `internal/mcp/tools_test.go`

- [ ] **Step 1: Write the test**

Create `internal/mcp/tools_test.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/codanael/docserve/internal/index"
)

func setupTestStore(t *testing.T) *index.Store {
	t.Helper()
	s, err := index.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	libID, _ := s.UpsertLibrary(index.Library{
		Name: "spring-boot", Repo: "spring-projects/spring-boot",
		Ref: "v3.4.1", CommitSHA: "abc123", FetchedAt: time.Now(),
	})
	s.ReplaceChunks(libID, []index.Chunk{
		{Path: "docs/actuator.md", Breadcrumb: "Actuator > Health", Content: "Configure the health endpoint for monitoring."},
		{Path: "docs/config.md", Breadcrumb: "Configuration > Database", Content: "Set up the datasource connection."},
	})

	libID2, _ := s.UpsertLibrary(index.Library{
		Name: "angular", Repo: "angular/angular",
		Ref: "main@def456", CommitSHA: "def456", FetchedAt: time.Now(),
	})
	s.ReplaceChunks(libID2, []index.Chunk{
		{Path: "docs/components.md", Breadcrumb: "Components", Content: "Angular component lifecycle."},
	})

	return s
}

func TestListLibraries(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	req := mcplib.CallToolRequest{}
	result, err := h.ListLibraries(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Error("unexpected error result")
	}

	// Parse the JSON text content.
	text := result.Content[0].(mcplib.TextContent).Text
	var libs []map[string]string
	if err := json.Unmarshal([]byte(text), &libs); err != nil {
		t.Fatalf("unmarshal: %v, text: %s", err, text)
	}
	if len(libs) != 2 {
		t.Errorf("expected 2 libraries, got %d", len(libs))
	}
}

func TestResolveLibrary(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	req := mcplib.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "spring"}

	result, err := h.ResolveLibrary(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "spring-boot") {
		t.Errorf("expected spring-boot in result, got: %s", text)
	}
}

func TestResolveLibraryNotFound(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	req := mcplib.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "nonexistent"}

	result, err := h.ResolveLibrary(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error result for nonexistent library")
	}
}

func TestGetLibraryDocs(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	req := mcplib.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"library":    "spring-boot",
		"query":      "health endpoint",
		"max_tokens": float64(5000),
	}

	result, err := h.GetLibraryDocs(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	if !containsStr(text, "health") {
		t.Errorf("expected health content, got: %s", text)
	}
}

func TestGetLibraryDocsNotFound(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	req := mcplib.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"library": "nonexistent",
		"query":   "anything",
	}

	result, err := h.GetLibraryDocs(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent library")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/mcp/... -v
```

Expected: compilation error.

- [ ] **Step 3: Add mcp-go dependency**

```bash
go get github.com/mark3labs/mcp-go
```

- [ ] **Step 4: Implement tools.go**

Create `internal/mcp/tools.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/codanael/docserve/internal/index"
)

type ToolHandlers struct {
	Store *index.Store
}

func (h *ToolHandlers) ListLibraries(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	libs, err := h.Store.ListLibraries()
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("list libraries: %v", err)), nil
	}

	type libEntry struct {
		Name      string `json:"name"`
		Ref       string `json:"ref"`
		FetchedAt string `json:"fetched_at"`
	}

	entries := make([]libEntry, len(libs))
	for i, lib := range libs {
		entries[i] = libEntry{
			Name:      lib.Name,
			Ref:       lib.Ref,
			FetchedAt: lib.FetchedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("marshal: %v", err)), nil
	}
	return mcplib.NewToolResultText(string(data)), nil
}

func (h *ToolHandlers) ResolveLibrary(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	query := req.GetString("query", "")
	if query == "" {
		return mcplib.NewToolResultError("query parameter is required"), nil
	}

	libs, err := h.Store.FindLibraries(query)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("search: %v", err)), nil
	}
	if len(libs) == 0 {
		return mcplib.NewToolResultError(fmt.Sprintf("no library found matching %q", query)), nil
	}

	type result struct {
		Name string `json:"name"`
		Ref  string `json:"ref"`
	}
	data, _ := json.Marshal(result{Name: libs[0].Name, Ref: libs[0].Ref})
	return mcplib.NewToolResultText(string(data)), nil
}

func (h *ToolHandlers) GetLibraryDocs(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	library := req.GetString("library", "")
	query := req.GetString("query", "")
	maxTokens := req.GetInt("max_tokens", 5000)

	if library == "" || query == "" {
		return mcplib.NewToolResultError("library and query parameters are required"), nil
	}

	lib, err := h.Store.GetLibrary(library)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("library %q not found", library)), nil
	}

	results, err := h.Store.SearchDocs(lib.ID, query, maxTokens)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("search: %v", err)), nil
	}

	type searchResult struct {
		Path       string  `json:"path"`
		Breadcrumb string  `json:"breadcrumb"`
		Content    string  `json:"content"`
		Score      float64 `json:"score"`
	}
	type response struct {
		Library string         `json:"library"`
		Version string         `json:"version"`
		Results []searchResult `json:"results"`
	}

	resp := response{Library: lib.Name, Version: lib.Ref}
	for _, r := range results {
		resp.Results = append(resp.Results, searchResult{
			Path: r.Path, Breadcrumb: r.Breadcrumb,
			Content: r.Content, Score: r.Score,
		})
	}

	data, _ := json.Marshal(resp)
	return mcplib.NewToolResultText(string(data)), nil
}
```

Also add `FindLibraries` to the store. In `internal/index/store.go`, add:

```go
// FindLibraries returns libraries whose name contains the query (fuzzy match).
func (s *Store) FindLibraries(query string) ([]Library, error) {
	rows, err := s.db.Query(
		"SELECT id, name, repo, ref, commit_sha, fetched_at FROM libraries WHERE name LIKE ? ORDER BY name",
		"%"+query+"%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var libs []Library
	for rows.Next() {
		var lib Library
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.Repo, &lib.Ref, &lib.CommitSHA, &lib.FetchedAt); err != nil {
			return nil, err
		}
		libs = append(libs, lib)
	}
	return libs, rows.Err()
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/mcp/... -v
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/tools_test.go internal/index/store.go
git commit -m "feat: MCP tool handlers for list, resolve, and search"
```

---

### Task 11: MCP Server + Streamable HTTP Transport

**Files:**
- Create: `internal/mcp/server.go`, `internal/mcp/server_test.go`

- [ ] **Step 1: Write the test**

Create `internal/mcp/server_test.go`:

```go
package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codanael/docserve/internal/index"
)

func TestMCPServerHealthz(t *testing.T) {
	store := setupTestStore(t)
	srv := NewServer(store, "test")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz: %d", resp.StatusCode)
	}
}

func TestMCPServerReadyz(t *testing.T) {
	store := setupTestStore(t)
	srv := NewServer(store, "test")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("readyz: %d (expected 200 — store has libraries)", resp.StatusCode)
	}
}

func TestMCPServerReadyzEmpty(t *testing.T) {
	store, _ := index.OpenStore(":memory:")
	defer store.Close()

	srv := NewServer(store, "test")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("readyz: %d (expected 503 — no libraries)", resp.StatusCode)
	}
}

func TestMCPServerInitialize(t *testing.T) {
	store := setupTestStore(t)
	srv := NewServer(store, "test")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Send MCP initialize request.
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":   map[string]any{},
			"clientInfo":     map[string]any{"name": "test-client", "version": "1.0.0"},
		},
	}
	body, _ := json.Marshal(initReq)

	resp, err := http.Post(ts.URL+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("initialize: HTTP %d, body: %s", resp.StatusCode, string(respBody))
	}

	// Parse response — should contain server info and capabilities.
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	resultMap, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response structure: %v", result)
	}

	serverInfo, ok := resultMap["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("missing serverInfo: %v", resultMap)
	}
	if serverInfo["name"] != "docserve" {
		t.Errorf("server name = %q", serverInfo["name"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/mcp/... -run TestMCPServer -v
```

Expected: compilation error — `NewServer`, `Handler` not defined.

- [ ] **Step 3: Implement server.go**

Create `internal/mcp/server.go`:

```go
package mcp

import (
	"net/http"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/codanael/docserve/internal/index"
)

type Server struct {
	mcpServer  *server.MCPServer
	httpServer *server.StreamableHTTPServer
	store      *index.Store
}

func NewServer(store *index.Store, version string) *Server {
	mcpSrv := server.NewMCPServer("docserve", version,
		server.WithToolCapabilities(false),
	)

	handlers := &ToolHandlers{Store: store}

	mcpSrv.AddTool(
		mcplib.NewTool("list-libraries",
			mcplib.WithDescription("List all indexed documentation libraries with their versions"),
		),
		handlers.ListLibraries,
	)

	mcpSrv.AddTool(
		mcplib.NewTool("resolve-library",
			mcplib.WithDescription("Find a documentation library by name (fuzzy match)"),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("Library name or partial name to search for")),
		),
		handlers.ResolveLibrary,
	)

	mcpSrv.AddTool(
		mcplib.NewTool("get-library-docs",
			mcplib.WithDescription("Search documentation within a specific library"),
			mcplib.WithString("library", mcplib.Required(), mcplib.Description("Exact library name (use resolve-library first)")),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("Search query")),
			mcplib.WithNumber("max_tokens", mcplib.Description("Maximum tokens to return (default: 5000)")),
		),
		handlers.GetLibraryDocs,
	)

	httpSrv := server.NewStreamableHTTPServer(mcpSrv,
		server.WithEndpointPath("/mcp"),
	)

	return &Server{
		mcpServer:  mcpSrv,
		httpServer: httpSrv,
		store:      store,
	}
}

// Handler returns an http.Handler that serves both MCP and health endpoints.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/mcp", s.httpServer)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if s.store.Ready() {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready: no libraries indexed"))
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/mcp/... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go
git commit -m "feat: MCP server with Streamable HTTP, health and ready endpoints"
```

---

### Task 12: CLI Subcommands

**Files:**
- Modify: `cmd/docserve/main.go`

- [ ] **Step 1: Implement the full CLI**

Replace `cmd/docserve/main.go` with:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/codanael/docserve/internal/config"
	"github.com/codanael/docserve/internal/index"
	mcpsrv "github.com/codanael/docserve/internal/mcp"
	"github.com/codanael/docserve/internal/source"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "fetch":
		cmdFetch(os.Args[2:])
	case "list":
		cmdList(os.Args[2:])
	case "search":
		cmdSearch(os.Args[2:])
	case "version":
		fmt.Println("docserve", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: docserve <command> [flags]

Commands:
  serve     Start the MCP documentation server
  fetch     Fetch/update documentation from sources
  list      List indexed libraries
  search    Search documentation (debug/test)
  version   Print version`)
}

func loadConfig(flagArgs []string) (*config.Config, *flag.FlagSet) {
	fs := flag.NewFlagSet("", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(flagArgs)

	resolved, err := config.ResolvePath(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load(resolved)
	if err != nil {
		log.Fatal(err)
	}
	return cfg, fs
}

func openStore(cfg *config.Config) *index.Store {
	dbPath := filepath.Join(cfg.DataDir, "docserve.db")
	os.MkdirAll(cfg.DataDir, 0755)
	store, err := index.OpenStore(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	return store
}

func buildClients(cfg *config.Config) (proxied, direct *http.Client) {
	direct = &http.Client{Timeout: 5 * time.Minute}

	proxied = &http.Client{Timeout: 5 * time.Minute}
	if cfg.Proxy.HTTP != "" || cfg.Proxy.HTTPS != "" {
		proxyURL := cfg.Proxy.HTTPS
		if proxyURL == "" {
			proxyURL = cfg.Proxy.HTTP
		}
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			log.Fatalf("invalid proxy URL %q: %v", proxyURL, err)
		}
		proxied.Transport = &http.Transport{Proxy: http.ProxyURL(parsed)}
	}

	return proxied, direct
}

func cmdServe(args []string) {
	cfg, _ := loadConfig(args)
	store := openStore(cfg)
	defer store.Close()

	srv := mcpsrv.NewServer(store, version)

	httpServer := &http.Server{
		Addr:    cfg.Listen,
		Handler: srv.Handler(),
	}

	// Graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("docserve %s listening on %s", version, cfg.Listen)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpServer.Shutdown(shutdownCtx)
}

func cmdFetch(args []string) {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	sourceName := fs.String("source", "", "Fetch only this source")
	force := fs.Bool("force", false, "Force re-fetch even if unchanged")
	fs.Parse(args)

	resolved, err := config.ResolvePath(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load(resolved)
	if err != nil {
		log.Fatal(err)
	}

	store := openStore(cfg)
	defer store.Close()

	proxied, direct := buildClients(cfg)
	fetcher := source.NewFetcher(store, cfg.DataDir)
	fetcher.Force = *force

	ctx := context.Background()

	for _, src := range cfg.Sources {
		if *sourceName != "" && src.Name != *sourceName {
			continue
		}

		client := proxied
		if !src.Proxy {
			client = direct
		}

		prov, err := source.NewProvider(src, client)
		if err != nil {
			log.Printf("[%s] error: %v", src.Name, err)
			continue
		}

		result, err := fetcher.FetchSource(ctx, src, prov)
		if err != nil {
			log.Printf("[%s] error: %v", src.Name, err)
			continue
		}

		if result.Updated {
			log.Printf("[%s] updated: %d chunks at %s", src.Name, result.ChunkCount, result.SHA)
		} else {
			log.Printf("[%s] up-to-date", src.Name)
		}
	}
}

func cmdList(args []string) {
	cfg, _ := loadConfig(args)
	store := openStore(cfg)
	defer store.Close()

	libs, err := store.ListLibraries()
	if err != nil {
		log.Fatal(err)
	}

	if len(libs) == 0 {
		fmt.Println("No libraries indexed. Run 'docserve fetch' first.")
		return
	}

	for _, lib := range libs {
		fmt.Printf("%-20s %s (fetched %s)\n", lib.Name, lib.Ref, lib.FetchedAt.Format("2006-01-02 15:04"))
	}
}

func cmdSearch(args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	maxTokens := fs.Int("max-tokens", 5000, "Maximum tokens to return")
	fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: docserve search [flags] <library> <query...>")
		os.Exit(1)
	}

	resolved, err := config.ResolvePath(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load(resolved)
	if err != nil {
		log.Fatal(err)
	}

	store := openStore(cfg)
	defer store.Close()

	libName := remaining[0]
	query := ""
	for i := 1; i < len(remaining); i++ {
		if query != "" {
			query += " "
		}
		query += remaining[i]
	}

	lib, err := store.GetLibrary(libName)
	if err != nil {
		log.Fatalf("library %q not found: %v", libName, err)
	}

	results, err := store.SearchDocs(lib.ID, query, *maxTokens)
	if err != nil {
		log.Fatal(err)
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		return
	}

	for i, r := range results {
		fmt.Printf("--- Result %d [%.2f] %s > %s ---\n", i+1, r.Score, r.Path, r.Breadcrumb)
		fmt.Println(r.Content)
		fmt.Println()
	}
}
```

- [ ] **Step 2: Verify it compiles**

```bash
make build
```

Expected: binary builds successfully.

- [ ] **Step 3: Test basic CLI behavior**

```bash
bin/docserve version
bin/docserve 2>&1 | head -1   # should show "Usage:"
bin/docserve badcmd 2>&1      # should show "unknown command"
```

- [ ] **Step 4: Commit**

```bash
git add cmd/docserve/main.go
git commit -m "feat: full CLI with serve, fetch, list, and search subcommands"
```

---

### Task 13: Scheduler

**Files:**
- Create: `internal/scheduler/scheduler.go`, `internal/scheduler/scheduler_test.go`

- [ ] **Step 1: Write the test**

Create `internal/scheduler/scheduler_test.go`:

```go
package scheduler

import (
	"testing"
	"time"
)

func TestParseCron(t *testing.T) {
	tests := []struct {
		expr string
		ok   bool
	}{
		{"0 3 * * 0", true},       // weekly at 3am on Sunday
		{"*/5 * * * *", true},     // every 5 minutes
		{"0 0 1 * *", true},       // monthly
		{"bad", false},
		{"", false},
	}
	for _, tt := range tests {
		_, err := parseCron(tt.expr)
		if (err == nil) != tt.ok {
			t.Errorf("parseCron(%q) error=%v, wantOk=%v", tt.expr, err, tt.ok)
		}
	}
}

func TestSchedulerStartStop(t *testing.T) {
	called := make(chan string, 10)
	s := New(func(name string) {
		called <- name
	})

	s.Add("test-job", "*/1 * * * *") // every minute
	s.Start()

	// Give it a moment to be running.
	time.Sleep(100 * time.Millisecond)

	s.Stop()

	// Scheduler should stop cleanly without panic.
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/scheduler/... -v
```

Expected: compilation error.

- [ ] **Step 3: Implement scheduler.go**

Create `internal/scheduler/scheduler.go`:

```go
package scheduler

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

type job struct {
	name     string
	schedule cronExpr
}

type Scheduler struct {
	jobs    []job
	fn      func(name string)
	stop    chan struct{}
	wg      sync.WaitGroup
}

func New(fn func(name string)) *Scheduler {
	return &Scheduler{
		fn:   fn,
		stop: make(chan struct{}),
	}
}

func (s *Scheduler) Add(name, cronExprStr string) error {
	expr, err := parseCron(cronExprStr)
	if err != nil {
		return fmt.Errorf("invalid schedule for %s: %w", name, err)
	}
	s.jobs = append(s.jobs, job{name: name, schedule: expr})
	return nil
}

func (s *Scheduler) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-s.stop:
				return
			case now := <-ticker.C:
				for _, j := range s.jobs {
					if j.schedule.matches(now) {
						log.Printf("[scheduler] triggering fetch for %s", j.name)
						go s.fn(j.name)
					}
				}
			}
		}
	}()
}

func (s *Scheduler) Stop() {
	close(s.stop)
	s.wg.Wait()
}

// cronExpr is a simplified 5-field cron expression (minute, hour, day-of-month, month, day-of-week).
type cronExpr struct {
	minute, hour, dom, month, dow cronField
}

type cronField struct {
	wildcard bool
	values   []int
	step     int
}

func (e cronExpr) matches(t time.Time) bool {
	return e.minute.matches(t.Minute()) &&
		e.hour.matches(t.Hour()) &&
		e.dom.matches(t.Day()) &&
		e.month.matches(int(t.Month())) &&
		e.dow.matches(int(t.Weekday()))
}

func (f cronField) matches(val int) bool {
	if f.wildcard {
		if f.step > 0 {
			return val%f.step == 0
		}
		return true
	}
	for _, v := range f.values {
		if v == val {
			return true
		}
	}
	return false
}

func parseCron(expr string) (cronExpr, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return cronExpr{}, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}
	var result cronExpr
	var err error
	result.minute, err = parseField(fields[0], 0, 59)
	if err != nil {
		return cronExpr{}, err
	}
	result.hour, err = parseField(fields[1], 0, 23)
	if err != nil {
		return cronExpr{}, err
	}
	result.dom, err = parseField(fields[2], 1, 31)
	if err != nil {
		return cronExpr{}, err
	}
	result.month, err = parseField(fields[3], 1, 12)
	if err != nil {
		return cronExpr{}, err
	}
	result.dow, err = parseField(fields[4], 0, 6)
	if err != nil {
		return cronExpr{}, err
	}
	return result, nil
}

func parseField(s string, min, max int) (cronField, error) {
	if s == "*" {
		return cronField{wildcard: true}, nil
	}
	if strings.HasPrefix(s, "*/") {
		step, err := strconv.Atoi(s[2:])
		if err != nil {
			return cronField{}, fmt.Errorf("invalid step: %s", s)
		}
		return cronField{wildcard: true, step: step}, nil
	}

	// Comma-separated values or single value.
	parts := strings.Split(s, ",")
	var values []int
	for _, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil {
			return cronField{}, fmt.Errorf("invalid value: %s", p)
		}
		if v < min || v > max {
			return cronField{}, fmt.Errorf("value %d out of range [%d, %d]", v, min, max)
		}
		values = append(values, v)
	}
	return cronField{values: values}, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/scheduler/... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/
git commit -m "feat: simple cron scheduler for periodic doc fetching"
```

---

### Task 14: Build and Release

**Files:**
- Create: `.goreleaser.yaml`, `Dockerfile`

- [ ] **Step 1: Create .goreleaser.yaml**

Create `.goreleaser.yaml`:

```yaml
version: 2

builds:
  - id: docserve
    main: ./cmd/docserve
    binary: docserve
    env:
      - CGO_ENABLED=0
    goos:
      - linux
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X main.version={{.Version}}

archives:
  - id: docserve
    builds:
      - docserve
    format: tar.gz
    name_template: "docserve_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "checksums.txt"

signs:
  - artifacts: checksum
    cmd: cosign
    args:
      - "sign-blob"
      - "--yes"
      - "--output-signature=${signature}"
      - "${artifact}"

dockers:
  - image_templates:
      - "ghcr.io/user/docserve:{{ .Version }}-amd64"
    dockerfile: Dockerfile
    build_flag_templates:
      - "--platform=linux/amd64"
    goarch: amd64
  - image_templates:
      - "ghcr.io/user/docserve:{{ .Version }}-arm64"
    dockerfile: Dockerfile
    build_flag_templates:
      - "--platform=linux/arm64"
    goarch: arm64

docker_manifests:
  - name_template: "ghcr.io/user/docserve:{{ .Version }}"
    image_templates:
      - "ghcr.io/user/docserve:{{ .Version }}-amd64"
      - "ghcr.io/user/docserve:{{ .Version }}-arm64"
  - name_template: "ghcr.io/user/docserve:latest"
    image_templates:
      - "ghcr.io/user/docserve:{{ .Version }}-amd64"
      - "ghcr.io/user/docserve:{{ .Version }}-arm64"
```

- [ ] **Step 2: Create Dockerfile**

Create `Dockerfile`:

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETARCH
RUN CGO_ENABLED=0 GOARCH=${TARGETARCH} go build -ldflags "-s -w" -o /docserve ./cmd/docserve

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /docserve /docserve
ENTRYPOINT ["/docserve"]
```

- [ ] **Step 3: Verify local build**

```bash
make build
bin/docserve version
```

Expected: `docserve dev` (or a git tag if one exists).

- [ ] **Step 4: Verify GoReleaser config**

```bash
goreleaser check
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add .goreleaser.yaml Dockerfile
git commit -m "feat: GoReleaser config and Dockerfile for multi-platform releases"
```

---

### Task 15: Integration Test

**Files:**
- Create: `integration_test.go`

- [ ] **Step 1: Write the integration test**

Create `integration_test.go` at the project root:

```go
//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codanael/docserve/internal/index"
	mcpsrv "github.com/codanael/docserve/internal/mcp"
)

func TestMCPProtocolFlow(t *testing.T) {
	// Setup store with test data.
	store, err := index.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	libID, _ := store.UpsertLibrary(index.Library{
		Name: "testlib", Repo: "org/test", Ref: "v1.0.0",
		CommitSHA: "integration-test-sha", FetchedAt: time.Now(),
	})
	store.ReplaceChunks(libID, []index.Chunk{
		{Path: "docs/guide.md", Breadcrumb: "Guide > Setup", Content: "Install the package with npm install testlib."},
		{Path: "docs/api.md", Breadcrumb: "API > Authentication", Content: "Use Bearer tokens for API authentication."},
	})

	srv := mcpsrv.NewServer(store, "test")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Step 1: Initialize
	sessionID := jsonRPC(t, ts.URL, 1, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":   map[string]any{},
		"clientInfo":     map[string]any{"name": "integration-test", "version": "1.0.0"},
	})

	// Step 2: Send initialized notification
	sendNotification(t, ts.URL, sessionID, "notifications/initialized", nil)

	// Step 3: List tools
	toolsResult := jsonRPCWithSession(t, ts.URL, sessionID, 2, "tools/list", nil)
	tools, ok := toolsResult["tools"].([]any)
	if !ok || len(tools) != 3 {
		t.Fatalf("expected 3 tools, got: %v", toolsResult)
	}

	// Step 4: Call list-libraries
	listResult := jsonRPCWithSession(t, ts.URL, sessionID, 3, "tools/call", map[string]any{
		"name":      "list-libraries",
		"arguments": map[string]any{},
	})
	content := listResult["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !jsonContains(text, "testlib") {
		t.Errorf("list-libraries should include testlib: %s", text)
	}

	// Step 5: Call get-library-docs
	searchResult := jsonRPCWithSession(t, ts.URL, sessionID, 4, "tools/call", map[string]any{
		"name": "get-library-docs",
		"arguments": map[string]any{
			"library":    "testlib",
			"query":      "authentication",
			"max_tokens": 5000,
		},
	})
	searchContent := searchResult["content"].([]any)
	searchText := searchContent[0].(map[string]any)["text"].(string)
	if !jsonContains(searchText, "Bearer") {
		t.Errorf("search should find Bearer tokens doc: %s", searchText)
	}
}

func jsonRPC(t *testing.T, baseURL string, id int, method string, params map[string]any) string {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", baseURL+"/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s: HTTP %d: %s", method, resp.StatusCode, string(respBody))
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	return resp.Header.Get("Mcp-Session-Id")
}

func jsonRPCWithSession(t *testing.T, baseURL, sessionID string, id int, method string, params map[string]any) map[string]any {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", baseURL+"/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: HTTP %d: %s", method, resp.StatusCode, string(respBody))
	}

	var result map[string]any
	json.Unmarshal(respBody, &result)

	if res, ok := result["result"].(map[string]any); ok {
		return res
	}
	t.Fatalf("unexpected response for %s: %s", method, string(respBody))
	return nil
}

func sendNotification(t *testing.T, baseURL, sessionID, method string, params map[string]any) {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", baseURL+"/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func jsonContains(jsonStr, substr string) bool {
	return bytes.Contains([]byte(jsonStr), []byte(substr))
}
```

- [ ] **Step 2: Run integration test**

```bash
go test -tags=integration -v ./...
```

Expected: `TestMCPProtocolFlow` PASSES — full MCP lifecycle works.

- [ ] **Step 3: Commit**

```bash
git add integration_test.go
git commit -m "test: integration test covering full MCP protocol flow"
```

---

### Task 16: Final Wiring — Scheduler in Serve

**Files:**
- Modify: `cmd/docserve/main.go`

- [ ] **Step 1: Wire scheduler into serve command**

In `cmd/docserve/main.go`, update `cmdServe` to start the scheduler:

```go
func cmdServe(args []string) {
	cfg, _ := loadConfig(args)
	store := openStore(cfg)
	defer store.Close()

	proxied, direct := buildClients(cfg)

	// Start scheduler for sources with schedules.
	sched := scheduler.New(func(name string) {
		for _, src := range cfg.Sources {
			if src.Name != name {
				continue
			}
			client := proxied
			if !src.Proxy {
				client = direct
			}
			prov, err := source.NewProvider(src, client)
			if err != nil {
				log.Printf("[scheduler] %s: %v", name, err)
				return
			}
			fetcher := source.NewFetcher(store, cfg.DataDir)
			result, err := fetcher.FetchSource(context.Background(), src, prov)
			if err != nil {
				log.Printf("[scheduler] %s: %v", name, err)
				return
			}
			if result.Updated {
				log.Printf("[scheduler] %s: updated %d chunks", name, result.ChunkCount)
			}
		}
	})

	for _, src := range cfg.Sources {
		if src.Schedule != "" {
			if err := sched.Add(src.Name, src.Schedule); err != nil {
				log.Printf("warning: %v", err)
			}
		}
	}
	sched.Start()

	srv := mcpsrv.NewServer(store, version)
	httpServer := &http.Server{
		Addr:    cfg.Listen,
		Handler: srv.Handler(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("docserve %s listening on %s", version, cfg.Listen)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	sched.Stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpServer.Shutdown(shutdownCtx)
}
```

Add the import for the scheduler package:
```go
"github.com/codanael/docserve/internal/scheduler"
```

- [ ] **Step 2: Verify build**

```bash
make build
```

Expected: builds successfully.

- [ ] **Step 3: Commit**

```bash
git add cmd/docserve/main.go
git commit -m "feat: wire scheduler into serve command for periodic doc fetching"
```

- [ ] **Step 4: Run full test suite**

```bash
make test
```

Expected: all tests PASS.

- [ ] **Step 5: Final commit tag**

```bash
git tag v0.1.0
```
