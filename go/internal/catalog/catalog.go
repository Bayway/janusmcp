// Package catalog provides ready-to-use account templates so users can scaffold
// a config entry with `janusmcp add <template> <id>` instead of writing JSON by hand.
//
// Users can add their own templates in a JSON file at
// $JANUS_TEMPLATES (or <user-config-dir>/janusmcp/templates.json):
//
//	{
//	  "myserver": {
//	    "description": "My remote MCP server",
//	    "account": { "transport": "http", "url": "https://example.com/mcp", "auth": "oauth" },
//	    "notes": ["Login opens in the browser on first use."]
//	  }
//	}
package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/bayway/janusmcp/internal/config"
)

// Template scaffolds a config.Account and tells the user what to fill in next.
type Template struct {
	Name        string
	Description string
	Build       func(id string) config.Account
	Notes       func(id string) []string
}

// httpOAuth builds a template for a remote MCP server that uses browser-based
// OAuth (dynamic client registration) — the multi-account sweet spot.
func httpOAuth(name, label, url, desc string) Template {
	return Template{
		Name:        name,
		Description: desc,
		Build: func(id string) config.Account {
			return config.Account{ID: id, Service: name, Label: label, Transport: "http", URL: url, Auth: "oauth"}
		},
		Notes: func(id string) []string {
			return []string{
				"Al primo uso si apre il browser per il login a " + label + ".",
				"Per un secondo account: janusmcp add " + name + " <altro-id> (login con l'altro account).",
			}
		},
	}
}

// sseOAuth builds a template for a remote MCP server that speaks the legacy SSE
// transport with browser-based OAuth (dynamic client registration). Same as
// httpOAuth but Transport:"sse".
func sseOAuth(name, label, url, desc string) Template {
	return Template{
		Name:        name,
		Description: desc,
		Build: func(id string) config.Account {
			return config.Account{ID: id, Service: name, Label: label, Transport: "sse", URL: url, Auth: "oauth"}
		},
		Notes: func(id string) []string {
			return []string{
				"Al primo uso si apre il browser per il login a " + label + ".",
				"Per un secondo account: janusmcp add " + name + " <altro-id> (login con l'altro account).",
				"Transport SSE (legacy): alcuni provider stanno migrando a Streamable HTTP (/mcp).",
			}
		},
	}
}

// googleWorkspace builds a template for a Google Workspace remote MCP server
// (Gmail, Drive, Calendar, …). These servers do NOT support dynamic client
// registration: they require an OAuth client you create in the Google Cloud
// Console, so the template seeds oauthClientId/oauthClientSecret (from env by
// default) plus the scopes to request.
func googleWorkspace(name, label, url, desc string, scopes []string) Template {
	return Template{
		Name:        name,
		Description: desc,
		Build: func(id string) config.Account {
			return config.Account{
				ID: id, Service: name, Label: label,
				Transport: "http", URL: url, Auth: "oauth",
				OAuthClientID:     "${GOOGLE_OAUTH_CLIENT_ID}",
				OAuthClientSecret: "${GOOGLE_OAUTH_CLIENT_SECRET}",
				Scopes:            scopes,
			}
		},
		Notes: func(id string) []string {
			return []string{
				"Google non supporta la registrazione dinamica del client: serve un TUO client OAuth.",
				"1) Google Cloud Console: abilita le MCP API del progetto (es. gmailmcp.googleapis.com).",
				"2) Configura la schermata di consenso OAuth e crea un client OAuth di tipo 'Desktop'.",
				"3) Esporta le credenziali come GOOGLE_OAUTH_CLIENT_ID e GOOGLE_OAUTH_CLIENT_SECRET",
				"   (oppure metti il secret nel vault e usa \"vault:<nome>\" in oauthClientSecret).",
				"Al primo uso si apre il browser per il login a " + label + "; il token va nel vault.",
				"Per un secondo account: janusmcp add " + name + " <altro-id> (login con l'altro account).",
				"Vedi docs/google-workspace.md per il setup completo.",
			}
		},
	}
}

