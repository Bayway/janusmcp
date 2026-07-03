package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAccountsForSelector(t *testing.T) {
	c := &Config{
		Accounts: []Account{{ID: "a", Command: "x"}, {ID: "b", Command: "x"}},
		Profiles: map[string][]string{"p": {"a", "b"}},
	}
	if ids, ok := c.AccountsForSelector("a"); !ok || len(ids) != 1 || ids[0] != "a" {
		t.Fatalf("account selector: ids=%v ok=%v", ids, ok)
	}
	if ids, ok := c.AccountsForSelector("p"); !ok || len(ids) != 2 {
		t.Fatalf("profile selector: ids=%v ok=%v", ids, ok)
	}
	if _, ok := c.AccountsForSelector("nope"); ok {
		t.Fatalf("unknown selector should be (nil,false)")
	}
	if _, ok := c.AccountsForSelector(""); ok {
		t.Fatalf("empty selector should be (nil,false)")
	}
}

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProfileValidation(t *testing.T) {
	good := writeTempConfig(t, `{"accounts":[{"id":"a","service":"s","command":"x"}],"profiles":{"p":["a"]}}`)
	if _, err := LoadRaw(good); err != nil {
		t.Fatalf("valid profile rejected: %v", err)
	}

	bad := writeTempConfig(t, `{"accounts":[{"id":"a","service":"s","command":"x"}],"profiles":{"p":["missing"]}}`)
	if _, err := LoadRaw(bad); err == nil {
		t.Fatalf("expected error for profile referencing an unknown account")
	}

	empty := writeTempConfig(t, `{"accounts":[{"id":"a","service":"s","command":"x"}],"profiles":{"p":[]}}`)
	if _, err := LoadRaw(empty); err == nil {
		t.Fatalf("expected error for an empty profile")
	}
}

// A remote OAuth account may carry a pre-registered client (clientId/secret) and
// explicit scopes for servers without dynamic client registration (e.g. Google).
func TestParsePreregisteredOAuthClient(t *testing.T) {
	p := writeTempConfig(t, `{"accounts":[{
		"id":"gmail_a","service":"gmail","transport":"http",
		"url":"https://gmailmcp.googleapis.com/mcp/v1","auth":"oauth",
		"oauthClientId":"cid","oauthClientSecret":"vault:google_secret",
		"scopes":["openid","https://www.googleapis.com/auth/gmail.readonly"]
	}]}`)
	cfg, err := LoadRaw(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	a := cfg.Accounts[0]
	if a.OAuthClientID != "cid" {
		t.Fatalf("oauthClientId = %q", a.OAuthClientID)
	}
	if a.OAuthClientSecret != "vault:google_secret" {
		t.Fatalf("oauthClientSecret = %q", a.OAuthClientSecret)
	}
	if len(a.Scopes) != 2 || a.Scopes[0] != "openid" {
		t.Fatalf("scopes = %v", a.Scopes)
	}
}

// LoadRawOrEmpty returns an empty config (no accounts) when the file is missing,
// so `serve` can boot with only the control tools.
func TestLoadRawOrEmptyMissingFile(t *testing.T) {
	cfg, err := LoadRawOrEmpty(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg == nil || len(cfg.Accounts) != 0 {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}
