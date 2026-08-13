package broker_test

// Multi round-trip requests (SEP-2322) through the broker.
//
// The Node mock upstream speaks an SDK version that cannot emit inputRequests,
// so these tests stand up an in-process Go upstream instead and reach it the
// way a real deployment would: a stateless Streamable HTTP server behind an
// account with transport "http". Nothing is stubbed inside the broker.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bayway/janusmcp/internal/broker"
	"github.com/bayway/janusmcp/internal/config"
)

// upstreamState is the opaque token the fake upstream expects back on a retry.
const upstreamState = "upstream-state-1"

// fakeUpstream is an MCP server that asks for input before answering.
type fakeUpstream struct {
	account string
	calls   int // total tool invocations, to prove a rejected retry never lands
}

// tools registers the fake's behaviour on a server.
//
// elicit_me answers the first call with an input request and only produces
// content once the client echoes the responses and the request state back.
// shed always answers with a non-nil but empty input request map, which is how
// the spec expresses load shedding.
func (f *fakeUpstream) register(srv *mcp.Server) {
	schema := &jsonschema.Schema{Type: "object"}

	srv.AddTool(&mcp.Tool{Name: "elicit_me", Description: "Asks for input, then answers.", InputSchema: schema},
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			f.calls++
			if len(req.Params.InputResponses) == 0 {
				return &mcp.CallToolResult{
					InputRequests: mcp.InputRequestMap{
						"who": &mcp.ElicitParams{
							Message:         "Who is asking?",
							RequestedSchema: &jsonschema.Schema{Type: "object"},
						},
					},
					RequestState: upstreamState,
				}, nil
			}
			if req.Params.RequestState != upstreamState {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: "bad request state: " + req.Params.RequestState}},
				}, nil
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "answered by " + f.account}},
			}, nil
		})

	srv.AddTool(&mcp.Tool{Name: "shed", Description: "Load sheds.", InputSchema: schema},
		func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			f.calls++
			return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{}}, nil
		})
}

// startFakeUpstream serves the fake over stateless Streamable HTTP, which is
// what lets it negotiate 2026-07-28 with the broker's client.
func startFakeUpstream(t *testing.T, account string) (*fakeUpstream, string) {
	t.Helper()
	f := &fakeUpstream{account: account}
	srv := mcp.NewServer(&mcp.Implementation{Name: "fake-" + account, Version: "1"}, nil)
	f.register(srv)

	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return f, ts.URL
}

// mrtrCore wires two accounts backed by independent fake upstreams.
func mrtrCore(t *testing.T) (*broker.Core, map[string]*fakeUpstream) {
	t.Helper()
	fakes := map[string]*fakeUpstream{}
	accounts := make([]config.Account, 0, 2)
	for _, id := range []string{"acct_a", "acct_b"} {
		f, url := startFakeUpstream(t, id)
		fakes[id] = f
		accounts = append(accounts, config.Account{ID: id, Service: "fake", Transport: "http", URL: url})
	}
	cfg := &config.Config{DefaultAccount: "acct_a", BindingMode: config.BindingSession, Accounts: accounts}
	core := &broker.Core{
		Cfg:      cfg,
		Manager:  broker.NewUpstreamManager(cfg, t.TempDir(), nil, nil),
		State:    broker.NewBrokerState(filepath.Join(t.TempDir(), "state.json"), "acct_a", config.BindingSession),
		Registry: broker.NewSessionRegistry(),
	}
	t.Cleanup(core.Manager.CloseAll)
	return core, fakes
}

// connectWithElicitation attaches a client that can answer input requests,
// which is what makes the SDK drive the retry loop automatically.
func connectWithElicitation(ctx context.Context, t *testing.T, core *broker.Core, id string) *mcp.ClientSession {
	t.Helper()
	s := broker.NewSession(ctx, core, id)
	ct, st := mcp.NewInMemoryTransports()
	if _, err := s.Server().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-" + id, Version: "0"}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"who": "tester"}}, nil
		},
	})
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		return ""
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		return ""
	}
	return tc.Text
}

// TestMRTRRoundTripThroughProxy is the end-to-end proof: before the relay, the
// broker answered the upstream's input request itself and the call failed.
func TestMRTRRoundTripThroughProxy(t *testing.T) {
	ctx := context.Background()
	core, fakes := mrtrCore(t)
	cs := connectWithElicitation(ctx, t, core, "s1")

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "elicit_me", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("elicit_me: %v", err)
	}
	if got, want := textOf(t, res), "answered by acct_a"; got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
	if fakes["acct_a"].calls != 2 {
		t.Fatalf("upstream calls = %d, want 2 (initial + retry)", fakes["acct_a"].calls)
	}
	if fakes["acct_b"].calls != 0 {
		t.Fatalf("the other account was called %d times", fakes["acct_b"].calls)
	}
}

