# Testing JanusMCP with two Supabase accounts

This is the fastest real-world test: two Supabase projects (ideally under two different
accounts/emails) behind one broker, switching identity without reconnecting.

The Supabase MCP server authenticates with a **Personal Access Token (PAT)**, so we keep
each PAT in the OS keychain and reference it from the config — no tokens on disk.

## 1. Get the pieces from Supabase

For each project you want to test:

- **Project ref**: Dashboard → Project Settings → General → "Reference ID"
  (looks like `abcdwxyzproject`).
- **Personal Access Token**: <https://supabase.com/dashboard/account/tokens> → "Generate new token".
  Use the token of the *account that owns that project* (that's the identity you're testing).

You also need Node on PATH (the Supabase MCP runs via `npx`).

## 2. Store the PATs in the vault

```bash
cd go
make build

./bin/janusmcp vault set supabase_client_a      # paste PAT for account/project A
./bin/janusmcp vault set supabase_client_b      # paste PAT for account/project B
```

By default these go into your OS keychain (macOS Keychain / Windows Credential Manager /
Linux Secret Service). On a headless box use the encrypted-file fallback:
`JANUS_VAULT=file ./bin/janusmcp vault set …`.

## 3. Configure

```bash
cp config.supabase.example.json config.json
# edit config.json: replace PROJECT_REF_A / PROJECT_REF_B with your two refs
```

## 4. Run + verify (stdio, e.g. via the MCP Inspector or Claude Desktop)

```bash
JANUS_CONFIG=./config.json ./bin/janusmcp serve
```

Wire it into a client (Claude Desktop snippet in the main README) or test it directly with
the MCP Inspector:

```bash
npx @modelcontextprotocol/inspector ./bin/janusmcp serve
```

Then, in the client:

1. `janus_list_accounts` → shows `client_a` (active) and `client_b`.
2. Call a Supabase tool, e.g. `list_tables` → you see **project A's** tables.
3. `janus_use_account` with `{"account_id":"client_b"}`.
4. `list_tables` again → now **project B's** tables, **without reconnecting**. ✅

That switch — same endpoint, different identity, no re-login — is the whole point.

## HTTP mode (ChatGPT / Gemini / Cursor)

```bash
JANUS_TRANSPORT=http JANUS_HTTP_PORT=7332 JANUS_CONFIG=./config.json ./bin/janusmcp serve
# → add http://127.0.0.1:7332/mcp as a remote MCP server
```

With `bindingMode: session` two clients (two tabs) can sit on different projects at the
same time; a `scope:"global"` switch moves the default for everyone.

## Option B — browser login (hosted remote MCP, OAuth)

Instead of PATs, you can use Supabase's **hosted** MCP server, which authenticates with a
**browser login** (MCP-native OAuth + dynamic client registration). No tokens on disk.

```bash
cp config.supabase-oauth.example.json config.json
JANUS_CONFIG=./config.json ./bin/janusmcp serve
```

Each account is just a remote endpoint:

```json
{ "id": "supabase_a", "service": "supabase", "transport": "http",
  "url": "https://mcp.supabase.com/mcp", "auth": "oauth" }
```

On the **first use** of each account (the first tool call after `janus_use_account`), the
broker opens your browser to authorize. Log into **account A** for `supabase_a` and
**account B** for `supabase_b` — two browser logins, two identities, one broker. The broker
discovers the auth server, registers itself dynamically, and captures the token on a
localhost loopback; the token never appears in chat or config.

Tip: log into account A in your normal browser profile and account B in a separate profile
(or after logging out) so the two logins don't collide.

> Status: the OAuth client flow uses the SDK's experimental client-side OAuth. The access
> token is **persisted in the vault** (key `remote_oauth_<account_id>`), so a valid token
> survives restarts — you only re-login when it actually expires. The PAT path (Option A)
> is still the most battle-tested.

## Troubleshooting

- **`upstream connect error`** → check `node`/`npx` on PATH and the PAT is valid
  (`./bin/janusmcp vault set …` again to overwrite).
- **Empty / unauthorized results** → the PAT belongs to a different account than the
  project ref, or the project ref is wrong.
- **First call is slow** → expected: upstreams connect lazily on first activation; the
  switch warms a fresh `npx` process the first time.
- **Want write access** → remove `--read-only` from that account's `args` (use with care).

> Note: the built-in `supabase` OAuth preset (`janusmcp providers`) targets Supabase OAuth
> *apps* (management API), a different flow. For day-to-day multi-account testing, the PAT
> path above is the right one.
