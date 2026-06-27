# First run — two Supabase accounts, end to end

A complete walkthrough as a brand-new user: install JanusMCP, add two Supabase projects
that live under **two different Supabase accounts**, and work across both from your LLM
client — switching identity with a tool call, no reconnecting.

> How "login" works here: Supabase authenticates with a **Personal Access Token (PAT)**,
> not an interactive OAuth login. You paste each PAT **locally** (it goes into your OS
> keychain), so the token never passes through the model's chat context. The model then
> uses the credentials only indirectly, via the `janus_*` tools — it never sees them.
> (For real OAuth providers there's `janusmcp login`, which opens a browser; see the end.)

---

## Step 1 — Install

Pick one:

```bash
# npm (works great inside MCP/npx setups)
npx janusmcp version

# Homebrew (macOS/Linux)
brew install bayway/janusmcp/janusmcp

# or build from source
git clone https://github.com/bayway/janusmcp && cd janusmcp/go && make build
```

For the rest of this guide we'll call the binary `janusmcp`.

## Step 2 — Get your Supabase credentials

For **each** of the two projects (ideally owned by two different accounts/emails):

- **Project ref** — Dashboard → Project Settings → General → "Reference ID".
- **Personal Access Token** — <https://supabase.com/dashboard/account/tokens>, logged in
  as the account that owns that project. That account *is* the identity you're testing.

So you end up with: `PAT_A` + `REF_A` (Client A), `PAT_B` + `REF_B` (Client B).

## Step 3 — "Log in": store each PAT in the vault

```bash
janusmcp vault set supabase_client_a      # paste PAT_A, press enter
janusmcp vault set supabase_client_b      # paste PAT_B, press enter
```

What happened: each token is now encrypted in your **OS keychain** (macOS Keychain /
Windows Credential Manager / Linux Secret Service). Nothing is written to your config in
clear text. This is the "login" step for Supabase — done once per account.

Verify they're stored (the values stay hidden):

```bash
janusmcp providers       # shows built-in presets; reminder that Supabase uses PAT+vault
```

## Step 4 — Configure the two accounts

```bash
cp config.supabase.example.json config.json
```

Edit `config.json` and set your two refs:

```jsonc
{
  "defaultAccount": "client_a",
  "bindingMode": "session",
  "accounts": [
    { "id": "client_a", "service": "supabase", "label": "Client A",
      "command": "npx",
      "args": ["-y", "@supabase/mcp-server-supabase@latest", "--read-only", "--project-ref=REF_A"],
      "env": { "SUPABASE_ACCESS_TOKEN": "vault:supabase_client_a" } },
    { "id": "client_b", "service": "supabase", "label": "Client B",
      "command": "npx",
      "args": ["-y", "@supabase/mcp-server-supabase@latest", "--read-only", "--project-ref=REF_B"],
      "env": { "SUPABASE_ACCESS_TOKEN": "vault:supabase_client_b" } }
  ]
}
```

The `vault:supabase_client_a` reference is resolved to the real PAT **at connect time**,
fresh each spawn — the token is never in this file.

## Step 5 — Connect it to your LLM client

**Claude Desktop** (`claude_desktop_config.json`):

```json
{ "mcpServers": { "janusmcp": {
  "command": "janusmcp", "args": ["serve"],
  "env": { "JANUS_CONFIG": "/abs/path/config.json" } } } }
```

Or test instantly with the MCP Inspector:

```bash
JANUS_CONFIG=./config.json npx @modelcontextprotocol/inspector janusmcp serve
```

## Step 6 — Use it: switch identity with a tool call

Now everything happens **inside the chat / client**, through tools. A typical session:

**1. See your accounts**

> Tool: `janus_list_accounts`

```json
{ "active": "client_a", "bindingMode": "session",
  "accounts": [
    { "id": "client_a", "label": "Client A", "active": true },
    { "id": "client_b", "label": "Client B", "active": false } ] }
```

**2. You're on Client A — run a Supabase tool**

> Tool: `list_tables`  → returns **Client A's** tables (e.g. `customers`, `orders`).

**3. Switch to Client B — the "login via tool" moment**

> Tool: `janus_use_account`  with `{ "account_id": "client_b" }`

```json
{ "ok": true, "active": "client_b", "scope": "session" }
```

The broker swaps the active identity and emits `tools/list_changed`. No reconnect, no
re-auth dialog, no editing config.

**4. Same tool, different identity**

> Tool: `list_tables`  → now returns **Client B's** tables (a different project, a
> different Supabase account).

That's the whole value: **one endpoint, two accounts, switch with a tool call.**

## Bonus — concurrent identities (HTTP mode)

```bash
JANUS_TRANSPORT=http JANUS_HTTP_PORT=7332 JANUS_CONFIG=./config.json janusmcp serve
# add http://127.0.0.1:7332/mcp as a remote MCP server in ChatGPT/Gemini/Cursor
```

With `bindingMode: session`, two chats/tabs can sit on Client A and Client B **at the same
time** through one broker process.

## What about an OAuth "login" tool?

Supabase uses PATs, so its "login" is Step 3. For providers that support OAuth
(`janusmcp providers`), you log in with a browser flow where the token never touches the
chat:

```bash
janusmcp login github my_work_github     # opens the browser, stores the token in the vault
```

then reference it from an account as `"oauth:my_work_github"`.

You can also do this **from inside the chat**, via the `janus_login` tool — secure, because
the token is captured by the broker and stored in the keychain, never in the chat:

> Tool: `janus_login` with `{ "provider": "github", "name": "my_work_github" }`

```json
{ "ok": true, "stored_as": "my_work_github", "reference": "oauth:my_work_github" }
```

Your browser opens, you approve, done — then reference `oauth:my_work_github` in an account.
(Supabase still uses the PAT path above; `janus_login` is for OAuth providers.)
