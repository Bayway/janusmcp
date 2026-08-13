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

	persistent         bool // stdio and the shared stateless server outlive any one connection
	mu                 sync.Mutex
	localActive        string // "" → follow the global active account/profile
	registeredUpstream []string
	routes             map[string]route // exposed tool name → upstream account + original name

	server *mcp.Server
}

// route maps an exposed tool name to the upstream account and its original tool
// name. With a single active account these are 1:1; with a profile (many accounts)
// it lets the proxy send each call to the right upstream, and disambiguates
// colliding tool names by namespacing them.
type route struct {
	account string
	tool    string
}

// effectiveAccounts resolves the session's active selector to the set of account
// ids whose tools should be exposed (one for an account, several for a profile).
func (s *Session) effectiveAccounts() []string {
	sel := s.effectiveActive()
	if ids, ok := s.core.Cfg.AccountsForSelector(sel); ok {
		return ids
	}
	return nil
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
	srv.AddReceivingMiddleware(privateCacheScope)
	s := &Session{ID: id, core: core, server: srv}
	s.registerControlTools()
	_ = s.applyActiveTools(ctx) // best-effort: a dead upstream shouldn't kill the session
	core.Registry.Add(s)
	return s
}

// privateCacheScope marks every cacheable list result as private.
//
// The SDK defaults cacheScope to "public" (2026-07-28 adds ttlMs/cacheScope
// hints to list responses), which is a false claim for a broker: what
// tools/list returns depends on the caller's active account, so no intermediary
// may share it. ttlMs stays 0 — "immediately stale" — because a switch can
// change the answer at any moment.
func privateCacheScope(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		if err != nil {
			return res, err
		}
		// The Cacheable fields are set inside the SDK's own handlers, and
		// CacheableResult exposes getters only, so the concrete types are the
		// only seam available.
		switch r := res.(type) {
		case *mcp.ListToolsResult:
			r.CacheScope, r.TTLMs = "private", 0
		case *mcp.ListPromptsResult:
			r.CacheScope, r.TTLMs = "private", 0
		case *mcp.ListResourcesResult:
			r.CacheScope, r.TTLMs = "private", 0
		case *mcp.ListResourceTemplatesResult:
			r.CacheScope, r.TTLMs = "private", 0
		case *mcp.DiscoverResult:
			// Capabilities depend on which control tools are registered
			// (janus_login only with a vault, janus_use_profile only with
			// profiles), so discovery is caller-specific too.
			r.CacheScope, r.TTLMs = "private", 0
		}
		return res, nil
	}
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

	// One-shot call on another account without switching the active one.
	s.server.AddTool(&mcp.Tool{
		Name: controlPrefix + "with_account",
		Description: "Run a single tool call on another account WITHOUT changing the active account. " +
			"Omit 'tool' to first list that account's available tools.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"account_id":  {Type: "string", Description: "Target account id."},
				"tool":        {Type: "string", Description: "Tool to call; omit to list the account's tools."},
				"arguments":   {Type: "object", Description: "Arguments object for the tool."},
				"full_schema": {Type: "boolean", Description: "When listing, include complete tool definitions. Intended for CLI discovery."},
			},
			Required: []string{"account_id"},
		},
		Annotations: &mcp.ToolAnnotations{Title: "Call a tool on another account", OpenWorldHint: boolPtr(true)},
	}, s.handleWithAccount)

	// Profiles: activate a whole client's set of accounts at once.
	if len(s.core.Cfg.Profiles) > 0 {
		s.server.AddTool(&mcp.Tool{
			Name: controlPrefix + "use_profile",
			Description: "Activate a profile (a named group of accounts) so the tools of all its " +
				"accounts are exposed at once. scope: 'session' or 'global'.",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"profile": {Type: "string", Description: "Profile name from config 'profiles'."},
					"scope":   {Type: "string", Enum: []any{"session", "global"}},
				},
				Required: []string{"profile"},
			},
			Annotations: &mcp.ToolAnnotations{Title: "Switch active profile", DestructiveHint: boolPtr(false), IdempotentHint: true},
		}, s.handleUseProfile)
	}
}

