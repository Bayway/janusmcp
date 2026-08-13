package broker_test

// Pins the legacy (pre-2026-07-28) Streamable HTTP contract of Core.HTTPHandler().
//
// This test speaks raw JSON-RPC over net/http on purpose: the SDK client always
// negotiates the latest protocol version and ClientSessionOptions.protocolVersion
// is unexported, so a legacy session cannot be requested through mcp.Client.
//
// It encodes the behaviour that must survive the stateless transport work:
// an initialize handshake at 2025-11-25 gets an Mcp-Session-Id, and the
// session-scoped janus_use_account + tool call pair share that session while
// staying invisible to a second connection.
//
// Requires `node` on PATH.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bayway/janusmcp/internal/broker"
	"github.com/bayway/janusmcp/internal/config"
)

const legacyProtocolVersion = "2025-11-25"

// rpcClient is a minimal JSON-RPC-over-HTTP client that keeps the session id the
// server hands out, mimicking a legacy MCP client.
type rpcClient struct {
	t         *testing.T
	endpoint  string
	sessionID string
	nextID    int
}

// call sends a request and returns its "result" object. Notifications (no id)
// are sent with notify instead.
func (c *rpcClient) call(method string, params any) map[string]any {
	c.t.Helper()
	c.nextID++
	body := map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method}
	if params != nil {
		body["params"] = params
	}
	raw := c.post(body)
	var env struct {
		Result map[string]any  `json:"result"`
		Error  *map[string]any `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		c.t.Fatalf("%s: decode response %q: %v", method, raw, err)
	}
	if env.Error != nil {
		c.t.Fatalf("%s: rpc error: %v", method, *env.Error)
	}
	return env.Result
}

func (c *rpcClient) notify(method string, params any) {
	c.t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		body["params"] = params
	}
	c.post(body)
}

// post performs the HTTP round trip and returns the single JSON-RPC message in
// the response, unwrapping the SSE framing the server uses by default.
func (c *rpcClient) post(body map[string]any) []byte {
	c.t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		c.t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.endpoint, bytes.NewReader(buf))
	if err != nil {
		c.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	// Deliberately no Mcp-Protocol-Version header: that is what a legacy client
	// sends on initialize, and it is what must keep reaching the stateful handler.
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("post %v: %v", body["method"], err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode >= 300 {
		c.t.Fatalf("post %v: status %d: %s", body["method"], resp.StatusCode, payload)
	}
	if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
		c.sessionID = id
	}
	// Notifications get 202 with no body.
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return sseData(c.t, payload)
	}
	return payload
}

// sseData extracts the first "data:" payload from an SSE response.
func sseData(t *testing.T, payload []byte) []byte {
	t.Helper()
	for _, line := range strings.Split(string(payload), "\n") {
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			return []byte(strings.TrimSpace(after))
		}
	}
	t.Fatalf("no data frame in SSE response: %q", payload)
	return nil
}

// initialize performs the legacy handshake and returns the negotiated version.
func (c *rpcClient) initialize() string {
	c.t.Helper()
	res := c.call("initialize", map[string]any{
		"protocolVersion": legacyProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "legacy-test", "version": "0"},
	})
	c.notify("notifications/initialized", map[string]any{})
	version, _ := res["protocolVersion"].(string)
	return version
}

// callTool invokes a tool and returns its first text content block.
func (c *rpcClient) callTool(name string, args map[string]any) string {
	c.t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	res := c.call("tools/call", map[string]any{"name": name, "arguments": args})
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		c.t.Fatalf("tools/call %s: no content in %v", name, res)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

func legacyTestCore(t *testing.T) *broker.Core {
	t.Helper()
	cdir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	mock := filepath.Join(cdir, "..", "..", "..", "spike", "mock-upstream", "server.mjs")

	cfg := &config.Config{
		DefaultAccount: "azienda_a",
		BindingMode:    config.BindingSession,
		Accounts: []config.Account{
			{ID: "azienda_a", Service: "mock", Command: "node", Args: []string{mock}, Env: map[string]string{"MOCK_ACCOUNT": "azienda_a"}},
			{ID: "azienda_b", Service: "mock", Command: "node", Args: []string{mock}, Env: map[string]string{"MOCK_ACCOUNT": "azienda_b"}},
		},
	}
	return &broker.Core{
		Cfg:      cfg,
		Manager:  broker.NewUpstreamManager(cfg, cdir, nil, nil),
		State:    broker.NewBrokerState(filepath.Join(t.TempDir(), "state.json"), "azienda_a", config.BindingSession),
		Registry: broker.NewSessionRegistry(),
	}
}

// TestLegacyHTTPSessionContract is the guard rail for the stateless migration:
// it must pass unchanged before and after the transport work.
func TestLegacyHTTPSessionContract(t *testing.T) {
	core := legacyTestCore(t)
	defer core.Manager.CloseAll()

	srv := httptest.NewServer(core.HTTPHandler())
	defer srv.Close()

	c1 := &rpcClient{t: t, endpoint: srv.URL}
	if got := c1.initialize(); got != legacyProtocolVersion {
		t.Fatalf("negotiated protocol version = %q, want %q", got, legacyProtocolVersion)
	}
	// A session id proves the request was served by the stateful handler.
	if c1.sessionID == "" {
		t.Fatal("legacy initialize returned no Mcp-Session-Id")
	}

	if got := c1.callTool("ping", nil); got != "pong from azienda_a" {
		t.Fatalf("c1 default account: %q", got)
	}

	// bindingMode=session, so an omitted scope means session scope.
	switched := c1.callTool("janus_use_account", map[string]any{"account_id": "azienda_b"})
	var sw struct {
		OK    bool   `json:"ok"`
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal([]byte(switched), &sw); err != nil {
		t.Fatalf("decode janus_use_account result %q: %v", switched, err)
	}
	if !sw.OK || sw.Scope != "session" {
		t.Fatalf("janus_use_account = %+v, want ok with session scope", sw)
	}

	// The switch and this call are separate HTTP requests: they must share a session.
	if got := c1.callTool("ping", nil); got != "pong from azienda_b" {
		t.Fatalf("c1 after session switch: %q", got)
	}

	// A second connection must not see c1's session-scoped choice.
	c2 := &rpcClient{t: t, endpoint: srv.URL}
	c2.initialize()
	if c2.sessionID == c1.sessionID {
		t.Fatal("second connection reused the first session id")
	}
	if got := c2.callTool("ping", nil); got != "pong from azienda_a" {
		t.Fatalf("c2 isolation broken: %q", got)
	}
}

// TestLegacyHTTPExplicitVersionHeader covers the same contract when the client
// does send Mcp-Protocol-Version, but at a version below the stateless cutover.
func TestLegacyHTTPExplicitVersionHeader(t *testing.T) {
	core := legacyTestCore(t)
	defer core.Manager.CloseAll()

	srv := httptest.NewServer(withHeader(core.HTTPHandler(), "Mcp-Protocol-Version", legacyProtocolVersion))
	defer srv.Close()

	c := &rpcClient{t: t, endpoint: srv.URL}
	if got := c.initialize(); got != legacyProtocolVersion {
		t.Fatalf("negotiated protocol version = %q, want %q", got, legacyProtocolVersion)
	}
	if c.sessionID == "" {
		t.Fatal("legacy initialize returned no Mcp-Session-Id")
	}
	if got := c.callTool("ping", nil); got != "pong from azienda_a" {
		t.Fatalf("ping: %q", got)
	}
}

// withHeader stamps a header on every request before it reaches next, so the
// test can exercise header-sensitive routing without touching rpcClient.
func withHeader(next http.Handler, key, value string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set(key, value)
		next.ServeHTTP(w, r)
	})
}
