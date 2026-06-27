package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Secrets is the minimal vault surface the token store needs (satisfied by internal/vault).
type Secrets interface {
	Get(name string) (string, error)
	Set(name, value string) error
}

// stored is the persisted wrapper: a token plus the provider that issued it (for refresh).
type stored struct {
	Provider string `json:"provider"`
	Token    Token  `json:"token"`
}

// Store persists OAuth tokens in the vault and serves fresh access tokens.
type Store struct {
	secrets   Secrets
	providers map[string]Provider
}

func NewStore(secrets Secrets, providers map[string]Provider) *Store {
	if providers == nil {
		providers = map[string]Provider{}
	}
	return &Store{secrets: secrets, providers: providers}
}

func vaultKey(name string) string { return "oauth_" + name }

// Login runs the interactive loopback flow for providerName and stores the token under name.
func (s *Store) Login(ctx context.Context, providerName, name string) error {
	p, ok := s.providers[providerName]
	if !ok {
		return fmt.Errorf("unknown oauth provider %q", providerName)
	}
	p.Name = providerName
	tok, err := Login(ctx, p)
	if err != nil {
		return err
	}
	return s.persist(name, providerName, tok)
}

// Put stores a token under name for providerName (seeding / imported tokens / tests).
func (s *Store) Put(name, providerName string, tok Token) error {
	return s.persist(name, providerName, &tok)
}

func (s *Store) persist(name, providerName string, tok *Token) error {
	b, err := json.Marshal(stored{Provider: providerName, Token: *tok})
	if err != nil {
		return err
	}
	return s.secrets.Set(vaultKey(name), string(b))
}

// AccessToken returns a valid access token for name, refreshing and re-persisting if expired.
func (s *Store) AccessToken(ctx context.Context, name string) (string, error) {
	raw, err := s.secrets.Get(vaultKey(name))
	if err != nil {
		return "", fmt.Errorf("no oauth token %q: %w", name, err)
	}
	var st stored
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return "", fmt.Errorf("decode stored token %q: %w", name, err)
	}
	if tokenValid(st.Token) {
		return st.Token.AccessToken, nil
	}
	if st.Token.RefreshToken == "" {
		return "", fmt.Errorf("token %q expired and has no refresh_token", name)
	}
	p, ok := s.providers[st.Provider]
	if !ok {
		return "", fmt.Errorf("unknown provider %q for token %q", st.Provider, name)
	}
	p.Name = st.Provider
	nt, err := Refresh(ctx, p, st.Token.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh token %q: %w", name, err)
	}
	if nt.RefreshToken == "" {
		nt.RefreshToken = st.Token.RefreshToken // providers often omit it on refresh
	}
	if err := s.persist(name, st.Provider, nt); err != nil {
		return "", err
	}
	return nt.AccessToken, nil
}

// tokenValid reports whether a token is still usable (60s safety margin).
func tokenValid(t Token) bool {
	exp := t.Expiry()
	if exp.IsZero() {
		return t.AccessToken != "" // no expiry info → assume valid if present
	}
	return time.Now().Before(exp.Add(-60 * time.Second))
}
