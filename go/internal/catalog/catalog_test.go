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

func TestRemoteOAuthTemplates(t *testing.T) {
	// Commonly-used remote servers added as browser-login (DCR) accounts.
	want := map[string]string{
		"linear":    "https://mcp.linear.app/mcp",
		"atlassian": "https://mcp.atlassian.com/v1/mcp/authv2",
		"vercel":    "https://mcp.vercel.com",
		"canva":     "https://mcp.canva.com/mcp",
		"neon":      "https://mcp.neon.tech/mcp",
		"netlify":   "https://netlify-mcp.netlify.app/mcp",
	}
	tmpls := Templates()
	for name, url := range want {
		tmpl, ok := tmpls[name]
		if !ok {
			t.Fatalf("%s template missing", name)
		}
		a := tmpl.Build(name + "_a")
		if a.Transport != "http" || a.Auth != "oauth" {
			t.Fatalf("%s: unexpected transport/auth: %+v", name, a)
		}
		if a.URL != url {
			t.Fatalf("%s: url = %q, want %q", name, a.URL, url)
		}
		// DCR servers must NOT carry a pre-registered client.
		if a.OAuthClientID != "" || a.OAuthClientSecret != "" {
			t.Fatalf("%s should use dynamic client registration: %+v", name, a)
		}
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
