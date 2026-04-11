# Refs Config Refactor — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace singular `ref`/`paths`/`depth` fields on `SourceConfig` with a `refs []RefConfig` list so each source can target multiple branches or Confluence pages, each producing its own library.

**Architecture:** Add `RefConfig` struct to config package. The fetcher pipeline changes from operating on a whole `SourceConfig` to operating on a `(SourceConfig, RefConfig)` pair. Library names become compound (`source/ref` for git, `source/space/id` for Confluence) unless overridden by `RefConfig.Name`. The scheduler and CLI iterate over `source.Refs` instead of treating each source as one unit.

**Tech Stack:** Go, YAML config, SQLite FTS5

---

### Task 1: Update config types and validation

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add RefConfig struct and update SourceConfig**

Replace the `Ref`, `Paths`, and `Depth` fields on `SourceConfig` with `Refs []RefConfig`. Add the new `RefConfig` struct. Update `rawSource` to match.

```go
// RefConfig describes a single target within a source.
type RefConfig struct {
	Name  string   `yaml:"name"`  // optional; overrides generated library name
	Ref   string   `yaml:"ref"`   // git branch/tag/SHA
	Paths []string `yaml:"paths"` // git: directories/files to index
	Space string   `yaml:"space"` // confluence: space key
	ID    string   `yaml:"id"`    // confluence: page ID
	Depth *int     `yaml:"depth"` // confluence: subtree depth (nil → unlimited)
}
```

Remove from `SourceConfig`: `Ref string`, `Paths []string`, `Depth *int`.
Add to `SourceConfig`: `Refs []RefConfig `yaml:"refs"``

Update `rawSource` the same way — remove `Ref`, `Paths`, `Depth`, add `Refs []RefConfig`.

Update `UnmarshalYAML`: remove the lines that copy `Ref`, `Depth`, `Paths` from raw. Add `s.Refs = raw.Refs`.

- [ ] **Step 2: Update validate() for new refs structure**

Replace the current per-provider validation with refs-based validation:

```go
func validate(cfg *Config) error {
	if len(cfg.Sources) == 0 {
		return fmt.Errorf("config must have at least one source")
	}

	libNames := make(map[string]bool)

	for i, src := range cfg.Sources {
		if src.Name == "" {
			return fmt.Errorf("source[%d]: name is required", i)
		}
		if src.Provider == "" {
			return fmt.Errorf("source %q: provider is required", src.Name)
		}

		switch src.Provider {
		case "github", "azure-devops":
			// no extra source-level checks
		case "confluence":
			if src.BaseURL == "" {
				return fmt.Errorf("source %q: base_url is required for confluence provider", src.Name)
			}
		default:
			return fmt.Errorf("source %q: unknown provider %q (must be github, azure-devops, or confluence)", src.Name, src.Provider)
		}

		if len(src.Refs) == 0 {
			return fmt.Errorf("source %q: at least one ref is required", src.Name)
		}

		for j, ref := range src.Refs {
			switch src.Provider {
			case "github", "azure-devops":
				if ref.Ref == "" {
					return fmt.Errorf("source %q refs[%d]: ref is required", src.Name, j)
				}
				if len(ref.Paths) == 0 {
					return fmt.Errorf("source %q refs[%d]: at least one path is required", src.Name, j)
				}
			case "confluence":
				if ref.Space == "" {
					return fmt.Errorf("source %q refs[%d]: space is required", src.Name, j)
				}
				if ref.ID == "" {
					return fmt.Errorf("source %q refs[%d]: id is required", src.Name, j)
				}
			}

			// Check library name uniqueness.
			libName := ref.Name
			if libName == "" {
				libName = defaultLibraryName(src, ref)
			}
			if libNames[libName] {
				return fmt.Errorf("source %q refs[%d]: duplicate library name %q", src.Name, j, libName)
			}
			libNames[libName] = true
		}
	}

	return nil
}
```

- [ ] **Step 3: Add LibraryName and defaultLibraryName helpers**

```go
// LibraryName returns the library name for a ref, using the override if set.
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
```

- [ ] **Step 4: Run tests (expect failures — tests not yet updated)**

