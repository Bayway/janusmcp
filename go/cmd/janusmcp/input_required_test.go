package main

// A tool answering with MCP 2026-07-28's input_required is asking the caller to
// collect input and retry. The CLI is non-interactive, so it must say so with
// its own exit code instead of printing an empty result and exiting 0.
//
// The Node mock upstream cannot express this (its SDK predates inputRequests),
// so this test stands up a small Go upstream over stateless Streamable HTTP.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startElicitingUpstream serves one tool that always asks for input.
func startElicitingUpstream(t *testing.T) string {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "eliciting", Version: "1"}, nil)
	srv.AddTool(
		&mcp.Tool{Name: "needs_input", Description: "Always asks for input.", InputSchema: &jsonschema.Schema{Type: "object"}},
		func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				InputRequests: mcp.InputRequestMap{
					"who": &mcp.ElicitParams{Message: "Who is asking?", RequestedSchema: &jsonschema.Schema{Type: "object"}},
				},
				RequestState: "state-1",
			}, nil
		})

	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	))
	t.Cleanup(ts.Close)
	return ts.URL
}

func elicitingCLIConfig(t *testing.T, url string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := map[string]any{
		"defaultAccount": "remote_a",
		"bindingMode":    "global",
		"accounts": []map[string]any{{
			"id": "remote_a", "service": "eliciting", "transport": "http", "url": url,
		}},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCallReportsInputRequired(t *testing.T) {
	t.Setenv("JANUS_CONFIG", elicitingCLIConfig(t, startElicitingUpstream(t)))
	t.Setenv("JANUS_DAEMON", "off")

	_, err := captureStdout(t, func() error { return runCall([]string{"needs_input"}) })
	if err == nil {
		t.Fatal("expected an error for an input-required result")
	}
	var ce *commandError
	if !errors.As(err, &ce) {
		t.Fatalf("error is not a commandError: %v", err)
	}
	if ce.code != exitInputRequired {
		t.Fatalf("exit code = %d, want %d", ce.code, exitInputRequired)
	}
	if ce.kind != "input_required" {
		t.Fatalf("kind = %q, want %q", ce.kind, "input_required")
	}
	// Exit code 6 means the tool failed; this is a different outcome and reusing
	// it would mislead an agent into retrying.
	if ce.code == exitTool {
		t.Fatal("input_required must not share the tool-error exit code")
	}
}
