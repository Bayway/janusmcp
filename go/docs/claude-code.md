# Using JanusMCP in Claude Code (two Supabase accounts)

JanusMCP runs as a local stdio MCP server; Claude Code launches it and talks to it.

## 1. Add the broker

```bash
claude mcp add janusmcp \
  --scope user \
  --env JANUS_CONFIG=/ABS/PATH/JanusMCP/go/config.json \
  -- /ABS/PATH/JanusMCP/go/bin/janusmcp serve
```

- `--scope user` → available in every project. Use `project` to share it via a checked-in
  `.mcp.json`, or `local` (default) for just the current directory.
- `--env JANUS_CONFIG=...` → the config holding your two Supabase accounts (absolute path).
- everything after `--` is the command Claude Code runs (the broker binary + `serve`).

Equivalent JSON (`.mcp.json` in a project, or `claude mcp add-json`):

```json
{
  "mcpServers": {
    "janusmcp": {
      "command": "/ABS/PATH/JanusMCP/go/bin/janusmcp",
      "args": ["serve"],
      "env": { "JANUS_CONFIG": "/ABS/PATH/JanusMCP/go/config.json" }
    }
  }
}
```

Verify: `claude mcp list`, and inside a session run `/mcp`.

## 2. Use it — switch identity by talking to Claude

The broker exposes control tools (`janus_list_accounts`, `janus_use_account`, `janus_whoami`)
plus the **active** account's Supabase tools. In Claude Code:

- "Which Supabase accounts do I have?" → `janus_list_accounts`
- "Use the **supabase-logix** account" → `janus_use_account {account_id: "supabase-logix"}`
- "List the tables" → Supabase `list_tables`, now routed to that account
- "Switch back to **supabase**" → instant switch, **no re-login** (tokens live in the keychain)

Tools appear namespaced (e.g. `mcp__janusmcp__janus_use_account`) — just talk normally.

## Notes

- Each Claude Code session starts on `defaultAccount`; switch per session with
  `janus_use_account`. Tokens persist in the OS keychain, so no browser login is needed
  unless a token has expired.
- Check login status anytime from a terminal: `janusmcp status`.
- This is stdio (local-first), ideal for Claude Code/Desktop. For ChatGPT/Gemini use
  `JANUS_TRANSPORT=http` and add the `http://127.0.0.1:7332/mcp` URL instead.

Source: Claude Code MCP docs — https://docs.claude.com/en/docs/claude-code/mcp