Run: `cd /home/agent/projects/mcp-docs && go build ./...`
Expected: compiles (downstream code will fail — that's fine, we fix tests later)

Actually, this will fail because downstream code still references the old fields. Just verify the config package compiles:

Run: `cd /home/agent/projects/mcp-docs && go vet ./internal/config/`
Expected: PASS (the config package itself should compile)

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "refactor(config): replace ref/paths/depth with refs list

Each source now declares a refs[] list. Git refs carry ref+paths,
Confluence refs carry space+id+depth. Each ref produces its own
library with a compound name (source/ref or source/space/id)."
```

---

### Task 2: Update config tests

**Files:**
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Update TestLoadConfig**

Change the YAML content to use `refs:` instead of `ref:`/`paths:`. Update assertions to check `cfg.Sources[0].Refs[0].Ref`, `.Refs[0].Paths`, etc.

GitHub source YAML becomes:
```yaml
    refs:
      - ref: main
        paths:
          - docs/
          - README.md
```

Azure DevOps source YAML becomes:
```yaml
    refs:
      - ref: refs/heads/main
        paths:
          - wiki/
```

Assertions change from `gh.Ref` → `gh.Refs[0].Ref`, `gh.Paths` → `gh.Refs[0].Paths`, `ado.Ref` is gone, check `ado.Refs[0].Ref` instead.

- [ ] **Step 2: Update TestLoadConfigDefaults**

YAML becomes:
```yaml
sources:
  - name: minimal-source
    provider: github
    repo: org/repo
    refs:
      - ref: main
        paths:
          - docs/
```

Assertion for Proxy stays the same (it's source-level).

- [ ] **Step 3: Update TestLoadConfigValidation**

Update validation test cases:
- "no ref" → "no refs" (empty refs list)
- "no paths" → refs entry with no paths
- Add: "confluence ref missing space"
- Add: "confluence ref missing id"
- Remove: "empty paths" (now validated per-ref)
- Add: "duplicate library names"

```go
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
      - space: DEV
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
```

- [ ] **Step 4: Update TestLoadConfigConfluence**

YAML becomes:
```yaml
sources:
  - name: my-confluence-docs
    provider: confluence
    base_url: https://confluence.example.com
    proxy: false
    schedule: "0 */2 * * *"
    auth:
      type: bearer
      token_env: CONFLUENCE_TOKEN
    refs:
      - space: MYSPACE
        id: "123456"
        depth: 3
```

Assertions change:
- `src.Ref` → `src.Refs[0].ID` (check it equals `"123456"`)
- `src.Depth` → `src.Refs[0].Depth`
- Remove assertions for `src.Paths` and `src.Repo` (no longer auto-populated for confluence)
- Add assertion for `src.Refs[0].Space == "MYSPACE"`

- [ ] **Step 5: Update TestLoadConfigConfluenceDepthDefault**

```yaml
sources:
  - name: confluence-no-depth
    provider: confluence
    base_url: https://confluence.example.com
    refs:
      - space: MYSPACE
        id: "123"
```

Assert `src.Refs[0].Depth == nil`.

- [ ] **Step 6: Update TestLoadConfigConfluenceValidation**

Replace with the new validation cases from Step 3 (confluence-specific ones). Remove the old "confluence missing ref" case.

- [ ] **Step 7: Add TestLibraryName**

```go
func TestLibraryName(t *testing.T) {
    cases := []struct {
        name     string
        src      config.SourceConfig
        ref      config.RefConfig
        expected string
    }{
        {
            name:     "github default",
            src:      config.SourceConfig{Name: "spring-boot", Provider: "github"},
            ref:      config.RefConfig{Ref: "main"},
            expected: "spring-boot/main",
        },
        {
            name:     "confluence default",
            src:      config.SourceConfig{Name: "wiki", Provider: "confluence"},
            ref:      config.RefConfig{Space: "DEV", ID: "123"},
            expected: "wiki/DEV/123",
        },
        {
            name:     "custom name override",
            src:      config.SourceConfig{Name: "wiki", Provider: "confluence"},
            ref:      config.RefConfig{Name: "my-custom-lib", Space: "DEV", ID: "123"},
            expected: "my-custom-lib",
        },
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := config.LibraryName(tc.src, tc.ref)
            if got != tc.expected {
                t.Errorf("LibraryName() = %q, want %q", got, tc.expected)
            }
        })
    }
}
```

- [ ] **Step 8: Run config tests**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/config/ -v`
Expected: all PASS

