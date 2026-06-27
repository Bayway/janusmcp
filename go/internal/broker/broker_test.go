package broker_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bayway/janusmcp/internal/broker"
	"github.com/bayway/janusmcp/internal/config"
)

// Requires `node` on PATH (spawns ../../../spike/mock-upstream/server.mjs).
// Run from the module root: go test ./internal/broker/ -run TestActiveAccount -v
func TestActiveAccountSwitch(t *testing.T) {
	ctx := context.Background()

	cdir, _ := filepath.Abs(".")
	cfg := &config.Config{
		DefaultAccount: "azienda_a",
		BindingMode:    config.BindingSession,
		Accounts: []config.Account{
			{ID: "azienda_a", Service: "mock", Command: "node",
				Args: []string{filepath.Join(cdir, "..", "..", "..", "spike", "mock-upstream", "server.mjs")},
				Env:  map[string]string{"MOCK_ACCOUNT": "azienda_a"}},
			{ID: "azienda_b", Service: "mock", Command: "node",
				Args: []string{filepath.Join(cdir, "..", "..", "..", "spike", "mock-upstream", "server.mjs")},
				Env:  map[string]string{"MOCK_ACCOUNT": "azienda_b"}},
		},
	}

	core := &broker.Core{
		Cfg:      cfg,
		Manager:  broker.NewUpstreamManager(cfg, cdir, nil, nil),
		State:    broker.NewBrokerState(filepath.Join(t.TempDir(), "state.json"), "azienda_a", config.BindingSession),
		Registry: broker.NewSessionRegistry(),
	}
	defer core.Manager.CloseAll()

	sess := broker.NewSession(ctx, core, "test")

	ct, st := mcp.NewInMemoryTransports()
	if _, err := sess.Server().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	call := func(name string, args map[string]any) string {
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

	if got := call("ping", nil); got != "pong from azienda_a" {
		t.Fatalf("ping A: got %q", got)
	}

	var out struct {
		OK     bool   `json:"ok"`
		Active string `json:"active"`
	}
	_ = json.Unmarshal([]byte(call("janus_use_account", map[string]any{"account_id": "azienda_b"})), &out)
	if !out.OK || out.Active != "azienda_b" {
		t.Fatalf("switch failed: %+v", out)
	}

	if got := call("ping", nil); got != "pong from azienda_b" {
		t.Fatalf("ping B after switch: got %q", got)
	}
}
