package main

import (
	"context"
	"flag"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bayway/janusmcp/internal/broker"
	"github.com/bayway/janusmcp/internal/config"
)

// Flags must be accepted before and after positionals: stdlib flag stops at the
// first positional, so `call ping --account b` would silently drop --account.
func TestParseInterleaved(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		wantPos []string
		wantAcc string
	}{
		{[]string{"ping", "--account", "b"}, []string{"ping"}, "b"},
		{[]string{"--account", "b", "ping"}, []string{"ping"}, "b"},
		{[]string{"ping"}, []string{"ping"}, ""},
		{[]string{"a", "--account", "b", "c"}, []string{"a", "c"}, "b"},
	} {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		acc := fs.String("account", "", "")
		pos, err := parseInterleaved(fs, tc.args)
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if strings.Join(pos, ",") != strings.Join(tc.wantPos, ",") || *acc != tc.wantAcc {
			t.Fatalf("%v: pos=%v account=%q; want pos=%v account=%q", tc.args, pos, *acc, tc.wantPos, tc.wantAcc)
		}
	}
}

func TestFirstSentence(t *testing.T) {
	if got := firstSentence("First line.\nSecond line."); got != "First line." {
		t.Fatalf("newline cut: %q", got)
	}
	if got := firstSentence("Does a thing. More detail here."); got != "Does a thing." {
		t.Fatalf("sentence cut: %q", got)
	}
	if got := firstSentence(strings.Repeat("x", 200)); len(got) != 120 || !strings.HasSuffix(got, "...") {
		t.Fatalf("cap: len=%d %q", len(got), got[len(got)-5:])
	}
}

// findTool resolves a tool through the selector's accounts, requires --account on
// a profile-wide name collision, and errors on unknown tools.
// Requires `node` on PATH (upstreams are the spike mock server).
func TestFindTool(t *testing.T) {
	cdir, _ := filepath.Abs(".")
	mock := filepath.Join(cdir, "..", "..", "..", "spike", "mock-upstream", "server.mjs")
	cfg := &config.Config{
		DefaultAccount: "azienda_a",
		Accounts: []config.Account{
			{ID: "azienda_a", Service: "mock", Command: "node", Args: []string{mock}, Env: map[string]string{"MOCK_ACCOUNT": "azienda_a"}},
			{ID: "azienda_b", Service: "mock", Command: "node", Args: []string{mock}, Env: map[string]string{"MOCK_ACCOUNT": "azienda_b"}},
		},
		Profiles: map[string][]string{"client_x": {"azienda_a", "azienda_b"}},
	}
	c := &cliCore{
		cfg:     cfg,
		manager: broker.NewUpstreamManager(cfg, cdir, nil, nil),
		state:   broker.NewBrokerState(filepath.Join(t.TempDir(), "state.json"), "azienda_a", config.BindingSession),
	}
	defer c.manager.CloseAll()
	ctx := context.Background()

	// Empty selector falls back to the persisted active account.
	id, tool, err := findTool(ctx, c, "", "ping")
	if err != nil || id != "azienda_a" || tool.Name != "ping" {
		t.Fatalf("active lookup: id=%q tool=%v err=%v", id, tool, err)
	}
	if id, _, err := findTool(ctx, c, "azienda_b", "ping"); err != nil || id != "azienda_b" {
		t.Fatalf("explicit account: id=%q err=%v", id, err)
	}
	if _, _, err := findTool(ctx, c, "client_x", "ping"); err == nil || !strings.Contains(err.Error(), "several accounts") {
		t.Fatalf("profile collision should be ambiguous, got %v", err)
	}
	if _, _, err := findTool(ctx, c, "", "nope"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown tool: %v", err)
	}
	if _, _, err := findTool(ctx, c, "ghost", "ping"); err == nil || !strings.Contains(err.Error(), "unknown account or profile") {
		t.Fatalf("unknown selector: %v", err)
	}
}