- [ ] **Step 9: Commit**

```bash
git add internal/config/config_test.go
git commit -m "test(config): update tests for refs list structure"
```

---

### Task 3: Update fetcher to accept RefConfig

**Files:**
- Modify: `internal/source/fetcher.go`

- [ ] **Step 1: Change FetchSource signature**

`FetchSource` currently takes `cfg config.SourceConfig` and reads `cfg.Ref`, `cfg.Paths`, `cfg.Name`. Change it to also accept a `config.RefConfig` and a `libName string`:

```go
func (f *Fetcher) FetchSource(ctx context.Context, cfg config.SourceConfig, ref config.RefConfig, libName string, prov Provider) (*FetchResult, error) {
```

Update `FetchResult.Source` to use `libName` instead of `cfg.Name`.

Replace all references inside the function:
- `cfg.Ref` → `ref.Ref` (for git) or `ref.ID` (for confluence — but Resolve already takes a string, so the caller passes the right value)
- `cfg.Paths` → `ref.Paths`
- `cfg.Name` → `libName` (in log messages, library lookup, raw dir path, library upsert)

The ref string passed to `prov.Resolve()` needs to differ by provider:
- Git: `ref.Ref`
- Confluence: `ref.ID`

Add a helper at the top of FetchSource:

```go
// Determine the resolve ref based on provider type.
resolveRef := ref.Ref
if cfg.Provider == "confluence" {
    resolveRef = ref.ID
}
```

Then use `resolveRef` where `cfg.Ref` was used.

For the short ref label (line 118), use:
```go
refLabel := ref.Ref
if cfg.Provider == "confluence" {
    refLabel = ref.Space + "/" + ref.ID
}
shortSHA := sha
if len(shortSHA) > 12 {
    shortSHA = shortSHA[:12]
}
refStr := refLabel + "@" + shortSHA
```

For `cfg.Paths` in the walk loop (line 73): use `ref.Paths`. For Confluence (which has no paths), default to `[]string{""}`:
```go
paths := ref.Paths
if len(paths) == 0 {
    paths = []string{""}
}
for _, pathPrefix := range paths {
```

For the `Repo` field in the library upsert: keep `cfg.Repo` for git. For confluence, construct it:
```go
repo := cfg.Repo
if cfg.Provider == "confluence" {
    repo = cfg.BaseURL + "/spaces/" + ref.Space + "/pages/" + ref.ID
}
```

- [ ] **Step 2: Run to verify compilation**

Run: `cd /home/agent/projects/mcp-docs && go vet ./internal/source/`
Expected: compilation errors in fetcher_test.go and callers — that's expected, we fix them next.

Actually, since tests are in the same package, it won't compile. Just verify the logic is correct by reading through it.

- [ ] **Step 3: Commit**

```bash
git add internal/source/fetcher.go
git commit -m "refactor(fetcher): accept RefConfig and libName parameters

FetchSource now takes a (SourceConfig, RefConfig, libName) triple
instead of reading ref/paths/depth from the source config directly."
```

---

### Task 4: Update providers to use RefConfig

**Files:**
- Modify: `internal/source/confluence.go`
- Modify: `internal/source/provider.go`

- [ ] **Step 1: Update ConfluenceProvider to store RefConfig**

The ConfluenceProvider currently reads `cfg.Depth` and `cfg.Ref` from the stored `SourceConfig`. Since depth and ref are now per-ref, the provider needs to receive these at construction or per-call.

The simplest approach: `NewConfluenceProvider` takes a `config.RefConfig` in addition to `config.SourceConfig`. Store `depth` from `ref.Depth` instead of `cfg.Depth`. In `Fetch`, use the stored ref's ID instead of `p.cfg.Ref`.

```go
type ConfluenceProvider struct {
	cfg    config.SourceConfig
	ref    config.RefConfig
	client *http.Client
	apiURL string
	depth  int
}

func NewConfluenceProvider(cfg config.SourceConfig, ref config.RefConfig, client *http.Client) *ConfluenceProvider {
	if client == nil {
		client = &http.Client{}
	}
	client = WrapClientAuth(client, cfg.Auth)

	depth := -1
	if ref.Depth != nil {
		depth = *ref.Depth
	}

	return &ConfluenceProvider{
		cfg:    cfg,
		ref:    ref,
		client: client,
		apiURL: strings.TrimRight(cfg.BaseURL, "/"),
		depth:  depth,
	}
}
```

