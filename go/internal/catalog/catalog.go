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

// Templates returns built-in templates merged with any user-defined ones.
func Templates() map[string]Template {
	m := map[string]Template{
		// Remote servers with native OAuth (browser login, multi-account ready).
		"supabase": httpOAuth("supabase", "Supabase", "https://mcp.supabase.com/mcp", "Supabase (hosted) — browser login, multi-account."),
		"github":   httpOAuth("github", "GitHub", "https://api.githubcopilot.com/mcp/", "GitHub remote MCP — browser login (e.g. two orgs)."),
		"figma":    httpOAuth("figma", "Figma", "https://mcp.figma.com/mcp", "Figma remote MCP — browser login."),
		"notion":   httpOAuth("notion", "Notion", "https://mcp.notion.com/mcp", "Notion remote MCP — browser login (e.g. two workspaces)."),
		"sentry":   httpOAuth("sentry", "Sentry", "https://mcp.sentry.dev/mcp", "Sentry remote MCP — browser login."),
		"stripe":   httpOAuth("stripe", "Stripe", "https://mcp.stripe.com", "Stripe remote MCP — browser login (e.g. two accounts)."),
		"hubspot":  httpOAuth("hubspot", "HubSpot", "https://mcp.hubspot.com/anthropic", "HubSpot remote MCP — browser login."),
		"paypal":   httpOAuth("paypal", "PayPal", "https://mcp.paypal.com/mcp", "PayPal remote MCP — browser login."),

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