func textResult(v any) *mcp.CallToolResult {
	b, _ := json.MarshalIndent(v, "", "  ")
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

func (s *Session) handleListAccounts(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	active := s.effectiveActive()
	activeSet := map[string]bool{}
	for _, id := range s.effectiveAccounts() {
		activeSet[id] = true
	}
	accts := make([]map[string]any, 0, len(s.core.Cfg.Accounts))
	for _, a := range s.core.Cfg.Accounts {
		accts = append(accts, map[string]any{
			"id": a.ID, "service": a.Service, "label": a.DisplayLabel(), "active": activeSet[a.ID],
		})
	}
	out := map[string]any{
		"active": active, "bindingMode": s.core.State.BindingMode(), "sessionId": s.ID, "accounts": accts,
	}
	if len(s.core.Cfg.Profiles) > 0 {
		out["profiles"] = s.core.Cfg.Profiles
	}
	return textResult(out), nil
}

func (s *Session) handleWhoami(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	active := s.effectiveActive()
	source := "global"
	if !s.localActiveEmpty() {
		source = "session"
	}
	if accounts, isProfile := s.core.Cfg.Profiles[active]; isProfile {
		return textResult(map[string]any{
			"type": "profile", "active": active, "accounts": accounts, "resolvedFrom": source, "sessionId": s.ID,
		}), nil
	}
	a, err := s.core.Manager.Account(active)
	if err != nil {
		return textResult(map[string]any{"active": active, "error": err.Error(), "sessionId": s.ID}), nil
	}
	return textResult(map[string]any{
		"type": "account", "id": a.ID, "label": a.DisplayLabel(), "service": a.Service, "resolvedFrom": source, "sessionId": s.ID,
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
	return s.setActiveSelector(ctx, in.AccountID, in.Scope), nil
}

// handleUseProfile activates a profile: the tools of all its accounts are exposed
// at once and each call is routed to the right upstream.
func (s *Session) handleUseProfile(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var in struct {
		Profile string `json:"profile"`
		Scope   string `json:"scope"`
	}
	if len(req.Params.Arguments) > 0 {
		_ = json.Unmarshal(req.Params.Arguments, &in)
	}
	if s.core.State.BindingMode() == config.BindingLocked {
		return textResult(map[string]any{"ok": false, "error": "bindingMode=locked: switching is disabled"}), nil
	}
	if _, ok := s.core.Cfg.Profiles[in.Profile]; !ok {
		return textResult(map[string]any{"ok": false, "error": "unknown profile: " + in.Profile}), nil
	}
	return s.setActiveSelector(ctx, in.Profile, in.Scope), nil
}

// setActiveSelector switches the active account-or-profile for the session or
// globally, then re-applies the exposed tool set.
func (s *Session) setActiveSelector(ctx context.Context, selector, scope string) *mcp.CallToolResult {
	if scope == "" {
		if s.core.State.BindingMode() == config.BindingGlobal {
			scope = "global"
		} else {
			scope = "session"
		}
	}
	if scope == "global" {
		if err := s.core.State.SetGlobal(selector); err != nil {
			return textResult(map[string]any{"ok": false, "error": err.Error()})
		}
		s.mu.Lock()
		s.localActive = ""
		s.mu.Unlock()
		s.core.Registry.Each(func(sess *Session) {
			if sess.localActiveEmpty() {
				_ = sess.applyActiveTools(ctx)
			}
		})
	} else {
		s.mu.Lock()
		s.localActive = selector
		s.mu.Unlock()
		_ = s.applyActiveTools(ctx)
	}
	return textResult(map[string]any{"ok": true, "active": selector, "scope": scope, "sessionId": s.ID})
}

// handleWithAccount runs a one-shot call on a non-active account without changing
// the session's active selection. With no "tool", it lists that account's tools.
func (s *Session) handleWithAccount(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var in struct {
		AccountID  string          `json:"account_id"`
		Tool       string          `json:"tool"`
		Arguments  json.RawMessage `json:"arguments"`
		FullSchema bool            `json:"full_schema"`
	}
	if len(req.Params.Arguments) > 0 {
		_ = json.Unmarshal(req.Params.Arguments, &in)
	}
	if in.AccountID == "" {
		return textResult(map[string]any{"ok": false, "error": "account_id is required"}), nil
	}
	if _, err := s.core.Manager.Account(in.AccountID); err != nil {
		return textResult(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	if in.Tool == "" {
		tools, err := s.core.Manager.Tools(ctx, in.AccountID)
		if err != nil {
			return textResult(map[string]any{"ok": false, "error": err.Error()}), nil
		}
		if in.FullSchema {
			return textResult(map[string]any{"account": in.AccountID, "tools": tools}), nil
		}
		list := make([]map[string]string, 0, len(tools))
		for _, t := range tools {
			list = append(list, map[string]string{"name": t.Name, "description": t.Description})
		}
		return textResult(map[string]any{"account": in.AccountID, "tools": list}), nil
	}
	cs, err := s.core.Manager.Session(ctx, in.AccountID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "upstream connect error: " + err.Error()}}}, nil
	}
	raw := in.Arguments
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	return callUpstream(ctx, cs, in.AccountID, in.Tool, raw, req.Params.Meta, req.Params.InputResponses, req.Params.RequestState)
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

// proxyHandler forwards a tool call to the upstream that owns the called tool.
// The routing map (built by applyActiveTools) resolves the exposed name to the
// right account and the original upstream tool name; this supports a single
// active account and a multi-account profile alike.
func (s *Session) proxyHandler() mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		s.mu.Lock()
		r, ok := s.routes[req.Params.Name]
		s.mu.Unlock()
		account, tool := r.account, r.tool
		if !ok {
			account, tool = s.effectiveActive(), req.Params.Name
		}
		cs, err := s.core.Manager.Session(ctx, account)
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
		return callUpstream(ctx, cs, account, tool, raw, req.Params.Meta, req.Params.InputResponses, req.Params.RequestState)
	}
}

// callUpstream forwards one tool call and relays a multi round-trip exchange
// (SEP-2322) in both directions.
//
// The broker is a proxy, so it must not answer the upstream's input requests
// itself: the upstream client is built with the SDK's automatic MRTR middleware
// disabled (see UpstreamManager.Session), which surfaces `input_required` here
// so it can be handed to the real client instead.
func callUpstream(
	ctx context.Context,
	cs *mcp.ClientSession,
	account, tool string,
	args json.RawMessage,
	meta mcp.Meta,
	inputResponses mcp.InputResponseMap,
	requestState string,
) (*mcp.CallToolResult, error) {
	upstreamState, err := unwrapRequestState(requestState, account, tool)
	if err != nil {
		// A stale or misrouted retry is the model's problem to recover from, so
		// report it as a tool error it can read rather than a protocol error.
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:           tool,
		Arguments:      args,
		Meta:           meta,
		InputResponses: inputResponses,
		RequestState:   upstreamState,
	})
	if err != nil {
		return nil, err
	}

	// A nil InputRequests means a normal result; a non-nil but empty one means
	// the upstream is load-shedding. Both must survive, so test for nil rather
	// than for length.
	if res.InputRequests == nil {
		return res, nil
	}
	out := *res // copy: carries the unexported result type across untouched
	out.RequestState = wrapRequestState(account, tool, res.RequestState)
	return &out, nil
}

