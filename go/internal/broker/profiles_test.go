package broker_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bayway/janusmcp/internal/broker"
	"github.com/bayway/janusmcp/internal/config"
)

// callText invokes a tool on the broker session and returns the first text content.
// Requires `node` on PATH (the upstreams are the spike mock server).
func callText(ctx context.Context, t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func twoMockAccounts() []config.Account {
	cdir, _ := filepath.Abs(".")
	mock := filepath.Join(cdir, "..", "..", "..", "spike", "mock-upstream", "server.mjs")
	return []config.Account{
		{ID: "azienda_a", Service: "mock", Command: "node", Args: []string{mock}, Env: map[string]string{"MOCK_ACCOUNT": "azienda_a"}},
		{ID: "azienda_b", Service: "mock", Command: "node", Args: []string{mock}, Env: map[string]string{"MOCK_ACCOUNT": "azienda_b"}},
	}
}

// A profile exposes the tools of ALL its accounts at once; colliding names are
// namespaced, and each call is routed to the right upstream.
func TestProfileExposesAndRoutes(t *testing.T) {
	ctx := context.Background()
	cdir, _ := filepath.Abs(".")
	cfg := &config.Config{
		DefaultAccount: "azienda_a",
		BindingMode:    config.BindingSession,
		Accounts:       twoMockAccounts(),
		Profiles:       map[string][]string{"client_x": {"azienda_a", "azienda_b"}},
	}
	core := &broker.Core{
		Cfg:      cfg,
		Manager:  broker.NewUpstreamManager(cfg, cdir, nil, nil),
		State:    broker.NewBrokerState(filepath.Join(t.TempDir(), "state.json"), "azienda_a", config.BindingSession),
		Registry: broker.NewSessionRegistry(),
	}
	defer core.Manager.CloseAll()

	cs := connectSession(ctx, t, core, "p1")
	defer cs.Close()

	var r struct {
		OK     bool   `json:"ok"`
		Active string `json:"active"`
	}
	_ = json.Unmarshal([]byte(callText(ctx, t, cs, "janus_use_profile", map[string]any{"profile": "client_x"})), &r)
	if !r.OK || r.Active != "client_x" {
		t.Fatalf("use_profile failed: %+v", r)
	}

	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range lt.Tools {
		names[tl.Name] = true
	}
	// First account keeps the original name; the colliding second is namespaced.
	if !names["ping"] || !names["azienda_b_ping"] {
		t.Fatalf("expected both ping and azienda_b_ping; got %v", names)
	}

	if got := callText(ctx, t, cs, "ping", nil); got != "pong from azienda_a" {
		t.Fatalf("ping should route to azienda_a, got %q", got)
	}
	if got := callText(ctx, t, cs, "azienda_b_ping", nil); got != "pong from azienda_b" {
		t.Fatalf("namespaced ping should route to azienda_b, got %q", got)
	}
}

// janus_with_account runs a one-shot call on another account without changing the
// active one, and lists that account's tools when no tool is given.
func TestWithAccountOneShot(t *testing.T) {
	ctx := context.Background()
	cdir, _ := filepath.Abs(".")
	cfg := &config.Config{
		DefaultAccount: "azienda_a",
		BindingMode:    config.BindingSession,
		Accounts:       twoMockAccounts(),
	}
	core := &broker.Core{
		Cfg:      cfg,
		Manager:  broker.NewUpstreamManager(cfg, cdir, nil, nil),
		State:    broker.NewBrokerState(filepath.Join(t.TempDir(), "state.json"), "azienda_a", config.BindingSession),
		Registry: broker.NewSessionRegistry(),
	}
	defer core.Manager.CloseAll()

	cs := connectSession(ctx, t, core, "w1")
	defer cs.Close()

	if got := callText(ctx, t, cs, "ping", nil); got != "pong from azienda_a" {
		t.Fatalf("active should be azienda_a, got %q", got)
	}
	// One-shot call on B.
	if got := callText(ctx, t, cs, "janus_with_account", map[string]any{"account_id": "azienda_b", "tool": "ping"}); got != "pong from azienda_b" {
		t.Fatalf("with_account ping on B, got %q", got)
	}
	// Active must be unchanged.
	if got := callText(ctx, t, cs, "ping", nil); got != "pong from azienda_a" {
		t.Fatalf("active changed after with_account, got %q", got)
	}
	// No tool → list the account's tools.
	listed := callText(ctx, t, cs, "janus_with_account", map[string]any{"account_id": "azienda_b"})
	if !strings.Contains(listed, "ping") || !strings.Contains(listed, "db_query") {
		t.Fatalf("with_account listing should include ping and db_query, got %s", listed)
	}
	full := callText(ctx, t, cs, "janus_with_account", map[string]any{"account_id": "azienda_b", "full_schema": true})
	if !strings.Contains(full, "inputSchema") || !strings.Contains(full, "instance_id") {
		t.Fatalf("full schema listing should contain complete definitions, got %s", full)
	}
}
