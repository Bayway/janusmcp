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
- **Works without an MCP session too** — agents can drive it **directly from the CLI**
  ([code-execution mode](#code-execution-mode--tools-from-the-terminal-zero-context-cost)):
  `janusmcp tools` / `schema` / `call` invoke tools on demand, with zero tool
  definitions loaded into context.
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
| `janusmcp tools [selector]` | Code-execution mode: compact tool list for the active (or given) account/profile. `--json` for full definitions. |
| `janusmcp schema <tool>` | Full JSON definition (input schema) of one tool. `--account <id\|profile>` to disambiguate. |
| `janusmcp call <tool>` | Invoke a tool and print its result. `--account`, `--args '<json>'` (or JSON on stdin). |
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

### Code-execution mode — tools from the terminal, zero context cost

Loading every MCP tool definition into an LLM context is expensive. In code-execution
mode an agent (or you) invokes tools **on demand from the shell** instead — à la
["code execution with MCP"](https://www.anthropic.com/engineering/code-execution-with-mcp) —
so the context holds only the results it actually asked for:

```bash
janusmcp tools                        # compact list for the active account/profile
janusmcp tools client_a               # ...or any account/profile explicitly
janusmcp schema list_tables           # full input schema of ONE tool, only when needed
janusmcp call list_tables --args '{"schemas":["public"]}'
janusmcp call ping --account azienda_b          # cross-account without switching
echo '{"sql":"select 1"}' | janusmcp call db_query   # JSON args via stdin too
```

`call` prints the tool's text content to stdout and exits non-zero on a tool error, so
it composes with pipes and scripts. Selectors resolve exactly like in the broker: the
persisted active account by default, or any account id / profile name; a name that
collides across a profile's accounts must be disambiguated with `--account`. Secrets
resolve through the same vault/OAuth stack as `serve` — nothing extra to configure.

This makes JanusMCP a first-class citizen for **CLI-driven agents** (Claude Code,
Codex, Cursor agents, or any agent with shell access): instead of registering it as
an MCP server, just tell the agent that `janusmcp tools` / `schema` / `call` exist.
Discovery, schemas and invocation happen on demand, credentials stay in the keychain,
and multi-account switching works the same as over MCP. Both modes share the config
and the persisted active account, so you can mix them freely.

## Key features

| | |
|---|---|
| **Multi-account, one endpoint** | N identities for the same service, no reconnecting |
| **Identity scoping** | per-call → per-session (`Mcp-Session-Id`) → global, via `bindingMode: global \| session \| locked` |
| **Dual transport** | stdio (local-first clients) + Streamable HTTP (remote-first clients), same process |
| **Secure vault** | OS keychain (macOS/Windows/Linux) + encrypted-file fallback; secrets as `vault:<name>` |
| **OAuth loopback** | `janusmcp login` (PKCE), tokens stored in the vault, auto-refresh, `oauth:<name>` |
| **Context-safe** | only the active account's tools are exposed; switching emits `tools/list_changed` |
| **MCP *and* CLI** | same broker as an MCP server or via `janusmcp tools` / `schema` / `call` — agents invoke tools on demand, zero definitions in context |

## How it's different

The MCP gateway space (MetaMCP, mcp-proxy, IBM ContextForge, …) aggregates *different*
servers behind one endpoint. JanusMCP solves the orthogonal, under-served problem:
**many identities for the same service**, without saturating the model's context, from any
LLM, fully local. It's a credential-aware broker, not a flat aggregator.

## Built-in connectors

Scaffold an account from a ready-made template with `janusmcp add <template>` (run
`janusmcp catalog` for the full, up-to-date list). Most use browser OAuth (dynamic client
registration); a few need extra setup, noted below.

- **Remote, browser login (OAuth):** `supabase`, `github`, `notion`, `sentry`, `stripe`,
  `hubspot`, `paypal`, `linear`, `vercel`, `canva`, `neon`, `netlify`, `zapier`.
- **SSE transport (legacy):** `asana`, `monday`, `intercom`, `webflow`, `wix`, `square`,
  `globalping`.
- **Cloudflare (Streamable HTTP):** `cloudflare-bindings`, `cloudflare-observability`,
  `cloudflare-radar`, `cloudflare-builds`, `cloudflare-browser`.
- **Google Workspace (bring-your-own OAuth client):** `gmail`, `google-drive`,
  `google-calendar`, `google-chat` — see [`go/docs/google-workspace.md`](go/docs/google-workspace.md).
- **Per-account URL:** `activecampaign` (paste your Remote MCP URL).
- **Figma:** `figma-desktop` (local, recommended) and `figma` (remote, restricted — see below).
- **Generic building blocks:** `http-oauth`, `sse-oauth`, `supabase-pat`, `stdio`.

Missing one? Add any remote server with `http-oauth` / `sse-oauth`, any local one with
`stdio`, or define your own template in `~/.config/janusmcp/templates.json`.

## Provider notes

### ActiveCampaign

ActiveCampaign ships an **official remote MCP server** with a **unique URL per account**
(ActiveCampaign → *Settings → Developer → Remote MCP URL*) and browser-based OAuth — a
natural fit for the multi-account broker. Add one account per client:

```bash
janusmcp add activecampaign ac_clientA   # then paste that client's Remote MCP URL in config.json
janusmcp connect ac_clientA              # browser login
```

Because the URL is per-account, the template seeds a `REPLACE_ACTIVECAMPAIGN_MCP_URL`
placeholder you must replace with your own URL. Login uses dynamic client registration, so
no client ID/secret is needed.

### Google Workspace (Gmail / Drive / Calendar / Chat)

Google offers **remote MCP servers** for Gmail, Drive, Calendar and Chat, but — unlike most
providers here — they require **your own OAuth client** (created in the Google Cloud
Console); they don't support dynamic client registration. JanusMCP supports this via the
`oauthClientId` / `oauthClientSecret` / `scopes` fields, preset by the `gmail`,
`google-drive`, `google-calendar` and `google-chat` templates:

```bash
export GOOGLE_OAUTH_CLIENT_ID=... GOOGLE_OAUTH_CLIENT_SECRET=...
janusmcp add gmail gmail_clientA
janusmcp connect gmail_clientA           # browser login as client A
```

Full one-time Google Cloud setup (enable MCP APIs, consent screen, Desktop OAuth client)
and scope details are in [`go/docs/google-workspace.md`](go/docs/google-workspace.md).

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
- [x] CLI / code-execution mode (`janusmcp tools` / `schema` / `call`) — invoke tools on
      demand instead of loading all definitions, to cut token usage
- [x] SSE upstream transport (in addition to Streamable HTTP + stdio) for SSE-only servers
- [x] Pre-registered OAuth clients (bring-your-own client for servers without dynamic client registration, e.g. Google Workspace)
- [x] Built-in connector catalog (25+ services) via `janusmcp catalog` / `add`
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
