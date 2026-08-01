<!-- Thanks for contributing to JanusMCP. Keep the PR focused and delete comments before submitting. -->

## Summary and motivation

<!-- What changes, why is it needed, and what is the user/developer impact? -->

## Change type

- [ ] Bug fix
- [ ] Feature
- [ ] Refactor or maintenance
- [ ] Documentation or positioning
- [ ] Security-sensitive change
- [ ] Release or packaging change

Related issue: <!-- Closes #123, if applicable -->
ADR: <!-- Link docs/adr/NNNN-*.md, or explain why no ADR is needed. -->

## Compatibility and release impact

- [ ] Existing MCP clients and transports remain compatible
- [ ] Existing CLI raw output remains compatible
- [ ] Config and persisted state remain compatible, or migration is documented
- [ ] This change does not require a release note
- [ ] This change requires a release note and `CHANGELOG.md` is updated

<!-- Describe any intentional compatibility break, migration, deprecation, or package impact. -->

## Security and privacy

- [ ] No secrets, bearer tokens, credentials, personal config, or runtime metadata are committed or logged
- [ ] New listeners remain loopback-only or their authentication and exposure are documented
- [ ] Credential/vault/OAuth changes preserve local storage and least-privilege assumptions

<!-- Describe changed trust boundaries, authentication, persistence, or network behavior. Write "None" if not applicable. -->

## Validation

- [ ] `cd go && go fmt ./... && go vet ./... && go test ./... -count=1`
- [ ] `cd go && go test -race ./... -count=1` when concurrency, sessions, or state code changed
- [ ] Raw CLI and `--json` behavior tested when CLI code changed
- [ ] MCP stdio/HTTP behavior tested when broker behavior changed
- [ ] Linux, macOS, and Windows implications considered
- [ ] Documentation checks pass: `python3 scripts/check-docs.py`

Commands/results:

```text

```

## Reviewer notes

<!-- Highlight risky areas, trade-offs, follow-up work, screenshots, or manual verification. -->
