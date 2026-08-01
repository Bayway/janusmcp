package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	callErr := fn()
	_ = w.Close()
	os.Stdout = old
	b, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(b), callErr
}

func mockCLIConfig(t *testing.T, bindingMode string) string {
	t.Helper()
	mock, err := filepath.Abs(filepath.Join("..", "..", "..", "spike", "mock-upstream", "server.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := map[string]any{
		"defaultAccount": "azienda_a",
		"bindingMode":    bindingMode,
		"accounts": []map[string]any{{
			"id": "azienda_a", "service": "mock", "command": "node", "args": []string{mock},
			"env": map[string]string{"MOCK_ACCOUNT": "azienda_a"},
		}},
	}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCallJSONEnvelopeAndToolError(t *testing.T) {
	t.Setenv("JANUS_CONFIG", mockCLIConfig(t, "global"))

	out, err := captureStdout(t, func() error { return runCall([]string{"structured_result", "--json"}) })
	if err != nil {
		t.Fatalf("structured call: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	if result["ok"] != true || result["account"] != "azienda_a" || result["tool"] != "structured_result" {
		t.Fatalf("unexpected envelope: %#v", result)
	}
	if _, ok := result["structuredContent"]; !ok {
		t.Fatalf("structuredContent missing: %#v", result)
	}

	out, err = captureStdout(t, func() error { return runCall([]string{"fail", "--json"}) })
	var ce *commandError
	if !errors.As(err, &ce) || ce.code != exitTool || !ce.rendered {
		t.Fatalf("tool error=%v, want rendered exit %d", err, exitTool)
	}
	result = nil
	if json.Unmarshal([]byte(out), &result) != nil || result["ok"] != false || result["isError"] != true {
		t.Fatalf("unexpected tool error envelope: %s", out)
	}
}

func TestCallTimeout(t *testing.T) {
	t.Setenv("JANUS_CONFIG", mockCLIConfig(t, "global"))
	_, err := captureStdout(t, func() error {
		return runCall([]string{"delay", "--args", `{"ms":500}`, "--timeout", "25ms", "--json"})
	})
	var ce *commandError
	if !errors.As(err, &ce) || ce.code != exitTimeout {
		t.Fatalf("timeout error=%v, want exit %d", err, exitTimeout)
	}
}

func TestCallRejectsNullArguments(t *testing.T) {
	t.Setenv("JANUS_CONFIG", mockCLIConfig(t, "global"))
	_, err := captureStdout(t, func() error {
		return runCall([]string{"ping", "--args", "null", "--json"})
	})
	var ce *commandError
	if !errors.As(err, &ce) || ce.code != exitUsage || ce.kind != "invalid_json" {
		t.Fatalf("null arguments error=%v", err)
	}
}

func TestUsePersistsAndHonorsLockedMode(t *testing.T) {
	configPath := mockCLIConfig(t, "global")
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("JANUS_CONFIG", configPath)
	t.Setenv("JANUS_STATE", statePath)
	out, err := captureStdout(t, func() error { return runUse([]string{"azienda_a", "--json"}) })
	if err != nil || !strings.Contains(out, `"active":"azienda_a"`) {
		t.Fatalf("use output=%q err=%v", out, err)
	}
	b, err := os.ReadFile(statePath)
	if err != nil || !strings.Contains(string(b), "azienda_a") {
		t.Fatalf("state=%q err=%v", b, err)
	}

	t.Setenv("JANUS_CONFIG", mockCLIConfig(t, "locked"))
	_, err = captureStdout(t, func() error { return runUse([]string{"azienda_a"}) })
	var ce *commandError
	if !errors.As(err, &ce) || ce.kind != "switching_locked" {
		t.Fatalf("locked error=%v", err)
	}
}

func TestCLIContextTimeoutPrecedence(t *testing.T) {
	t.Setenv("JANUS_CLI_TIMEOUT", "1h")
	ctx, cancel, err := cliContext("20ms")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("context error=%v", ctx.Err())
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("flag timeout did not override environment")
	}
}

func TestRenderCLIErrorJSON(t *testing.T) {
	var b strings.Builder
	code := renderCLIError(&b, jsonErr(cliErr(exitSelection, "unknown_selector", errors.New("missing"))))
	if code != exitSelection || !strings.Contains(b.String(), `"code":"unknown_selector"`) {
		t.Fatalf("code=%d output=%s", code, b.String())
	}
}

func TestClassifyAuthenticationError(t *testing.T) {
	err := classifyUpstreamError("upstream_error", errors.New("OAuth token refresh returned status 401"))
	var ce *commandError
	if !errors.As(err, &ce) || ce.code != exitAuth || ce.kind != "authentication_error" {
		t.Fatalf("classified error=%v", err)
	}
}
