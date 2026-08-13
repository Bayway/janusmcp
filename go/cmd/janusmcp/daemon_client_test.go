package main

// The daemon-routed CLI talks to the broker over MCP, so the arguments it builds
// have to survive JSON encoding. They did not: a []byte payload marshals to a
// base64 string, so the broker saw no arguments at all and answered
// "account_id is required" — while the shell smoke test still passed, because it
// compared two empty strings for equality.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bayway/janusmcp/internal/broker"
	"github.com/bayway/janusmcp/internal/config"
)

func TestDaemonCallSendsDecodableArguments(t *testing.T) {
	ctx := context.Background()

	mock, err := filepath.Abs(filepath.Join("..", "..", "..", "spike", "mock-upstream", "server.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DefaultAccount: "azienda_a",
		BindingMode:    config.BindingGlobal,
		Accounts: []config.Account{
			{ID: "azienda_a", Service: "mock", Command: "node", Args: []string{mock},
				Env: map[string]string{"MOCK_ACCOUNT": "azienda_a"}},
		},
	}
	core := &broker.Core{
		Cfg:      cfg,
		Manager:  broker.NewUpstreamManager(cfg, t.TempDir(), nil, nil),
		State:    broker.NewBrokerState(filepath.Join(t.TempDir(), "state.json"), "azienda_a", config.BindingGlobal),
		Registry: broker.NewSessionRegistry(),
	}
	defer core.Manager.CloseAll()

	srv := httptest.NewServer(core.HTTPHandler())
	defer srv.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "janusmcp-cli", Version: "test"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer cs.Close()

	c := &daemonCLICore{cfg: cfg, state: core.State, cs: cs}

	// A tool with arguments: the encoding bug only shows up once the broker has
	// to read them back.
	payload, err := json.Marshal(map[string]any{"sql": "select 1"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Call(ctx, "azienda_a", "db_query", payload)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported an error: %s", firstContentText(res.Content))
	}
	got := firstContentText(res.Content)
	if got == "" {
		t.Fatal("empty result: the broker most likely received no arguments")
	}
	// The mock echoes the SQL back, so this proves the arguments arrived intact.
	if want := "select 1"; !strings.Contains(got, want) {
		t.Fatalf("result %q does not echo %q", got, want)
	}

	// And a tool without arguments, which is what the CI smoke test exercises.
	res, err = c.Call(ctx, "azienda_a", "instance_id", nil)
	if err != nil {
		t.Fatalf("call instance_id: %v", err)
	}
	if firstContentText(res.Content) == "" {
		t.Fatal("instance_id returned nothing")
	}
}
