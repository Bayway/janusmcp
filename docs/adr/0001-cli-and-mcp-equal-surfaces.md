# ADR-0001: Keep CLI and MCP as equal surfaces over one broker

- Status: Accepted
- Date: 2026-08-01
- Owners: JanusMCP maintainers
- Supersedes: None
- Superseded by: None

## Context

JanusMCP began as a multi-account MCP broker. Agents increasingly invoke tools through direct
CLI execution to avoid loading large tool catalogs upfront. Renaming the project around the CLI
or replacing MCP would weaken existing client compatibility and duplicate credential, routing,
and protocol logic.

## Decision

Keep the JanusMCP name, package coordinates, domain, and MCP client integrations. Present direct
CLI and MCP as equal public surfaces over the same local credential broker. MCP remains the
upstream protocol and the downstream protocol for MCP clients; CLI commands perform on-demand
discovery and calls through the same configuration and broker semantics.

## Alternatives considered

### Replace MCP with provider-specific CLI integrations

Rejected because it would duplicate protocol adapters, fragment behavior, and lose compatibility
with existing MCP servers and clients.

### Rename or fork a CLI-only product

Rejected because identity, package, domain, and installed-client continuity have value, while the
new CLI surface is additive rather than a different product.

### Keep CLI as an undocumented implementation detail

Rejected because agents and search engines need a supported, explicit discovery and invocation
workflow.

## Consequences

### Positive

- Existing MCP users keep their integration unchanged.
- CLI agents discover schemas only when needed.
- Both surfaces share account, profile, OAuth, vault, routing, and test behavior.

### Negative

- Documentation and CI must test two public surfaces.
- CLI and MCP behavior can drift unless compatibility is reviewed explicitly.

## Compatibility and migration

No migration is required. MCP clients continue using `janusmcp serve`; CLI commands are additive.
The compatibility rules are documented in [compatibility.md](../compatibility.md).

## Security and privacy

Both surfaces use the same local credential resolver and vault. CLI output must never expose
resolved credentials.

## Validation

Run MCP broker regression tests plus raw/JSON CLI discovery and call tests against the same mock
upstreams.
