package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

	// Issuer records which authorization server minted ClientID, so credentials
	// are never replayed against a different one. Empty means the entry predates
	// issuer tracking, which disables validation rather than breaking a login
	// that used to work.
	Issuer string `json:"issuer,omitempty"`
	// IssParamSupported mirrors the authorization server's
	// authorization_response_iss_parameter_supported metadata (RFC 9207).
	IssParamSupported bool `json:"iss_param_supported,omitempty"`
}

// remoteOAuthHandler implements the SDK's auth.OAuthHandler interface
// (TokenSource + Authorize) with persistent, refreshable credentials.
type remoteOAuthHandler struct {
	clientName string // client_name for DCR (some servers allowlist it, e.g. Figma)
	resource   string // the remote MCP endpoint (the OAuth "resource")
	// Pre-registered client for servers WITHOUT dynamic client registration
	// (e.g. Google Workspace). When clientID is set, the handler skips DCR.
	clientID     string
	clientSecret string
	scopes       []string
	secrets      oauth.Secrets
	key          string // vault key, e.g. remote_oauth_<account-id>
}

// newOAuthHandler builds the OAuth handler for a remote upstream. The loopback
// callback port is fixed (JANUS_OAUTH_PORT, default 7334) so the registered
// redirect URI is stable across restarts and the DCR client can be reused.
//
// clientID/clientSecret/scopes are optional: set them for servers that do NOT
// support dynamic client registration and require a pre-registered OAuth client.
func newOAuthHandler(clientName, resourceURL, clientID, clientSecret string, scopes []string, secrets oauth.Secrets, key string) (*remoteOAuthHandler, error) {
	return &remoteOAuthHandler{
		clientName:   clientName,
		resource:     resourceURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		scopes:       scopes,
		secrets:      secrets,
		key:          key,
	}, nil
}