Update `Fetch` method — replace `p.cfg.Ref` with `p.ref.ID`:
```go
func (p *ConfluenceProvider) Fetch(ctx context.Context, _ string, _ []string, destDir string) error {
	ref := p.ref.ID
	// ... rest stays the same, using ref instead of p.cfg.Ref
```

- [ ] **Step 2: Update NewProvider factory**

`NewProvider` needs to accept `RefConfig` for Confluence. For consistency, pass it for all providers (git providers can ignore it):

```go
func NewProvider(cfg config.SourceConfig, ref config.RefConfig, client *http.Client) (Provider, error) {
	switch cfg.Provider {
	case "github":
		return NewGitHubProvider(cfg, client), nil
	case "azure-devops":
		return NewAzureDevOpsProvider(cfg, client), nil
	case "confluence":
		return NewConfluenceProvider(cfg, ref, client), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/source/confluence.go internal/source/provider.go
git commit -m "refactor(providers): pass RefConfig to provider constructors

Confluence provider reads depth and page ID from RefConfig instead of
SourceConfig. NewProvider factory forwards RefConfig to all providers."
```

---

### Task 5: Update fetcher tests

**Files:**
- Modify: `internal/source/fetcher_test.go`

- [ ] **Step 1: Update sourceCfg helper and test calls**

```go
func sourceCfg(name string) config.SourceConfig {
	return config.SourceConfig{
		Name:     name,
		Provider: "github",
		Repo:     "owner/repo",
	}
}

func refCfg(ref string, paths []string) config.RefConfig {
	return config.RefConfig{
		Ref:   ref,
		Paths: paths,
	}
}
```

Update all `FetchSource` calls to pass the new arguments:

```go
cfg := sourceCfg("mylib")
ref := refCfg("main", []string{"docs"})
libName := config.LibraryName(cfg, ref)

result, err := fetcher.FetchSource(ctx, cfg, ref, libName, prov)
```

Update assertions: `result.Source` will now be `"mylib/main"` instead of `"mylib"`. Update `store.GetLibrary(cfg.Name)` → `store.GetLibrary(libName)`.

- [ ] **Step 2: Run fetcher tests**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/source/ -run TestFetch -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/source/fetcher_test.go
git commit -m "test(fetcher): update tests for RefConfig parameters"
```

---

### Task 6: Update Confluence provider tests

**Files:**
- Modify: `internal/source/confluence_test.go`

- [ ] **Step 1: Update makeConfluenceProvider helper**

```go
func makeConfluenceProvider(srv *httptest.Server, depth *int) *ConfluenceProvider {
	cfg := config.SourceConfig{
		Provider: "confluence",
		BaseURL:  srv.URL,
	}
	ref := config.RefConfig{
		Space: "TEST",
		ID:    "100",
	}
	if depth != nil {
		ref.Depth = depth
	}
	p := NewConfluenceProvider(cfg, ref, srv.Client())
	p.apiURL = srv.URL
	return p
}
```

- [ ] **Step 2: Update TestConfluenceFetchSanitizesTitles and TestConfluenceFetchRetryOn503**

These tests construct `config.SourceConfig` directly with `Ref: "100"`. Update to use the new pattern:

```go
cfg := config.SourceConfig{
    Provider: "confluence",
    BaseURL:  srv.URL,
}
ref := config.RefConfig{
    Space: "TEST",
    ID:    "100",
}
p := NewConfluenceProvider(cfg, ref, srv.Client())
p.apiURL = srv.URL
```

- [ ] **Step 3: Run confluence tests**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/source/ -run TestConfluence -v`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add internal/source/confluence_test.go
git commit -m "test(confluence): update tests for RefConfig parameters"
```

---

### Task 7: Update main.go (scheduler, fetch command, CLI)

**Files:**
- Modify: `cmd/docserve/main.go`

- [ ] **Step 1: Update cmdServe scheduler callback**

The scheduler currently registers one job per source (`src.Name`). Now register one job per ref, using the library name:

```go
sched := scheduler.New(func(name string) {
    for _, src := range cfg.Sources {
        for _, ref := range src.Refs {
            libName := config.LibraryName(src, ref)
            if libName != name {
                continue
            }
            client := proxied
            if !src.Proxy {
                client = direct
            }
            prov, err := source.NewProvider(src, ref, client)
            if err != nil {
                log.Printf("[scheduler] %s: %v", name, err)
                return
            }
            fetcher := source.NewFetcher(store, cfg.DataDir)
            result, err := fetcher.FetchSource(context.Background(), src, ref, libName, prov)
            if err != nil {
                log.Printf("[scheduler] %s: %v", name, err)
                return
            }
            if result.Updated {
                log.Printf("[scheduler] %s: updated %d chunks", name, result.ChunkCount)
            }
        }
    }
})

