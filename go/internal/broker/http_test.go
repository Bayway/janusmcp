package broker_test

// The modern half of the Streamable HTTP entrypoint: version routing, the
// stateless identity model, and the notification path that depends on the
// stateless branch sharing one mcp.Server.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectModern dials the handler with a stock SDK client, which negotiates
// 2026-07-28 and therefore lands on the stateless branch.
func connectModern(ctx context.Context, t *testing.T, url string, opts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "modern", Version: "0"}, opts)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestModernClientGetsStatelessTransport(t *testing.T) {
	ctx := context.Background()
	core, _ := mrtrCore(t)
	srv := httptest.NewServer(core.HTTPHandler())
	defer srv.Close()

	cs := connectModern(ctx, t, srv.URL, nil)

	// Negotiating 2026-07-28 at all proves the stateless handler served it: the
	// stateful handler rejects that version outright.
	if got := cs.InitializeResult().ProtocolVersion; got != "2026-07-28" {
		t.Fatalf("negotiated %q, want 2026-07-28", got)
	}

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if !hasTool(res.Tools, "janus_with_account") {
		t.Fatal("janus_with_account missing from the stateless tool list")
	}
	if res.CacheScope != "private" {
		t.Fatalf("cacheScope = %q, want private", res.CacheScope)
	}
}

// TestStatelessRefusesSessionScope pins the deliberate behaviour change: a
// session-scoped switch has no meaning without sessions, and silently promoting
// it to a global one would let a caller change every other client's identity.
func TestStatelessRefusesSessionScope(t *testing.T) {
	ctx := context.Background()
	core, _ := mrtrCore(t)
	srv := httptest.NewServer(core.HTTPHandler())
	defer srv.Close()

	cs := connectModern(ctx, t, srv.URL, nil)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "janus_use_account",
		Arguments: map[string]any{"account_id": "acct_b", "scope": "session"},
	})
	if err != nil {
		t.Fatalf("janus_use_account: %v", err)
	}
	var env struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &env); err != nil {
		t.Fatalf("decode %q: %v", textOf(t, res), err)
	}
	if env.OK {
		t.Fatal("session scope was accepted on the stateless transport")
	}
	if !strings.Contains(env.Hint, "janus_with_account") {
		t.Fatalf("hint does not point at the supported path: %q", env.Hint)
	}
}

func TestStatelessAllowsGlobalScope(t *testing.T) {
	ctx := context.Background()
	core, _ := mrtrCore(t)
	srv := httptest.NewServer(core.HTTPHandler())
	defer srv.Close()

	cs := connectModern(ctx, t, srv.URL, nil)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "janus_use_account",
		Arguments: map[string]any{"account_id": "acct_b", "scope": "global"},
	})
	if err != nil {
		t.Fatalf("janus_use_account: %v", err)
	}
	var env struct {
		OK    bool   `json:"ok"`
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &env); err != nil {
		t.Fatalf("decode %q: %v", textOf(t, res), err)
	}
	if !env.OK || env.Scope != "global" {
		t.Fatalf("global switch = %+v, want ok", env)
	}
	if got := core.State.GlobalActive(); got != "acct_b" {
		t.Fatalf("global active = %q, want acct_b", got)
	}
}

