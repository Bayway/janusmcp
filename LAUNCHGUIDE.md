# JanusMCP

## Tagline
One MCP endpoint, every account — switch identity without reconnecting.

## Description
JanusMCP is a local MCP broker that sits between your LLM client and the real MCP
servers (Supabase, GitHub, Google Workspace, Stripe, Linear, Cloudflare, and more).
The MCP protocol has no notion of "account" — one session means one identity and one
set of credentials — so switching between, say, two clients' Supabase projects or two
Google Workspace accounts normally means disconnecting, reconnecting, and redoing the
OAuth login every time.

JanusMCP fixes that: add N accounts once for the same service, then switch the active
identity on the fly from any LLM client, with no reconnect and no re-login. It only
exposes the *active* account's tools (or a whole "profile" of accounts across services),
so the model's context stays clean. It runs entirely on your machine — your keychain,
your control, no backend, no telemetry. It speaks standard MCP over stdio and Streamable
HTTP, so it works with Claude, ChatGPT, Cursor, VS Code, Gemini, and others. Agents with
shell access don't even need an MCP session: the code-execution mode (`janusmcp tools` /
`schema` / `call`) lets them discover and invoke tools on demand from the CLI, with zero
tool definitions loaded into context.

It's for anyone who juggles more than one account of the same service: agencies and
consultants managing many clients, teams with multiple orgs/workspaces, or developers
switching between personal and work identities.

## Setup Requirements
JanusMCP needs no API key to start; run `npx -y @bayway/janusmcp serve` (or install the
binary) and add accounts with `janusmcp add <template>`. Per-account secrets are kept in
your OS keychain (`janusmcp vault set`) or obtained via browser OAuth (`janusmcp connect`)
— never in environment variables or the chat context. The following env vars are optional:

- `JANUS_CONFIG` (optional): Path to the accounts config file. Defaults to the OS config dir (`<user-config>/janusmcp/config.json`). https://github.com/bayway/janusmcp
- `JANUS_TRANSPORT` (optional): `stdio` | `http` | `both`. Default `stdio`.
- `JANUS_HTTP_HOST` / `JANUS_HTTP_PORT` (optional): Bind address for HTTP transport. Default `127.0.0.1:7332`.
- `JANUS_VAULT` (optional): `keychain` (default) or `file` for the encrypted-file fallback on headless machines.
- `GOOGLE_OAUTH_CLIENT_ID` / `GOOGLE_OAUTH_CLIENT_SECRET` (optional): Only for the Google Workspace templates, which require your own OAuth client. https://github.com/bayway/janusmcp/blob/main/go/docs/google-workspace.md

## Category
Developer Tools

## Use Cases
Multi-account management, Identity switching, Agencies & consultants, Multi-tenant workflows, Local-first credential vault, Prototyping, CI/CD

## Features
- Add N accounts for the same service and switch the active identity without reconnecting or re-logging in.
- Exposes only the active account's tools, so the model's context stays small (switching emits `tools/list_changed`).
- Profiles: activate a whole client's stack (e.g. Supabase + GitHub + Slack of Client A) at once, each call routed to the right upstream.
- One-shot cross-account calls (`janus_with_account`) without changing the active account.
- Dual transport: stdio (local-first clients) and Streamable HTTP; SSE upstream supported for legacy servers.
- Secure vault: OS keychain (macOS/Windows/Linux) with an encrypted-file fallback; secrets referenced as `vault:<name>`.
- Browser OAuth loopback (PKCE) with auto-refresh, including bring-your-own pre-registered clients (e.g. Google Workspace).
- Built-in catalog of ready-to-use connectors: Supabase, GitHub, Notion, Sentry, Stripe, HubSpot, PayPal, Linear, Vercel, Canva, Neon, Netlify, Zapier, ActiveCampaign, Google Workspace (Gmail/Drive/Calendar/Chat), Cloudflare, Asana, Monday, Intercom, Webflow, Wix, Square, Globalping, Figma.
- Code-execution mode (`janusmcp tools` / `schema` / `call`) to invoke tools on demand from the shell and cut token usage.
- Fully local: no backend, no telemetry; credentials never pass through the model context.

## Getting Started
- "List my accounts and tell me which one is active" (uses `janus_list_accounts` / `janus_whoami`)
- "Switch to client_b and list its Supabase tables" (uses `janus_use_account`, then the upstream tool)
- "Without switching, run list_tables on client_a" (uses `janus_with_account`)
- Tool: janus_list_accounts — List all configured accounts/profiles and the active one.
- Tool: janus_use_account — Set the active account (per-call, per-session, or global depending on binding mode).
- Tool: janus_whoami — Show the currently active account/profile.
- Tool: janus_use_profile — Activate a profile (a client's accounts across several services at once).
- Tool: janus_with_account — Run a single tool call on another account without changing the active one.
- Tool: janus_login — Complete a browser OAuth login for an account.

## Tags
mcp, broker, multi-account, oauth, proxy, identity, local-first, keychain, developer-tools, stdio, streamable-http, agencies, code-execution, agents

## Documentation URL
https://github.com/bayway/janusmcp

## Health Check URL
Not applicable — JanusMCP is a local broker (stdio + local HTTP), not a hosted remote server.