// Templates returns built-in templates merged with any user-defined ones.
func Templates() map[string]Template {
	m := map[string]Template{
		// Remote servers with native OAuth (browser login, multi-account ready).
		"supabase": httpOAuth("supabase", "Supabase", "https://mcp.supabase.com/mcp", "Supabase (hosted) — browser login, multi-account."),
		"github":   httpOAuth("github", "GitHub", "https://api.githubcopilot.com/mcp/", "GitHub remote MCP — browser login (e.g. two orgs)."),
		"notion":   httpOAuth("notion", "Notion", "https://mcp.notion.com/mcp", "Notion remote MCP — browser login (e.g. two workspaces)."),
		"sentry":   httpOAuth("sentry", "Sentry", "https://mcp.sentry.dev/mcp", "Sentry remote MCP — browser login."),
		"stripe":   httpOAuth("stripe", "Stripe", "https://mcp.stripe.com", "Stripe remote MCP — browser login (e.g. two accounts)."),
		"hubspot":  httpOAuth("hubspot", "HubSpot", "https://mcp.hubspot.com/anthropic", "HubSpot remote MCP — browser login."),
		"paypal":   httpOAuth("paypal", "PayPal", "https://mcp.paypal.com/mcp", "PayPal remote MCP — browser login."),
		"linear":   httpOAuth("linear", "Linear", "https://mcp.linear.app/mcp", "Linear remote MCP — browser login (e.g. two workspaces)."),
		"vercel":   httpOAuth("vercel", "Vercel", "https://mcp.vercel.com", "Vercel remote MCP — browser login (e.g. two teams)."),
		"canva":    httpOAuth("canva", "Canva", "https://mcp.canva.com/mcp", "Canva remote MCP — browser login (e.g. two brand accounts)."),
		"neon":     httpOAuth("neon", "Neon", "https://mcp.neon.tech/mcp", "Neon (serverless Postgres) remote MCP — browser login (e.g. two accounts)."),
		"netlify":  httpOAuth("netlify", "Netlify", "https://netlify-mcp.netlify.app/mcp", "Netlify remote MCP — browser login (e.g. two accounts)."),

		// Cloudflare product MCP servers — Streamable HTTP (/mcp), browser login.
		"cloudflare-bindings":      httpOAuth("cloudflare-bindings", "Cloudflare Bindings", "https://bindings.mcp.cloudflare.com/mcp", "Cloudflare Workers Bindings remote MCP — browser login (e.g. two accounts)."),
		"cloudflare-observability": httpOAuth("cloudflare-observability", "Cloudflare Observability", "https://observability.mcp.cloudflare.com/mcp", "Cloudflare Observability remote MCP — browser login."),
		"cloudflare-radar":         httpOAuth("cloudflare-radar", "Cloudflare Radar", "https://radar.mcp.cloudflare.com/mcp", "Cloudflare Radar (Internet insights) remote MCP — browser login."),
		"cloudflare-builds":        httpOAuth("cloudflare-builds", "Cloudflare Builds", "https://builds.mcp.cloudflare.com/mcp", "Cloudflare Workers Builds remote MCP — browser login."),
		"cloudflare-browser":       httpOAuth("cloudflare-browser", "Cloudflare Browser", "https://browser.mcp.cloudflare.com/mcp", "Cloudflare Browser Rendering remote MCP — browser login."),

		// Remote servers over the legacy SSE transport (browser login).
		"globalping": sseOAuth("globalping", "Globalping", "https://mcp.globalping.dev/sse", "Globalping (network measurements) remote MCP (SSE) — browser login."),
		"asana":      sseOAuth("asana", "Asana", "https://mcp.asana.com/sse", "Asana remote MCP (SSE) — browser login (e.g. two workspaces)."),
		"monday":     sseOAuth("monday", "monday.com", "https://mcp.monday.com/sse", "monday.com remote MCP (SSE) — browser login (e.g. two accounts)."),
		"intercom":   sseOAuth("intercom", "Intercom", "https://mcp.intercom.com/sse", "Intercom remote MCP (SSE) — browser login (e.g. two workspaces)."),
		"webflow":    sseOAuth("webflow", "Webflow", "https://mcp.webflow.com/sse", "Webflow remote MCP (SSE) — browser login (e.g. two accounts)."),
		"wix":        sseOAuth("wix", "Wix", "https://mcp.wix.com/sse", "Wix remote MCP (SSE) — browser login (e.g. two accounts)."),
		"square":     sseOAuth("square", "Square", "https://mcp.squareup.com/sse", "Square remote MCP (SSE) — browser login (e.g. two accounts)."),

		// Google Workspace remote MCP servers — bring-your-own OAuth client (no DCR).
		"gmail":           googleWorkspace("gmail", "Gmail", "https://gmailmcp.googleapis.com/mcp/v1", "Gmail remote MCP — bring-your-own Google OAuth client (multi-account).", []string{"openid", "email", "https://www.googleapis.com/auth/gmail.readonly"}),
		"google-drive":    googleWorkspace("google-drive", "Google Drive", "https://drivemcp.googleapis.com/mcp/v1", "Google Drive remote MCP — bring-your-own Google OAuth client.", []string{"openid", "email", "https://www.googleapis.com/auth/drive.readonly"}),
		"google-calendar": googleWorkspace("google-calendar", "Google Calendar", "https://calendarmcp.googleapis.com/mcp/v1", "Google Calendar remote MCP — bring-your-own Google OAuth client.", []string{"openid", "email", "https://www.googleapis.com/auth/calendar.readonly"}),
		"google-chat":     googleWorkspace("google-chat", "Google Chat", "https://chatmcp.googleapis.com/mcp/v1", "Google Chat remote MCP — bring-your-own Google OAuth client.", []string{"openid", "email", "https://www.googleapis.com/auth/chat.spaces.readonly", "https://www.googleapis.com/auth/chat.messages.readonly"}),

		// Zapier remote MCP — browser login (OAuth). The server exposes the Zap
		// actions you enable on mcp.zapier.com; identity comes from the login.
		"zapier": {
			Name:        "zapier",
			Description: "Zapier remote MCP — browser login; exposes the Zap actions you enable on mcp.zapier.com.",
			Build: func(id string) config.Account {
				return config.Account{ID: id, Service: "zapier", Label: "Zapier", Transport: "http", URL: "https://mcp.zapier.com/api/v1/connect", Auth: "oauth"}
			},
			Notes: func(id string) []string {
				return []string{
					"1) Su https://mcp.zapier.com crea un server e scegli quali app/azioni esporre (tab 'Tools').",
					"2) Al primo uso si apre il browser per il login OAuth (tab 'Connect').",
					"Per un secondo account: janusmcp add zapier <altro-id> (login con l'altro account Zapier).",
					"Alternativa senza OAuth: crea un server tipo 'Other', genera un token e imposta come 'url'",
					"  l'URL completo con il token (dal tab 'Connect'), es. ...?token=${ZAPIER_MCP_TOKEN}.",
				}
			},
		},

		// ActiveCampaign remote MCP — browser login (OAuth), one URL per account.
		"activecampaign": {
			Name:        "activecampaign",
			Description: "ActiveCampaign remote MCP — browser login; each account has its own Remote MCP URL.",
			Build: func(id string) config.Account {
				return config.Account{ID: id, Service: "activecampaign", Label: "ActiveCampaign", Transport: "http", URL: "REPLACE_ACTIVECAMPAIGN_MCP_URL", Auth: "oauth"}
			},
			Notes: func(id string) []string {
				return []string{
					"1) In ActiveCampaign: Settings (icona ingranaggio) → Developer → copia la tua 'Remote MCP URL'.",
					"2) Sostituisci REPLACE_ACTIVECAMPAIGN_MCP_URL con quella URL in config.json (è unica per account).",
					"Al primo uso si apre il browser per il login; il token va nel vault.",
					"Per un secondo account: janusmcp add activecampaign <altro-id> con l'URL dell'altro account.",
				}
			},
		},

		// Figma Dev Mode MCP server running locally in the desktop app — the
		// recommended, legitimate path (no OAuth allowlist, no 403).
		"figma-desktop": {
			Name:        "figma-desktop",
			Description: "Figma Dev Mode MCP server, local in the Figma desktop app (no OAuth).",
			Build: func(id string) config.Account {
				return config.Account{ID: id, Service: "figma", Label: "Figma (desktop)", Transport: "http", URL: "http://127.0.0.1:3845/mcp"}
			},
			Notes: func(id string) []string {
				return []string{
					"1) Apri l'app desktop di Figma → pannello Inspect (Dev Mode) → 'Enable desktop MCP server'.",
					"2) Richiede un posto Dev/Full su un piano Figma a pagamento.",
					"Nessun OAuth: il broker si collega in locale a http://127.0.0.1:3845/mcp.",
					"Nota: usa l'account loggato nell'app desktop (un account alla volta).",
				}
			},
		},

		// Figma's REMOTE server restricts MCP access to an allowlist of approved
		// clients, so OAuth dynamic client registration from a third party is
		// rejected (403) unless you spoof an approved client_name.
		"figma": {
			Name:        "figma",
			Description: "Figma remote MCP — restricted to Figma-approved clients (see notes).",
			Build: func(id string) config.Account {
				return config.Account{ID: id, Service: "figma", Label: "Figma", Transport: "http", URL: "https://mcp.figma.com/mcp", Auth: "oauth"}
			},
			Notes: func(id string) []string {
				return []string{
					"⚠️ Figma consente l'accesso al server REMOTO solo a client approvati (VS Code, Cursor, Claude Code…).",
					"Da un client non approvato la registrazione OAuth fallisce con 403 Forbidden.",
					"Via legittima per lavorare: usa invece il template 'figma-desktop' (server locale dell'app).",
					"Workaround opt-in per il remoto (a tuo rischio, possibile violazione ToS): in config.json",
					"  aggiungi all'account \"clientName\": \"Claude Code\", poi: janusmcp connect " + id + ".",
				}
			},
		},

		// Generic building blocks.
		"http-oauth": {
			Name:        "http-oauth",
			Description: "Any remote MCP server with native OAuth (browser login, dynamic client registration).",
			Build: func(id string) config.Account {
				return config.Account{ID: id, Service: id, Label: id, Transport: "http", URL: "REPLACE_MCP_URL", Auth: "oauth"}
			},
			Notes: func(id string) []string {
				return []string{
					"Sostituisci REPLACE_MCP_URL con l'endpoint MCP remoto (es. https://.../mcp).",
					"Al primo uso si aprirà il browser per autorizzare; il token va nel vault.",
				}
			},
		},
		"sse-oauth": {
			Name:        "sse-oauth",
			Description: "Any remote MCP server on the legacy SSE transport with native OAuth (browser login).",
			Build: func(id string) config.Account {
				return config.Account{ID: id, Service: id, Label: id, Transport: "sse", URL: "REPLACE_MCP_SSE_URL", Auth: "oauth"}
			},
			Notes: func(id string) []string {
				return []string{
					"Sostituisci REPLACE_MCP_SSE_URL con l'endpoint SSE remoto (es. https://.../sse).",
					"Al primo uso si aprirà il browser per autorizzare; il token va nel vault.",
					"Se il provider offre anche un endpoint Streamable HTTP (/mcp), preferisci il template 'http-oauth'.",
				}
			},
		},
		"supabase-pat": {
			Name:        "supabase-pat",
			Description: "Supabase via local npx server with a Personal Access Token (kept in the vault).",
			Build: func(id string) config.Account {
				return config.Account{
					ID: id, Service: "supabase", Label: "Supabase (PAT)",
					Command: "npx",
					Args:    []string{"-y", "@supabase/mcp-server-supabase@latest", "--read-only", "--project-ref=REPLACE_PROJECT_REF"},
					Env:     map[string]string{"SUPABASE_ACCESS_TOKEN": "vault:" + id + "_token"},
				}
			},
			Notes: func(id string) []string {
				return []string{
					"1) Sostituisci REPLACE_PROJECT_REF con il project ref in config.json.",
					"2) Salva il PAT nel vault:  janusmcp vault set " + id + "_token",
				}
			},
		},
		"stdio": {
			Name:        "stdio",
			Description: "Any local MCP server launched as a process (command + args).",
			Build: func(id string) config.Account {
				return config.Account{ID: id, Service: id, Label: id, Command: "REPLACE_COMMAND", Args: []string{}}
			},
			Notes: func(id string) []string {
				return []string{
					"Imposta 'command' e 'args' del server MCP locale in config.json.",
					"Per i segreti usa env con \"vault:<nome>\" e crea il segreto con: janusmcp vault set <nome>.",
				}
			},
		},
	}
	loadCustom(m)
	return m
}

// customTemplate is the on-disk shape of a user-defined template.
type customTemplate struct {
	Description string         `json:"description"`
	Account     config.Account `json:"account"`
	Notes       []string       `json:"notes"`
}

func customTemplatesPath() string {
	if p := os.Getenv("JANUS_TEMPLATES"); p != "" {
		return p
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "janusmcp", "templates.json")
	}
	return ""
}

// loadCustom merges user-defined templates (if the file exists) over the built-ins.
func loadCustom(m map[string]Template) {
	path := customTemplatesPath()
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var raw map[string]customTemplate
	if json.Unmarshal(b, &raw) != nil {
		return
	}
	for name, ct := range raw {
		name, ct := name, ct
		m[name] = Template{
			Name:        name,
			Description: ct.Description + " (custom)",
			Build: func(id string) config.Account {
				a := ct.Account
				a.ID = id
				if a.Service == "" {
					a.Service = name
				}
				if a.Label == "" {
					a.Label = id
				}
				return a
			},
			Notes: func(id string) []string { return ct.Notes },
		}
	}
}

// Names returns the template names, sorted.
func Names() []string {
	t := Templates()
	out := make([]string, 0, len(t))
	for k := range t {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
