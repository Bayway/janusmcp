<div align="center">

# JanusMCP

**One MCP endpoint, every account.**
Add your credentials once, switch identity without reconnecting — from any LLM.

[![CI](https://github.com/bayway/janusmcp/actions/workflows/ci.yml/badge.svg)](https://github.com/bayway/janusmcp/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/bayway/janusmcp?sort=semver)](https://github.com/bayway/janusmcp/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
![Status: alpha](https://img.shields.io/badge/status-alpha-orange)
[![Website](https://img.shields.io/badge/website-janusmcp.dev-7c8cff)](https://janusmcp.dev)

<br/>

[![Add to Cursor](https://img.shields.io/badge/Add_to-Cursor-0098FF?style=for-the-badge)](cursor://anysphere.cursor-deeplink/mcp/install?name=janusmcp&config=eyJjb21tYW5kIjogIm5weCIsICJhcmdzIjogWyIteSIsICJAYmF5d2F5L2phbnVzbWNwIiwgInNlcnZlIl19)
[![Install in VS Code](https://img.shields.io/badge/Install-VS_Code-007ACC?style=for-the-badge&logo=visualstudiocode)](https://insiders.vscode.dev/redirect/mcp/install?name=janusmcp&config=%7B%22command%22%3A%20%22npx%22%2C%20%22args%22%3A%20%5B%22-y%22%2C%20%22%40bayway/janusmcp%22%2C%20%22serve%22%5D%7D)

<sub>One-click buttons require the npm package to be published. See <a href="RELEASING.md">RELEASING.md</a>.</sub>

</div>

## The problem

If you work with more than one company, you use the *same* MCP server (Supabase, GitHub,
Slack…) with **different identities** — a different account, email and token per client.
Today most LLM clients hold **one account at a time** per connector: to switch client you
disconnect, reconnect, and redo the OAuth login. Every time.

The MCP protocol has no notion of "account": **one session = one identity = one set of
credentials.** JanusMCP fills that gap.

## What it does

JanusMCP is a **local broker** that sits between your LLM client and the real MCP servers:

- **Add N accounts once** for the same service and keep them all available.
- **Switch identity without reconnecting** — no re-login, no fiddling with config.
- **Works with any LLM client** — it just speaks standard MCP (stdio + Streamable HTTP).
- **Runs locally** — your machine, your keychain, your control.
- **Keeps the context clean** — it exposes only the *active* account's tools, not N×tools.

```
                 ┌─────────────────────────────┐   ┌─ Supabase (Client A)
LLM client ─MCP─▶│   JanusMCP broker           │─▶ ├─ Supabase (Client B)
(Claude/GPT/     │   active-account · vault ·  │   ├─ GitHub  (Client A)
 Gemini/…)       │   per-session scoping       │   └─ …
                 └─────────────────────────────┘
```

You drive it with three control tools that appear in any client:
`janus_list_accounts`, `janus_use_account`, `janus_whoami`.

## Install

Once released, install via your favorite channel (all published automatically on each
tag — see [RELEASING.md](RELEASING.md)):

```bash
npx @bayway/janusmcp serve                                   # npm (works inside MCP/npx setups)
brew install bayway/janusmcp/janusmcp     # Homebrew (macOS/Linux)
scoop install janusmcp                               # Windows
docker run --rm -p 7332:7332 ghcr.io/bayway/janusmcp:latest
```

…or download a prebuilt binary from [Releases](https://github.com/bayway/janusmcp/releases).

## Build from source (60-second quickstart)

```bash
git clone https://github.com/bayway/janusmcp
cd janusmcp/go
make build                       # produces ./bin/janusmcp
cp config.example.json config.json   # edit with your accounts
./bin/janusmcp serve             # stdio, for Claude Desktop/Code
```

Two Supabase clients, PATs kept in your OS keychain (never in the config):

```bash
./bin/janusmcp vault set supabase_client_a   # paste the PAT
./bin/janusmcp vault set supabase_client_b
```

```jsonc
// config.json
{
  "bindingMode": "session",
  "accounts": [
    { "id": "client_a", "service": "supabase", "command": "npx",
      "args": ["-y", "@supabase/mcp-server-supabase@latest", "--read-only", "--project-ref=REF_A"],
      "env": { "SUPABASE_ACCESS_TOKEN": "vault:supabase_client_a" } },
    { "id": "client_b", "service": "supabase", "command": "npx",
      "args": ["-y", "@supabase/mcp-server-supabase@latest", "--read-only", "--project-ref=REF_B"],
      "env": { "SUPABASE_ACCESS_TOKEN": "vault:supabase_client_b" } }
  ]
}
```

Add it to Claude Desktop:

```json
{ "mcpServers": { "janusmcp": {
  "command": "/abs/path/janusmcp/go/bin/janusmcp", "args": ["serve"],
  "env": { "JANUS_CONFIG": "/abs/path/janusmcp/go/config.json" } } } }
```

For ChatGPT / Gemini / Cursor / Copilot, run HTTP and point them at the URL:

```bash
JANUS_TRANSPORT=http JANUS_HTTP_PORT=7332 ./bin/janusmcp serve
# → http://127.0.0.1:7332/mcp
```

## Commands

Run `janusmcp help` for the full reference. The essentials:

| Command | What it does |
|---|---|
| `janusmcp serve` | Run the broker (default). Transports via env: `JANUS_TRANSPORT=stdio\|http\|both`, `JANUS_HTTP_HOST`, `JANUS_HTTP_PORT`. |
| `janusmcp ui` | Open the local control panel — add accounts, log in, set secrets. |
| `janusmcp add <template> [id]` | Add an account from a template (`janusmcp catalog` lists them). |
| `janusmcp catalog` | List the built-in account templates. |
| `janusmcp connect <id>` | Connect an account; for remote OAuth, opens the browser. |
| `janusmcp status` | Show each account's login/secret status (no secret values). |
| `janusmcp vault set <name>` / `delete <name>` | Store / remove a secret in the OS keychain. |
| `janusmcp login <provider> <name>` | OAuth loopback login; token referenced as `oauth:<name>`. |
| `janusmcp providers` | List built-in OAuth providers. |
| `janusmcp install <client>` | Configure an LLM client to launch JanusMCP. |
| `janusmcp uninstall <client>` | Remove JanusMCP from an LLM client's config. |
| `janusmcp version` · `janusmcp help` | Version · this reference. |

Supported `<client>` values for `install` / `uninstall`: `claude-desktop`, `claude-code`,
`cursor`, `vscode`, `gemini`, `codex`, `chatgpt`, `print`. Run `janusmcp install list`
(or `uninstall list`) to see each target and whether it's already configured.

```bash
janusmcp install claude-desktop      # one-command setup
janusmcp uninstall claude-desktop    # clean removal (restart the client afterwards)
```

Inside any connected client you also get the control tools `janus_list_accounts`,
`janus_use_account`, `janus_whoami`, `janus_login`, `janus_use_profile`, and
`janus_with_account`.

### Profiles — a whole client's stack at once

A **profile** groups accounts of the same client across different services. Activating
it exposes the tools of *all* its accounts together, and each call is routed to the
right upstream:

```jsonc
// config.json
{
  "accounts": [
    { "id": "supabase_a", "service": "supabase", "transport": "http", "url": "https://mcp.supabase.com/mcp", "auth": "oauth" },
    { "id": "github_a",   "service": "github",   "transport": "http", "url": "https://api.githubcopilot.com/mcp/", "auth": "oauth" }
  ],
  "profiles": {
    "client_a": ["supabase_a", "github_a"]
  }
}
```

Then in chat: `janus_use_profile` with `{ "profile": "client_a" }` → Supabase **and**
GitHub tools for Client A are available simultaneously. Colliding tool names across
accounts are namespaced (`<account>_<tool>`).

### One-shot cross-account calls

`janus_with_account` runs a single call on another account **without** changing the
active one — e.g. `{ "account_id": "client_b", "tool": "list_tables" }`. Omit `tool`
to list that account's available tools first.

## Key features

| | |
|---|---|
| **Multi-account, one endpoint** | N identities for the same service, no reconnecting |
| **Identity scoping** | per-call → per-session (`Mcp-Session-Id`) → global, via `bindingMode: global \| session \| locked` |
| **Dual transport** | stdio (local-first clients) + Streamable HTTP (remote-first clients), same process |
| **Secure vault** | OS keychain (macOS/Windows/Linux) + encrypted-file fallback; secrets as `vault:<name>` |
| **OAuth loopback** | `janusmcp login` (PKCE), tokens stored in the vault, auto-refresh, `oauth:<name>` |
| **Context-safe** | only the active account's tools are exposed; switching emits `tools/list_changed` |

## How it's different

The MCP gateway space (MetaMCP, mcp-proxy, IBM ContextForge, …) aggregates *different*
servers behind one endpoint. JanusMCP solves the orthogonal, under-served problem:
**many identities for the same service**, without saturating the model's context, from any
LLM, fully local. It's a credential-aware broker, not a flat aggregator.

## Provider notes

### Figma

Figma offers two MCP servers, handled differently here:

- **Local Dev Mode server (recommended).** Figma's desktop app hosts an MCP server on
  `http://127.0.0.1:3845/mcp`. It's local, needs no OAuth, and works out of the box:
  enable it in the desktop app (Dev Mode → Inspect → *Enable desktop MCP server*) and add it
  with `janusmcp add figma-desktop figma_work`. Requires a Dev/Full seat on a paid Figma plan.

- **Remote server (`https://mcp.figma.com/mcp`) — restricted.** Figma **allowlists the OAuth
  `client_name`** during dynamic client registration and returns **403 Forbidden** to any client
  that isn't in its [MCP catalog](https://www.figma.com/mcp-catalog/) (VS Code, Cursor, Claude
  Code, …). JanusMCP is not (yet) an approved client.

  > ⚠️ **Workaround — opt-in, use at your own risk.** You can make JanusMCP register under an
  > approved name by setting `"clientName": "Claude Code"` on a remote `figma` account in your
  > `config.json`. This impersonates an approved client and **may violate Figma's Terms of
  > Service**; it can also break whenever Figma updates its allowlist. It is **not** enabled by
  > default. Prefer the local Dev Mode server above for real work.

  The proper long-term fix is for JanusMCP to be submitted to and approved for Figma's MCP
  catalog, so no `client_name` override is needed. This is planned.

## Status & roadmap

Alpha — the core is implemented and tested in Go.

- [x] Active-account model, per-session / global / locked scoping
- [x] stdio + Streamable HTTP transports
- [x] OS-keychain vault + encrypted-file fallback
- [x] OAuth loopback (PKCE) with auto-refresh and per-spawn token resolution
- [x] One-command client install (`janusmcp install …`), Claude Desktop `.mcpb`, registry `server.json`
- [x] Multi-server "profiles" per client (Supabase + GitHub + Slack of Client A at once)
- [x] `with_account` one-shot cross-account calls
- [ ] CLI / code-execution mode — invoke tools on demand via a CLI instead of loading all
      tool definitions, to cut token usage (à la "code execution with MCP")
- [ ] SSE upstream transport (in addition to Streamable HTTP + stdio) for SSE-only servers
- [ ] Signed, per-OS release binaries & registry auto-publish in CI

See [`design-broker-mcp-multi-account.md`](design-broker-mcp-multi-account.md) for the full design.

## Repository layout

- [`go/`](go/) — the broker (Go). This is the real implementation. **[Build & docs →](go/README.md)**
- [`spike/`](spike/) — the original TypeScript spike, kept as a verified reference of behavior.
- [`design-broker-mcp-multi-account.md`](design-broker-mcp-multi-account.md) — architecture & rationale.

## Privacy Policy

JanusMCP runs entirely on your machine. It has **no backend servers, collects no data, and
contains no analytics or telemetry** — the developers receive nothing about you or your usage.
Credentials and tokens are stored in your OS keychain (or, if you opt in, an encrypted local
file) and are used only to authenticate to the services you configure; they never pass through
the model context. Network connections are made only to the MCP servers and OAuth providers you
configure. Because everything is local, data is retained only on your own device for as long as
you keep it, and removing it (`janusmcp vault delete <name>`, or uninstalling) removes it
entirely.

Full policy: <https://janusmcp.dev/privacy>.

## Contributing

Contributions are very welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). New connector
presets, client integration guides, and packaging help are especially appreciated.

## License

MIT — see [LICENSE](LICENSE).