// TestStatelessToolListChangedReachesSubscriber covers the notification path on
// the stateless transport: under 2026-07-28 a client only receives
// tools/list_changed through a subscriptions/listen stream it opened itself, so
// a global switch made by one client has to reach another client's listener.
//
// Note what this does not prove: the delivery survives even if HTTPHandler
// builds a server per request, because a global switch fans out through the
// session registry rather than relying on a single shared instance. The shared
// instance is justified by cost and registry hygiene, not by this test.
func TestStatelessToolListChangedReachesSubscriber(t *testing.T) {
	ctx := context.Background()
	core, _ := mrtrCore(t)
	srv := httptest.NewServer(core.HTTPHandler())
	// Registered before the clients so it runs last: cleanups are LIFO, and
	// httptest.Server.Close blocks on outstanding requests — the listener's
	// subscriptions/listen stream is one, and only the client can end it.
	t.Cleanup(srv.Close)

	changed := make(chan struct{}, 8)
	listener := connectModern(ctx, t, srv.URL, &mcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
			select {
			case changed <- struct{}{}:
			default:
			}
		},
	})
	// Make sure the subscription is established before switching.
	if _, err := listener.ListTools(ctx, nil); err != nil {
		t.Fatalf("tools/list: %v", err)
	}

	switcher := connectModern(ctx, t, srv.URL, nil)
	if _, err := switcher.CallTool(ctx, &mcp.CallToolParams{
		Name:      "janus_use_account",
		Arguments: map[string]any{"account_id": "acct_b", "scope": "global"},
	}); err != nil {
		t.Fatalf("global switch: %v", err)
	}

	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("tools/list_changed never reached the subscriber")
	}
}

func TestProtocolRouterDispatch(t *testing.T) {
	core, _ := mrtrCore(t)
	handler := core.HTTPHandler()

	// The stateful branch is the only one that issues a session id, so its
	// presence identifies which handler served the request. Malformed versions
	// are routed to the stateful branch too, where the SDK rejects them with the
	// same 400 it has always returned.
	const (
		wantSession  = "session"   // stateful handler, session id issued
		wantStateles = "stateless" // stateless handler, no session id
		wantRejected = "rejected"  // routed to stateful, refused there
	)
	cases := []struct {
		version string
		want    string
	}{
		{version: "", want: wantSession},
		{version: "2024-11-05", want: wantSession},
		{version: "2025-06-18", want: wantSession},
		{version: "2025-11-25", want: wantSession},
		{version: "2026-07-28", want: wantStateles},
		// Junk must not sort above the cutover and slip into the stateless
		// branch; it lands on the stateful one and is refused there, exactly as
		// before. The full version table lives in TestIsStatelessVersion.
		{version: "abc", want: wantRejected},
		{version: "9999", want: wantRejected},
	}

	for _, tc := range cases {
		t.Run("version="+tc.version, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			// Send what a client of that version would actually send: the SDK
			// rejects a body whose declared version disagrees with the header,
			// so a single probe body cannot exercise both branches.
			body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}`
			if tc.want == wantStateles {
				body = `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{` +
					`"io.modelcontextprotocol/protocolVersion":"` + tc.version + `",` +
					`"io.modelcontextprotocol/clientCapabilities":{}}}}`
			}
			req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if tc.version != "" {
				req.Header.Set("Mcp-Protocol-Version", tc.version)
			}
			if tc.want == wantStateles {
				// 2026-07-28 requires the method to be mirrored in a header.
				req.Header.Set("Mcp-Method", "server/discover")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			got := wantStateles
			switch {
			case resp.StatusCode >= 400:
				got = wantRejected
			case resp.Header.Get("Mcp-Session-Id") != "":
				got = wantSession
			}
			if got != tc.want {
				t.Fatalf("version %q routed to %s, want %s (status %d)",
					tc.version, got, tc.want, resp.StatusCode)
			}
		})
	}
}

func TestHTTPProtocolLegacyEscapeHatch(t *testing.T) {
	t.Setenv("JANUS_HTTP_PROTOCOL", "legacy")
	ctx := context.Background()
	core, _ := mrtrCore(t)
	srv := httptest.NewServer(core.HTTPHandler())
	defer srv.Close()

	// With the stateless branch disabled, a modern client falls back to the
	// legacy handshake exactly as it did before this work.
	client := mcp.NewClient(&mcp.Implementation{Name: "modern", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer cs.Close()

	if got := cs.InitializeResult().ProtocolVersion; got == "2026-07-28" {
		t.Fatal("stateless transport served a request despite JANUS_HTTP_PROTOCOL=legacy")
	}
}

func hasTool(tools []*mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
