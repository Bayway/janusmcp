# ADR-0005: Serve MCP 2026-07-28 alongside the session-based transport

- Status: Accepted
- Date: 2026-08-13
- Owners: JanusMCP maintainers
- Supersedes: None
- Superseded by: None

## Context

MCP revision `2026-07-28` replaced the session-based core with a stateless one. There is no
`initialize`/`initialized` handshake and no `Mcp-Session-Id`; every request carries its protocol
version, capabilities and client identity in `_meta`.

JanusMCP already depends on the Go SDK version that defaults to this revision, which puts the two
transports in different places:

- **stdio** — the default transport, used by Claude Desktop and Claude Code — already negotiates
  `2026-07-28`. Nothing had to change for that to happen, and nothing announced it.
- **Streamable HTTP** could not serve it at all. The SDK only accepts `2026-07-28` on a transport
  configured as stateless (`StreamableServerTransport.SupportsProtocolVersion`), and the broker's
  handler is stateful.

The HTTP situation is not a hard failure today. `server/discover` is exempt from the version
rejection, so a modern SDK client discovers that the broker does not offer `2026-07-28`, falls
back to the legacy `initialize` at `2025-11-25`, and gets an ordinary stateful session with
session-scoped identity. That is exactly what the daemon CLI does. A non-SDK client that requires
`2026-07-28` is the one genuinely broken: it gets an HTTP 400.

The hard part is not the protocol version. It is that `bindingMode: session` — one of JanusMCP's
three identity models — is anchored to `Mcp-Session-Id`, which no longer exists.

## Decision

Mount both handlers behind `/mcp` and route each request by its `Mcp-Protocol-Version` header:
at or above `2026-07-28` goes to a stateless handler, everything else to the existing stateful
handler, unchanged.

Header-only dispatch is sufficient because the SDK sets that header on every modern request,
including the first `server/discover`, and rejects a body that declares a version without the
matching header. The header value is validated as a date before comparing, because MCP versions
are compared as strings and junk like `"9999"` sorts above `"2026-07-28"`.

On the stateless branch:

- One `mcp.Server` serves every request, rather than one per request.
- Identity is **per call**: `janus_with_account` carries the account and the call together.
- `janus_use_account` / `janus_use_profile` with `scope: "session"` are refused with an error
  envelope that names `janus_with_account`. `scope: "global"` keeps working.
- `tools/list` reflects the global selector and is identical for every stateless caller.
- `janus_whoami` and `janus_list_accounts` report `transport: "stateless" | "session"`.

`JANUS_HTTP_PROTOCOL=legacy` disables the stateless branch entirely.

## Alternatives considered

### Switch everything to stateless

The SDK's stateless transport also serves every legacy version, so a single handler would have
covered all clients and halved the surface to maintain. Rejected because legacy HTTP clients would
lose session continuity: `bindingMode: session` would silently degrade to global or per-call, with
no error to notice. Retiring a documented identity model by accident is worse than maintaining two
paths.

### Keep HTTP legacy-only and adopt the rest of the revision

Tempting, since the live defects (multi round-trip relay, cache scope, the registry leak) are
transport-independent and were fixed separately. Rejected because it leaves non-SDK modern clients
on an HTTP 400 with no path forward, and defers a decision that only gets more expensive as the
deprecation window closes.

### Key the stateless server by token subject or by an account header

`getServer` receives the `*http.Request`, so identity could be keyed on `TokenInfo.Subject`, a
`?account=` parameter, or an `X-Janus-Account` header, restoring per-client tool lists with
long-lived per-account servers. Rejected for now: the token option degenerates to a single server
because the daemon has exactly one token, and the header option is a non-standard contract no
client speaks. Worth revisiting if per-client tool lists on HTTP become a requirement.

## Consequences

### Positive

- Modern clients get the current revision on both transports; older clients are untouched.
- The stateless identity model is the per-call one JanusMCP already implemented, so the primitive
  did not have to be invented.
- The shared stateless server avoids rebuilding the control tools and the full active tool set on
  every request.

### Negative

- Two HTTP paths to maintain and test.
- On the stateless branch the SDK refuses server-to-client requests, so legacy elicitation and
  sampling do not work there. Multi round-trip requests do, because the client drives the retry.
- `applyActiveTools` had to become incremental. On a shared server, removing the whole tool set
  before re-adding it would briefly empty the table for every concurrent caller and turn in-flight
  calls into `unknown tool` errors.

## Compatibility and migration

The behaviour change worth stating plainly: **an SDK-based HTTP client that used to fall back to
`2025-11-25` and get session-scoped identity now negotiates `2026-07-28` and gets per-call
identity.** This affects any such client, not only the daemon CLI. Four migration routes:

1. `janus_with_account` for a one-shot call on a specific account — the intended path.
2. `scope: "global"` when the default should change for every client.
3. stdio, where sessions are real connections and session scope still works.
4. `JANUS_HTTP_PROTOCOL=legacy` to restore the previous behaviour wholesale.

The legacy branch is the unmodified handler expression, and `TestLegacyHTTPSessionContract` pins
its observable contract in raw HTTP — initialize at `2025-11-25`, an `Mcp-Session-Id`, a
session-scoped `janus_use_account` shared with the following call, and isolation between
connections. It passes unchanged before and after this work.

The daemon CLI previously switched the session's account and then called the tool: two requests
that only work in one session. It now issues a single `janus_with_account` call, which returns the
upstream result verbatim, so `call --json` envelopes and exit codes are unchanged.

Configuration, persisted state, and package layout are untouched. `JANUS_HTTP_PROTOCOL` is new and
optional; unset means `auto`.

## Security and privacy

- The stateless branch shares one server across callers, so a session-scoped switch is refused
  rather than applied: honouring it would leak one caller's identity into every other caller's
  tool list.
- `requestState` handed to clients is signed and bound to the account and tool it was minted for
  (ADR context: the route table is rebuilt on every account switch, so an unbound retry could
  deliver a user's input responses to a different tenant). The key lives for the process only;
  a restart invalidates in-flight retries, which surfaces as a readable tool error.
- List results are `cacheScope: private`, since what the broker exposes depends on the caller.
- No new listener, no new persisted secret. The daemon's bearer authentication wraps the router,
  so it fronts both branches.

## Validation

- `TestLegacyHTTPSessionContract`, `TestLegacyHTTPExplicitVersionHeader` — raw-HTTP pin of the
  legacy contract, written to pass against the pre-change handler.
- `TestIsStatelessVersion` — the routing predicate, including non-date values that sort above the
  cutover.
- `TestProtocolRouterDispatch` — end-to-end routing, identified by whether a session id is issued.
- `TestModernClientGetsStatelessTransport`, `TestStatelessRefusesSessionScope`,
  `TestStatelessAllowsGlobalScope`, `TestHTTPProtocolLegacyEscapeHatch`.
- `TestStatelessToolListChangedReachesSubscriber` — notification delivery over
  `subscriptions/listen`. It does not discriminate between a shared and a per-request server: a
  global switch fans out through the session registry either way.
- `go test ./... -race` on the CI matrix, plus the existing daemon smoke tests asserting that two
  `--direct` calls report different upstream instances and two `--daemon` calls report the same.
