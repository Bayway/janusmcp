#!/usr/bin/env bash
#
# One-shot setup for JanusMCP publishing:
#   1. creates the Homebrew tap + Scoop bucket repos
#   2. stores the 3 release secrets on the main repo
#
# Requirements: GitHub CLI (`gh`) authenticated (`gh auth login`).
# You will need:
#   - an npm token (Automation/Granular with publish rights)   → https://www.npmjs.com (Access Tokens)
#   - a GitHub PAT (fine-grained, Contents: read/write on the tap+bucket repos)
#
# Usage:  bash scripts/setup-publishing.sh
set -euo pipefail

OWNER="${OWNER:-bayway}"
REPO="${REPO:-$OWNER/janusmcp}"
TAP="$OWNER/homebrew-janusmcp"
BUCKET="$OWNER/scoop-janusmcp"

command -v gh >/dev/null || { echo "error: GitHub CLI 'gh' not found → https://cli.github.com"; exit 1; }
gh auth status >/dev/null 2>&1 || { echo "error: run 'gh auth login' first"; exit 1; }

create_repo() {
  local full="$1" desc="$2"
  if gh repo view "$full" >/dev/null 2>&1; then
    echo "✓ repo already exists: $full"
  else
    gh repo create "$full" --public --description "$desc" --disable-wiki
    echo "✓ created: $full"
  fi
}

echo "== 1. Package index repos =="
create_repo "$TAP"    "Homebrew tap for JanusMCP"
create_repo "$BUCKET" "Scoop bucket for JanusMCP"

echo
echo "== 2. Release secrets on $REPO =="
echo "(input is hidden; nothing is written to disk or shell history)"

read -rsp "npm token (NPM_TOKEN): " NPM_TOK; echo
printf '%s' "$NPM_TOK" | gh secret set NPM_TOKEN --repo "$REPO"
echo "✓ NPM_TOKEN set"

read -rsp "GitHub PAT for tap+bucket (reused for both secrets): " PAT; echo
printf '%s' "$PAT" | gh secret set HOMEBREW_TAP_GITHUB_TOKEN --repo "$REPO"
printf '%s' "$PAT" | gh secret set SCOOP_BUCKET_GITHUB_TOKEN --repo "$REPO"
echo "✓ HOMEBREW_TAP_GITHUB_TOKEN + SCOOP_BUCKET_GITHUB_TOKEN set"

echo
echo "Done. Next:"
echo "  • dry-run:  cd .. && goreleaser release --snapshot --clean"
echo "  • release:  git tag v0.1.0 && git push origin v0.1.0"
echo "  • registry: mcp-publisher login github && mcp-publisher publish"
