# Testing JanusMCP

## Prerequisites

- Go 1.25 or newer.
- Node.js 22 or newer.
- Mock dependencies installed with `cd spike && npm install --no-audit --no-fund`.

## Required local checks

Run from the repository root:

```bash
python3 scripts/check-docs.py
node --check spike/mock-upstream/server.mjs
goreleaser check

cd go
go fmt ./...
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
go build ./cmd/janusmcp
```

Run `gofmt` only when you intend to update formatting. A PR should not include unrelated
formatting changes.

## CLI contract checks

Use the mock config in `spike/config.json` and a temporary file vault. Verify:

- `tools` raw and JSON discovery;
- `schema` output;
- `call` raw, stdin arguments, and the structured JSON envelope;
- invalid input and stable exit codes;
- opt-in timeout and `JANUS_CLI_TIMEOUT` precedence;
- account/profile selection, collisions, and `bindingMode=locked`.

## MCP regression checks

Broker tests must continue to exercise:

- stdio and Streamable HTTP sessions;
- both HTTP protocol branches: the raw-HTTP legacy pin (`TestLegacyHTTPSessionContract`) and the
  stateless path (`http_test.go`), plus the version routing table (`TestIsStatelessVersion`);
- control tools and tool-list refresh;
- global/session/locked selection, and the refusal of session scope on the stateless transport;
- profiles and collision namespacing;
- multi round-trip relay, including the rejection of a `requestState` minted for another account;
- remote HTTP/SSE upstreams and OAuth credential resolution;
- structured content, multiple content items, and MCP tool errors.

The multi round-trip and stateless tests use an in-process Go upstream built on the MCP SDK, so
they need no Node. The Node mock (`spike/mock-upstream/server.mjs`) predates `inputRequests` and
cannot express them; do not extend it for that purpose. Tests that use the Node mock still require
`node` on PATH and `npm install` in `spike/`.

## Daemon lifecycle checks

On Linux, macOS, and Windows verify start, repeated start, status, repeated stop, restart, stale
metadata, crash recovery, occupied port, invalid token, and configuration/version mismatch.
Two direct `instance_id` calls must report different upstream instances; two daemon calls must
report the same instance. `--direct`, `--daemon`, and every `JANUS_DAEMON` mode must be covered.

## Release checks

Follow [RELEASING.md](../RELEASING.md). Test produced artifacts, not only the workspace binary:
GitHub archive binary, npm/npx wrapper, and Homebrew/Scoop packages where runners are available.
After deployment, verify the site title, canonical URLs, JSON-LD, `/cli/`, sitemap, package
descriptions, and GitHub release assets.
