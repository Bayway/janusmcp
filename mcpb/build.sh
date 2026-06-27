#!/usr/bin/env bash
# Build a Claude Desktop one-click extension (.mcpb) for the current platform.
# Requires Go and Node (the mcpb packer runs via npx).
set -euo pipefail
cd "$(dirname "$0")"

mkdir -p server
echo "→ building broker binary…"
( cd ../go && go build -ldflags "-s -w" -o ../mcpb/server/janusmcp ./cmd/janusmcp )

echo "→ packing .mcpb…"
npx -y @anthropic-ai/mcpb pack

echo "done → janusmcp.mcpb  (double-click it to install in Claude Desktop)"
echo "note: this bundle contains the binary for THIS platform only."
echo "      build on macOS / Windows / Linux separately for cross-platform releases."
