# Compatibility contract

JanusMCP is pre-1.0, but releases still preserve documented interfaces unless a migration and
explicit release note say otherwise. This contract protects existing MCP clients while the CLI
and optional daemon evolve.

## MCP compatibility

- Existing stdio and Streamable HTTP entrypoints continue to use standard MCP semantics.
- Upstream tool names, descriptions, schemas, content, structured content, and `isError` values
  are forwarded without reinterpretation except documented profile collision namespacing.
- Multi round-trip requests are relayed, not absorbed: an upstream's `inputRequests` reach the
  client, and the client's `inputResponses` reach the upstream. The `requestState` in between is
  the broker's own signed value, bound to the account and tool it was minted for; a retry that
  does not match the route resolved for the call is refused. A `requestState` without the
  broker's envelope is passed through unchanged.
- List results are advertised as `cacheScope: private`, because what the broker exposes depends
  on the caller's active account.
- The `janus_*` control tools remain additive broker tools. Adding an optional field to a control
  tool is compatible; removing or renaming a tool or required field is not.
- Account/profile switching continues to respect `global`, `session`, and `locked` binding modes.
- The direct CLI and daemon must not change what an external MCP client sees from ordinary
  `janusmcp serve`.

## CLI compatibility

- Raw success output remains the default for `call`.
- Existing success formats for `tools --json` and `schema` remain unchanged.
- `call --json` returns `ok`, `account`, `tool`, `isError`, and `content`, plus
  `structuredContent` when supplied upstream.
- Operational JSON errors use `{"ok":false,"error":{"code":"…","message":"…"}}` on stderr.
  MCP tool errors retain upstream content and exit with code 6.
- Exit codes, once assigned, are not reused for a different class of failure. Exit code 7 means
  the tool asked for interactive input the CLI cannot supply; it is not a tool error (6) and
  retrying from the shell will not change the outcome.
- Timeout remains opt-in. A flag overrides `JANUS_CLI_TIMEOUT`.

## Configuration and state

- Existing valid account and profile configuration continues to load without migration.
- New configuration fields should be optional and have backward-compatible defaults.
- Config files contain references to secrets, not plaintext credentials.
- Persisted selector and daemon metadata changes require safe handling of absent, stale, and
  older files. Failure to persist must not silently report success.

## Daemon compatibility

- The daemon is optional; one-shot direct operation remains supported.
- `JANUS_DAEMON=auto` may fall back to direct mode. `require` fails rather than silently falling
  back, and `off` never contacts daemon state.
- A configuration or binary-version mismatch never reuses the daemon silently.
- Daemon routing must preserve CLI raw/JSON output, timeout, selector, and exit-code behavior.

## Change policy

A PR that changes one of these contracts must include:

1. an ADR describing rationale and alternatives;
2. a migration or deprecation path where feasible;
3. tests for old and new behavior;
4. a `CHANGELOG.md` entry;
5. an explicit compatibility note in the PR description.

Before 1.0, an unavoidable breaking change requires a minor version increment. Security fixes
may remove unsafe behavior immediately, but must document impact and remediation.