for _, src := range cfg.Sources {
    if src.Schedule != "" {
        for _, ref := range src.Refs {
            libName := config.LibraryName(src, ref)
            if err := sched.Add(libName, src.Schedule); err != nil {
                log.Printf("warning: %v", err)
            }
        }
    }
}
```

- [ ] **Step 2: Update cmdFetch**

Change the loop to iterate over refs within each source:

```go
for _, src := range cfg.Sources {
    client := proxied
    if !src.Proxy {
        client = direct
    }

    for _, ref := range src.Refs {
        libName := config.LibraryName(src, ref)

        if *sourceName != "" && libName != *sourceName && src.Name != *sourceName {
            continue
        }

        authClient := source.WrapClientAuth(client, src.Auth)
        prov, err := source.NewProvider(src, ref, authClient)
        if err != nil {
            log.Printf("[%s] error creating provider: %v", libName, err)
            continue
        }

        result, err := fetcher.FetchSource(ctx, src, ref, libName, prov)
        if err != nil {
            log.Printf("[%s] fetch error: %v", libName, err)
            continue
        }

        if result.Updated {
            log.Printf("[%s] updated: sha=%s chunks=%d", result.Source, result.SHA, result.ChunkCount)
        } else {
            log.Printf("[%s] already up to date: sha=%s", result.Source, result.SHA)
        }
    }
}
```

Note: `--source` flag now matches against both library name and source name (to allow fetching all refs for a source).

- [ ] **Step 3: Verify compilation**

Run: `cd /home/agent/projects/mcp-docs && go build ./cmd/docserve/`
Expected: compiles successfully

- [ ] **Step 4: Commit**

```bash
git add cmd/docserve/main.go
git commit -m "refactor(main): iterate refs for scheduling and fetching

Each ref in a source is now a separate scheduler job and fetch unit.
The --source flag matches both library names and source names."
```

---

### Task 8: Update config files and documentation

**Files:**
- Modify: `docserve.yaml`
- Modify: `docserve.example.yaml`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update docserve.yaml**

```yaml
data_dir: ./data
listen: ":8080"

sources:
  - name: spring-boot
    provider: github
    repo: spring-projects/spring-boot
    proxy: false
    refs:
      - ref: main
        paths:
          - "documentation/spring-boot-docs/"

  - name: atlassian-confluence
    provider: confluence
    base_url: https://confluence.atlassian.com
    proxy: false
    refs:
      - space: CONF
        id: "1627457156"      # Info/Tip/Note/Warning Macros page
        depth: 0

  - name: apache-wiki
    provider: confluence
    base_url: https://cwiki.apache.org/confluence
    proxy: false
    refs:
      - space: KAFKA
        id: "50859233"        # Kafka Improvement Proposals
        depth: 1
      - space: FLINK
        id: "308152924"       # Creating a Flink CDC Release
        depth: 0
```

Note: the three separate Confluence sources sharing `cwiki.apache.org` are now collapsed into one source with two refs. The Atlassian one stays separate (different instance).

- [ ] **Step 2: Update docserve.example.yaml**

```yaml
# docserve example configuration
# Copy to docserve.yaml and adjust to your environment.

# Directory where fetched documentation is stored.
# Defaults to "data" relative to the working directory.
data_dir: /var/lib/docserve

# Address on which the server listens.
# Defaults to ":8080".
listen: ":8080"

# Optional HTTP/HTTPS proxy used when fetching sources.
# Individual sources can opt out by setting proxy: false.
proxy:
  http: http://proxy.example.com:3128
  https: http://proxy.example.com:3128

