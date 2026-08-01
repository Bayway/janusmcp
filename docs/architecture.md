# JanusMCP architecture

JanusMCP is a local, multi-account credential and tool broker. It speaks MCP to upstream
servers and to MCP clients, and exposes the same discovery/call capabilities as a direct CLI.
There is no hosted backend or telemetry service.

## System view

```mermaid
flowchart LR
    Agent["Agent or shell"] --> CLI["JanusMCP CLI"]
    Client["MCP client"] --> Downstream["Downstream MCP session"]
    CLI -->|direct| Manager["UpstreamManager"]
    Downstream --> Core
    Core --> State["Broker state and session registry"]
    Core --> Manager
    Manager --> Vault["OS keychain or encrypted file vault"]
    Manager --> A["Upstream account A"]
    Manager --> B["Upstream account B"]
```

The CLI and downstream MCP server are two surfaces over the same account configuration,
credential resolver, MCP SDK, and upstream semantics.

## Components

| Component | Responsibility | Source |
|---|---|---|
| Command entrypoint | Dispatches CLI, MCP server, UI, and install commands | `go/cmd/janusmcp/main.go` |
| CLI access | Discovers schemas and invokes tools directly | `go/cmd/janusmcp/tools.go` |
| Broker Core | Creates downstream MCP sessions and control tools | `go/internal/broker/server.go` |
| UpstreamManager | Lazily opens per-account MCP sessions and caches tool definitions | `go/internal/broker/upstream.go` |
| BrokerState | Persists the global selector and tracks session-local selection | `go/internal/broker/state.go` |
| Config | Validates accounts, profiles, transports, and binding mode | `go/internal/config/config.go` |
| OAuth and vault | Resolves credentials at connection time and stores secrets locally | `go/internal/oauth/`, `go/internal/vault/` |

## Runtime paths

### MCP client path

An MCP client connects over stdio or Streamable HTTP. JanusMCP creates a downstream session,
registers the `janus_*` control tools, exposes the selected account/profile tools, and routes
calls through `UpstreamManager`. Switching a selector refreshes the session tool set and emits
the MCP tool-list change notification.

### Direct CLI path

`tools`, `schema`, and `call` load the same configuration and credential stack, create a
one-shot `UpstreamManager`, perform the operation, and close the upstream sessions. Raw output
is the human-compatible default; `--json` adds the machine contract.

## Selection and routing

Selectors resolve to one account or to all accounts in a profile. Precedence is:

1. explicit per-call selector;
2. session-local selector;
3. persisted global selector;
4. configured default account.

`bindingMode=global` permits persistent global switching, `session` isolates downstream
sessions, and `locked` rejects switching. Profile tool-name collisions are namespaced so a call
has one deterministic owner.

## Persistence

- Configuration contains account metadata and secret references, not token values.
- OAuth and vault secrets remain in the OS keychain or encrypted file-vault fallback.
- Global active state is atomically persisted with owner-only permissions.
- Upstream sessions and tool caches are in memory and disappear when their owning process exits.

## Trust boundaries and invariants

- JanusMCP binds local HTTP services to loopback unless the user explicitly configures the
  ordinary MCP HTTP server differently.
- OAuth tokens, vault values, and resolved secret environment variables must not
  enter logs or model-visible tool output.
- MCP remains the upstream protocol and the downstream client contract.
- CLI and MCP routes must preserve upstream tool results and account-selection semantics.

See the [compatibility contract](compatibility.md), [security policy](../SECURITY.md), and
[ADRs](adr/README.md) before changing these boundaries.