// TestMRTRRoundTripThroughWithAccount covers the one-shot path, which becomes
// the primary call route for stateless clients.
func TestMRTRRoundTripThroughWithAccount(t *testing.T) {
	ctx := context.Background()
	core, fakes := mrtrCore(t)
	cs := connectWithElicitation(ctx, t, core, "s1")

	args, err := json.Marshal(map[string]any{"account_id": "acct_b", "tool": "elicit_me", "arguments": map[string]any{}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "janus_with_account", Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatalf("janus_with_account: %v", err)
	}
	if got, want := textOf(t, res), "answered by acct_b"; got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
	if fakes["acct_b"].calls != 2 {
		t.Fatalf("upstream calls = %d, want 2", fakes["acct_b"].calls)
	}
}

// TestMRTRRequestStateIsWrapped checks the broker never leaks the upstream's
// own state to the client. The client here has no elicitation handler, so the
// input-required result is returned instead of being retried.
func TestMRTRRequestStateIsWrapped(t *testing.T) {
	ctx := context.Background()
	core, _ := mrtrCore(t)

	s := broker.NewSession(ctx, core, "s1")
	ct, st := mcp.NewInMemoryTransports()
	if _, err := s.Server().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "raw", Version: "0"}, &mcp.ClientOptions{
		MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true},
	})
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "elicit_me", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("elicit_me: %v", err)
	}
	if !res.NeedsInput() {
		t.Fatal("expected an input-required result")
	}
	if res.RequestState == upstreamState {
		t.Fatal("upstream request state leaked to the client verbatim")
	}
	if !strings.HasPrefix(res.RequestState, "janus1:") {
		t.Fatalf("request state = %q, want a janus1: envelope", res.RequestState)
	}
}

// TestMRTRRejectsForeignRequestState is the security case: a retry carrying an
// envelope minted for another account must not reach any upstream.
func TestMRTRRejectsForeignRequestState(t *testing.T) {
	ctx := context.Background()
	core, fakes := mrtrCore(t)

	s := broker.NewSession(ctx, core, "s1")
	ct, st := mcp.NewInMemoryTransports()
	if _, err := s.Server().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "raw", Version: "0"}, &mcp.ClientOptions{
		MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true},
	})
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// Mint a real envelope against acct_b via the one-shot path.
	args, err := json.Marshal(map[string]any{"account_id": "acct_b", "tool": "elicit_me", "arguments": map[string]any{}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	first, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "janus_with_account", Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatalf("seed call: %v", err)
	}
	if !first.NeedsInput() {
		t.Fatal("expected an input-required result to harvest state from")
	}
	stolen := first.RequestState

	before := fakes["acct_a"].calls
	// Replay it against acct_a's copy of the same tool name.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:           "elicit_me", // routes to the active account, acct_a
		Arguments:      map[string]any{},
		InputResponses: mcp.InputResponseMap{"who": &mcp.ElicitResult{Action: "accept"}},
		RequestState:   stolen,
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !res.IsError {
		t.Fatalf("cross-account replay was accepted: %+v", res)
	}
	if !strings.Contains(textOf(t, res), "different tool") {
		t.Fatalf("unexpected error text: %q", textOf(t, res))
	}
	if fakes["acct_a"].calls != before {
		t.Fatalf("upstream was called despite the rejected envelope (%d → %d)", before, fakes["acct_a"].calls)
	}
}

// TestMRTRLoadSheddingPreserved pins the nil-versus-empty distinction: an empty
// but non-nil InputRequests means "retry later", and collapsing it to nil would
// turn a load-shed into a bogus success.
func TestMRTRLoadSheddingPreserved(t *testing.T) {
	ctx := context.Background()
	core, _ := mrtrCore(t)

	s := broker.NewSession(ctx, core, "s1")
	ct, st := mcp.NewInMemoryTransports()
	if _, err := s.Server().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "raw", Version: "0"}, &mcp.ClientOptions{
		MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true},
	})
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "shed", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("shed: %v", err)
	}
	if !res.NeedsInput() {
		t.Fatal("load-shedding result lost its input-required marker")
	}
	if len(res.InputRequests) != 0 {
		t.Fatalf("InputRequests = %v, want empty", res.InputRequests)
	}
}
