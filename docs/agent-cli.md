# JanusMCP CLI instructions for agents

JanusMCP is a local multi-account tool broker. Agents can discover and invoke its
upstream MCP tools from the shell without loading every tool definition upfront.
Credentials remain in the JanusMCP vault and must never be printed or copied into
prompts, scripts, or project files.

## Copy into agent instructions

```text
Use JanusMCP from the shell when a task needs an external service.

1. Run `janusmcp status` to understand the configured accounts and profiles.
2. Run `janusmcp tools [account-or-profile]` to discover relevant tool names.
3. Before the first invocation, run `janusmcp schema <tool> --account <selector>`.
4. Call the tool with `janusmcp call <tool> --account <selector> --json --timeout 2m`.
5. Supply arguments with `--args '<json-object>'` or pipe a JSON object on stdin.
6. Never invent tool names or arguments; return to `tools` and `schema` when uncertain.
7. Treat every non-zero exit code as a failure and inspect the JSON error or stderr.
8. Do not expose vault values, OAuth tokens, or config secrets.
```

## Account safety

- Prefer `--account <id|profile>` for an explicit one-shot operation.
- Use `janusmcp use <account|profile>` only when subsequent commands should share a
  new persisted default.
- A tool name shared by several accounts is intentionally rejected until an explicit
  account is provided.
- `bindingMode=locked` disables persisted switching; explicit direct calls remain
  available.

## Machine-readable calls

`janusmcp call <tool> --json` emits one JSON object containing `ok`, `account`,
`tool`, `isError`, `content`, and optional `structuredContent`. Operational errors
are emitted as JSON on stderr. The default raw output remains suitable for pipes and
existing scripts.

`--timeout` accepts Go duration values such as `30s` or `2m`. It is opt-in; set
`JANUS_CLI_TIMEOUT` to apply a default for an agent environment.
