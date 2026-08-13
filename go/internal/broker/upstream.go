package broker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bayway/janusmcp/internal/config"
	"github.com/bayway/janusmcp/internal/oauth"
)

// UpstreamManager lazily connects to upstream MCP servers (one per account) and
// caches their tool lists. Connections are shared across broker sessions.
type UpstreamManager struct {
	cfg       *config.Config
	configDir string
	resolve   config.SecretResolver // resolves vault:/oauth:/${ENV} at spawn time
	secrets   oauth.Secrets         // optional: persists remote OAuth tokens

	mu        sync.Mutex
	sessions  map[string]*upstreamConn
	toolCache map[string][]*mcp.Tool
}

// upstreamConn is an in-flight or completed connection attempt. Holding the
// entry (rather than the session) in the map lets concurrent first callers
// share one attempt instead of each spawning their own upstream process.
type upstreamConn struct {
	ready chan struct{} // closed when cs/err are set
	cs    *mcp.ClientSession
	err   error
}

// NewUpstreamManager builds the manager. resolve is applied to each account's env
// and args at connect time (lazy), so OAuth tokens are always fresh per spawn.
// secrets (optional) persists remote OAuth tokens so they survive restarts.
// If resolve is nil, values are used verbatim.
func NewUpstreamManager(cfg *config.Config, configDir string, resolve config.SecretResolver, secrets oauth.Secrets) *UpstreamManager {
	if resolve == nil {
		resolve = func(v string) (string, error) { return v, nil }
	}
	return &UpstreamManager{
		cfg:       cfg,
		configDir: configDir,
		resolve:   resolve,
		secrets:   secrets,
		sessions:  map[string]*upstreamConn{},
		toolCache: map[string][]*mcp.Tool{},
	}
}

func (m *UpstreamManager) Account(id string) (*config.Account, error) {
	for i := range m.cfg.Accounts {
		if m.cfg.Accounts[i].ID == id {
			return &m.cfg.Accounts[i], nil
		}
	}
	return nil, fmt.Errorf("unknown account: %s", id)
}

// Session returns a connected upstream client session for an account, connecting lazily.
//
// Connecting happens outside the manager lock — it spawns a process or performs
// an OAuth handshake — so the map holds a placeholder for the attempt. Without
// it, two concurrent first calls for the same account each spawn an upstream and
// one gets silently orphaned, which also breaks the guarantee that daemon-routed
// CLI calls reuse a single upstream process.
func (m *UpstreamManager) Session(ctx context.Context, id string) (*mcp.ClientSession, error) {
	m.mu.Lock()
	if conn, ok := m.sessions[id]; ok {
		m.mu.Unlock()
		<-conn.ready
		return conn.cs, conn.err
	}
	conn := &upstreamConn{ready: make(chan struct{})}
	m.sessions[id] = conn
	m.mu.Unlock()

	// Publish the outcome exactly once, and drop a failed attempt so the next
	// caller retries rather than inheriting the error forever.
	defer func() {
		close(conn.ready)
		if conn.err != nil {
			m.mu.Lock()
			if m.sessions[id] == conn {
				delete(m.sessions, id)
			}
			m.mu.Unlock()
		}
	}()

	a, err := m.Account(id)
	if err != nil {
		conn.err = err
		return nil, err
	}

	transport, err := m.transportFor(ctx, a)
	if err != nil {
		conn.err = err
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "janusmcp", Version: "0.1.0"}, &mcp.ClientOptions{
		// The broker is a proxy, not the end user. Left enabled, the SDK's
		// multi round-trip middleware would try to answer an upstream's input
		// requests here — and with no elicitation handler behind it, every
		// elicitation-capable tool would simply fail. Disabling it surfaces
		// `input_required` to callUpstream, which relays it to the real client.
		MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true},
		// With nil capabilities the SDK advertises roots.listChanged, which the
		// broker does not implement and which SEP-2577 deprecates. An explicit
		// empty value drops the claim on the 2026-07-28 discovery path; on the
		// legacy initialize path `"roots":{}` still ships, because
		// ClientCapabilities.Roots is a non-pointer struct and encoding/json
		// ignores omitempty for structs.
		Capabilities: &mcp.ClientCapabilities{},
	})
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		conn.err = fmt.Errorf("connect upstream %s: %w", id, err)
		return nil, conn.err
	}
	conn.cs = cs
	return cs, nil
}

