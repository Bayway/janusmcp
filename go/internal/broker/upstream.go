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
	sessions  map[string]*mcp.ClientSession
	toolCache map[string][]*mcp.Tool
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
		sessions:  map[string]*mcp.ClientSession{},
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
func (m *UpstreamManager) Session(ctx context.Context, id string) (*mcp.ClientSession, error) {
	m.mu.Lock()
	if cs, ok := m.sessions[id]; ok {
		m.mu.Unlock()
		return cs, nil
	}
	m.mu.Unlock()

	a, err := m.Account(id)
	if err != nil {
		return nil, err
	}

	transport, err := m.transportFor(a)
	if err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "janusmcp", Version: "0.1.0"}, nil)
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect upstream %s: %w", id, err)
	}

	m.mu.Lock()
	m.sessions[id] = cs
	m.mu.Unlock()
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
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.toolCache[id] = res.Tools
	m.mu.Unlock()
	return res.Tools, nil
}

func (m *UpstreamManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cs := range m.sessions {
		_ = cs.Close()
	}
}

// transportFor builds the right MCP transport for an account: a local process
// (stdio) or a remote Streamable HTTP endpoint, with MCP-native OAuth when requested.
func (m *UpstreamManager) transportFor(a *config.Account) (mcp.Transport, error) {
	if a.IsHTTP() {
		url, err := m.resolve(a.URL)
		if err != nil {
			return nil, fmt.Errorf("resolve url for %s: %w", a.ID, err)
		}
		t := &mcp.StreamableClientTransport{Endpoint: url}
		if a.Auth == "oauth" {
			h, err := newOAuthHandler("JanusMCP", m.secrets, "remote_oauth_"+a.ID)
			if err != nil {
				return nil, err
			}
			t.OAuthHandler = h
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
