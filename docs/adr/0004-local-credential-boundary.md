# ADR-0004: Keep credentials local and resolve them at connection time

- Status: Accepted
- Date: 2026-08-01
- Owners: JanusMCP maintainers
- Supersedes: None
- Superseded by: None

## Context

JanusMCP routes multiple work identities, so a credential leak can cross client and account
boundaries. Plaintext configuration, eager token expansion, remote control planes, or logging
resolved process environments would increase the blast radius.

## Decision

Run the broker locally with no backend or telemetry. Store OAuth tokens and named secrets in the
OS keychain, with an explicitly selected encrypted file-vault fallback. Configuration stores only
metadata and `vault:`, `oauth:`, or environment references. Resolve those references immediately
before connecting or spawning an upstream so refreshed credentials are used without persisting
plaintext values.

## Alternatives considered

### Store credentials directly in configuration

Rejected because config is commonly copied, backed up, or committed.

### Use a hosted credential broker

Rejected because it adds an external trust boundary, backend operations, and telemetry/privacy
questions outside the product scope.

### Resolve every secret once at startup

Rejected because OAuth tokens may refresh and long-running broker processes need current values
when a new upstream connection is created.

## Consequences

### Positive

- Secrets remain under the user's OS account and do not enter model context.
- Lazy upstream connections receive current credentials.
- JanusMCP has no remote database or telemetry surface to secure.

### Negative

- OS keychain behavior and setup differ across platforms.
- Headless environments may need the encrypted file-vault fallback.

## Compatibility and migration

Existing secret-reference syntax remains supported. New credential mechanisms must preserve the
same local boundary or require a superseding ADR.

## Security and privacy

Logs, errors, tool results, state metadata, and diagnostics must never include vault values,
OAuth tokens or resolved environment values.

## Validation

Test vault persistence and permissions, resolution at spawn/connect time, OAuth refresh, error
redaction, and the absence of plaintext secrets in config and logs.