// Tools returns (and caches) the upstream tool list for an account.
func (m *UpstreamManager) Tools(ctx context.Context, id string) ([]*mcp.Tool, error) {
	m.mu.Lock()
	if t, ok := m.toolCache[id]; ok {
		m.mu.Unlock()
		return t, nil
	}
	m.mu.Unlock()

	cs, err := m.Session(ctx, id)
	if err != nil {
		return nil, err
	}
	// Follow the cursor: a single ListTools call returns one page, so an upstream
	// with more tools than the server's page size would be silently truncated.
	var tools []*mcp.Tool
	params := &mcp.ListToolsParams{}
	for {
		res, err := cs.ListTools(ctx, params)
		if err != nil {
			return nil, err
		}
		tools = append(tools, res.Tools...)
		if res.NextCursor == "" {
			break
		}
		params = &mcp.ListToolsParams{Cursor: res.NextCursor}
	}

	m.mu.Lock()
	m.toolCache[id] = tools
	m.mu.Unlock()
	return tools, nil
}

func (m *UpstreamManager) CloseAll() {
	// Snapshot under the lock, then wait outside it: an in-flight connect can be
	// slow (a process spawn, or an interactive OAuth login), and blocking on it
	// while holding the mutex would stall every other caller.
	m.mu.Lock()
	conns := make([]*upstreamConn, 0, len(m.sessions))
	for _, conn := range m.sessions {
		conns = append(conns, conn)
	}
	m.sessions = map[string]*upstreamConn{}
	m.toolCache = map[string][]*mcp.Tool{}
	m.mu.Unlock()

	for _, conn := range conns {
		<-conn.ready
		if conn.cs != nil {
			_ = conn.cs.Close()
		}
	}
}

// transportFor builds the right MCP transport for an account: a local process
// (stdio) or a remote endpoint over Streamable HTTP or SSE, with MCP OAuth when
// requested.
func (m *UpstreamManager) transportFor(ctx context.Context, a *config.Account) (mcp.Transport, error) {
	if a.IsRemote() {
		url, err := m.resolve(a.URL)
		if err != nil {
			return nil, fmt.Errorf("resolve url for %s: %w", a.ID, err)
		}

		var handler *remoteOAuthHandler
		if a.Auth == "oauth" {
			clientName := a.ClientName
			if clientName == "" {
				clientName = "JanusMCP"
			}
			clientID, err := m.resolve(a.OAuthClientID)
			if err != nil {
				return nil, fmt.Errorf("resolve oauth client id for %s: %w", a.ID, err)
			}
			clientSecret, err := m.resolve(a.OAuthClientSecret)
			if err != nil {
				return nil, fmt.Errorf("resolve oauth client secret for %s: %w", a.ID, err)
			}
			if handler, err = newOAuthHandler(clientName, url, clientID, clientSecret, a.Scopes, m.secrets, "remote_oauth_"+a.ID); err != nil {
				return nil, err
			}
		}

		// SSE upstream: the SDK's SSE transport has no OAuthHandler, so OAuth is
		// injected via an authenticating HTTP client (interactive login on first use).
		if a.IsSSE() {
			t := &mcp.SSEClientTransport{Endpoint: url}
			if handler != nil {
				hc, err := handler.httpClient(ctx)
				if err != nil {
					return nil, fmt.Errorf("oauth for %s: %w", a.ID, err)
				}
				t.HTTPClient = hc
			}
			return t, nil
		}

		// Streamable HTTP upstream: use the SDK's native OAuth handler (auto-login on 401).
		t := &mcp.StreamableClientTransport{Endpoint: url}
		if handler != nil {
			t.OAuthHandler = handler
		}
		return t, nil
	}

	args, env, err := m.resolveAccount(a)
	if err != nil {
		return nil, fmt.Errorf("resolve secrets for %s: %w", a.ID, err)
	}
	cmd := exec.Command(a.Command, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Dir = m.configDir
	return &mcp.CommandTransport{Command: cmd}, nil
}

// resolveAccount resolves secret refs in an account's args and env at spawn time.
func (m *UpstreamManager) resolveAccount(a *config.Account) (args []string, env []string, err error) {
	args = make([]string, 0, len(a.Args))
	for _, v := range a.Args {
		rv, err := m.resolve(v)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, rv)
	}
	env = make([]string, 0, len(a.Env))
	for k, v := range a.Env {
		rv, err := m.resolve(v)
		if err != nil {
			return nil, nil, err
		}
		env = append(env, k+"="+rv)
	}
	return args, env, nil
}
