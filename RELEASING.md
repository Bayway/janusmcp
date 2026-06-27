# Releasing JanusMCP

The release pipeline is **dormant until you push a tag**. One tag publishes to every
channel in parallel via GitHub Actions ([`.github/workflows/release.yml`](.github/workflows/release.yml)
+ [`.goreleaser.yaml`](.goreleaser.yaml)).

```bash
git tag v0.1.0
git push origin v0.1.0
```

That single push produces, automatically:

| Channel | Output | Powered by |
|---|---|---|
| GitHub Releases | binaries (macOS/Linux/Windows · amd64/arm64) + checksums | GoReleaser |
| Homebrew | formula pushed to `homebrew-janusmcp` tap | GoReleaser `brews` |
| Scoop (Windows) | manifest pushed to `scoop-janusmcp` bucket | GoReleaser `scoops` |
| Linux packages | `.deb` / `.rpm` / `.apk` attached to the release | GoReleaser `nfpms` |
| Docker | `ghcr.io/<owner>/janusmcp:<ver>` + `:latest` (amd64+arm64) | GoReleaser `dockers` |
| npm | `npx janusmcp` (downloads the matching binary) | `npm` job in the workflow |

## One-time setup (before the first tag)

1. **Rename references** if your GitHub repo is not `bayway/janusmcp`:
   search `bayway/janusmcp` in `.goreleaser.yaml`, `npm/install.js`, README.
2. **Create two empty repos** for the package indexes:
   - `homebrew-janusmcp` (Homebrew tap)
   - `scoop-janusmcp` (Scoop bucket)
3. **Add repository secrets** (Settings → Secrets and variables → Actions):
   - `HOMEBREW_TAP_GITHUB_TOKEN` — a PAT with `repo` scope that can push to the tap.
   - `SCOOP_BUCKET_GITHUB_TOKEN` — a PAT with `repo` scope that can push to the bucket.
   - `NPM_TOKEN` — an npm automation token with publish rights.
   - `GITHUB_TOKEN` is provided automatically (used for the release + GHCR push).
4. **npm package name**: `janusmcp` may be taken. Check `npm view janusmcp`. If taken,
   switch to a scope: set `"name": "@bayway/janusmcp"` in `npm/package.json`
   (publish stays `--access public`).
5. **Enable GHCR**: the workflow logs in with `GITHUB_TOKEN` (needs `packages: write`,
   already granted in the workflow permissions). The first image makes the package public
   from the repo's Packages settings.

## After it works → installers users will love

```bash
brew install bayway/janusmcp/janusmcp     # via your tap
scoop bucket add janusmcp https://github.com/bayway/scoop-janusmcp && scoop install janusmcp
npx janusmcp serve
docker run --rm -p 7332:7332 ghcr.io/bayway/janusmcp:latest
```

## Publish to the official MCP Registry

The cross-client discovery standard ([registry.modelcontextprotocol.io](https://registry.modelcontextprotocol.io)).
`server.json` (repo root) is ready; the npm package already declares the matching
`mcpName: io.github.bayway/janusmcp`.

```bash
brew install mcp-publisher          # or download the prebuilt binary
mcp-publisher login github          # OAuth for the io.github.* namespace
mcp-publisher publish               # validates server.json + npm ownership, then publishes
```

Requirements already in place: the npm package `janusmcp` carries `mcpName`, and `server.json`
uses the `io.github.bayway/janusmcp` name. Publish the npm package first (the
release workflow does this), then run `mcp-publisher publish`. Bump the `version` in
`server.json` for each release (can be wired into CI later).

> Tip: you can also list the Claude Desktop `.mcpb` as an `mcpb` package in `server.json`
> (with its `file_sha256`) once you attach a built `.mcpb` to a GitHub release.

## One-click install buttons

The README has **Add to Cursor** / **Install in VS Code** badges. They launch the client with
`npx -y janusmcp serve`, so they start working once the npm package is published. Regenerate
the encoded configs if you change the command (see the badge URLs in `README.md`).

## Manual / follow-up channels (not automated here)

These need external repos or per-distro PRs; add once you have traction:

- **Homebrew core** (`brew install janusmcp` with no tap) — submit once the repo is
  "notable" (historically ~30+ stars and stable).
- **winget** — PR a manifest to `microsoft/winget-pkgs` (can be automated later).
- **AUR** (Arch) — publish a `PKGBUILD`.
- **Nixpkgs** — submit a package derivation.
- **MCP registries** — list JanusMCP on the official MCP registry, Smithery, Glama,
  mcp.so, PulseMCP and `awesome-mcp` lists. This is the highest-leverage *discovery*
  channel for this ecosystem.

## Test the pipeline without publishing

```bash
cd .. && goreleaser release --snapshot --clean   # builds everything locally, publishes nothing
```
