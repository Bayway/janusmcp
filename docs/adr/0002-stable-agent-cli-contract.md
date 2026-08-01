# ADR-0002: Define a stable agent-oriented CLI contract

- Status: Accepted
- Date: 2026-08-01
- Owners: JanusMCP maintainers
- Supersedes: None
- Superseded by: None

## Context

Human-readable output is convenient interactively, while agents require deterministic results,
machine-readable errors, bounded execution, and stable process exit codes. Changing defaults
would break existing scripts, and a mandatory timeout could interrupt OAuth or long-running tools.

## Decision

Keep raw call output as the default and add `call --json`. The JSON success envelope includes
`ok`, `account`, `tool`, `isError`, `content`, and optional `structuredContent`. Operational JSON
errors go to stderr. MCP tool errors preserve upstream content and use exit code 6.

Define stable exit classes for success, usage, selection/configuration, authentication, upstream
protocol, MCP tool failure, timeout, and interruption. Add an opt-in duration timeout whose
precedence is command flag, `JANUS_CLI_TIMEOUT`, then no timeout. Add `use` for validated,
persisted global selection while rejecting switching in locked mode.

## Alternatives considered

### Make JSON the default

Rejected because it would break the existing human-facing raw contract.

### Return every failure as exit code 1

Rejected because agents cannot distinguish retryable upstream failures, invalid input, missing
selectors, authentication, and tool-level failure.

### Apply a mandatory default timeout

Rejected because safe duration varies by OAuth provider and tool workload.

## Consequences

### Positive

- Agents can parse results and classify failures without scraping prose.
- Existing raw scripts remain compatible.
- Timeout behavior is deterministic and opt-in.

### Negative

- New exit codes and JSON fields become compatibility commitments.
- Both success and failure paths need raw and JSON tests.

## Compatibility and migration

The change is additive. Existing `tools --json`, `schema`, and raw `call` success formats remain
unchanged.

## Security and privacy

Operational diagnostics remain on stderr and must redact credentials. Explicit selectors reduce
the risk of invoking a tool against the wrong account.

## Validation

Test raw compatibility, structured content, multiple content items, stdin, interleaved flags,
timeouts, every exit class, profiles, collisions, persistence, and locked mode.
