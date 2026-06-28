package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bayway/janusmcp/internal/config"
	"github.com/bayway/janusmcp/internal/oauth"
)

const controlPrefix = "janus_"

// Core is the shared broker state (config, upstreams, global state, session registry).
type Core struct {
	Cfg      *config.Config
	Manager  *UpstreamManager
	State    *BrokerState
	Registry *SessionRegistry
	Store    *oauth.Store // optional: enables the janus_login tool
}

// Session owns one downstream MCP server (per connection). Its registered tool set
// reflects the session's effective active account (active-account model).
type Session struct {
	ID   string
	core *Core

	mu                 sync.Mutex
	localActive        string // "" → follow the global active account
	registeredUpstream []string

	server *mcp.Server
}

// Server exposes the underlying MCP server (used by tests to attach a transport).
func (s *Session) Server() *mcp.Server { return s.server }

func (s *Session) effectiveActive() string {
	s.mu.Lock()
	la := s.localActive
	s.mu.Unlock()
	if la != "" {
		return la
	}
	return s.core.State.GlobalActive()
}

func (s *Session) localActiveEmpty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localActive == ""
}

// NewSession builds a broker MCP server for one connection and registers tools.
func NewSession(ctx context.Context, core *Core, id string) *Session {
	srv := mcp.NewServer(&mcp.Implementation{Name: "janusmcp", Version: "0.1.0"}, nil)
	s := &Session{ID: id, core: core, server: srv}
	s.registerControlTools()
	_ = s.applyActiveTools(ctx) // best-effort: a dead upstream shouldn't kill the session
	core.Registry.Add(s)
	return s
}

func emptyObjectSchema() *jsonschema.Schema {
	return &jsonschema.Schema{Type: "object"}
}

func boolPtr(b bool) *bool { return &b }

func (s *Session) registerControlTools() {
	s.server.AddTool(&mcp.Tool{
		Name:        controlPrefix + "list_accounts",
		Description: "List configured accounts/services and which is active for this session.",
		InputSchema: emptyObjectSchema(),
		Annotations: &mcp.ToolAnnotations{Title: "List accounts", ReadOnlyHint: true},
	}, s.handleListAccounts)

	s.server.AddTool(&mcp.Tool{
		Name: controlPrefix + "use_account",
		Description: "Set the active account/identity. Reloads upstream tools and notifies the client. " +
			"scope: 'session' or 'global' (defaults to the broker bindingMode).",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"account_id": {Type: "string"},
				"scope":      {Type: "string", Enum: []any{"session", "global"}},
			},
			Required: []string{"account_id"},
		},
		// Switches the active identity (local broker state) — not destructive,
		// and idempotent (selecting the same account again is a no-op).
		Annotations: &mcp.ToolAnnotations{
			Title:           "Switch active account",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
		},
	}, s.handleUseAccount)

	s.server.AddTool(&mcp.Tool{
		Name:        controlPrefix + "whoami",
		Description: "Return the active account for this session and how it was resolved.",
		InputSchema: emptyObjectSchema(),
		Annotations: &mcp.ToolAnnotations{Title: "Who am I", ReadOnlyHint: true},
	}, s.handleWhoami)

	// Exposed only when an OAuth store is configured.
	if s.core.Store != nil {
		s.server.AddTool(&mcp.Tool{
			Name: controlPrefix + "login",
			Description: "Start an OAuth login for <provider> in your browser and store the token as <name>. " +
				"The token is captured locally and saved to the OS keychain — it never passes through this chat. " +
				"Afterwards reference it from an account env as \"oauth:<name>\". Built-in providers: github, google, supabase.",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"provider": {Type: "string", Description: "OAuth provider preset, e.g. github, google."},
					"name":     {Type: "string", Description: "Name to store the token under (used as oauth:<name>)."},
				},
				Required: []string{"provider", "name"},
			},
			// Opens a browser OAuth flow with an external provider (open world) and
			// writes a token to the OS keychain — state-changing but not destructive.
			Annotations: &mcp.ToolAnnotations{
				Title:           "OAuth login (opens browser)",
				ReadOnlyHint:    false,
				DestructiveHint: boolPtr(false),
				OpenWorldHint:   boolPtr(true),
			},
		}, s.handleLogin)
	}
}

