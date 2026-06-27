# Repository setup — security & contributor governance

Files in the repo cover part of this (CODEOWNERS, SECURITY.md, Dependabot, CodeQL, templates,
least-privilege CI). The rest are **GitHub settings** you must enable once. Goal: open to
contributors, but `main` and releases cannot be overwritten or published by anyone but you.

## 1. Protect `main` (branch ruleset)

Settings → Rules → Rulesets → New branch ruleset, target `refs/heads/main`, enforcement
**Active**, with:

- ✅ Require a pull request before merging — **1 approval**, **require review from Code Owners**,
  dismiss stale approvals on push, require conversation resolution
- ✅ Require status checks to pass — add **`build & test (Go)`** and **`CodeQL (go)`**
  (names appear after the first CI/CodeQL run), "strict" (branch up to date)
- ✅ Block force pushes (non-fast-forward) and ✅ Restrict deletions
- ✅ Require linear history
- (optional) Require signed commits

Apply it with `gh` instead of clicking (uses [`ruleset-main.json`](#rulesets-json)):

```bash
gh api -X POST repos/bayway/janusmcp/rulesets --input ruleset-main.json
```

## 2. Protect release tags `v*` (this controls who can publish)

A pushed `v*` tag triggers the release pipeline (npm/brew/GHCR). **Only you should create them.**
Settings → Rules → Rulesets → New **tag** ruleset, target `refs/tags/v*`, enforcement Active:

- ✅ Restrict creations, updates, deletions
- Bypass list: **Repository admin** only

## 3. GitHub Actions hardening

Settings → Actions → General:

- Workflow permissions → **Read repository contents** (default). Each workflow that needs more
  declares its own `permissions:` (release.yml asks for `contents`/`packages` write).
- ✅ Require approval for workflow runs from **all outside/first-time contributors**
  (prevents fork PRs from running anything you didn't review).
- Fork pull requests do **not** receive secrets by default — keep it that way; never echo secrets.

## 4. Security features

Settings → Code security:

- ✅ Dependabot alerts + security updates (config: `.github/dependabot.yml`)
- ✅ CodeQL / code scanning (workflow: `.github/workflows/codeql.yml`)
- ✅ Secret scanning + **Push protection**
- ✅ Private vulnerability reporting (powers the link in `SECURITY.md`)

## 5. Release secrets (least exposure)

Settings → Secrets and variables → Actions:

- `NPM_TOKEN`, `HOMEBREW_TAP_GITHUB_TOKEN`, `SCOOP_BUCKET_GITHUB_TOKEN`
- (optional but recommended) put them in an **Environment** named `release` and add
  *Required reviewers* = you, then reference `environment: release` in the release jobs, so a
  human approves every publish.

## 6. Account / org

- Enable **2FA** on your account.
- Keep `main` owner-only; everyone else contributes via forks + PRs.

---

## Rulesets JSON

`ruleset-main.json`:

```json
{
  "name": "protect main",
  "target": "branch",
  "enforcement": "active",
  "conditions": { "ref_name": { "include": ["refs/heads/main"], "exclude": [] } },
  "rules": [
    { "type": "deletion" },
    { "type": "non_fast_forward" },
    { "type": "required_linear_history" },
    { "type": "pull_request", "parameters": {
        "required_approving_review_count": 1,
        "require_code_owner_review": true,
        "dismiss_stale_reviews_on_push": true,
        "required_review_thread_resolution": true,
        "require_last_push_approval": false
    } },
    { "type": "required_status_checks", "parameters": {
        "strict_required_status_checks_policy": true,
        "required_status_checks": [
          { "context": "build & test (Go)" },
          { "context": "CodeQL (go)" }
        ]
    } }
  ]
}
```

> After the first CI run, confirm the exact check names in a PR's "Checks" tab and adjust the
> `context` values if needed.
