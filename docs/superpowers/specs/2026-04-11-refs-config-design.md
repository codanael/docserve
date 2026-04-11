# Refs Config Refactor

## Problem

Each Confluence source entry can only target a single page tree (one `ref` + `depth`). Scraping multiple pages from the same instance requires duplicating `base_url`, `auth`, `proxy`, and `schedule` across separate source entries. The same limitation applies to git providers that need multiple branches indexed.

## Design

### Config shape

Replace the top-level `ref`, `paths`, and `depth` fields on `SourceConfig` with a `refs` list. Each entry in `refs` is a `RefConfig` with provider-specific fields.

```yaml
# Git providers (github, azure-devops)
sources:
  - name: spring-boot
    provider: github
    repo: spring-projects/spring-boot
    proxy: false
    refs:
      - ref: main
        paths:
          - documentation/spring-boot-docs/
      - ref: v3.2.0
        name: spring-boot-v3  # optional override
        paths:
          - docs/

# Confluence
  - name: my-confluence
    provider: confluence
    base_url: https://confluence.example.com
    auth:
      type: bearer
      token_env: CONFLUENCE_PAT
    refs:
      - space: DEVOPS
        id: "123456"
        depth: 0
      - space: ARCH
        id: "789012"
        depth: 3
        name: arch-decisions  # optional override
```

### RefConfig struct

```go
type RefConfig struct {
    Name  string   `yaml:"name"`    // optional; overrides default library name
    Ref   string   `yaml:"ref"`     // git branch/tag/SHA
    Paths []string `yaml:"paths"`   // git paths to index
    Space string   `yaml:"space"`   // confluence space key
    ID    string   `yaml:"id"`      // confluence page ID
    Depth *int     `yaml:"depth"`   // confluence subtree depth (-1=unlimited when nil)
}
```

### Library naming

Each ref entry produces its own library in the FTS index:

- **Git:** `{source.name}/{ref}` (e.g. `spring-boot/main`)
- **Confluence:** `{source.name}/{space}/{id}` (e.g. `my-confluence/DEVOPS/123456`)
- **Override:** if `RefConfig.Name` is set, use it as-is instead of the generated name

### Removed fields

The following fields are removed from `SourceConfig`:
- `ref` (string) — moved into `RefConfig.Ref`
- `paths` ([]string) — moved into `RefConfig.Paths`
- `depth` (*int) — moved into `RefConfig.Depth`

### Validation rules

**All providers:**
- `refs` must have at least one entry
- Library names (generated or overridden) must be unique across the entire config

**Git providers (github, azure-devops):**
- Each ref entry must have `ref` (non-empty)
- Each ref entry must have at least one `paths` entry

**Confluence:**
- Source must have `base_url`
- Each ref entry must have `space` (non-empty)
- Each ref entry must have `id` (non-empty)

### Impact

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `RefConfig`, update `SourceConfig` (remove `ref`/`paths`/`depth`, add `refs`), update `rawSource`, update `validate()` |
| `internal/config/config_test.go` | Update all tests for new shape |
| `internal/source/provider.go` | `NewProvider` signature may need a `RefConfig` parameter, or providers iterate refs |
| `internal/source/confluence.go` | Read `space`, `id`, `depth` from `RefConfig` instead of `SourceConfig` |
| `internal/source/github.go` | Read `ref`, `paths` from `RefConfig` instead of `SourceConfig` |
| `internal/source/azure_devops.go` | Same as github |
| `internal/scheduler/scheduler.go` | Iterate `source.Refs`, construct library name per ref, pass ref to provider |
| `internal/mcp/tools.go` | Library listing/resolution uses compound names |
| `docserve.yaml` | Update to new format |
| `docserve.example.yaml` | Update to new format with examples |
| `CLAUDE.md` | Update if config shape is documented |
| All test files referencing old config shape | Update |
