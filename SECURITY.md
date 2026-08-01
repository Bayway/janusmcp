# Security Policy

JanusMCP brokers access to other services and stores credentials, so we take security seriously.

## Reporting a vulnerability

**Do not open a public issue for security problems.**

Use **GitHub → Security → "Report a vulnerability"** (private vulnerability reporting) on this
repository. We aim to acknowledge within 72 hours and to ship a fix or mitigation as fast as
the severity warrants. Please include reproduction steps and affected versions.

## Scope & threat model

- JanusMCP runs **locally** and stores secrets in the **OS keychain** (or an encrypted file
  fallback). Tokens are never written to config files and never pass through the model's chat.
- The HTTP transport binds to **loopback (127.0.0.1)** by default. Do not expose it to a
  public interface without an authenticating proxy.
- The managed daemon always binds to **127.0.0.1**. MCP, health, and shutdown requests require
  a random bearer token stored in daemon metadata with owner-only permissions. Treat that
  metadata file as a credential: never copy it into issues, logs, shell transcripts, or chat.
- OAuth uses Authorization Code + PKCE with a localhost loopback redirect and dynamic client
  registration; tokens are persisted in the vault.

## Good practice for users

- Prefer the OS keychain backend over the encrypted-file fallback when possible.
- Scope upstream credentials minimally (e.g. read-only, per-project) where the service allows.
- Restart the daemon after changing configuration or upgrading JanusMCP; restart rotates its
  bearer token and reloads the binary and configuration.
- Keep the binary up to date; releases are signed/checksummed via the release pipeline.

## Supported versions

Until 1.0, only the latest release receives security fixes.
