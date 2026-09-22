# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Optional bearer-token authentication for `/mcp` (`auth_token_env`)
- Origin validation with `allowed_origins`; foreign browser origins get 403
- Tool `title`, `outputSchema` for `list-libraries` and `resolve-library`, server `instructions`
- Truncation notice at the end of `get-library-docs` output when results were cut
- Tool-call and protocol-error logging

### Changed
- mcp-go bumped to v1.1.0: MCP specification 2026-07-28 with legacy fallback; transport is now stateless (no `Mcp-Session-Id`)
- `list-libraries` structured output is now an object `{"libraries": [...]}` and includes `repo` and `commit_sha`
- Tool arguments are validated against their schema; unknown or missing arguments return `isError` results
- `openWorldHint` is now `false` on all tools
- HTTP server timeouts and a 1 MiB request body limit
- mcp-go's loopback Host check is disabled; Origin validation (see `allowed_origins`) is the DNS-rebinding defense, so same-host reverse proxies that preserve the client Host header keep working
- Search terms are matched literally: a trailing `*` no longer performs FTS5 prefix matching (stemming still applies)

### Fixed
- FTS5 operators and quotes in queries no longer cause SQLite syntax errors
- `%` and `_` in `resolve-library` queries are matched literally
- Store read queries honour request cancellation

## [1.0.0] - 2026-04-11

### Added
- Confluence Data Center provider with page tree traversal, depth limiting, and XHTML-to-Markdown conversion
- `.deb` and `.rpm` packages with systemd service file
- SBOM generation (SPDX JSON) for all release archives
- HTTP security headers on all endpoints
- Cosign-signed checksums and Docker images

### Changed
- Source config now supports multiple refs per source (`refs` list replaces flat `ref`/`paths`/`depth`)
- GoReleaser changelog groups by commit type

### Fixed
- Properly handle all unchecked error returns
- Handle error when reading Confluence API error response body
- Pin Alpine base image digest in Dockerfile for reproducible builds

### Removed
- Unused `chunk_meta` database table (was written to but never queried)

## [0.1.0] - 2026-04-10

### Changed
- Refactored source config to support multiple refs per source

### Fixed
- Resolved all golangci-lint errcheck and staticcheck issues
- Properly handle all unchecked error returns
- Dockerfile build context for GoReleaser dockers_v2
- ARG TARGETPLATFORM placement in Dockerfile

## [0.0.1] - 2026-04-09

### Added
- Initial release
- GitHub and Azure DevOps providers
- SQLite FTS5 full-text search with BM25 ranking
- Markdown and AsciiDoc chunkers
- MCP Streamable HTTP server with `list-libraries`, `resolve-library`, `get-library-docs` tools
- Cron-based scheduled sync
- CLI with `serve`, `fetch`, `list`, `search`, `version` commands
- Per-source proxy routing
- GoReleaser config with multi-arch Docker builds
- GitHub Actions release workflow with cosign signing

[1.0.0]: https://github.com/codanael/docserve/compare/v0.1.0...v1.0.0
[0.1.0]: https://github.com/codanael/docserve/compare/v0.0.1...v0.1.0
[0.0.1]: https://github.com/codanael/docserve/releases/tag/v0.0.1
