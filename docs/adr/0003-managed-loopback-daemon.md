# ADR-0003: Add an optional authenticated loopback daemon

- Status: Proposed
- Date: 2026-08-01
- Owners: JanusMCP maintainers
- Supersedes: None
- Superseded by: None

## Context

One-shot CLI commands start new upstream MCP processes and repeat connection and schema discovery.
Agents making several calls benefit from reusing upstream sessions, but the direct path must remain
available and process lifecycle must behave consistently on Unix and Windows.

## Decision

Add one managed daemon per OS user. It binds only to `127.0.0.1`, owns the Broker Core,
`UpstreamManager`, upstream sessions, and tool cache, and accepts temporary MCP HTTP sessions from
CLI commands. MCP, health, and shutdown endpoints require a random bearer token stored with
owner-only permissions. Shutdown uses an authenticated endpoint rather than portable signal
assumptions.

CLI routing defaults to automatic daemon detection. `--direct`/`JANUS_DAEMON=off` force one-shot
operation; `--daemon`/`require` fail if a compatible daemon is unavailable. In automatic mode,
absence, unhealthy state, configuration/version mismatch, or locked binding falls back to direct
operation with an appropriate warning.

## Alternatives considered

### Make the daemon mandatory

Rejected because it adds lifecycle state to simple calls and creates a new single point of failure.

### Use Unix signals and PID files only

Rejected because Windows process and signal semantics differ and stale PID reuse is unsafe.

### Expose an unauthenticated localhost endpoint

Rejected because other local processes could invoke tools or stop the daemon.

### Let CLI selectors mutate global state

Rejected because concurrent agents could silently redirect each other's calls.

## Consequences

### Positive

- Repeated CLI calls reuse upstream processes, sessions, and schemas.
- Direct mode preserves recovery and debugging.
- Authenticated lifecycle works across supported operating systems.

### Negative

- Metadata, process detachment, readiness, stale state, and token rotation add complexity.
- Configuration and binary changes require daemon restart.

## Compatibility and migration

The daemon is optional and adds no MCP client migration. Raw output, JSON envelopes, timeout,
selectors, and exit codes remain identical between routing paths.

## Security and privacy

The endpoint is loopback-only and bearer-authenticated. The token is never logged and daemon
metadata is treated as a credential. Restart rotates the token.

## Validation

Cover lifecycle idempotence, crash/stale metadata, occupied ports, invalid tokens, config/version
mismatch, direct/required modes, cross-platform detachment, and upstream process reuse.