func textResult(v any) *mcp.CallToolResult {
	b, _ := json.MarshalIndent(v, "", "  ")
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

func (s *Session) handleListAccounts(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	active := s.effectiveActive()
	accts := make([]map[string]any, 0, len(s.core.Cfg.Accounts))
	for _, a := range s.core.Cfg.Accounts {
		accts = append(accts, map[string]any{
			"id": a.ID, "service": a.Service, "label": a.DisplayLabel(), "active": a.ID == active,
		})
	}
	return textResult(map[string]any{
		"active": active, "bindingMode": s.core.State.BindingMode(), "sessionId": s.ID, "accounts": accts,
	}), nil
}

func (s *Session) handleWhoami(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	active := s.effectiveActive()
	a, err := s.core.Manager.Account(active)
	if err != nil {
		return textResult(map[string]any{"error": err.Error()}), nil
	}
	source := "global"
	if !s.localActiveEmpty() {
		source = "session"
	}
	return textResult(map[string]any{
		"id": a.ID, "label": a.DisplayLabel(), "service": a.Service, "resolvedFrom": source, "sessionId": s.ID,
	}), nil
}

func (s *Session) handleUseAccount(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var in struct {
		AccountID string `json:"account_id"`
		Scope     string `json:"scope"`
	}
	if len(req.Params.Arguments) > 0 {
		_ = json.Unmarshal(req.Params.Arguments, &in)
	}
	if s.core.State.BindingMode() == config.BindingLocked {
		return textResult(map[string]any{"ok": false, "error": "bindingMode=locked: switching is disabled"}), nil
	}
	if _, err := s.core.Manager.Account(in.AccountID); err != nil {
		return textResult(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	scope := in.Scope
	if scope == "" {
		if s.core.State.BindingMode() == config.BindingGlobal {
			scope = "global"
		} else {
			scope = "session"
		}
	}

	if scope == "global" {
		s.core.State.SetGlobal(in.AccountID)
		s.mu.Lock()
		s.localActive = ""
		s.mu.Unlock()
		// Re-apply to every session that has no local override.
		s.core.Registry.Each(func(sess *Session) {
			if sess.localActiveEmpty() {
				_ = sess.applyActiveTools(ctx)
			}
		})
	} else {
		s.mu.Lock()
		s.localActive = in.AccountID
		s.mu.Unlock()
		_ = s.applyActiveTools(ctx)
	}
	return textResult(map[string]any{"ok": true, "active": in.AccountID, "scope": scope, "sessionId": s.ID}), nil
}

func (s *Session) handleLogin(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.core.Store == nil {
		return textResult(map[string]any{"ok": false, "error": "login is not configured"}), nil
	}
	var in struct {
		Provider string `json:"provider"`
		Name     string `json:"name"`
	}
	if len(req.Params.Arguments) > 0 {
		_ = json.Unmarshal(req.Params.Arguments, &in)
	}
	if in.Provider == "" || in.Name == "" {
		return textResult(map[string]any{"ok": false, "error": "both 'provider' and 'name' are required"}), nil
	}
	// Opens the browser and blocks until the loopback callback completes. The token
	// is captured by the broker process and stored in the vault — never returned here.
	if err := s.core.Store.Login(ctx, in.Provider, in.Name); err != nil {
		return textResult(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	return textResult(map[string]any{
		"ok":        true,
		"stored_as": in.Name,
		"reference": "oauth:" + in.Name,
		"hint":      "set an account's env to \"oauth:" + in.Name + "\" to use this identity",
	}), nil
}

// proxyHandler forwards a tool call to this session's currently active upstream.
func (s *Session) proxyHandler() mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		active := s.effectiveActive()
		cs, err := s.core.Manager.Session(ctx, active)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "upstream connect error: " + err.Error()}},
			}, nil
		}
		// Forward arguments verbatim. Always send an object (never null/omitted):
		// upstream schemas expect a record, and CallToolParams.Arguments is
		// `omitempty`, so an empty map would be dropped — use raw {} instead.
		raw := json.RawMessage(req.Params.Arguments)
		if len(raw) == 0 || string(raw) == "null" {
			raw = json.RawMessage("{}")
		}
		return cs.CallTool(ctx, &mcp.CallToolParams{Name: req.Params.Name, Arguments: raw})
	}
}

// applyActiveTools swaps the registered upstream tools to match the active account.
// AddTool/RemoveTools after initialization trigger tools/list_changed notifications.
func (s *Session) applyActiveTools(ctx context.Context) error {
	active := s.effectiveActive()
	tools, err := s.core.Manager.Tools(ctx, active)
	if err != nil {
		return err
	}
	s.mu.Lock()
	old := s.registeredUpstream
	s.mu.Unlock()

	if len(old) > 0 {
		s.server.RemoveTools(old...)
	}
	names := make([]string, 0, len(tools))
	h := s.proxyHandler()
	for _, t := range tools {
		s.server.AddTool(t, h)
		names = append(names, t.Name)
	}
	s.mu.Lock()
	s.registeredUpstream = names
	s.mu.Unlock()
	return nil
}

// RunStdio serves a single stdio session (Claude Desktop/Code).
func (c *Core) RunStdio(ctx context.Context) error {
	s := NewSession(ctx, c, "stdio")
	return s.server.Run(ctx, &mcp.StdioTransport{})
}

// HTTPHandler returns a Streamable HTTP handler that builds one broker server per session.
func (c *Core) HTTPHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		s := NewSession(r.Context(), c, randomID())
		return s.server
	}, nil)
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
