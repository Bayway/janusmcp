package broker

import (
	"encoding/json"
	"os"
	"sync"

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

func (s *BrokerState) SetGlobal(id string) {
	s.mu.Lock()
	s.globalActive = id
	path := s.statePath
	s.mu.Unlock()
	if b, err := json.MarshalIndent(persisted{GlobalActive: id}, "", "  "); err == nil {
		_ = os.WriteFile(path, b, 0o600) // best-effort persistence
	}
}

// SessionRegistry tracks live sessions so a global switch can re-apply tools to each.
type SessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{sessions: map[string]*Session{}}
}

func (r *SessionRegistry) Add(s *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.ID] = s
}

func (r *SessionRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
}

func (r *SessionRegistry) Each(fn func(*Session)) {
	r.mu.Lock()
	list := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		list = append(list, s)
	}
	r.mu.Unlock()
	for _, s := range list {
		fn(s)
	}
}
