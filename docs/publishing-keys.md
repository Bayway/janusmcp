# Publishing: keys, tokens & claiming the name

What each channel needs, where to get the token, and how the name gets reserved.
Repo handle: **`bayway/janusmcp`** · npm/binary/command: **`janusmcp`**.

> **Fast path:** after you have an npm token and a GitHub PAT, run
> `bash scripts/setup-publishing.sh` — it creates the tap + bucket repos and sets the three
> secrets for you. The sections below explain how to get those two tokens.

| Channel | Secret needed | Name reserved by |
|---|---|---|
| GitHub Releases | `GITHUB_TOKEN` (automatic) | the repo itself |
| npm | `NPM_TOKEN` | first `npm publish` of `janusmcp` |
| Homebrew (tap) | `HOMEBREW_TAP_GITHUB_TOKEN` (PAT) | creating repo `bayway/homebrew-janusmcp` |
| Scoop (bucket) | `SCOOP_BUCKET_GITHUB_TOKEN` (PAT) | creating repo `bayway/scoop-janusmcp` |
| Docker (GHCR) | `GITHUB_TOKEN` (automatic) | `ghcr.io/bayway/janusmcp` on first push |
| MCP Registry | none (interactive GitHub login) | `io.github.bayway/janusmcp` (your GH user) |

---

## 1. npm — token + claim the name

1. Create/sign in at <https://www.npmjs.com>, enable 2FA.
2. Avatar → **Access Tokens** → **Generate New Token** → choose **Granular Access Token**
   with **Read and write** for Packages (or a classic **Automation** token — it skips the
   2FA OTP in CI, which is what we want for the release workflow).
3. Copy the token → it goes into the GitHub secret `NPM_TOKEN` (step 6).
4. The name `janusmcp` is **free** and becomes yours on the **first successful publish**.
   The release pipeline publishes it automatically on a tag (step 7); you don't publish by hand.
   - To grab the name *right now* (optional): `cd npm && npm login && npm publish --access public`.
     Note the package's `postinstall` downloads the binary from a GitHub release, so a manual
     publish before any release exists would fail for installers — prefer claiming at first release.

## 2. GitHub PAT for the Homebrew tap and Scoop bucket

Both use a GitHub Personal Access Token that can push to those two repos.

1. GitHub → Settings → **Developer settings** → **Personal access tokens** →
   **Fine-grained tokens** → Generate new token.
2. Repository access: **Only select repositories** → pick `homebrew-janusmcp` and
   `scoop-janusmcp` (create them first, step 3). Permissions → **Contents: Read and write**.
3. One token can cover both repos — reuse its value for both secrets below.
   (Classic token alternative: scope `repo`.)

## 3. Create the tap & bucket repos (this is how the names are "taken")

Create two **public** repos under your account:

- `bayway/homebrew-janusmcp`  → enables `brew install bayway/janusmcp/janusmcp`
- `bayway/scoop-janusmcp`     → enables `scoop bucket add janusmcp https://github.com/bayway/scoop-janusmcp`

They can be empty; GoReleaser pushes the formula/manifest into them on each release.

## 4. Docker / GHCR

Nothing to create or tokenize: the release workflow logs in with the automatic
`GITHUB_TOKEN` and pushes `ghcr.io/bayway/janusmcp`. After the first release, open
**Packages → janusmcp → Package settings** and set visibility to **Public** (and link it to the repo).

## 5. MCP Registry (`io.github.bayway/janusmcp`)

No stored secret — interactive login tied to your GitHub user.

```bash
brew install mcp-publisher          # or download the prebuilt binary
mcp-publisher login github          # browser OAuth
mcp-publisher publish               # validates server.json + npm ownership, publishes
```

Requires the npm package published first (the registry checks that npm `janusmcp` has
`"mcpName": "io.github.bayway/janusmcp"` — already set in `npm/package.json`).

## 6. Add the secrets to the repo

UI: repo → Settings → Secrets and variables → Actions → New repository secret. Or via `gh`:

```bash
gh secret set NPM_TOKEN                       # paste the npm token
gh secret set HOMEBREW_TAP_GITHUB_TOKEN       # paste the GitHub PAT
gh secret set SCOOP_BUCKET_GITHUB_TOKEN       # paste the same PAT (or a second one)
```

## 7. Ship the first release (claims npm + GHCR + formula + manifest)

```bash
git tag v0.1.0
git push origin v0.1.0
```

That runs GoReleaser (binaries, Homebrew formula, Scoop manifest, .deb/.rpm/.apk, GHCR image)
and the npm publish job. Afterwards run `mcp-publisher publish` (step 5) and, optionally,
register the domain **janusmcp.dev**.

> Tip: test the build without publishing first — `goreleaser release --snapshot --clean`.
