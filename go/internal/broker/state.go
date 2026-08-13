package broker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bayway/janusmcp/internal/atomicfile"
	"github.com/bayway/janusmcp/internal/config"
)

// BrokerState holds the persisted global active account, shared across sessions.
type BrokerState struct {
	mu           sync.Mutex
	globalActive string
	bindingMode  config.BindingMode
	statePath    string
}

type persisted struct {
	GlobalActive string `json:"globalActive"`
}

func NewBrokerState(statePath, defaultActive string, mode config.BindingMode) *BrokerState {
	active := defaultActive
	if b, err := os.ReadFile(statePath); err == nil {
		var p persisted
		if json.Unmarshal(b, &p) == nil && p.GlobalActive != "" {
			active = p.GlobalActive
		}
	}
	return &BrokerState{globalActive: active, bindingMode: mode, statePath: statePath}
}

func (s *BrokerState) GlobalActive() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.globalActive
}

func (s *BrokerState) BindingMode() config.BindingMode { return s.bindingMode }

func (s *BrokerState) SetGlobal(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.statePath
	b, err := json.MarshalIndent(persisted{GlobalActive: id}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode broker state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create broker state directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".janusmcp-state-*")
	if err != nil {
		return fmt.Errorf("create broker state file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure broker state file: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write broker state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close broker state: %w", err)
	}
	if err := atomicfile.Replace(tmpPath, path); err != nil {
		return fmt.Errorf("replace broker state: %w", err)
	}
	s.globalActive = id
	return nil
}

// unconnectedGrace is how long a registered session may go without ever having
// carried an MCP session before it is treated as abandoned. The HTTP handler
// registers a session before the transport connects, so a brand-new entry is
// legitimately empty for a moment.
const unconnectedGrace = 2 * time.Minute

// SessionRegistry tracks live sessions so a global switch can re-apply tools to each.
//
// Entries are pruned by liveness rather than by an explicit close hook, because
// the SDK exposes no session-close callback: neither ServerOptions nor
// StreamableHTTPOptions offers one. Each broker session owns exactly one
// mcp.Server, so Server.Sessions() answers "is anyone still connected to this?".
type SessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]*regEntry
}

// regEntry remembers enough to tell "closed" apart from "not connected yet".
type regEntry struct {
	session *Session
	// sawConnection latches once the entry has been observed with a live MCP
	// session; only then does emptiness mean the peer went away.
	sawConnection bool
	registered    time.Time
}

func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{sessions: map[string]*regEntry{}}
}

func (r *SessionRegistry) Add(s *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.ID] = &regEntry{session: s, registered: time.Now()}
	r.prune()
}

func (r *SessionRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
}

// Len reports how many sessions are currently tracked, after pruning.
func (r *SessionRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	return len(r.sessions)
}

func (r *SessionRegistry) Each(fn func(*Session)) {
	r.mu.Lock()
	r.prune()
	list := make([]*Session, 0, len(r.sessions))
	for _, e := range r.sessions {
		list = append(list, e.session)
	}
	r.mu.Unlock()
	for _, s := range list {
		fn(s)
	}
}

// prune drops sessions whose peer has gone. Callers must hold r.mu.
func (r *SessionRegistry) prune() {
	for id, e := range r.sessions {
		if e.session.persistent {
			continue // stdio, and the shared stateless server: alive for the process
		}
		if hasLiveSession(e.session) {
			e.sawConnection = true
			continue
		}
		if e.sawConnection || time.Since(e.registered) > unconnectedGrace {
			delete(r.sessions, id)
		}
	}
}

func hasLiveSession(s *Session) bool {
	for range s.server.Sessions() {
		return true
	}
	return false
}
