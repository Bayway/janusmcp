# Changelog

All notable changes to JanusMCP are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed

- Relayed MCP multi round-trip requests (SEP-2322) instead of answering them in the broker.
  Upstream clients had the SDK's automatic middleware enabled with no elicitation handler
  behind it, so every elicitation-capable upstream tool failed. stdio already negotiates
  2026-07-28, so this affected current installs.
- Forwarded the downstream request's `_meta`, progress token, input responses, and request
  state to the upstream; only the tool name and arguments used to survive the hop.
- Pruned closed sessions from the broker registry. `SessionRegistry.Remove` was never called,
  so every HTTP connection and daemon-routed CLI invocation leaked a session and its server.
- Shared one connection attempt per upstream account. Concurrent first calls each spawned an
  upstream process and orphaned all but the last.
- Followed the `tools/list` cursor. Upstreams with more tools than the server's page size were
  silently truncated to the first page.

### Added

- Exit code 7 (`input_required`) for a tool that asks for interactive input the CLI cannot
  provide. It is a distinct outcome from a tool error, not a retryable failure.

### Security

- Signed and account-bound the `requestState` handed to clients. A retry is refused unless its
  envelope matches the account and tool now resolved for the call, so an account switch landing
  mid-exchange cannot deliver the user's input responses to another tenant.
- Validated the `iss` authorization-response parameter (RFC 9207) for remote OAuth upstreams,
  and bound stored client credentials to the issuer that minted them.
- Marked cacheable list results `private`. The SDK defaults `cacheScope` to `public`, which is
  false for a broker whose tool list depends on the caller's active account.

### Compatibility

- stdio and Streamable HTTP protocol behaviour is unchanged; a new test pins the legacy HTTP
  session contract (initialize at 2025-11-25, `Mcp-Session-Id`, session-scoped switching).
- `call --json` success and error envelopes are unchanged. Exit code 7 is new and is not
  reused from any existing class; codes 1-6, 124 and 130 keep their meanings.
- A `requestState` that does not carry the broker's envelope is forwarded verbatim, so calls
  in flight across an upgrade keep working.
- Vault entries written before issuer tracking skip `iss` validation until their authorization
  server metadata is discovered again, so existing logins are not invalidated.

## [0.5.1] - 2026-08-01

### Fixed

- Corrected the npm launcher to execute the downloaded `janusmcp` binary instead of the retired
  `multimcp` name, restoring `npx @bayway/janusmcp` installations.
- Added an npm launcher contract test to CI and the release source verification.

## [0.5.0] - 2026-08-01

### Added

- Managed local daemon lifecycle with authenticated loopback MCP, health, and shutdown endpoints.
- Transparent CLI routing with `--direct`, `--daemon`, and `JANUS_DAEMON=auto|require|off`.
- Cross-platform process detachment, readiness, stale-state handling, and upstream-session reuse.

### Security

- Random daemon bearer tokens stored with owner-only permissions and rotated on restart.
- Daemon endpoints restricted to `127.0.0.1` and protected from unauthenticated local access.

## [0.4.0] - 2026-08-01

### Added

- Stable `call --json` success and error envelopes for agents.
- Optional command timeout through `--timeout` and `JANUS_CLI_TIMEOUT`.
- Stable documented exit codes and persisted `janusmcp use <account|profile>` selection.
- CLI-first homepage, `/cli/` guide, agent instructions, sitemap, JSON-LD, and package metadata.
- Architecture, compatibility, testing, ADR, and contribution governance documentation.
- Expanded pull request and architecture proposal templates.

### Changed

- Positioned JanusMCP as a multi-account tool broker for AI agents through CLI and MCP.
- Replaced absolute context-cost claims with context-efficient tool discovery language.

### Compatibility

- Preserved MCP stdio/HTTP behavior, existing control tools, raw call output, `tools --json`, and
  `schema` success formats.

[Unreleased]: https://github.com/bayway/janusmcp/compare/v0.5.1...HEAD
[0.5.1]: https://github.com/bayway/janusmcp/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/bayway/janusmcp/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/bayway/janusmcp/compare/v0.3.0...v0.4.0
