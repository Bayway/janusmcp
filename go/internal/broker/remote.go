package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/bayway/janusmcp/internal/oauth"
)

// remoteAuthState is the per-account OAuth state persisted in the vault. Unlike the
// SDK's handler (whose registered client is not exported), we own the dynamic client
// registration so we can REUSE the same client across process restarts and refresh
// the access token with its refresh token — no re-login until the refresh token dies.
type remoteAuthState struct {
	ClientID     string        `json:"client_id,omitempty"`
	ClientSecret string        `json:"client_secret,omitempty"`
	AuthURL      string        `json:"auth_url,omitempty"`
	TokenURL     string        `json:"token_url,omitempty"`
	Scopes       []string      `json:"scopes,omitempty"`
	Token        *oauth2.Token `json:"token,omitempty"`
}

// remoteOAuthHandler implements the SDK's auth.OAuthHandler interface
// (TokenSource + Authorize) with persistent, refreshable credentials.
type remoteOAuthHandler struct {
	clientName string // client_name for DCR (some servers allowlist it, e.g. Figma)
	resource   string // the remote MCP endpoint (the OAuth "resource")
	secrets    oauth.Secrets
	key        string // vault key, e.g. remote_oauth_<account-id>
}

// newOAuthHandler builds the OAuth handler for a remote upstream. The loopback
// callback port is fixed (JANUS_OAUTH_PORT, default 7334) so the registered
// redirect URI is stable across restarts and the DCR client can be reused.
func newOAuthHandler(clientName, resourceURL string, secrets oauth.Secrets, key string) (auth.OAuthHandler, error) {
	return &remoteOAuthHandler{clientName: clientName, resource: resourceURL, secrets: secrets, key: key}, nil
}

func callbackRedirect() (string, string) {
	port := os.Getenv("JANUS_OAUTH_PORT")
	if port == "" {
		port = "7334"
	}
	return port, fmt.Sprintf("http://127.0.0.1:%s/callback", port)
}

func (h *remoteOAuthHandler) load() *remoteAuthState {
	raw, err := h.secrets.Get(h.key)
	if err != nil || raw == "" {
		return nil
	}
	var s remoteAuthState
	if json.Unmarshal([]byte(raw), &s) != nil {
		return nil
	}
	return &s
}

func (h *remoteOAuthHandler) save(s *remoteAuthState) {
	if b, err := json.Marshal(s); err == nil {
		_ = h.secrets.Set(h.key, string(b))
	}
}

func (h *remoteOAuthHandler) oauthConfig(s *remoteAuthState, redirect string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.ClientID,
		ClientSecret: s.ClientSecret,
		RedirectURL:  redirect,
		Scopes:       s.Scopes,
		Endpoint:     oauth2.Endpoint{AuthURL: s.AuthURL, TokenURL: s.TokenURL},
	}
}

// TokenSource returns a refreshing token source if we have a persisted token and
// client; oauth2 transparently refreshes the access token using the refresh token.
// Returns (nil, nil) when there's nothing yet, so the transport triggers Authorize.
func (h *remoteOAuthHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	s := h.load()
	if s == nil || s.Token == nil || s.TokenURL == "" || s.ClientID == "" {
		return nil, nil
	}
	base := h.oauthConfig(s, "").TokenSource(ctx, s.Token)
	return &persistingTokenSource{src: base, h: h, st: s}, nil
}