// seed applies the handler's pre-registered client (if any) to the persisted
// state: it fills a missing client_id/secret and default scopes without
// clobbering values already obtained from a previous login. It is a pure helper
// so the DCR-skipping logic can be unit-tested without touching the network.
func (h *remoteOAuthHandler) seed(s *remoteAuthState) *remoteAuthState {
	if s == nil {
		s = &remoteAuthState{}
	}
	if s.ClientID == "" && h.clientID != "" {
		s.ClientID = h.clientID
		s.ClientSecret = h.clientSecret
	}
	if len(s.Scopes) == 0 && len(h.scopes) > 0 {
		s.Scopes = append([]string(nil), h.scopes...)
	}
	return s
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

// httpClient returns an *http.Client that injects a valid OAuth bearer token on
// every request. The SDK's SSE transport (unlike the Streamable HTTP one) has no
// OAuthHandler hook, so we authenticate SSE upstreams through the client instead:
// if no token is persisted yet, run the interactive browser login first, then
// wrap a refreshing token source in an oauth2.Transport.
func (h *remoteOAuthHandler) httpClient(ctx context.Context) (*http.Client, error) {
	ts, err := h.TokenSource(ctx)
	if err != nil {
		return nil, err
	}
	if ts == nil {
		if err := h.Authorize(ctx, nil, nil); err != nil {
			return nil, err
		}
		if ts, err = h.TokenSource(ctx); err != nil {
			return nil, err
		}
		if ts == nil {
			return nil, fmt.Errorf("oauth: no token after authorize for %s", h.resource)
		}
	}
	return &http.Client{Transport: &oauth2.Transport{Source: ts, Base: http.DefaultTransport}}, nil
}

// Authorize runs the full interactive flow on the first login (or after the refresh
// token is rejected): discover endpoints, dynamically register a client (reused
// afterwards), then authorization-code + PKCE in the browser, and persist everything.
func (h *remoteOAuthHandler) Authorize(ctx context.Context, _ *http.Request, _ *http.Response) error {
	s := h.seed(h.load())

	port, redirect := callbackRedirect()

	if s.AuthURL == "" || s.TokenURL == "" || s.ClientID == "" {
		meta, err := h.discover(ctx)
		if err != nil {
			return err
		}
		s.AuthURL, s.TokenURL = meta.AuthorizationEndpoint, meta.TokenEndpoint
		// A client registered with one authorization server must never be reused
		// against another: 2026-07-28 binds credentials to their issuer.
		if s.Issuer != "" && !sameIssuer(s.Issuer, meta.Issuer) {
			return fmt.Errorf(
				"%s: authorization server changed from %q to %q; the stored client credentials are not valid there",
				h.resource, s.Issuer, meta.Issuer)
		}
		s.Issuer = meta.Issuer
		s.IssParamSupported = meta.AuthorizationResponseIssParameterSupported
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
	if err := validateIssuerResponse(res.Iss, s.Issuer, s.IssParamSupported); err != nil {
		return err
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

	// Build the list of authorization-server issuers to try. First any advertised
	// by protected-resource metadata (RFC 9728), then the resource origin itself as
	// a fallback: some servers (e.g. Intercom) don't publish PRM but expose
	// authorization-server metadata directly at their origin.
	var issuers []string
	addIssuer := func(s string) {
		s = strings.TrimRight(s, "/")
		for _, e := range issuers {
			if e == s {
				return
			}
		}
		issuers = append(issuers, s)
	}
	for _, cand := range prmCandidates {
		if m, e := oauthex.GetProtectedResourceMetadata(ctx, cand, h.resource, nil); e == nil && m != nil && len(m.AuthorizationServers) > 0 {
			addIssuer(m.AuthorizationServers[0])
			break
		}
	}
	addIssuer(origin)

	for _, issuer := range issuers {
		for _, asURL := range wellKnownURLs(issuer) {
			if meta, e := oauthex.GetAuthServerMeta(ctx, asURL, issuer, nil); e == nil && meta != nil {
				return meta, nil
			}
		}
	}

	// Second pass: some servers advertise an authorization server whose metadata
	// declares a different, canonical issuer than the URL it was served from
	// (e.g. Vercel: mcp.vercel.com advertises metadata whose issuer is vercel.com).
	// Follow that declared issuer and validate strictly there.
	for _, issuer := range issuers {
		for _, asURL := range wellKnownURLs(issuer) {
			declared := fetchDeclaredIssuer(ctx, asURL)
			if declared == "" || declared == issuer {
				continue
			}
			for _, u2 := range wellKnownURLs(declared) {
				if meta, e := oauthex.GetAuthServerMeta(ctx, u2, declared, nil); e == nil && meta != nil {
					return meta, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("could not discover authorization server for %s", h.resource)
}

// fetchDeclaredIssuer reads just the `issuer` field of an authorization-server
// metadata document, without the strict same-URL validation, so the caller can
// follow it to the canonical issuer and validate there.
func fetchDeclaredIssuer(ctx context.Context, metadataURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var m struct {
		Issuer string `json:"issuer"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m) != nil {
		return ""
	}
	return m.Issuer
}

// wellKnownURLs returns the metadata URLs to try for an issuer. The OAuth flavor
// (RFC 8414) inserts the well-known segment between the host and the issuer path
// (so a tenant path is preserved), while the OpenID Connect flavor appends it. The
// issuer string is otherwise left intact (including any trailing slash) so it can
// be validated verbatim against the metadata's `issuer` field.
func wellKnownURLs(issuer string) []string {
	iu, err := url.Parse(issuer)
	if err != nil {
		return []string{
			issuer + "/.well-known/oauth-authorization-server",
			issuer + "/.well-known/openid-configuration",
		}
	}
	origin := iu.Scheme + "://" + iu.Host
	path := strings.TrimRight(iu.Path, "/") // "" for a bare or trailing-slash issuer
	return []string{
		origin + "/.well-known/oauth-authorization-server" + path,
		strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration",
	}
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

// validateIssuerResponse checks the "iss" authorization-response parameter
// (RFC 9207), which closes the authorization-server mix-up attack: without it a
// malicious server can hand back a code minted elsewhere.
//
// The SDK implements this but does not export it, so it is reimplemented here.
//
// Validation is skipped when the stored issuer is unknown, which means the vault
// entry predates issuer tracking. Enforcing the "must be absent" rule on those
// would break logins that worked before the upgrade; they re-enter validation
// once the metadata has been discovered again.
func validateIssuerResponse(got, want string, supported bool) error {
	if want == "" {
		return nil
	}
	if !supported {
		if got != "" {
			return fmt.Errorf("authorization server returned an iss parameter (%q) but does not declare support for it", got)
		}
		return nil
	}
	if got == "" {
		return fmt.Errorf("authorization server declares iss support (RFC 9207) but omitted it from the response")
	}
	if !sameIssuer(got, want) {
		return fmt.Errorf("issuer mismatch: response came from %q, expected %q", got, want)
	}
	return nil
}

// sameIssuer compares issuer identifiers, ignoring a trailing slash — issuer
// URLs are commonly written both ways.
func sameIssuer(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
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
		// iss (RFC 9207) lets the client detect an authorization-server mix-up
		// before redeeming the code; 2026-07-28 requires validating it.
		resCh <- &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}
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
