<div align="center">

# JanusMCP

**One MCP endpoint, every account.**
Add your credentials once, switch identity without reconnecting — from any LLM.

[![CI](https://github.com/bayway/janusmcp/actions/workflows/ci.yml/badge.svg)](https://github.com/bayway/janusmcp/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/bayway/janusmcp)](https://goreportcard.com/report/github.com/bayway/janusmcp)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
![Status: alpha](https://img.shields.io/badge/status-alpha-orange)

<br/>

[![Add to Cursor](https://img.shields.io/badge/Add_to-Cursor-0098FF?style=for-the-badge)](cursor://anysphere.cursor-deeplink/mcp/install?name=janusmcp&config=eyJjb21tYW5kIjogIm5weCIsICJhcmdzIjogWyIteSIsICJqYW51c21jcCIsICJzZXJ2ZSJdfQ==)
[![Install in VS Code](https://img.shields.io/badge/Install-VS_Code-007ACC?style=for-the-badge&logo=visualstudiocode)](https://insiders.vscode.dev/redirect/mcp/install?name=janusmcp&config=%7B%22command%22%3A%22npx%22%2C%22args%22%3A%5B%22-y%22%2C%22janusmcp%22%2C%22serve%22%5D%7D)

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
npx janusmcp serve                                   # npm (works inside MCP/npx setups)
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

## Status & roadmap

Alpha — the core is implemented and tested in Go.

- [x] Active-account model, per-session / global / locked scoping
- [x] stdio + Streamable HTTP transports
- [x] OS-keychain vault + encrypted-file fallback
- [x] OAuth loopback (PKCE) with auto-refresh and per-spawn token resolution
- [x] One-command client install (`janusmcp install …`), Claude Desktop `.mcpb`, registry `server.json`
- [ ] Multi-server "profiles" per client (Supabase + GitHub + Slack of Client A at once)
- [ ] `with_account` one-shot cross-client calls
- [ ] CLI / code-execution mode — invoke tools on demand via a CLI instead of loading all
      tool definitions, to cut token usage (à la "code execution with MCP")
- [ ] SSE upstream transport (in addition to Streamable HTTP + stdio) for SSE-only servers
- [ ] Signed, per-OS release binaries & registry auto-publish in CI

See [`design-broker-mcp-multi-account.md`](design-broker-mcp-multi-account.md) for the full design.

## Repository layout

- [`go/`](go/) — the broker (Go). This is the real implementation. **[Build & docs →](go/README.md)**
- [`spike/`](spike/) — the original TypeScript spike, kept as a verified reference of behavior.
- [`design-broker-mcp-multi-account.md`](design-broker-mcp-multi-account.md) — architecture & rationale.

## Contributing

Contributions are very welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). New connector
presets, client integration guides, and packaging help are especially appreciated.

## License

MIT — see [LICENSE](LICENSE).