sources:
  # GitHub source example — uses bearer token auth via GITHUB_TOKEN env var.
  - name: my-github-docs
    provider: github
    repo: myorg/myrepo        # owner/repo
    proxy: true               # use the global proxy (this is the default)
    schedule: "0 * * * *"     # cron expression; omit to disable scheduled sync
    auth:
      type: bearer
      token_env: GITHUB_TOKEN # name of the environment variable holding the token
    refs:
      - ref: main             # branch, tag, or commit SHA
        paths:
          - docs/             # directory (trailing slash) or exact file path
          - README.md
      - ref: v2.0             # index a second branch/tag from the same repo
        name: my-docs-v2      # optional: override the default library name
        paths:
          - docs/

  # Azure DevOps source example — uses basic auth.
  # Uncomment and fill in to enable.
  #
  # - name: my-azure-docs
  #   provider: azure-devops
  #   org: myorg              # Azure DevOps organisation
  #   project: myproject      # Azure DevOps project
  #   repo: myrepo            # Git repository name
  #   base_url: https://dev.azure.com
  #   proxy: false            # bypass the global proxy for this source
  #   schedule: "30 * * * *"
  #   auth:
  #     type: basic
  #     username_env: ADO_USER
  #     password_env: ADO_PAT
  #   refs:
  #     - ref: refs/heads/main
  #       paths:
  #         - wiki/

  # Confluence source example — uses personal access token.
  # Each ref targets a specific page (by space + page ID) and its descendants.
  # Uncomment and fill in to enable.
  #
  # - name: my-confluence-docs
  #   provider: confluence
  #   base_url: https://confluence.example.com  # Confluence instance URL
  #   proxy: false
  #   schedule: "0 */4 * * *"
  #   auth:
  #     type: bearer
  #     token_env: CONFLUENCE_PAT  # personal access token (DC 7.9+)
  #   refs:
  #     - space: DEV                   # Confluence space key
  #       id: "123456"                 # page ID of the root page to index
  #       depth: 3                     # subtree depth: 0=root only, -1=unlimited (default)
  #     - space: ARCH
  #       id: "789012"
  #       name: architecture-decisions  # optional: override library name
  #
  # # Alternative auth: basic auth (username + password/token)
  # # auth:
  # #   type: basic
  # #   username_env: CONFLUENCE_USER
  # #   password_env: CONFLUENCE_PASS
```

- [ ] **Step 3: Update CLAUDE.md**

In the "Key Design Decisions" section, after the FTS5 search bullet, the config shape is not explicitly documented in CLAUDE.md. No changes needed there.

However, update the "Adding a New Provider" section — step 3 currently says add to `validProviders` which doesn't exist. Leave this as-is since it's already slightly wrong. The config structure info in the file header is implicit via the project structure description.

No changes needed to CLAUDE.md for this refactor.

- [ ] **Step 4: Commit**

```bash
git add docserve.yaml docserve.example.yaml
git commit -m "config: update config files for refs list format

Collapse multiple Confluence sources sharing the same instance into a
single source with multiple refs entries. Update example config with
the new refs structure and documentation."
```

---

### Task 9: Update integration test

**Files:**
- Modify: `integration_test.go`

- [ ] **Step 1: Update test — no config changes needed**

The integration test in `integration_test.go` creates libraries directly via `store.UpsertLibrary()` — it doesn't go through the config or fetcher. It creates a library named `"testlib"` with `Ref: "main"`. This test doesn't need config changes; the library naming in the store is independent of config.

Verify the test still passes as-is. The only thing that changed is `NewProvider` signature, which the integration test doesn't call.

Run: `cd /home/agent/projects/mcp-docs && go test -tags=integration ./... -v`
Expected: PASS

- [ ] **Step 2: Commit (if any changes needed)**

If no changes: skip this commit.

---

### Task 10: Run full test suite and verify

- [ ] **Step 1: Run all unit tests**

Run: `cd /home/agent/projects/mcp-docs && go test ./... -v`
Expected: all PASS

- [ ] **Step 2: Run integration tests**

Run: `cd /home/agent/projects/mcp-docs && go test -tags=integration ./... -v`
Expected: all PASS

- [ ] **Step 3: Run linter**

Run: `cd /home/agent/projects/mcp-docs && make lint`
Expected: no issues (or only pre-existing ones)

- [ ] **Step 4: Build binary**

Run: `cd /home/agent/projects/mcp-docs && make build`
Expected: binary builds successfully
