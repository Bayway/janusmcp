package broker

// Streamable HTTP dispatch.
//
// MCP 2026-07-28 replaced the session-based core with a stateless one, and the
// Go SDK will only serve that version from a transport configured as stateless.
// The previous handler is stateful, so a modern client silently negotiated down
// to 2025-11-25 and a client that requires 2026-07-28 got an HTTP 400.
//
// Rather than switching everyone to stateless — which would take session-scoped
// identity away from clients that rely on it today — both handlers are mounted
// and each request goes to the one that matches its protocol version.

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// statelessProtocolVersion is the first MCP revision with a stateless core.
const statelessProtocolVersion = "2026-07-28"

// protocolVersionHeader is required on every 2026-07-28 request, including the
// first server/discover, which is what makes header-only dispatch reliable: the
// SDK rejects a body that declares the version without the matching header.
const protocolVersionHeader = "Mcp-Protocol-Version"

// HTTPHandler returns the Streamable HTTP entrypoint for the broker.
//
// Set JANUS_HTTP_PROTOCOL=legacy to serve only the stateful handler, which
// restores the pre-2026-07-28 behaviour for every client.
func (c *Core) HTTPHandler() http.Handler {
	legacy := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return NewSession(r.Context(), c, randomID()).server
	}, nil)

	if strings.EqualFold(os.Getenv("JANUS_HTTP_PROTOCOL"), "legacy") {
		return legacy
	}

	// One server for every stateless request, not one per request.
	//
	// Statelessness is a property of the protocol, not a reason to rebuild the
	// broker on each call: NewSession registers the control tools and the whole
	// active tool set, so per-request construction would repeat that work on
	// every request and add a registry entry each time. A single instance also
	// gives subscriptions/listen a stable home — the SDK keys a subscription to
	// the mcp.Server that served it — and keeps the exposed tool set consistent
	// for every stateless caller.
	shared := NewSession(context.Background(), c, "http-stateless")
	shared.persistent = true
	shared.stateless = true

	modern := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return shared.server },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	return &protocolRouter{legacy: legacy, modern: modern}
}

// protocolRouter sends each request to the handler that can serve its protocol
// version.
type protocolRouter struct {
	legacy http.Handler
	modern http.Handler
}

func (p *protocolRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isStatelessVersion(r.Header.Get(protocolVersionHeader)) {
		p.modern.ServeHTTP(w, r)
		return
	}
	p.legacy.ServeHTTP(w, r)
}

// isStatelessVersion reports whether a protocol version string is a date at or
// after the stateless cutover.
//
// The date shape is checked before comparing, because MCP versions are compared
// lexically: without it, junk like "9999" or "abc" sorts above "2026-07-28" and
// would be routed to the stateless handler. Sending malformed values to the
// legacy handler keeps the SDK's existing 400, which names the versions it
// supports.
func isStatelessVersion(v string) bool {
	return isDateVersion(v) && v >= statelessProtocolVersion
}

func isDateVersion(v string) bool {
	if len(v) != len("2006-01-02") {
		return false
	}
	for i, ch := range v {
		switch i {
		case 4, 7:
			if ch != '-' {
				return false
			}
		default:
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
}
