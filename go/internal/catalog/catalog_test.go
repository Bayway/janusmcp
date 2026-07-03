package catalog

import "testing"

func TestGoogleWorkspaceTemplate(t *testing.T) {
	tmpl, ok := Templates()["gmail"]
	if !ok {
		t.Fatal("gmail template missing")
	}
	a := tmpl.Build("gmail_a")
	if a.Transport != "http" || a.Auth != "oauth" {
		t.Fatalf("unexpected transport/auth: %+v", a)
	}
	if a.URL != "https://gmailmcp.googleapis.com/mcp/v1" {
		t.Fatalf("url = %q", a.URL)
	}
	if a.OAuthClientID == "" || a.OAuthClientSecret == "" {
		t.Fatalf("expected pre-registered client placeholders: %+v", a)
	}
	if len(a.Scopes) == 0 {
		t.Fatalf("expected default scopes, got none")
	}
}

func TestActiveCampaignTemplate(t *testing.T) {
	tmpl, ok := Templates()["activecampaign"]
	if !ok {
		t.Fatal("activecampaign template missing")
	}
	a := tmpl.Build("ac_a")
	if a.Transport != "http" || a.Auth != "oauth" {
		t.Fatalf("unexpected transport/auth: %+v", a)
	}
	if a.URL == "" {
		t.Fatalf("expected a URL placeholder, got empty")
	}
	// ActiveCampaign uses dynamic client registration: no pre-registered client.
	if a.OAuthClientID != "" || a.OAuthClientSecret != "" {
		t.Fatalf("activecampaign should not carry a pre-registered client: %+v", a)
	}
}
