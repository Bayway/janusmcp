# JanusMCP — Claude Desktop one-click extension (.mcpb)

A [`.mcpb` bundle](https://github.com/modelcontextprotocol/mcpb) packages the broker so users
install it in **Claude Desktop with a double-click** — no JSON, no terminal.

## Build it

```bash
cd mcpb
./build.sh          # needs Go + Node; produces janusmcp.mcpb
```

Then double-click `janusmcp.mcpb` (or Claude Desktop → Settings → Extensions → install file).
After install, open the control panel to add accounts and log in:

```bash
janusmcp ui
```

(If you also want the `janusmcp` CLI on your PATH, install it separately via `go install ./cmd/janusmcp`, Homebrew, or `npx @bayway/janusmcp`.)

## Notes

- The bundle embeds the binary for the **platform it was built on**. For a public release,
  build one `.mcpb` per OS (macOS/Windows/Linux) — ideally wired into the release pipeline.
- Leave the "Config file" option empty at install: the broker then uses a default per-user
  config location that `janusmcp ui` manages for you.
- Submitting to the official Claude extensions directory (Anthropic-reviewed) is a great
  discovery channel once the project is stable.

See the manifest spec: https://github.com/modelcontextprotocol/mcpb
