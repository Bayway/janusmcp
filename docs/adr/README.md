# Architecture Decision Records

ADRs record durable decisions, their context, alternatives, and consequences. They complement
the current [architecture overview](../architecture.md); they are not implementation manuals.

## Index

| ADR | Status | Decision |
|---|---|---|
| [0001](0001-cli-and-mcp-equal-surfaces.md) | Accepted | Keep CLI and MCP as equal surfaces over one broker |
| [0002](0002-stable-agent-cli-contract.md) | Accepted | Define a stable, additive agent-oriented CLI contract |
| [0003](0003-managed-loopback-daemon.md) | Accepted | Add an optional authenticated loopback daemon |
| [0004](0004-local-credential-boundary.md) | Accepted | Keep credentials local and resolve them at connection time |

## Process

1. Copy [0000-template.md](0000-template.md) and assign the next four-digit number.
2. Open the ADR as `Proposed`, preferably with the implementation PR.
3. Record the status quo and credible alternatives, not just the selected design.
4. Mark it `Accepted` when the decision is merged.
5. Do not rewrite accepted history. A later decision uses a new ADR and marks the old one
   `Superseded by ADR-NNNN`.

Use an ADR for public contracts, persistence, process/network boundaries, security decisions,
or significant architectural trade-offs. Small fixes and reversible internal refactors do not
need one.
