package broker_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bayway/janusmcp/internal/broker"
	"github.com/bayway/janusmcp/internal/config"
)

// Two sessions sharing one Core, as the HTTP transport would create per Mcp-Session-Id.
// Verifies session-scoped isolation and global propagation of the active account.
// Requires `node` on PATH.
func TestSessionIsolationAndGlobalSwitch(t *testing.T) {
	ctx := context.Background()
	cdir, _ := filepath.Abs(".")
	mock := filepath.Join(cdir, "..", "..", "..", "spike", "mock-upstream", "server.mjs")

	cfg := &config.Config{
		DefaultAccount: "azienda_a",
		BindingMode:    config.BindingSession,
		Accounts: []config.Account{
			{ID: "azienda_a", Service: "mock", Command: "node", Args: []string{mock}, Env: map[string]string{"MOCK_ACCOUNT": "azienda_a"}},
			{ID: "azienda_b", Service: "mock", Command: "node", Args: []string{mock}, Env: map[string]string{"MOCK_ACCOUNT": "azienda_b"}},
		},
	}
	core := &broker.Core{
		Cfg:      cfg,
		Manager:  broker.NewUpstreamManager(cfg, cdir, nil, nil),
		State:    broker.NewBrokerState(filepath.Join(t.TempDir(), "state.json"), "azienda_a", config.BindingSession),
		Registry: broker.NewSessionRegistry(),
	}
	defer core.Manager.CloseAll()

	c1 := connectSession(ctx, t, core, "s1")
	defer c1.Close()
	c2 := connectSession(ctx, t, core, "s2")
	defer c2.Close()

	ping := func(cs *mcp.ClientSession) string {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "ping", Arguments: map[string]any{}})
		if err != nil {
			t.Fatalf("ping: %v", err)
		}
		if tc, ok := res.Content[0].(*mcp.TextContent); ok {
			return tc.Text
		}
		return ""
	}
	use := func(cs *mcp.ClientSession, account, scope string) {
		args := map[string]any{"account_id": account}
		if scope != "" {
			args["scope"] = scope
		}
		if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "janus_use_account", Arguments: args}); err != nil {
			t.Fatalf("use_account: %v", err)
		}
	}

	// Both start on the global default (A).
	if got := ping(c1); got != "pong from azienda_a" {
		t.Fatalf("c1 default: %q", got)
	}
	if got := ping(c2); got != "pong from azienda_a" {
		t.Fatalf("c2 default: %q", got)
	}

	// Session-scoped switch in c1 must not affect c2.
	use(c1, "azienda_b", "") // bindingMode=session → session scope
	if got := ping(c1); got != "pong from azienda_b" {
		t.Fatalf("c1 after session switch: %q", got)
	}
	if got := ping(c2); got != "pong from azienda_a" {
		t.Fatalf("c2 isolation broken: %q", got)
	}

	// Global switch from c1 flips c2 (which has no local override).
	use(c1, "azienda_b", "global")
	if got := ping(c2); got != "pong from azienda_b" {
		t.Fatalf("c2 after global switch: %q", got)
	}
}

func connectSession(ctx context.Context, t *testing.T, core *broker.Core, id string) *mcp.ClientSession {
	t.Helper()
	s := broker.NewSession(ctx, core, id)
	ct, st := mcp.NewInMemoryTransports()
	if _, err := s.Server().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-" + id, Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	return cs
}
