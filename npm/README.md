# janusmcp

Multi-account tool broker for AI agents — CLI and MCP. Add credentials once, switch
identity without reconnecting, and invoke tools on demand without loading every
definition upfront.

```bash
npx @bayway/janusmcp serve            # run it as an MCP server
npm i -g @bayway/janusmcp             # or install globally
janusmcp version
```

Agents (or you) can also skip the MCP session entirely and call tools from the shell:

```bash
janusmcp tools                                   # compact tool list, active account
janusmcp schema list_tables                      # one tool's full schema, on demand
janusmcp call list_tables --args '{"schemas":["public"]}'
janusmcp call ping --account client_b            # cross-account without switching
janusmcp call ping --json --timeout 30s           # stable agent-readable output
janusmcp daemon start                             # reuse upstream sessions
```

On install this package downloads the prebuilt native binary matching your platform
from the [GitHub release](https://github.com/bayway/janusmcp/releases).

Full docs: https://github.com/bayway/janusmcp
