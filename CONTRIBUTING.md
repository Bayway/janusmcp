# Contributing to JanusMCP

Thanks for your interest — JanusMCP gets better with real-world accounts, clients and
connectors, so contributions of all sizes are welcome.

## Ways to help

- **Connector presets** — add an OAuth provider preset (`go/internal/oauth/providers.go`)
  or a documented config example for an MCP server you use.
- **Client integration guides** — short docs for wiring JanusMCP into a specific LLM client.
- **Packaging** — Homebrew formula, scoop manifest, release automation.
- **Bugs & features** — open an issue with steps to reproduce or a clear use case.

## Development

The broker lives in [`go/`](go/). You need **Go 1.23+** and **Node 22+** (the integration
tests spawn a Node mock upstream from [`spike/`](spike/)).

```bash
cd go
make build          # build ./bin/janusmcp
make vet            # go vet
cd ../spike && npm install && cd ../go
make test           # runs all Go tests (broker tests need node on PATH)
```

The TypeScript spike in [`spike/`](spike/) is a verified reference of the intended
behavior; when changing broker semantics, keep the two in sync or update the reference.

## Pull requests

- Keep PRs focused; describe the motivation and the user-facing effect.
- Add or update tests for behavior changes (`go test ./...` must pass).
- Run `go vet ./...` and `gofmt`.
- By contributing you agree your work is licensed under the project's [MIT license](LICENSE).

## Code of conduct

Be kind and constructive. Assume good faith. We're here to make a useful tool together.