// Authorize runs the full interactive flow on the first login (or after the refresh
// token is rejected): discover endpoints, dynamically register a client (reused
// afterwards), then authorization-code + PKCE in the browser, and persist everything.
func (h *remoteOAuthHandler) Authorize(ctx context.Context, _ *http.Request, _ *http.Response) error {
	s := h.load()
	if s == nil {
		s = &remoteAuthState{}
	}

	port, redirect := callbackRedirect()

	if s.AuthURL == "" || s.TokenURL == "" || s.ClientID == "" {
		meta, err := h.discover(ctx)
		if err != nil {
			return err
		}
		s.AuthURL, s.TokenURL = meta.AuthorizationEndpoint, meta.TokenEndpoint
		if len(s.Scopes) == 0 {
			s.Scopes = meta.ScopesSupported
		}
		if s.ClientID == "" {
			if meta.RegistrationEndpoint == "" {
				return fmt.Errorf("%s: no registration endpoint and no preregistered client", h.resource)
			}
			reg, err := oauthex.RegisterClient(ctx, meta.RegistrationEndpoint, &oauthex.ClientRegistrationMetadata{
				RedirectURIs:    []string{redirect},
				ClientName:      h.clientName,
				ApplicationType: "native",
				GrantTypes:      []string{"authorization_code", "refresh_token"},
			}, nil)
			if err != nil {
				return fmt.Errorf("dynamic client registration (%s): %w", h.clientName, err)
			}
			s.ClientID, s.ClientSecret = reg.ClientID, reg.ClientSecret
		}
		h.save(s)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return fmt.Errorf("oauth callback listen on :%s (set JANUS_OAUTH_PORT if busy): %w", port, err)
	}

	cfg := h.oauthConfig(s, redirect)
	verifier := oauth2.GenerateVerifier()
	state := randomState()
	authURL := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier))

	res, err := captureAuthCode(ctx, ln, authURL)
	if err != nil {
		return err
	}
	if res.State != state {
		return fmt.Errorf("oauth state mismatch (possible CSRF)")
	}
	tok, err := cfg.Exchange(ctx, res.Code, oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("token exchange: %w", err)
	}
	s.Token = tok
	h.save(s)
	return nil
}

// discover resolves the authorization server metadata for the remote resource via
// the RFC 9728 well-known locations derived from the resource origin.
func (h *remoteOAuthHandler) discover(ctx context.Context) (*oauthex.AuthServerMeta, error) {
	u, err := url.Parse(h.resource)
	if err != nil {
		return nil, fmt.Errorf("parse resource url: %w", err)
	}
	origin := u.Scheme + "://" + u.Host

	// Candidate protected-resource-metadata URLs (path-based first, per RFC 9728).
	prmCandidates := []string{
		origin + "/.well-known/oauth-protected-resource" + u.Path,
		origin + "/.well-known/oauth-protected-resource",
	}

	var prm *oauthex.ProtectedResourceMetadata
	for _, cand := range prmCandidates {
		if m, e := oauthex.GetProtectedResourceMetadata(ctx, cand, h.resource, nil); e == nil {
			prm = m
			break
		}
	}
	if prm == nil || len(prm.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("could not discover authorization server for %s", h.resource)
	}

	issuer := strings.TrimRight(prm.AuthorizationServers[0], "/")
	for _, asURL := range []string{
		issuer + "/.well-known/oauth-authorization-server",
		issuer + "/.well-known/openid-configuration",
	} {
		if meta, e := oauthex.GetAuthServerMeta(ctx, asURL, issuer, nil); e == nil && meta != nil {
			return meta, nil
		}
	}
	return nil, fmt.Errorf("could not fetch authorization server metadata for %s", issuer)
}

func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// persistingTokenSource saves refreshed tokens back to the vault so a refreshed
// access token (and any rotated refresh token) survives the next restart.
type persistingTokenSource struct {
	src oauth2.TokenSource
	h   *remoteOAuthHandler
	st  *remoteAuthState
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := p.src.Token()
	if err != nil {
		return nil, err
	}
	if tok != nil && (p.st.Token == nil || tok.AccessToken != p.st.Token.AccessToken || tok.RefreshToken != p.st.Token.RefreshToken) {
		p.st.Token = tok
		p.h.save(p.st)
	}
	return tok, nil
}

// captureAuthCode opens authURL in the browser and waits for the authorization
// server to redirect back to our loopback listener, returning code + state.
func captureAuthCode(ctx context.Context, ln net.Listener, authURL string) (*auth.AuthorizationResult, error) {
	resCh := make(chan *auth.AuthorizationResult, 1)
	errCh := make(chan error, 1)

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			errCh <- fmt.Errorf("authorization error: %s", e)
			http.Error(w, "authorization failed", http.StatusBadRequest)
			return
		}
		resCh <- &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state")}
		w.Header().Set("content-type", "text/html")
		_, _ = w.Write([]byte("<h3>Account collegato. Puoi chiudere questa finestra.</h3>"))
	})}
	go srv.Serve(ln)
	defer srv.Close()

	_ = oauth.OpenBrowser(authURL)
	fmt.Fprintf(os.Stderr, "[janusmcp] authorize in your browser:\n%s\n", authURL)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case res := <-resCh:
		return res, nil
	}
}
