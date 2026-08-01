# Changelog

All notable changes to JanusMCP are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Documentation

- Added architecture, compatibility, testing, ADR, and contribution governance documentation.
- Expanded pull request and architecture proposal templates.

## [0.4.0] - 2026-08-01

### Added

- Stable `call --json` success and error envelopes for agents.
- Optional command timeout through `--timeout` and `JANUS_CLI_TIMEOUT`.
- Stable documented exit codes and persisted `janusmcp use <account|profile>` selection.
- CLI-first homepage, `/cli/` guide, agent instructions, sitemap, JSON-LD, and package metadata.

### Changed

- Positioned JanusMCP as a multi-account tool broker for AI agents through CLI and MCP.
- Replaced absolute context-cost claims with context-efficient tool discovery language.

### Compatibility

- Preserved MCP stdio/HTTP behavior, existing control tools, raw call output, `tools --json`, and
  `schema` success formats.

[Unreleased]: https://github.com/bayway/janusmcp/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/bayway/janusmcp/compare/v0.3.0...v0.4.0
