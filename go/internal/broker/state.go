package broker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