// applyActiveTools swaps the registered upstream tools to match the active account.
// AddTool/RemoveTools after initialization trigger tools/list_changed notifications.
func (s *Session) applyActiveTools(ctx context.Context) error {
	accounts := s.effectiveAccounts()

	s.mu.Lock()
	old := s.registeredUpstream
	s.mu.Unlock()
	if len(old) > 0 {
		s.server.RemoveTools(old...)
	}

	names := make([]string, 0)
	routes := make(map[string]route)
	h := s.proxyHandler()
	var firstErr error
	for _, acc := range accounts {
		tools, err := s.core.Manager.Tools(ctx, acc)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue // a dead/unauthorized upstream shouldn't drop the others
		}
		for _, t := range tools {
			name := t.Name
			if _, clash := routes[name]; clash {
				name = acc + "_" + t.Name // namespace on collision across accounts
			}
			reg := *t
			reg.Name = name
			s.server.AddTool(&reg, h)
			routes[name] = route{account: acc, tool: t.Name}
			names = append(names, name)
		}
	}

	s.mu.Lock()
	s.registeredUpstream = names
	s.routes = routes
	s.mu.Unlock()
	return firstErr
}

// RunStdio serves a single stdio session (Claude Desktop/Code).
func (c *Core) RunStdio(ctx context.Context) error {
	s := NewSession(ctx, c, "stdio")
	// One connection for the life of the process: never prune it, even in the
	// window before the transport attaches.
	s.persistent = true
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
