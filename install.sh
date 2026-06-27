#!/usr/bin/env bash
# JanusMCP installer: builds the broker and installs it to a bin directory.
# Usage:  ./install.sh            # installs to /usr/local/bin (or ~/.local/bin if not writable)
#         PREFIX=~/bin ./install.sh
set -euo pipefail

cd "$(dirname "$0")/go"

if ! command -v go >/dev/null 2>&1; then
  echo "error: Go toolchain not found. Install Go 1.23+ from https://go.dev/dl/" >&2
  exit 1
fi

echo "→ building janusmcp…"
make build

# Choose an install prefix.
PREFIX="${PREFIX:-/usr/local/bin}"
if [ ! -w "$PREFIX" ]; then
  PREFIX="$HOME/.local/bin"
  mkdir -p "$PREFIX"
fi

install -m 0755 bin/janusmcp "$PREFIX/janusmcp"
echo "→ installed: $PREFIX/janusmcp"

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *) echo "note: add $PREFIX to your PATH" ;;
esac

echo "done. Try:  janusmcp version"
