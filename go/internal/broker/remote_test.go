package broker

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bayway/janusmcp/internal/config"
)

func TestWellKnownURLs(t *testing.T) {
	cases := []struct{ issuer, wantOAuth, wantOIDC string }{
		// Bare host: well-known at the root.
		{"https://mcp.linear.app", "https://mcp.linear.app/.well-known/oauth-authorization-server", "https://mcp.linear.app/.well-known/openid-configuration"},
		// Trailing slash (Netlify): must not corrupt the URL, issuer kept verbatim.
		{"https://netlify-mcp.netlify.app/", "https://netlify-mcp.netlify.app/.well-known/oauth-authorization-server", "https://netlify-mcp.netlify.app/.well-known/openid-configuration"},
		// Path issuer (RFC 8414): well-known inserted after the host, path preserved.
		{"https://auth.example.com/tenant", "https://auth.example.com/.well-known/oauth-authorization-server/tenant", "https://auth.example.com/tenant/.well-known/openid-configuration"},
	}
	for _, c := range cases {
		got := wellKnownURLs(c.issuer)
		if got[0] != c.wantOAuth {
			t.Errorf("%s: oauth well-known = %q, want %q", c.issuer, got[0], c.wantOAuth)
		}
		if got[1] != c.wantOIDC {
			t.Errorf("%s: oidc well-known = %q, want %q", c.issuer, got[1], c.wantOIDC)
		}
	}
}

// transportFor must pick the SDK's SSE transport for a transport:"sse" account,
// and the Streamable HTTP transport for transport:"http".
func TestTransportForSelectsRemoteKind(t *testing.T) {
	cfg := &config.Config{Accounts: []config.Account{
		{ID: "sse_a", Service: "s", Transport: "sse", URL: "https://example.com/sse"},
		{ID: "http_a", Service: "s", Transport: "http", URL: "https://example.com/mcp"},
	}}
	m := NewUpstreamManager(cfg, ".", nil, nil)

	tr, err := m.transportFor(context.Background(), &cfg.Accounts[0])
	if err != nil {
		t.Fatalf("sse transportFor: %v", err)
	}
	if _, ok := tr.(*mcp.SSEClientTransport); !ok {
		t.Fatalf("expected *mcp.SSEClientTransport, got %T", tr)
	}

	tr, err = m.transportFor(context.Background(), &cfg.Accounts[1])
	if err != nil {
		t.Fatalf("http transportFor: %v", err)
	}
	if _, ok := tr.(*mcp.StreamableClientTransport); !ok {
		t.Fatalf("expected *mcp.StreamableClientTransport, got %T", tr)
	}
}

// A pre-registered client (e.g. Google Workspace, which has no dynamic client
// registration) must be seeded into fresh state so Authorize skips DCR.
func TestSeedPreregisteredClient(t *testing.T) {
	h := &remoteOAuthHandler{
		clientID:     "cid",
		clientSecret: "secret",
		scopes:       []string{"openid", "email"},
	}
	s := h.seed(nil)
	if s == nil {
		t.Fatal("seed(nil) returned nil")
	}
	if s.ClientID != "cid" || s.ClientSecret != "secret" {
		t.Fatalf("client not seeded: %+v", s)
	}
	if len(s.Scopes) != 2 || s.Scopes[0] != "openid" {
		t.Fatalf("scopes not seeded: %+v", s.Scopes)
	}
}

// seed must not clobber a client/scopes obtained from a previous login (DCR).
func TestSeedDoesNotOverrideExisting(t *testing.T) {
	h := &remoteOAuthHandler{clientID: "cid", clientSecret: "secret", scopes: []string{"openid"}}
	existing := &remoteAuthState{ClientID: "registered", ClientSecret: "reg-secret", Scopes: []string{"repo"}}
	s := h.seed(existing)
	if s.ClientID != "registered" || s.ClientSecret != "reg-secret" {
		t.Fatalf("seed overrode an existing client: %+v", s)
	}
	if len(s.Scopes) != 1 || s.Scopes[0] != "repo" {
		t.Fatalf("seed overrode existing scopes: %+v", s.Scopes)
	}
}

// With no pre-registered client, seed leaves state empty so the DCR path runs.
func TestSeedNoPreregisteredClient(t *testing.T) {
	h := &remoteOAuthHandler{}
	s := h.seed(nil)
	if s.ClientID != "" || len(s.Scopes) != 0 {
		t.Fatalf("expected empty state for DCR path, got %+v", s)
	}
}
